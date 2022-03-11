package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Deploy is a wrapper around NewApp().Deploy() that automatically closes
// the app and deploys it into the devenv
func Deploy(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.Deploy(ctx)
}

// deployCommand returns the command that should be run to deploy the application
// There are two ways to deploy:
// 1. If there's an override script for the deployment, we use that.
// 2. If there's no override script, we use devspace deploy directly.
// We also check if devspace is able to deploy the app (has deployments configuration).
func (a *App) deployCommand(ctx context.Context) (*exec.Cmd, error) {
	// 1. We check whether there's an override script for the deployment.
	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "deploy-to-dev.sh")); err == nil {
		return cmdutil.CreateKubernetesCommand(ctx, a.Path, "./scripts/deploy-to-dev.sh", "deploy")
	}

	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "devenv-apps-deploy.sh")); err == nil {
		return cmdutil.CreateKubernetesCommand(ctx, a.Path, "./scripts/devenv-apps-deploy.sh", "deploy")
	}

	// 2. We check whether there's a devspace.yaml file in the repository.
	var devspaceYamlPath string

	if _, err := os.Stat(filepath.Join(a.Path, "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, "devspace.yaml")
	} else if _, err := os.Stat(filepath.Join(a.Path, ".bootstrap", "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, ".bootstrap", "devspace.yaml")
	}

	// 3. We check whether the devspace has deployments configured.
	if devspaceYamlPath != "" {
		// 4. We do have to make sure devspace CLI is installed.
		devspace, err := ensureDevspace(a.log)
		if err != nil {
			return nil, errors.Wrap(err, "failed to ensure devspace is installed")
		}

		// We assume individual profiles don't add deployment configs. If they do, this won't work.
		cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, "print", "--config", devspaceYamlPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create devspace print command")
		}

		vars, err := a.commandEnv(ctx)
		if err != nil {
			return nil, err
		}

		cmd.Env = append(cmd.Env, vars...)
		devspaceConfig, err := cmd.CombinedOutput()
		if err != nil {
			return nil, errors.Wrap(err, "failed to run devspace print command")
		}

		deploymentsExp := regexp.MustCompile("deployments:")
		cfgPos := deploymentsExp.FindIndex(devspaceConfig)

		if len(cfgPos) == 0 {
			return nil, errors.New("no deployments found in devspace.yaml")
		}

		args := []string{"deploy", "--config", devspaceYamlPath}
		// We know ahead of time what namespace bootstrap apps deploy to. so we can use that.
		if a.Type == TypeBootstrap {
			args = append(args, "--namespace", fmt.Sprintf("%s--bento1a", a.RepositoryName), "--no-warn")
		}

		return cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, args...)
	}

	return nil, fmt.Errorf("no way to deploy application")
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

func (a *App) getImageRegistry(ctx context.Context) (registry string, err error) {
	switch a.kr.Type {
	case kubernetesruntime.RuntimeTypeLocal:
		registry = "outreach.local"
	case kubernetesruntime.RuntimeTypeRemote:
		registry, err = apps.DevImageRegistry(ctx, a.log, a.box, a.kr.ClusterName)
	}
	return
}

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

// Deploy deploys the application into the devenv
func (a *App) Deploy(ctx context.Context) error { //nolint:funlen
	// Delete all jobs with a db-migration annotation.
	err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
		// TODO: the namespace is not quiet right I think.
		Namespaces: []string{a.RepositoryName, fmt.Sprintf("%s--bento1a", a.RepositoryName)},
		Type: &batchv1.Job{
			TypeMeta: v1.TypeMeta{
				Kind:       "Job",
				APIVersion: batchv1.SchemeGroupVersion.Identifier(),
			},
		},
		Validator: func(obj *unstructured.Unstructured) bool {
			var job *batchv1.Job
			err := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &job)
			if err != nil {
				return true
			}

			// filter jobs without our annotation
			return job.Annotations[DeleteJobAnnotation] != "true"
		},
	})
	if err != nil {
		a.log.WithError(err).Error("failed to delete jobs")
	}

	cmd, err := a.deployCommand(ctx)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to deploy application")
	}

	if err := devenvutil.WaitForAllPodsToBeReady(ctx, a.k, a.log); err != nil {
		return err
	}

	return a.appsClient.Set(ctx, &apps.App{Name: a.RepositoryName, Version: a.Version})
}
