package run

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/cmd/devenv/apps/run/stop"
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
	runLongDesc = `
		Runs the application from source code.
	`
	runExample = `
		# Runs the application from source code.
		devenv apps run

		# Opens a terminal into dev container - not all apps support this.
		devenv apps run --with-terminal

		# Builds image from source code and deploys the app with that image, then replaces the pods with dev image and runs the app.
		devenv apps run --with-local-image

		# Replace non-default deployment with dev container.
		devenv apps run --deployment=deployment-name

		# Don't forward ports
		devenv apps run --skip-portforwarding

		# Clean up after dev image deployments.
		devenv apps run stop
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

	// LocalImage is a flag for enabling building images from source instead of using images from CI
	LocalImage bool

	// Terminal is a flag passed to devspace as DEVENV_DEV_TERMINAL=true. It activates the profile for starting terminal instead of service.
	Terminal bool

	// SkipPortforwarding is a flag passed to devspace as DEVENV_DEV_SKIP_PORTFORWARDING=true. It skips port forwarding.
	SkipPortForwarding bool

	// DeployDependencies is flag to determine if app dependencies should be deployed as well.
	DeployDependencies bool
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
		Name:        "run",
		Aliases:     []string{"dev"},
		Usage:       "Runs the application from source code.",
		Description: cmdutil.NewDescription(runLongDesc, runExample),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "with-local-image",
				Aliases: []string{"i", "local-image"},
				Usage:   "Use images built from source. By default dev uses images from CI for deployments to speed up the process.",
			},
			&cli.BoolFlag{
				Name:    "with-terminal",
				Aliases: []string{"t", "terminal"},
				Usage:   "Open an interactive terminal to the dev container instead of running the application",
			},
			&cli.BoolFlag{
				Name:  "with-dependencies",
				Usage: "Deploys app dependencies as well. This will be true by default in the future.",
			},
			&cli.BoolFlag{
				Name:    "skip-portforwarding",
				Aliases: []string{"p"},
				Usage:   "Skip forwarding ports; useful when running multiple `devenv apps run` commands at once",
			},
			&cli.StringFlag{
				Name:  "deployment",
				Usage: "When project has multiple deployments, specify which deployment to substitute for the dev container",
			},
		},
		Subcommands: []*cli.Command{
			stop.NewCmd(log),
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
			o.LocalImage = c.Bool("with-local-image")
			o.Terminal = c.Bool("with-terminal")
			o.SkipPortForwarding = c.Bool("skip-portforwarding")
			o.DeployDependencies = c.Bool("with-dependencies")

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

	return app.Run(ctx, o.log, o.k, b, o.conf, o.AppNameOrPath, kr.GetConfig(),
		app.RunOptions{
			UseLocalImage:      o.LocalImage,
			SkipPortForwarding: o.SkipPortForwarding,
			OpenTerminal:       o.Terminal,
			DeploymentProfile:  o.DeploymentProfile,
			DeployDependencies: o.DeployDependencies,
		})
}
