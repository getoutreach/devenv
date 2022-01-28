package delete

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/internal/apps"
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
	deleteLongDesc = `
	  Deletes an Outreach application in your developer environment, should match a repository name
	`
	deleteExample = `
		# Delete an application in of the developer environment
		devenv apps delete <appName>

		# Delete a local directory application in the developer environment
		devenv apps delete .

		# Delete a local application in the developer environment
		devenv apps delete ./outreach-accounts
	`
)

type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// App to delete
	App string
}

// NewOptions creates a new options struct for this command
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

// NewCmd creates a new cli.Command for this command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Aliases:     []string{"purge"},
		Usage:       "Delete an application in the developer environment",
		Description: cmdutil.NewDescription(deleteLongDesc, deleteExample),
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

// Run runs this command
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

	appsClient := apps.NewKubernetesConfigmapClient(o.k, "")
	deployedApps, err := appsClient.List(ctx)
	if err != nil {
		return err
	}

	found := false
	for _, a := range deployedApps {
		if a.Name == o.App {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Failed to find application named '%s' that was deployed", o.App)
	}

	return app.Delete(ctx, o.log, o.k, b, o.conf, o.App, kr.GetConfig())
}
