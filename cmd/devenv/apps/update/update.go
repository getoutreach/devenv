package update

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"k8s.io/client-go/kubernetes"
)

//nolint:gochecknoglobals
var (
	updateLongDesc = `
		updates your applications running in your developer environment
	`
	updateExample = `
		# Update all your applications
		devenv apps update

		# Update a specific application
		devenv apps update <name>
	`
)

type Options struct {
	log logrus.FieldLogger
	k   kubernetes.Interface
	b   *box.Config

	AppName string
}

func NewOptions(log logrus.FieldLogger) *Options {
	b, err := box.LoadBox()
	if err != nil {
		panic(err)
	}

	return &Options{
		log: log,
		b:   b,
	}
}

// NewCmd returns a cli.Command for this command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "update",
		Usage:       "Update application(s) in your developer environment",
		Description: cmdutil.NewDescription(updateLongDesc, updateExample),
		Action: func(c *cli.Context) error {
			o := NewOptions(log)
			o.AppName = c.Args().First()

			k, err := kube.GetKubeClient()
			if err != nil {
				return errors.Wrap(err, "failed to create kubernetes client")
			}
			o.k = k

			return o.Run(c.Context)
		},
	}
}

// Run runs the update-apps command
func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	//nolint:govet // Why: err shadow
	if _, err := devenvutil.EnsureDevenvRunning(ctx, conf, b); err != nil {
		return err
	}

	appsClient := apps.NewKubernetesConfigmapClient(o.k, "")
	deployedApps, err := appsClient.List(ctx)
	if err != nil {
		return err
	}

	if o.AppName != "" {
		// check if app is in list
		var foundApp *apps.App
		for i := range deployedApps {
			a := &deployedApps[i]
			if a.Name == o.AppName {
				foundApp = a
				break
			}
		}
		if foundApp == nil {
			return fmt.Errorf("Unknown app '%s', please use deploy-app to deploy it first", o.AppName)
		}

		// only update the app provided
		deployedApps = []apps.App{*foundApp}
	}

	o.log.Infof("Updating %d service(s)", len(deployedApps))

	// iterate over all the apps, and run deploy on them...
	// this will force the manifests+images (set VERSION) to be
	// updated.

	return nil
}
