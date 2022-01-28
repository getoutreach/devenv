package update

import (
	"context"
	"fmt"
	"io"

	"github.com/getoutreach/devenv/internal/apps"
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
	updateLongDesc = `
		Updates applications in your developer environment. This usually involves updating Docker images/Kubernetes manifests, but is up to the application.
	`
	updateExample = `
		# Update all your applications
		devenv apps update

		# Update a specific application
		devenv apps update <name>
	`
)

type Options struct {
	log   logrus.FieldLogger
	k     kubernetes.Interface
	kconf *rest.Config
	b     *box.Config

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

			k, rconf, err := kube.GetKubeClientWithConfig()
			if err != nil {
				return errors.Wrap(err, "failed to create kubernetes client")
			}
			o.k = k
			o.kconf = rconf

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

	kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}
	krConfig := kr.GetConfig()

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
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

	o.log.Infof("Checking %d service(s) for updates", len(deployedApps))
	for _, a := range deployedApps {
		log := o.log.WithField("app.name", a.Name)
		newVersion, err := app.NewApp(ctx, &logrus.Logger{Out: io.Discard}, o.k, o.b, o.kconf, a.Name, &krConfig)
		if err != nil {
			log.WithError(err).Warn("Failed to check/stage for updates")
			continue
		}
		log = log.WithField("app.version", newVersion.Version)

		if newVersion.Version == a.Version {
			log.Info("No new updates available")
			continue
		}

		log.WithField("app.old_version", a.Version).Info("Updating application")

		err = newVersion.Deploy(ctx)
		newVersion.Close() //nolint:errcheck // Why: Best effort
		if err != nil {
			log.WithError(err).Warn("Failed to update")
		}
	}

	return nil
}
