package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func Delete(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig, useDevspace bool) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	if useDevspace {
		return app.DeleteDevspace(ctx)
	}

	return app.Delete(ctx)
}

// deleteLegacy attempts to delete an application by running the file at
// ./scripts/deploy-to-dev.sh, relative to the repository root.
func (a *App) deleteLegacy(ctx context.Context) error {
	a.log.Info("Deleting application from devenv...")
	return errors.Wrap(cmdutil.RunKubernetesCommand(ctx, a.Path, true, "./scripts/deploy-to-dev.sh", "delete"), "failed to delete application")
}

// deleteBootstrap deletes a bootstrapped repository from
// the devenv
func (a *App) deleteBootstrap(ctx context.Context) error {
	a.log.Info("Deleting application from devenv...")

	deployScript := "./scripts/shell-wrapper.sh"
	deployScriptArgs := []string{"deploy-to-dev.sh", "delete"}

	if err := cmdutil.RunKubernetesCommand(ctx, a.Path, true, deployScript, deployScriptArgs...); err != nil {
		return errors.Wrap(err, "failed to delete application")
	}

	return nil
}

// deleteCommand returns the command that should be run to delete the application
// There are two ways to deploy:
// 1. If there's an override script for the deployment, we use that.
// 2. If there's no override script, we use devspace purge directly.
// We also check if devspace is able to deploy the app (has deployments configuration).
func (a *App) deleteCommand(ctx context.Context) (*exec.Cmd, error) {
	return a.command(ctx, &devspaceCommandOptions{
		requiredConfig: "deployments",
		devspaceArgs:   []string{"purge"},

		fallbackCommandPaths: []string{
			"./scripts/deploy-to-dev.sh",
			"./scripts/devenv-apps-deploy.sh",
		},
		fallbackCommandArgs: []string{"delete"},
	})
}

func (a *App) Delete(ctx context.Context) error {
	var err error
	switch a.Type {
	case TypeBootstrap:
		err = a.deleteBootstrap(ctx)
	case TypeLegacy:
		err = a.deleteLegacy(ctx)
	default:
		// If this ever fires, there is an issue with *App.determineType.
		return fmt.Errorf("unknown application type %s", a.Type)
	}
	if err != nil {
		return err
	}

	return a.appsClient.Delete(ctx, a.RepositoryName)
}

// DeleteDevspace deletes the application using devspace purge commnad
func (a *App) DeleteDevspace(ctx context.Context) error {
	cmd, err := a.deleteCommand(ctx)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to delete application")
	}

	return a.appsClient.Delete(ctx, a.RepositoryName)
}
