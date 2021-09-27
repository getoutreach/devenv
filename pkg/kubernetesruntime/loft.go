package kubernetesruntime

import (
	"context"
	"encoding/json"
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
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	managementv1 "github.com/loft-sh/api/pkg/apis/management/v1"
	loftapi "github.com/loft-sh/api/pkg/client/clientset_generated/clientset"
	loftconfig "github.com/loft-sh/loftctl/pkg/client"
	clientauthv1alpha1 "k8s.io/client-go/pkg/apis/clientauthentication/v1alpha1"
)

const (
	loftVersion     = "v1.15.0"
	loftDownloadURL = "https://github.com/loft-sh/loft/releases/download/" + loftVersion + "/loft-" + runtime.GOOS + "-" + runtime.GOARCH
)

type LoftRuntime struct {
	// kubeConfig stores the kubeconfig of the last created
	// cluster by Create()
	kubeConfig []byte

	box      *box.Config
	log      logrus.FieldLogger
	loft     loftapi.Interface
	loftUser *managementv1.Self

	clusterName   string
	clusterNameMu sync.Mutex
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
	lcli, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	loftConf, err := lr.getLoftConfigPath()
	if err != nil {
		return errors.Wrap(err, "failed to determine loft config path")
	}

	f, err := os.Open(loftConf)
	if err != nil {
		lr.log.WithError(err).Info("Authenticating with loft")
		err = cmdutil.RunKubernetesCommand(ctx, "", false, lcli, "login", lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft.URL)
		if err != nil {
			return errors.Wrap(err, "failed to authenticate with loft")
		}

		f, err = os.Open(loftConf)
		if err != nil {
			return errors.Wrap(err, "failed to read loft config after authenticating")
		}
	}
	defer f.Close()

	var conf loftconfig.Config
	if err := json.NewDecoder(f).Decode(&conf); err != nil { //nolint:govet // Why: We're OK shadowing error.
		return errors.Wrap(err, "failed to read loft config")
	}

	restConf := &rest.Config{
		Host:        "https://" + path.Join(strings.TrimPrefix(conf.Host, "https://"), "kubernetes", "management"),
		BearerToken: conf.AccessKey,
	}

	loftClient, err := loftapi.NewForConfig(restConf)
	if err != nil {
		return errors.Wrap(err, "failed to create client to talk to loft apiserver")
	}

	self, err := loftClient.ManagementV1().Selves().Create(ctx, &managementv1.Self{}, metav1.CreateOptions{})
	if err != nil || self.Status.User == "" { // auth token likely expired, so just refresh it
		lr.log.WithError(err).Info("Authenticating with loft")
		err = cmdutil.RunKubernetesCommand(ctx, "", false, lcli, "login", conf.Host)
		if err != nil {
			return errors.Wrap(err, "failed to authenticate with loft")
		}

		// ensure that the new credentials are valid
		return lr.PreCreate(ctx)
	}

	// we have valid credentials, so set the client.
	lr.loft = loftClient
	lr.loftUser = self

	return nil
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

		lr.clusterName = u.Username + "-devenv"
	}
	lr.clusterNameMu.Unlock()

	return RuntimeConfig{
		Name:        "loft",
		Type:        RuntimeTypeRemote,
		ClusterName: lr.clusterName,
	}
}

func (lr *LoftRuntime) Status(ctx context.Context) RuntimeStatus {
	resp := RuntimeStatus{status.Status{
		Status: status.Unprovisioned,
	}}

	lcli, err := lr.ensureLoft(lr.log)
	if err != nil {
		resp.Status.Status = status.Unknown
		resp.Status.Reason = errors.Wrap(err, "failed to get loft CLI").Error()
		return resp
	}

	out, err := exec.CommandContext(ctx, lcli, "list", "vclusters").CombinedOutput()
	if err != nil {
		resp.Status.Status = status.Unknown
		resp.Status.Reason = errors.Wrap(err, "failed to list clusters").Error()
		return resp
	}

	// TODO(jaredallard): See if we can hit loft's API instead of this
	// hacky not totally valid contains check.
	if strings.Contains(string(out), lr.clusterName) {
		resp.Status.Status = status.Running
	}

	return resp
}

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

	cmd := exec.CommandContext(ctx, loft, "create", "vcluster",
		"--sleep-after", "3600", // sleeps after 1 hour
		"--template", "devenv", lr.clusterName)
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

	out, err := exec.CommandContext(ctx, loft, "delete", "vcluster", lr.clusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to delete loft vcluster: %s", out)
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

func (lr *LoftRuntime) getKubeConfigForVCluster(_ context.Context, vc *managementv1.ClusterVirtualCluster) *api.Config {
	loftCLIPath, _ := lr.ensureLoft(lr.log)   //nolint:errcheck
	loftConfPath, _ := lr.getLoftConfigPath() //nolint:errcheck

	authInfo := api.NewAuthInfo()
	authInfo.Exec = &api.ExecConfig{
		APIVersion: clientauthv1alpha1.SchemeGroupVersion.String(),
		Command:    loftCLIPath,
		Args:       []string{"token", "--silent", "--config", loftConfPath},
	}

	contextName := "dev-environment"
	return &api.Config{
		Clusters: map[string]*api.Cluster{
			contextName: {
				Server: lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft.URL + "/kubernetes/virtualcluster/" +
					vc.Cluster + "/" + vc.VirtualCluster.Namespace + "/" + vc.VirtualCluster.Name,
			},
		},
		CurrentContext: KindClusterName,
		Contexts: map[string]*api.Context{
			contextName: {
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
	clusters, err := lr.loft.ManagementV1().Users().ListVirtualClusters(ctx, lr.loftUser.Status.User, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list available clusters")
	}

	rclusters := make([]*RuntimeCluster, len(clusters.VirtualClusters))
	for i := range clusters.VirtualClusters {
		c := &clusters.VirtualClusters[i]

		rclusters[i] = &RuntimeCluster{
			RuntimeName: lr.GetConfig().Name,
			Name:        c.VirtualCluster.Name,
			KubeConfig:  lr.getKubeConfigForVCluster(ctx, c),
		}
	}

	return rclusters, nil
}
