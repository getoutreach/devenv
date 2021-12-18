package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func Delete(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(log, k, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}

	return app.Delete(ctx)
}

// deleteLegacy attempts to delete an application by running the file at
// ./scripts/deploy-to-dev.sh, relative to the repository root.
func (a *App) deleteLegacy(ctx context.Context) error {
	a.log.Info("Deleting application from devenv...")
	return errors.Wrap(cmdutil.RunKubernetesCommand(ctx, a.Path, true, "./scripts/deploy-to-dev.sh", "delete"), "failed to delete application")
}

func (a *App) deleteBootstrap(ctx context.Context) error {
	if err := a.determineRepositoryName(); err != nil {
		return errors.Wrap(err, "determine repository name")
	}
	a.log = a.log.WithField("app.name", a.RepositoryName)

	a.log.Info("Deleting application from devenv...")

	deployScript := "./scripts/deploy-to-dev.sh"
	deployScriptArgs := []string{"delete"}

	// Cheap way of detecting bootstrap v6 w/o importing bootstrap.lock
	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "shell-wrapper.sh")); err == nil {
		deployScript = "./scripts/shell-wrapper.sh"
		deployScriptArgs = append([]string{"deploy-to-dev.sh"}, deployScriptArgs...)
	}

	if err := cmdutil.RunKubernetesCommand(ctx, a.Path, true, deployScript, deployScriptArgs...); err != nil {
		return errors.Wrap(err, "failed to delete application")
	}

	return nil
}

func (a *App) Delete(ctx context.Context) error {
	// Download the repository if it doesn't already exist on disk.
	if a.Path == "" {
		cleanup, err := a.downloadRepository(ctx, a.RepositoryName)
		defer cleanup()

		if err != nil {
			return err
		}
	}

	if err := a.determineType(); err != nil {
		return errors.Wrap(err, "determine repository type")
	}

	switch a.Type {
	case TypeBootstrap:
		return a.deleteBootstrap(ctx)
	case TypeLegacy:
		return a.deleteLegacy(ctx)
	}

	// If this ever fires, there is an issue with *App.determineType.
	return fmt.Errorf("unknown application type %s", a.Type)
}
