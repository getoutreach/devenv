package devspace

import (
	"context"
	"runtime"

	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	devspaceVersion     = "v5.18.2"
	devspaceDownloadURL = "https://github.com/loft-sh/devspace/releases/download/" +
		devspaceVersion + "/devspace-" + runtime.GOOS + "-" + runtime.GOARCH
)

type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	args []string
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	k, conf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return &Options{
		k:    k,
		conf: conf,
		log:  log,
	}, nil
}

func NewCmdDevspace(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:            "devspace",
		Usage:           "Run devspace commands in your developer environment",
		SkipFlagParsing: true,
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			o.args = c.Args().Slice()

			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	_, err = devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	devspacePath, err := ensureDevspace(o.log)
	if err != nil {
		return errors.Wrap(err, "failed to download devspace CLI")
	}

	contextSet := false
	noWarn := false
	for _, a := range o.args {
		if a == "--kube-context" {
			contextSet = true
		}
		if a == "--no-warn" {
			noWarn = true
		}
	}
	if !contextSet {
		o.args = append(o.args, "--kube-context", "dev-environment")
	}
	// We are specifying namespaces in devspace.yaml and kube context here. No need to frighten people with useless warnings.
	if !noWarn && !contextSet {
		o.args = append(o.args, "--no-warn")
	}

	return errors.Wrap(cmdutil.RunKubernetesCommand(ctx, "", false, devspacePath, o.args...), "failed to run devspace command")
}

func ensureDevspace(log logrus.FieldLogger) (string, error) {
	return cmdutil.EnsureBinary(log, "devspace-"+devspaceVersion, "Devspace CLI", devspaceDownloadURL, "")
}
