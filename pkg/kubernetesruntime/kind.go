package kubernetesruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/embed"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
)

const (
	KindVersion     = "v0.12.0-outreach.1"
	KindDownloadURL = "https://github.com/getoutreach/kind/releases/download/" + KindVersion + "/kind-" + runtime.GOOS + "-" + runtime.GOARCH
	KindClusterName = "dev-environment"
)

var configTemplate = template.Must(template.New("kind.yaml").Parse(string(embed.MustRead(embed.Config.ReadFile("config/kind.yaml")))))

// Deprecated: This will be removed when there's a new way of doing this.
// EnsureKind downloads kind
var EnsureKind = (&KindRuntime{}).ensureKind

type KindRuntime struct {
	log logrus.FieldLogger
}

// NewKindRuntime creates a new kind runtime
func NewKindRuntime() *KindRuntime {
	return &KindRuntime{}
}

// ensureKind ensures that Kind exists and returns
// the location of kind. Note: this outputs text
// if kind is being downloaded
func (*KindRuntime) ensureKind(log logrus.FieldLogger) (string, error) { //nolint:funlen
	return cmdutil.EnsureBinary(log, "kind-"+KindVersion, "Kubernetes Runtime", KindDownloadURL, "")
}

func (*KindRuntime) PreCreate(ctx context.Context) error {
	return nil
}

func (kr *KindRuntime) Configure(log logrus.FieldLogger, _ *box.Config) {
	kr.log = log
}

func (*KindRuntime) GetConfig() RuntimeConfig {
	return RuntimeConfig{
		Name:        "kind",
		Type:        RuntimeTypeLocal,
		ClusterName: "dev-environment",
	}
}

// Status gets the status of a runtime
func (kr *KindRuntime) Status(ctx context.Context) RuntimeStatus {
	resp := RuntimeStatus{status.Status{
		Status: status.Unknown,
	}}

	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		resp.Reason = errors.Wrap(err, "failed to connect to docker").Error()
		return resp
	}

	// check the status of the k3s container to determine
	// if it's stopped
	cont, err := d.ContainerInspect(ctx, containerruntime.ContainerName)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			resp.Status.Status = status.Unprovisioned
			return resp
		}

		// we don't know of the error, so... cry
		resp.Reason = errors.Wrap(err, "failed to inspect container").Error()
		return resp
	}

	// read the version of the container
	if _, ok := cont.Config.Labels["io.outreach.devenv.version"]; ok {
		resp.Version = cont.Config.Labels["io.outreach.devenv.version"]
	}

	// parse the container state
	if cont.State.Status == "exited" {
		resp.Status.Status = status.Stopped
		return resp
	}

	if cont.State.Status == "running" {
		resp.Status.Status = status.Running
	}

	return resp
}

// Create creates a new Kind cluster
func (kr *KindRuntime) Create(ctx context.Context) error {
	kind, err := kr.ensureKind(kr.log)
	if err != nil {
		return err
	}

	renderedConfig, err := os.CreateTemp("", "kind-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(renderedConfig.Name())

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "failed to get user home dir")
	}

	tagSuffix := ""
	if runtime.GOARCH != "amd64" {
		tagSuffix = "-" + runtime.GOARCH
	}

	err = configTemplate.Execute(renderedConfig, map[string]string{
		"Home":          homeDir,
		"Name":          "",
		"DevenvVersion": app.Info().Version,
		"TagSuffix":     tagSuffix,
	})
	if err != nil {
		return errors.Wrap(err, "failed to generate kind configuration")
	}

	// we use a temp file for the kubeconfig because we don't actually use it
	cmd := exec.CommandContext(ctx, kind, "create", "cluster", "--name", KindClusterName, "--wait", "5m", "--config", renderedConfig.Name(),
		"--kubeconfig", filepath.Join(os.TempDir(), "devenv-kubeconfig-tmp.yaml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return errors.Wrap(cmd.Run(), "failed to run kind")
}

// Destroy destroys a kind cluster
func (kr *KindRuntime) Destroy(ctx context.Context) error {
	kind, err := kr.ensureKind(kr.log)
	if err != nil {
		return err
	}

	b, err := exec.CommandContext(ctx, kind, "delete", "cluster", "--name", KindClusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to run kind: %s", b)
}

// Stop stops a kind cluster
func (kr *KindRuntime) Stop(ctx context.Context) error {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}

	timeout := time.Duration(0)
	return d.ContainerStop(ctx, containerruntime.ContainerName, &timeout)
}

// Start starts a kind cluster
func (kr *KindRuntime) Start(ctx context.Context) error {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}

	return d.ContainerStart(ctx, containerruntime.ContainerName, types.ContainerStartOptions{})
}

// GetKubeConfig reads a kubeconfig from Kind and returns it
// This is based on the original shell hack, but a lot safer:
// "$kindPath" get kubeconfig --name "$(yq -r ".name" <"$LIBDIR/kind.yaml")"
//   | sed 's/kind-dev-environment/dev-environment/' >"$KUBECONFIG"
func (kr *KindRuntime) GetKubeConfig(ctx context.Context) (*api.Config, error) {
	kind, err := kr.ensureKind(logrus.New())
	if err != nil {
		return nil, err
	}

	b, err := exec.CommandContext(ctx, kind, "get", "kubeconfig", "--name", KindClusterName).Output()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to run kind: %s", b)
	}

	kubeconfig, err := clientcmd.Load(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load client config")
	}

	if c, ok := kubeconfig.Contexts["kind-"+KindClusterName]; ok {
		kubeconfig.Contexts[KindClusterName] = c
		delete(kubeconfig.Contexts, "kind-"+KindClusterName)
	}

	kubeconfig.CurrentContext = KindClusterName

	return kubeconfig, nil
}

func (kr *KindRuntime) GetClusters(ctx context.Context) ([]*RuntimeCluster, error) {
	curStatus := kr.Status(ctx).Status.Status

	if curStatus == status.Unprovisioned || curStatus == status.Unknown {
		// Only return a cluster if it's actively running
		return []*RuntimeCluster{}, nil
	}

	kubeconfig, err := kr.GetKubeConfig(ctx)
	if err != nil {
		return nil, err
	}

	return []*RuntimeCluster{
		{
			Name:        KindClusterName,
			RuntimeName: kr.GetConfig().Name,
			KubeConfig:  kubeconfig,
		},
	}, nil
}
