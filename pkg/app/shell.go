package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Run is a wrapper around NewApp().Run()
func Shell(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig, deploymentProfile string) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.Shell(ctx, deploymentProfile)
}

// runCommand returns the command that should be run to start the dev mode for the application.
// There are two ways to start the dev mode:
// 1. If there's an override script for opening a shell, we use that.
// 2. If there's no override script, we use devspace enter -s directly.
// We also check if devspace is able to start dev mode of the app (has dev configuration).
func (a *App) shellCommand(ctx context.Context, deploymentProfile string) (*exec.Cmd, error) {
	vars := make([]string, 0)
	if deploymentProfile != "" {
		vars = append(vars, fmt.Sprintf("DEVENV_DEV_DEPLOYMENT_PROFILE=%s", deploymentProfile))
	}

	return a.command(ctx, &commandBuilderOptions{
		environmentVariabes: vars,

		requiredConfig: "dev",
		devspaceArgs:   []string{"enter", "-s"},

		fallbackCommandPaths: []string{"./scripts/devenv-apps-shell.sh"},
	})
}

// Dev starts the development mode for the application.
func (a *App) Shell(ctx context.Context, deploymentProfile string) error {
	// We detach from ctx because the child processes handle kill/interupt signals.
	cmd, err := a.shellCommand(context.Background(), deploymentProfile)
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

		return errors.Wrap(err, "failed to open shell for the application")
	}

	return nil
}
