package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Dev is a wrapper around NewApp().Dev()
func Dev(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config,
	conf *rest.Config, appNameOrPath string, kr kubernetesruntime.RuntimeConfig) error {
	app, err := NewApp(ctx, log, k, b, conf, appNameOrPath, &kr)
	// TODO: figure out what version do we want to use where.
	app.Version = "latest"
	if err != nil {
		return errors.Wrap(err, "parse app")
	}
	defer app.Close()

	return app.Dev(ctx)
}

// devCommand returns the command that should be run to start the dev mode for the application.
// There are two ways to start the dev mode:
// 1. If there's an override script for the dev mode, we use that.
// 2. If there's no override script, we use devspace dev directly.
// We also check if devspace is able to start dev mode of the app (has dev configuration).
func (a *App) devCommand(ctx context.Context) (*exec.Cmd, error) {
	// 1. We check whether there's an override script for the deployment.
	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "devenv-apps-dev.sh")); err == nil {
		return cmdutil.CreateKubernetesCommand(ctx, a.Path, "./scripts/devenv-apps-deploy.sh", "start")
	}

	// 2. We check whether there's a devspace.yaml file in the repository.
	var devspaceYamlPath string
	if _, err := os.Stat(filepath.Join(a.Path, "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, "devspace.yaml")
	} else if _, err := os.Stat(filepath.Join(a.Path, ".bootstrap", "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, ".bootstrap", "devspace.yaml")
	}

	// 3. We check whether the devspace has dev configured.
	if devspaceYamlPath != "" {
		// 4. We do have to make sure devspace CLI is installed.
		devspace, err := ensureDevspace(a.log)
		if err != nil {
			return nil, errors.Wrap(err, "failed to ensure devspace is installed")
		}

		// We assume individual profiles don't add dev configs. If they do, this won't work.
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

		devExp := regexp.MustCompile("dev:")
		cfgPos := devExp.FindIndex(devspaceConfig)

		if len(cfgPos) == 0 {
			return nil, errors.New("no dev found in devspace.yaml")
		}

		args := []string{"dev", "--config", devspaceYamlPath}
		// We know ahead of time what namespace bootstrap apps deploy to. so we can use that.
		if a.Type == TypeBootstrap {
			args = append(args, "--namespace", fmt.Sprintf("%s--bento1a", a.RepositoryName), "--no-warn")
		}

		return cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, args...)
	}

	return nil, fmt.Errorf("no way to start dev mode for the application")
}

// Dev starts the development mode for the application.
func (a *App) Dev(ctx context.Context) error {
	// Delete all jobs with a db-migration annotation.
	// TODO: think this through
	// - which namespace should it affect?
	// - should this be handled only for bootstrap?
	// - should this be handled in fallback scripts?
	// err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
	// 	// TODO: the namespace is not quiet right I think.
	// 	Namespaces: []string{a.RepositoryName, fmt.Sprintf("%s--bento1a", a.RepositoryName)},
	// 	Type: &batchv1.Job{
	// 		TypeMeta: v1.TypeMeta{
	// 			Kind:       "Job",
	// 			APIVersion: batchv1.SchemeGroupVersion.Identifier(),
	// 		},
	// 	},
	// 	Validator: func(obj *unstructured.Unstructured) bool {
	// 		var job *batchv1.Job
	// 		err := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &job)
	// 		if err != nil {
	// 			return true
	// 		}

	// 		// filter jobs without our annotation
	// 		return job.Annotations[DeleteJobAnnotation] != "true"
	// 	},
	// })
	// if err != nil {
	// 	a.log.WithError(err).Error("failed to delete jobs")
	// }

	cmd, err := a.devCommand(ctx)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to start dev mode for the application")
	}

	return a.appsClient.Set(ctx, &apps.App{Name: a.RepositoryName, Version: a.Version})
}
