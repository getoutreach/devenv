package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ensureDevspace ensures that devspace exists and returns
// the location of devspace binary.
// Note: this outputs text if devspace is being downloaded
func ensureDevspace(log logrus.FieldLogger) (string, error) {
	devspaceVersion := "v5.18.4"
	devspaceDownloadURL := fmt.Sprintf(
		"https://github.com/loft-sh/devspace/releases/download/%s/devspace-%s-%s",
		devspaceVersion,
		runtime.GOOS,
		runtime.GOARCH)

	return cmdutil.EnsureBinary(log, "devspace-"+devspaceVersion, "devspace", devspaceDownloadURL, "")
}

func (a *App) getImageRegistry(ctx context.Context) (registry string, err error) {
	if !a.Local {
		return a.box.DeveloperEnvironmentConfig.ImageRegistry, nil
	}

	switch a.kr.Type {
	case kubernetesruntime.RuntimeTypeLocal:
		registry = "devenv.local"
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

	vars := []string{
		fmt.Sprintf("DEPLOY_TO_DEV_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_DEPLOY_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_DEPLOY_IMAGE_REGISTRY=%s", registry),
		fmt.Sprintf("DEVENV_DEPLOY_APPNAME=%s", a.RepositoryName),
		fmt.Sprintf("DEVENV_TYPE=%s", a.kr.Name),

		// We need to override IMAGE_REGISTRY devspace variable otherwise things fail for local deployments
		fmt.Sprintf("DEVSPACE_FLAGS=--var=IMAGE_REGISTRY=%s", registry),
	}

	if a.kr.Type == kubernetesruntime.RuntimeTypeLocal {
		kind, err := kubernetesruntime.EnsureKind(a.log)
		if err != nil {
			return nil, errors.Wrap(err, "failed to ensure kind is installed")
		}

		vars = append(vars, fmt.Sprintf("DEVENV_KIND_BIN=%s", kind))
	}

	return vars, nil
}

type devspaceCommandOptions struct {
	requiredConfig string
	devspaceArgs   []string

	// If one of these exists, we don't invoke devspace.
	fallbackCommandPaths []string
	fallbackCommandArgs  []string
}

func (a *App) command(ctx context.Context, opts *devspaceCommandOptions) (*exec.Cmd, error) {
	// We can grab the env vars here, we'll need them in almost every case.
	vars, err := a.commandEnv(ctx)
	if err != nil {
		return nil, err
	}

	// 1. We check whether there's an override script for the deployment.
	for _, p := range opts.fallbackCommandPaths {
		if _, err := os.Stat(filepath.Join(a.Path, p)); err != nil {
			continue
		}

		cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, p, opts.fallbackCommandArgs...)
		if err != nil {
			return nil, err
		}

		cmd.Env = append(cmd.Env, vars...)

		return cmd, nil
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

		if err := a.clusterTypeSupported(ctx, devspace, devspaceYamlPath, vars); err != nil {
			return nil, err
		}

		// We assume individual profiles don't add dev configs. If they do, this won't work.
		cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, "print", "--config", devspaceYamlPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create devspace print command")
		}
		cmd.Env = append(cmd.Env, vars...)

		devspaceConfig, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Print(string(devspaceConfig))
			return nil, errors.Wrap(err, "failed to run devspace print command")
		}

		configExp := regexp.MustCompile(fmt.Sprintf("%s:", opts.requiredConfig))
		cfgPos := configExp.FindIndex(devspaceConfig)
		if len(cfgPos) == 0 {
			return nil, fmt.Errorf("no %s found in devspace.yaml", opts.requiredConfig)
		}

		args := opts.devspaceArgs
		args = append(args, "--config", devspaceYamlPath)
		// We know ahead of time what namespace bootstrap apps deploy to. so we can use that.
		if a.Type == TypeBootstrap {
			args = append(args, "--namespace", fmt.Sprintf("%s--bento1a", a.RepositoryName), "--no-warn")
		}

		a.log.Infof("Running devspace %s", strings.Join(args, " "))
		cmd, err = cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, args...)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create devspace command")
		}
		cmd.Env = append(cmd.Env, vars...)
		return cmd, nil
	}

	return nil, fmt.Errorf("no fallback script or devspace.yaml found for the application")
}

func (a *App) clusterTypeSupported(ctx context.Context, devspaceBin, devspaceConfigPath string, envVars []string) error {
	if a.Local && a.kr.Type == kubernetesruntime.RuntimeTypeLocal && a.Type == TypeBootstrap {
		// For KiND and devspace, a kind profile must be configured.
		// We can skip the check if app is not built locally.
		cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path,
			devspaceBin, "list", "profiles", "--disable-profile-activation", "--config", devspaceConfigPath)
		if err != nil {
			return errors.Wrap(err, "failed to create devspace print command")
		}
		cmd.Env = append(cmd.Env, envVars...)

		// devspaceProfiles will look something like this:
		// Name   Active   Description
		// KiND   false    Enables deploying to KiND dev-environment. Automatically acti...
		devspaceProfiles, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Print(string(devspaceProfiles))
			return errors.Wrap(err, "failed to run devspace list profiles command")
		}

		kindProfileExp := regexp.MustCompile("KiND")
		cfgPos := kindProfileExp.FindIndex(devspaceProfiles)
		if len(cfgPos) == 0 {
			return errors.New("local devenv not supported with devspace")
		}
	}

	return nil
}
