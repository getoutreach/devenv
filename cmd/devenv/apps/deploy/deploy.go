package deploy

import (
	"context"
	"fmt"
	"os"

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
	deployLongDesc = `
		Deploys an Outreach application into your developer environment.
		The application name (appName) provided should match, exactly, an Outreach repository name.
	`
	deployExample = `
		# Deploy an application to the developer environment
		devenv apps deploy <appName>

		# Deploy a local directory application to the developer environment
		devenv apps deploy .
	`
)

// Options are various options for the `apps deploy` command
type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// App is the app to deploy
	App string

	// UseDevspace is a flag that determines whether to use devspace for deployment or not.
	UseDevspace bool

	// DeployDependencies is a flag that determines whether to deploy app dependencies or not.
	DeployDependencies bool
}

// NewOptions create an initialized options struct for the `apps deploy` command
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

// NewCmd creates a new cli.Command for the `apps deploy` command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "deploy",
		Usage:       "Deploy an application to the developer environment",
		Description: cmdutil.NewDescription(deployLongDesc, deployExample),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "x-use-devspace",
				EnvVars: []string{"DEVENV_DEPLOY_USE_DEVSPACE"},
				Usage:   "Uses devspace to deploy the application. Might not be supported by all applications and all environments.",
			},
			&cli.BoolFlag{
				Name:    "with-dependencies",
				Aliases: []string{"with-deps"},
				Usage:   "Deploys app dependencies as well. This will be true by default in the future.",
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() == 0 {
				return fmt.Errorf("missing application")
			}
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			o.App = c.Args().First()
			o.UseDevspace = c.Bool("x-use-devspace")
			if o.UseDevspace {
				// This flag can be set either using $DEVENV_DEPLOY_USE_DEVSPACE or `--x-use-devspace` flag.
				// We want to ensure that child processes inherit this flag automaatically.
				os.Setenv("DEVENV_DEPLOY_USE_DEVSPACE", "1")
			}
			o.DeployDependencies = c.Bool("with-dependencies")
			return o.Run(c.Context)
		},
	}
}

// Run runs the `apps deploy` command
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

	return app.Deploy(ctx, o.log, o.k, b, o.conf, o.App, kr.GetConfig(),
		app.DeploymentOptions{
			UseDevspace:        o.UseDevspace,
			SkipDeployed:       false,
			DeployDependencies: o.DeployDependencies,
		})
}
