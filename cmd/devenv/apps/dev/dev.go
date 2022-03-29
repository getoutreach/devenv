package dev

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/cmd/devenv/apps/dev/stop"
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
	devLongDesc = `
		Starts the development mode for the application.
	`
	devExample = `
		# Starts the development mode for the application.
		devenv apps dev

		# Stop the development mode for the application.
		devenv apps dev stop
	`
)

// Options are various options for the `apps dev` command
type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// Path is the app to dev
	DeploymentProfile string
	AppNameOrPath     string
	LocalImage        bool
	Terminal          bool
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
		Name:        "dev",
		Usage:       "Starts the development mode for the application.",
		Description: cmdutil.NewDescription(devLongDesc, devExample),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "local-image",
				Usage: "Use images built from source. By default dev uses images from CI for deployments to speed up the process.",
			},
			&cli.BoolFlag{
				Name:  "terminal",
				Usage: "Open an interactive terminal to the dev container instead of running the application",
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
			o.LocalImage = c.Bool("local-image")
			o.Terminal = c.Bool("terminal")

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

	return app.Dev(ctx, o.log, o.k, b, o.conf, o.AppNameOrPath, kr.GetConfig(), o.LocalImage, o.Terminal, o.DeploymentProfile)
}
