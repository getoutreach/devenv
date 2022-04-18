package shell

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/app"
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

//nolint:gochecknoglobals
var (
	longDesc = `
		Opens a shell into running dev container
	`
	example = `
		# Opens a shell into running dev container
		devenv apps shell
	`
)

// Options are various options for the `apps dev` command
type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// DeploymentProfile is the profile to use with devspace, it's passed in in an env variable $DEVENV_DEV_DEPLOYMENT_PROFILE
	DeploymentProfile string

	// AppNameOrPath is the app to dev either (in case of dev it should always be path)
	AppNameOrPath string
}

// NewOptions create an initialized options struct for the `apps dev` command
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

// NewCmd creates a new cli.Command for the `apps dev` command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "shell",
		Usage:       "Opens a shell into running dev container.",
		Description: cmdutil.NewDescription(longDesc, example),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "deployment",
				Usage: "When project has multiple deployments, specify which deployment to substitute for the dev container",
			},
		},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}
			o.AppNameOrPath = c.Args().First()
			// TODO(DTSS-1494) use git to get root directory
			if o.AppNameOrPath == "" {
				o.AppNameOrPath = "."
			}

			// If not set, go with default deployment
			deploymentFlag := c.String("deployment")
			if deploymentFlag != "" {
				o.DeploymentProfile = fmt.Sprintf("deployment__%s", deploymentFlag)
			}
			return o.Run(c.Context)
		},
	}
}

// Run runs the `apps dev` command
func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	return app.Shell(ctx, o.log, o.k, b, o.conf, o.AppNameOrPath, kr.GetConfig(), o.DeploymentProfile)
}
