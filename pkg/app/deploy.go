package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/sshhelper"
	"github.com/getoutreach/gobox/pkg/trace"
	dockerparser "github.com/novln/docker-parser"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Deploy deploys an application by name, to the devenv.
func Deploy(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, conf *rest.Config, appNameOrPath string) error {
	app, err := NewApp(log, k, conf, appNameOrPath)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}

	return app.Deploy(ctx)
}

// deployLegacy attempts to deploy an application by running the file at
// ./scripts/deploy-to-dev.sh, relative to the repository root.
func (a *App) deployLegacy(ctx context.Context) error {
	a.log.Info("Deploying application into devenv...")
	return errors.Wrap(cmdutil.RunKubernetesCommand(ctx, a.Path, true, "./scripts/deploy-to-dev.sh", "update"), "failed to deploy changes")
}

func (a *App) deployBootstrap(ctx context.Context) error { //nolint:funlen
	if err := a.determineRepositoryName(); err != nil {
		return errors.Wrap(err, "determine repository name")
	}
	a.log = a.log.WithField("app.name", a.RepositoryName)

	// Only build a docker image if we're not using the latest version
	// or if we're in local mode
	builtDockerImage := false
	if a.Version != "" || a.Local {
		if err := a.buildDockerImage(ctx); err != nil {
			return errors.Wrap(err, "failed to build image")
		}
		builtDockerImage = true
	}

	a.log.Info("Deploying application into devenv...")

	deployScript := "./scripts/deploy-to-dev.sh"
	deployScriptArgs := []string{"update"}

	// Cheap way of detecting bootstrap v6 w/o importing bootstrap.lock
	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "shell-wrapper.sh")); err == nil {
		deployScript = "./scripts/shell-wrapper.sh"
		deployScriptArgs = append([]string{"deploy-to-dev.sh"}, deployScriptArgs...)
	}

	if err := cmdutil.RunKubernetesCommand(ctx, a.Path, true, deployScript, deployScriptArgs...); err != nil {
		return errors.Wrap(err, "failed to deploy changes")
	}

	if builtDockerImage {
		// Delete pods to ensure they are using the latest docker image we pushed
		return devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
			Namespaces: []string{a.RepositoryName + "--bento1a"},
			// TODO: We have to be able to get this information elsewhere.
			Type: &corev1.Pod{
				TypeMeta: v1.TypeMeta{
					Kind:       "Pod",
					APIVersion: corev1.SchemeGroupVersion.Identifier(),
				},
			},
			Validator: func(obj *unstructured.Unstructured) bool {
				var pod *corev1.Pod
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &pod)
				if err != nil {
					return true
				}

				for i := range pod.Spec.Containers {
					cont := &pod.Spec.Containers[i]

					ref, err := dockerparser.Parse(cont.Image)
					if err != nil {
						continue
					}

					// check if it matched our applications image name.
					// eventually we should do a better job at checking this (not building it ourself)
					if !strings.Contains(ref.Name(), fmt.Sprintf("outreach-docker/%s", a.RepositoryName)) {
						continue
					}

					// return false here to not filter out the pod
					// because we found a container we wanted
					return false
				}

				return true
			},
		})
	}

	return nil
}

// buildDockerImage builds a docker image from a bootstrap repo
// and deploys it into the developer environment cache
func (a *App) buildDockerImage(ctx context.Context) error {
	ctx = trace.StartCall(ctx, "deployapp.buildDockerImage")
	defer trace.EndCall(ctx)

	a.log.Info("Configuring ssh-agent for Docker")

	sshAgent := sshhelper.GetSSHAgent()

	_, err := sshhelper.LoadDefaultKey("github.com", sshAgent, a.log)
	if err != nil {
		return errors.Wrap(err, "failed to load Github SSH key into in-memory keyring")
	}

	a.log.Info("Building Docker image (this may take awhile)")
	err = cmdutil.RunKubernetesCommand(ctx, a.Path, true, "make", "docker-build")
	if err != nil {
		return err
	}

	a.log.Info("Pushing built Docker Image into Kubernetes")
	kindPath, err := kubernetesruntime.EnsureKind(a.log)
	if err != nil {
		return errors.Wrap(err, "failed to find/download Kind")
	}

	err = cmdutil.RunKubernetesCommand(
		ctx,
		a.Path,
		true,
		kindPath,
		"load",
		"docker-image",
		fmt.Sprintf("gcr.io/outreach-docker/%s", a.RepositoryName),
		"--name",
		kubernetesruntime.KindClusterName,
	)

	return errors.Wrap(err, "failed to push docker image to Kubernetes")
}

func (a *App) Deploy(ctx context.Context) error { //nolint:funlen
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

	// Delete all jobs with a db-migration annotation.

	err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
		Namespaces: []string{a.RepositoryName, fmt.Sprintf("%s--bento1a", a.RepositoryName)},
		// TODO: We have to be able to get this information elsewhere.
		Type: &batchv1.Job{
			TypeMeta: v1.TypeMeta{
				Kind:       "Job",
				APIVersion: batchv1.SchemeGroupVersion.Identifier(),
			},
		},
		Validator: func(obj *unstructured.Unstructured) bool {
			var job *batchv1.Job
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &job)
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

	switch a.Type {
	case TypeBootstrap:
		err = a.deployBootstrap(ctx)
	case TypeLegacy:
		err = a.deployLegacy(ctx)
	default:
		err = fmt.Errorf("unknown application type %s", a.Type)
	}
	if err != nil {
		return err
	}

	return devenvutil.WaitForAllPodsToBeReady(ctx, a.k, a.log)
}
