package kubernetesruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/embed"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	KindVersion     = "v0.12.0-outreach.1"
	KindDownloadURL = "https://github.com/getoutreach/kind/releases/download/" + KindVersion + "/kind-" + runtime.GOOS + "-" + runtime.GOARCH
	KindClusterName = "dev-environment"
)

var configTemplate = template.Must(template.New("kind.yaml").Parse(string(embed.MustRead(embed.Config.ReadFile("config/kind.yaml")))))

// EnsureKind ensures that Kind exists and returns
// the location of kind. Note: this outputs text
// if kind is being downloaded
func EnsureKind(log logrus.FieldLogger) (string, error) { //nolint:funlen
	return cmdutil.EnsureBinary(log, "kind-"+KindVersion, "Kubernetes Runtime", KindDownloadURL, "")
}

// InitKind creates a Kubernetes cluster
func InitKind(ctx context.Context, log logrus.FieldLogger) error {
	kind, err := EnsureKind(log)
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

	cmd := exec.CommandContext(ctx, kind, "create", "cluster", "--name", KindClusterName, "--wait", "5m", "--config", renderedConfig.Name(),
		"--kubeconfig", filepath.Join(os.TempDir(), "devenv-kubeconfig-tmp.yaml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return errors.Wrap(cmd.Run(), "failed to run kind")
}

// ResetKind nukes an existing Kubernetes cluster
func ResetKind(ctx context.Context, log logrus.FieldLogger) error {
	kind, err := EnsureKind(log)
	if err != nil {
		return err
	}

	b, err := exec.CommandContext(ctx, kind, "delete", "cluster", "--name", KindClusterName).CombinedOutput()
	return errors.Wrapf(err, "failed to run kind: %s", b)
}

// GetKubeConfig reads a kubeconfig from Kind and returns it
// This is based on the original shell hack, but a lot safer:
// "$kindPath" get kubeconfig --name "$(yq -r ".name" <"$LIBDIR/kind.yaml")"
//   | sed 's/kind-dev-environment/dev-environment/' >"$KUBECONFIG"
func GetKubeConfig(ctx context.Context, log logrus.FieldLogger) (*api.Config, error) {
	kind, err := EnsureKind(log)
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
