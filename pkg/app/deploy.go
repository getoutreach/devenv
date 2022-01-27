package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
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

// Deploy is a wrapper around NewApp().Deploy() that automatically closes
// the app and deploys it into the devenv
func Deploy(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, box *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(ctx, log, k, box, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.Deploy(ctx)
}

// deployLegacy attempts to deploy an application by running the file at
// ./scripts/deploy-to-dev.sh, relative to the repository root.
func (a *App) deployLegacy(ctx context.Context) error {
	a.log.Info("Deploying application into devenv...")
	return errors.Wrap(cmdutil.RunKubernetesCommand(ctx, a.Path, true,
		"./scripts/deploy-to-dev.sh", "update"), "failed to deploy changes")
}

// deployBootstrap deploys an application created by Bootstrap
func (a *App) deployBootstrap(ctx context.Context) error { //nolint:funlen
	// Only build a docker image if we're running locally.
	// Note: This is deprecated, devspace/tilt instead.
	builtDockerImage := false
	if a.Local {
		if a.kr.Type == kubernetesruntime.RuntimeTypeLocal {
			a.log.Warn("Building a local docker image via apps deploy/deploy-app is deprecated")
			if err := a.buildDockerImage(ctx); err != nil {
				return errors.Wrap(err, "failed to build image")
			}
			builtDockerImage = true
		} else {
			a.log.Warn("Skipping docker image build, not supported with remote clusters")
		}
	}

	a.log.Info("Deploying application into devenv...")

	deployScript := "./scripts/shell-wrapper.sh"
	deployScriptArgs := []string{"deploy-to-dev.sh", "update"}

	if err := cmdutil.RunKubernetesCommand(ctx, a.Path, true, deployScript, deployScriptArgs...); err != nil {
		return errors.Wrap(err, "failed to deploy changes")
	}

	// Deprecated: Use devspace/tilt instead. Will be removed soon.
	if builtDockerImage {
		// Delete pods to ensure they are using our image we just pushed into the env
		return devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
			Namespaces: []string{a.RepositoryName + "--bento1a"},
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
					if !strings.Contains(ref.Name(), a.RepositoryName) {
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
//
// !!! Note: This is deprecated: devspace, or tilt, should be used
// !!! for development instead. This will be removed in a future release.
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
	err = cmdutil.RunKubernetesCommand(ctx, a.Path, false, "make", "docker-build")
	if err != nil {
		return err
	}

	a.log.Info("Pushing built Docker Image into Kubernetes")
	//nolint:staticcheck // Why: we're aware of the deprecation
	kindPath, err := kubernetesruntime.EnsureKind(a.log)
	if err != nil {
		return errors.Wrap(err, "failed to find/download Kind")
	}

	baseImage := fmt.Sprintf("gcr.io/outreach-docker/%s", a.RepositoryName)
	taggedImage := fmt.Sprintf("%s:%s", baseImage, a.Version)

	// tag the image to be the same as the version, which is a required format
	// to be followed
	if err = cmdutil.RunKubernetesCommand(ctx, a.Path, true,
		"docker", "tag", baseImage, taggedImage); err != nil {
		return errors.Wrap(err, "failed to tag image")
	}

	// load the docker image into the kind cache
	err = cmdutil.RunKubernetesCommand(
		ctx,
		a.Path,
		true,
		kindPath,
		"load",
		"docker-image",
		taggedImage,
		"--name",
		kubernetesruntime.KindClusterName,
	)

	return errors.Wrap(err, "failed to push docker image to Kubernetes")
}

// Deploy deploys the application into the devenv
func (a *App) Deploy(ctx context.Context) error { //nolint:funlen
	// Delete all jobs with a db-migration annotation.
	err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
		Namespaces: []string{a.RepositoryName, fmt.Sprintf("%s--bento1a", a.RepositoryName)},
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

	if err := devenvutil.WaitForAllPodsToBeReady(ctx, a.k, a.log); err != nil {
		return err
	}

	return a.appsClient.Set(ctx, &apps.App{Name: a.RepositoryName, Version: a.Version})
}
