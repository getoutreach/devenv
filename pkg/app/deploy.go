package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/internal/apps"
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
	return a.command(ctx, &devspaceCommandOptions{
		requiredConfig: "deployments",
		devspaceArgs:   []string{"deploy"},

		fallbackCommandPaths: []string{
			"./scripts/deploy-to-dev.sh",
			"./scripts/devenv-apps-deploy.sh",
		},
		fallbackCommandArgs: []string{"deploy"},
	})
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
