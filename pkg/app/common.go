package app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ensureDevspace ensures that loft exists and returns
// the location of devspace binary.
// Note: this outputs text if loft is being downloaded
func ensureDevspace(log logrus.FieldLogger) (string, error) {
	devspaceVersion := "v5.18.4"
	devspaceDownloadURL := fmt.Sprintf(
		"https://github.com/loft-sh/devspace/releases/download/%s/devspace-%s-%s",
		devspaceVersion,
		runtime.GOOS,
		runtime.GOARCH)

	return cmdutil.EnsureBinary(log, "devspace-"+devspaceVersion, "Kubernetes Runtime", devspaceDownloadURL, "")
}

func (a *App) getImageRegistry(ctx context.Context) (registry string, err error) {
	switch a.kr.Type {
	case kubernetesruntime.RuntimeTypeLocal:
		registry = "outreach.local"
	case kubernetesruntime.RuntimeTypeRemote:
		registry, err = apps.DevImageRegistry(ctx, a.log, a.box, a.kr.ClusterName)
	}
	return
}

// commandEnv returns the environment variables that should be set for the deploy/dev commands
func (a *App) commandEnv(ctx context.Context) ([]string, error) {
	registry, err := a.getImageRegistry(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get image registry")
	}

	fmt.Printf("DEVENV_IMAGE_REGISTRY=%s\n", registry)

	vars := []string{
		fmt.Sprintf("DEPLOY_TO_DEV_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_APP_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_IMAGE_REGISTRY=%s", registry),
		fmt.Sprintf("DEVENV_KIND=%s", a.kr.Name),
		fmt.Sprintf("DEVENV_APPNAME=%s", a.RepositoryName),
	}

	return vars, nil
}
