package app

import (
	"context"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Dev is a wrapper around NewApp().Dev()
func Dev(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig, localImage, terminal bool) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	app.Local = localImage
	if !localImage && app.Version == AppVersionLocal {
		app.Version = AppVersionLatest
	}

	return app.Dev(ctx, terminal)
}

// DevStop is a wrapper around NewApp().DevStop()
func DevStop(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.DevStop(ctx)
}

// devCommand returns the command that should be run to start the dev mode for the application.
// There are two ways to start the dev mode:
// 1. If there's an override script for the dev mode, we use that.
// 2. If there's no override script, we use devspace dev directly.
// We also check if devspace is able to start dev mode of the app (has dev configuration).
func (a *App) devCommand(ctx context.Context, terminal bool) (*exec.Cmd, error) {
	vars := make([]string, 0)
	if terminal {
		vars = append(vars, "DEVENV_DEV_TERMINAL=true")
	}

	return a.command(ctx, &commandBuilderOptions{
		environmentVariabes: vars,

		requiredConfig: "dev",
		devspaceArgs:   []string{"dev"},

		fallbackCommandPaths: []string{"./scripts/devenv-apps-dev.sh"},
		fallbackCommandArgs:  []string{"start"},
	})
}

// devStopCommand returns the command that should be run to start the dev mode for the application.
// There are two ways to start the dev mode:
// 1. If there's an override script for the dev mode, we use that.
// 2. If there's no override script, we use devspace reset pods directly.
// We also check if devspace is able to start dev mode of the app (has dev configuration).
func (a *App) devStopCommand(ctx context.Context) (*exec.Cmd, error) {
	return a.command(ctx, &commandBuilderOptions{
		requiredConfig: "dev",
		devspaceArgs:   []string{"reset", "pods"},

		fallbackCommandPaths: []string{"./scripts/devenv-apps-dev.sh"},
		fallbackCommandArgs:  []string{"stop"},
	})
}

// Dev starts the development mode for the application.
func (a *App) Dev(ctx context.Context, terminal bool) error {
	// TODO(DTSS-1496): Handle deleting jobs. devspace v6 will support doing this.

	// We detach from ctx because the child processes handle kill/interupt signals.
	// Iterrupt is a valid use case in which we want to stop the dev mode. Bootstrap devspace.yaml has special
	// handling for devCommand:interrupt event and calls devenv apps dev stop.
	cmd, err := a.devCommand(context.Background(), terminal)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		// We don't want to return an error if the app has been interrupted/killed. It's an expected state.
		if ctx.Err() != nil {
			return nil
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 130 {
				return nil
			}
			a.log.Infof("exit code: %d", exitErr.ExitCode())
		}

		return errors.Wrap(err, "failed to start dev mode for the application")
	}

	return a.appsClient.Set(ctx, &apps.App{Name: a.RepositoryName, Version: a.Version})
}

// Dev stop the development mode for the application.
func (a *App) DevStop(ctx context.Context) error {
	cmd, err := a.devStopCommand(ctx)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to stop dev mode for the application")
	}

	return a.appsClient.Set(ctx, &apps.App{Name: a.RepositoryName, Version: a.Version})
}
