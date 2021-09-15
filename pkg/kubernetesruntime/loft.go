package kubernetesruntime

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"runtime"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	loftVersion     = "v1.15.0"
	loftDownloadURL = "https://github.com/loft-sh/loft/releases/download/" + loftVersion + "/loft-" + runtime.GOOS + "-" + runtime.GOARCH
)

type LoftRuntime struct {
	// kubeConfig stores the kubeconfig of the last created
	// cluster by Create()
	kubeConfig []byte
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

func (*LoftRuntime) GetConfig() RuntimeConfig {
	return RuntimeConfig{
		Name: "loft",
		Type: RuntimeTypeRemote,
	}
}

func (*LoftRuntime) Status(ctx context.Context, log logrus.FieldLogger) RuntimeStatus {
	resp := RuntimeStatus{status.Status{
		Status: status.Unknown,
	}}

	return resp
}

func (*LoftRuntime) getVclusterName() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", errors.Wrap(err, "failed to lookup current user")
	}

	return u.Username + "-devenv", nil
}

func (lr *LoftRuntime) Create(ctx context.Context, log logrus.FieldLogger) error {
	loft, err := lr.ensureLoft(log)
	if err != nil {
		return err
	}

	vclusterName, err := lr.getVclusterName()
	if err != nil {
		return err
	}

	kubeConfig, err := os.CreateTemp("", "loft-kubeconfig-*")
	if err != nil {
		return err
	}
	kubeConfig.Close() //nolint:errcheck
	defer os.Remove(kubeConfig.Name())

	cmd := exec.CommandContext(ctx, loft, "create", "vcluster", "--template", "devenv", vclusterName)
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

func (lr *LoftRuntime) Destroy(ctx context.Context, log logrus.FieldLogger) error {
	loft, err := lr.ensureLoft(log)
	if err != nil {
		return err
	}

	vclusterName, err := lr.getVclusterName()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, loft, "delete", "vcluster", vclusterName)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	return errors.Wrap(cmd.Run(), "failed to delete loft vcluster")
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
