package devspace

import (
	"context"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/internal/vault"
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
	_, err := exec.LookPath("devspace")
	if err != nil {
		return errors.Wrap(err, "failed to find devspace CLI. run `orc setup` to install it")
	}

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

	//nolint:gosec // Why: This is a pass through command to devspace, we want to pass through all arguments.
	cmd := exec.CommandContext(ctx, "devspace", o.args...)

	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
