package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Deploy is a wrapper around NewApp().Deploy() that automatically closes
// the app and deploys it into the devenv
func Deploy(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig, useDevspace bool) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	if useDevspace {
		return app.DeployDevspace(ctx)
	}
	return app.Deploy(ctx)
}

// deployLegacy attempts to deploy an application by running the file at
// ./scripts/deploy-to-dev.sh, relative to the repository root.
func (a *App) deployLegacy(ctx context.Context) error {
	a.log.Info("Deploying application into devenv...")
	cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, "./scripts/deploy-to-dev.sh", "update")
	if err != nil {
		return errors.Wrap(err, "failed to create command")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(cmd.Env, "DEPLOY_TO_DEV_VERSION="+a.Version)
	return cmd.Run()
}

// deployBootstrap deploys an application created by Bootstrap
func (a *App) deployBootstrap(ctx context.Context) error { //nolint:funlen
	// Only build a docker image if we're running locally.
	// Note: This is deprecated, devspace/tilt instead.
	builtDockerImage := false
	if a.Local {
		if a.kr.Type == kubernetesruntime.RuntimeTypeLocal {
			if err := a.buildDockerImage(ctx); err != nil {
				return errors.Wrap(err, "failed to build image")
			}
			builtDockerImage = true
		} else {
			a.log.Warn("Skipping docker image build, not supported with remote clusters")
			a.log.Warn("This will likely be stuck at \"Waiting for Pods\".")
		}
	}

	a.log.Info("Deploying application into devenv...")

	// Note: This is done this way because a.Version is not sanitized and could
	// be used to run arbitrary shell commands.
	cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, "./scripts/shell-wrapper.sh", "deploy-to-dev.sh", "update")
	if err != nil {
		return errors.Wrap(err, "failed to create command")
	}

	cmd.Env = append(cmd.Env, "DEPLOY_TO_DEV_VERSION="+a.Version)
	if b, err := cmd.CombinedOutput(); err != nil {
		a.log.Error(string(b))
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
				err := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &pod)
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
	var err error
	if err = a.deleteJobs(ctx); err != nil {
		a.log.WithError(err).Error("failed to delete jobs")
	}

	//nolint:exhaustive // Why: We don't want to delete the app that supports devspace without the x-use-devspace flag.
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

// deployCommand returns the command that should be run to deploy the application
// There are two ways to deploy:
// 1. If there's an override script for the deployment, we use that.
// 2. If there's no override script, we use devspace deploy directly.
// We also check if devspace is able to deploy the app (has deployments configuration).
// Skips building images locally if app is already prebuilt (!Local)
func (a *App) deployCommand(ctx context.Context) (*exec.Cmd, error) {
	args := []string{"deploy"}
	if !a.Local {
		args = append(args, "--skip-build")
	}

	return a.command(ctx, &devspaceCommandOptions{
		requiredConfig: "deployments",
		devspaceArgs:   args,

		fallbackCommandPaths: []string{
			"./scripts/deploy-to-dev.sh",
			"./scripts/devenv-apps-deploy.sh",
		},
		fallbackCommandArgs: []string{"update"},
	})
}

// Deploy deploys the application into the devenv
func (a *App) DeployDevspace(ctx context.Context) error { //nolint:funlen
	if err := a.deleteJobs(ctx); err != nil {
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

func (a *App) deleteJobs(ctx context.Context) error {
	// Delete all jobs with a db-migration annotation.
	err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
		// TODO(DTSS-1455): the namespace is not quiet right I think.
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

	return err
}
