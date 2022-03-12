package app

import (
	"context"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func Delete(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.Delete(ctx)
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
