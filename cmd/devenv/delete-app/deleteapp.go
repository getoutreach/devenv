package deployapp

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/app"
	"github.com/getoutreach/devenv/pkg/box"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

//nolint:gochecknoglobals
var (
	deployAppLongDesc = `
		delete-app deletes an Outreach application into your developer environment. The application name (appName) provided should match, exactly, an Outreach repository name.
	`
	deployAppExample = `
		# Delete an application in of the developer environment
		devenv deploy-app <appName>

		# Delete a local directory application in the developer environment
		devenv deploy-app .

		# Delete a local application in the developer environment
		devenv deploy-app ./outreach-accounts
	`
)

type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	App string
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

func NewCmdDeleteApp(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "delete-app",
		Usage:       "Delete an application in the developer environment",
		Description: cmdutil.NewDescription(deployAppLongDesc, deployAppExample),
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			if c.Args().Len() == 0 {
				return fmt.Errorf("missing application")
			}
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			o.App = c.Args().First()
			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	err = devenvutil.EnsureDevenvRunning(ctx)
	if err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	return app.Delete(ctx, o.log, o.k, o.conf, o.App)
}
