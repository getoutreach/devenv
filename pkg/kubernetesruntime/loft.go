package kubernetesruntime

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/region"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	v1 "github.com/loft-sh/agentapi/v2/pkg/apis/loft/storage/v1"
	loftctlclient "github.com/loft-sh/loftctl/v2/pkg/client"
	loftctlhelper "github.com/loft-sh/loftctl/v2/pkg/client/helper"
	loftctllog "github.com/loft-sh/loftctl/v2/pkg/log"
	clientauthv1alpha1 "k8s.io/client-go/pkg/apis/clientauthentication/v1alpha1"
)

const (
	loftVersion     = "v2.0.2"
	loftDownloadURL = "https://github.com/loft-sh/loft/releases/download/" + loftVersion + "/loft-" + runtime.GOOS + "-" + runtime.GOARCH
)

type LoftRuntime struct {
	// kubeConfig stores the kubeconfig of the last created
	// cluster by Create()
	kubeConfig []byte

	box *box.Config
	log logrus.FieldLogger

	// loftctl is a client provided by loftctl, this is much easier
	// than writing the logic ourselves but may potentially make future
	// upgrades harder.
	loftctl loftctlclient.Client

	clusterName   string
	clusterNameMu sync.Mutex
}

func newLoftLogger() loftctllog.Logger {
	return loftctllog.NewStreamLogger(os.Stderr, logrus.InfoLevel)
}

func NewLoftRuntime() *LoftRuntime {
	return &LoftRuntime{}
}

// ensureLoft ensures that loft exists and returns
// the location of kind. Note: this outputs text
// if loft is being downloaded
func (*LoftRuntime) ensureLoft(log logrus.FieldLogger) (string, error) {
	return cmdutil.EnsureBinary(log, "loft-"+loftVersion, "Kubernetes Runtime", loftDownloadURL, "")
}

func (lr *LoftRuntime) Configure(log logrus.FieldLogger, conf *box.Config) {
	lr.box = conf
	lr.log = log
}

func (lr *LoftRuntime) getLoftConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get user's home dir")
	}

	return filepath.Join(homeDir, ".loft", "config.json"), nil
}

func (lr *LoftRuntime) PreCreate(ctx context.Context) error { //nolint:funlen // Why: will address later
	loftConf, err := lr.getLoftConfigPath()
	if err != nil {
		return errors.Wrap(err, "failed to determine loft config path")
	}

	lr.loftctl, err = loftctlclient.NewClientFromPath(loftConf)
	if err != nil {
		return err
	}

	// Check if we're authenticated already
	if lr.loftctl != nil {
		managementClient, err := lr.loftctl.Management()
		if err == nil {
			if _, _, err := loftctlhelper.GetCurrentUser(ctx, managementClient); err == nil {
				return nil
			}
		}
	}

	// We're probably not, re-authenticate
	return lr.loftctl.Login(lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft.URL, false,
		loftctllog.NewStreamLogger(os.Stderr, logrus.InfoLevel))
}

func (lr *LoftRuntime) GetConfig() RuntimeConfig {
	// Generate the cluster name. Ensure that this is
	// thread safe.
	lr.clusterNameMu.Lock()
	if lr.clusterName == "" {
		u, err := user.Current()
		if err != nil {
			u = &user.User{
				Username: "unknown",
			}
		}

		lr.clusterName = strings.ReplaceAll(u.Username, ".", "-") + "-devenv"
	}
	lr.clusterNameMu.Unlock()

	return RuntimeConfig{
		Name:        "loft",
		Type:        RuntimeTypeRemote,
		ClusterName: lr.clusterName,
	}
}

// Status returns the status of our cluster
func (lr *LoftRuntime) Status(ctx context.Context) RuntimeStatus {
	resp := RuntimeStatus{status.Status{
		Status: status.Unprovisioned,
	}}

	clusters, err := loftctlhelper.GetVirtualClusters(lr.loftctl, newLoftLogger())
	if err != nil {
		resp.Status.Status = status.Unknown
		resp.Status.Reason = errors.Wrap(err, "failed to list vclusters").Error()
		return resp
	}

	for i := range clusters {
		c := &clusters[i]

		// Skip clusters that we don't have access to
		if c.Name != lr.clusterName {
			continue
		}

		if c.Status.Phase == v1.VirtualClusterDeployed {
			resp.Status.Status = status.Running
			return resp
		}

		resp.Status.Status = status.Unknown
		resp.Status.Reason = fmt.Errorf("unknown phase '%s'", c.Status.Phase).Error()
		return resp
	}

	// Assume we're unprovisioned at this point
	return resp
}

// getPreferredCluster returns the backing cluster that should be used for this devenv
func (lr *LoftRuntime) getPreferredCluster(ctx context.Context) string {
	loftConfig := &lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft
	clusters := loftConfig.Clusters
	regions := clusters.Regions()
	cloudName := loftConfig.DefaultCloud

	cloud := region.CloudFromCloudName(cloudName)
	if cloud == nil {
		lr.log.Warn("failed to get cloud from box config, this may result in issues")
		return ""
	}

	regionName, err := cloud.Regions(ctx).Filter(regions).Nearest(ctx, lr.log)
	if err != nil {
		regionName = loftConfig.DefaultRegion
		lr.log.WithError(err).Warnf("failed to determine nearest region, will fallback to '%s'", regionName)
	}

	for _, c := range clusters {
		if c.Region == regionName {
			return c.Name
		}
	}

	lr.log.WithField("region", regionName).Warn("failed to find backing cluster for region")
	return ""
}

// TODO(jaredallard): Move to use loftctlcmdcreate
func (lr *LoftRuntime) Create(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	kubeConfig, err := os.CreateTemp("", "loft-kubeconfig-*")
	if err != nil {
		return err
	}
	kubeConfig.Close() //nolint:errcheck
	defer os.Remove(kubeConfig.Name())

	args := []string{"create", "vcluster",
		"--sleep-after", "3600", // sleeps after 1 hour
		"--template", "devenv"}

	backingCluster := lr.getPreferredCluster(ctx)
	if backingCluster == "" {
		lr.log.Warn(
			//nolint:lll // Why: Not much we can do here.
			"failed to find a cluster in your region, will fallback to a random one, this may result in a degraded user experience. This should be reported as it's likely a box configuration issue",
		)
	}
	args = append(args, "--cluster", backingCluster, lr.clusterName)

	lr.log.WithField("args", args).Info("Creating vcluster")
	cmd := exec.CommandContext(ctx, loft, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeConfig.Name())
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	err = cmd.Run()
	if err != nil {
		return errors.Wrap(err, "failed to create loft vcluster")
	}

	lr.kubeConfig, err = ioutil.ReadFile(kubeConfig.Name())
	return errors.Wrap(err, "failed to read kubeconfig")
}

func (lr *LoftRuntime) Destroy(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, loft, "delete", "vcluster", "--delete-space", lr.clusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to delete loft vcluster: %s", out)
}

func (lr *LoftRuntime) Stop(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, loft, "sleep", "vcluster-"+lr.clusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to put loft vcluster to sleep: %s", out)
}

func (lr *LoftRuntime) Start(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, loft, "wakeup", "vcluster-"+lr.clusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to wakeup loft vcluster: %s", out)
}

func (lr *LoftRuntime) GetKubeConfig(ctx context.Context) (*api.Config, error) {
	if len(lr.kubeConfig) == 0 {
		return nil, fmt.Errorf("found no kubeconfig, was a cluster created?")
	}

	kubeconfig, err := clientcmd.Load(lr.kubeConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load kube config from loft")
	}

	// Assume the first context is the one we want
	for k := range kubeconfig.Contexts {
		kubeconfig.Contexts[KindClusterName] = kubeconfig.Contexts[k]
		delete(kubeconfig.Contexts, k)
		break
	}
	kubeconfig.CurrentContext = KindClusterName

	return kubeconfig, nil
}

// TODO: Move this to use the loftctl code.
//nolint:funlen // Why: It's OK.
func (lr *LoftRuntime) getKubeConfigForVCluster(ctx context.Context, vc *loftctlhelper.ClusterVirtualCluster) *api.Config {
	loftCLIPath, _ := lr.ensureLoft(lr.log)   //nolint:errcheck
	loftConfPath, _ := lr.getLoftConfigPath() //nolint:errcheck

	authInfo := api.NewAuthInfo()
	authInfo.Exec = &api.ExecConfig{
		APIVersion: clientauthv1alpha1.SchemeGroupVersion.String(),
		Command:    loftCLIPath,
		Args:       []string{"token", "--silent", "--config", loftConfPath},
	}

	isDirectEndpoint := false
	endpoint := lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft.URL
	paths := []string{vc.Namespace, vc.Name}

	// Check if this backend cluster has a direct endpoint configured
	managementClient, err := lr.loftctl.Management()
	if err != nil {
		return nil
	}

	if backingCluster, err := managementClient.Loft().ManagementV1().Clusters().Get(ctx,
		vc.ClusterName, metav1.GetOptions{}); err == nil {
		if directEndpoint, ok := backingCluster.Annotations["loft.sh/direct-cluster-endpoint"]; ok {
			endpoint = "https://" + directEndpoint
			isDirectEndpoint = true
		}
	}

	if isDirectEndpoint {
		authInfo.Exec.Args = append(authInfo.Exec.Args, "--direct-cluster-endpoint")
	} else {
		paths = append([]string{vc.ClusterName}, paths...)
	}

	contextName := vc.Name
	return &api.Config{
		Clusters: map[string]*api.Cluster{
			contextName: {
				Server: endpoint +
					path.Join(append([]string{"/kubernetes/virtualcluster"}, paths...)...),
			},
		},
		// IDEA: If we ever merge this into ~/.kube/config we could support
		// setting this to the virtual cluster name.
		CurrentContext: "dev-environment",
		Contexts: map[string]*api.Context{
			contextName: {
				Cluster:  contextName,
				AuthInfo: contextName,
			},

			// Compat with tools that want this context.
			"dev-environment": {
				Cluster:  contextName,
				AuthInfo: contextName,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			contextName: authInfo,
		},
	}
}

// GetClusters gets a list of current devenv clusters that are available
// to the current user.
func (lr *LoftRuntime) GetClusters(ctx context.Context) ([]*RuntimeCluster, error) {
	clusters, err := loftctlhelper.GetVirtualClusters(lr.loftctl, newLoftLogger())
	if err != nil {
		return nil, err
	}

	rclusters := make([]*RuntimeCluster, len(clusters))
	for i := range clusters {
		c := &clusters[i]

		rclusters[i] = &RuntimeCluster{
			RuntimeName: lr.GetConfig().Name,
			Name:        c.Name,
			KubeConfig:  lr.getKubeConfigForVCluster(ctx, c),
		}
	}

	return rclusters, nil
}
