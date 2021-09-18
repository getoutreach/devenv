package kubernetesruntime

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/gobox/pkg/box"
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

	box *box.Config
	log logrus.FieldLogger
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

func (lr *LoftRuntime) PreCreate(ctx context.Context) error {
	lcli, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, lcli, "login")
	out, err := cmd.CombinedOutput()

	// HACK: Currently `loft login` doesn't return a non-zero exit code
	// when not logged in. Very sad.
	if err != nil || strings.Contains(string(out), "Not logged in") {
		lr.log.Info("Authenticating with loft")
		return cmdutil.RunKubernetesCommand(ctx, "", false, lcli, "login", lr.box.DeveloperEnvironmentConfig.RuntimeConfig.Loft.URL)
	}

	return nil
}

func (*LoftRuntime) GetConfig() RuntimeConfig {
	return RuntimeConfig{
		Name: "loft",
		Type: RuntimeTypeRemote,
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

	clusterName, err := lr.getVclusterName()
	if err != nil {
		resp.Status.Status = status.Unknown
		resp.Status.Reason = errors.Wrap(err, "failed to get cluster name").Error()
		return resp
	}

	// TODO(jaredallard): See if we can hit loft's API instead of this
	// hacky not totally valid contains check.
	if strings.Contains(string(out), clusterName) {
		resp.Status.Status = status.Running
	}

	return resp
}

func (*LoftRuntime) getVclusterName() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", errors.Wrap(err, "failed to lookup current user")
	}

	return u.Username + "-devenv", nil
}

func (lr *LoftRuntime) Create(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
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

func (lr *LoftRuntime) Destroy(ctx context.Context) error {
	loft, err := lr.ensureLoft(lr.log)
	if err != nil {
		return err
	}

	vclusterName, err := lr.getVclusterName()
	if err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, loft, "delete", "vcluster", vclusterName).CombinedOutput()
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
