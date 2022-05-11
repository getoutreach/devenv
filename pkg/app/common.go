package app

import (
	"bytes"
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
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// ensureDevspace ensures that devspace exists and returns
// the location of devspace binary.
// Note: this outputs text if devspace is being downloaded
func ensureDevspace(log logrus.FieldLogger) (string, error) {
	devspaceVersion := "v6.0.0-alpha.13"
	devspaceDownloadURL := fmt.Sprintf(
		"https://github.com/loft-sh/devspace/releases/download/%s/devspace-%s-%s",
		devspaceVersion,
		runtime.GOOS,
		runtime.GOARCH)

	devspace, err := cmdutil.EnsureBinary(log, "devspace-"+devspaceVersion, "devspace", devspaceDownloadURL, "")
	if err != nil {
		return "", err
	}

	b, err := exec.Command(devspace, "list", "plugins").Output()
	if err != nil {
		// We don't care enough about the telemetry plugin to crash the whole process
		return devspace, nil
	}

	// Adding and updating the telemetry plugin is a best effort attempt.
	// It should not block or slow down the deployment process.
	if !bytes.Contains(b, []byte("devtel")) {
		go func() {
			//nolint:errcheck // Why: We don't care enough about the telemetry plugin to crash the whole process
			_ = exec.Command(devspace, "add", "plugin", "https://github.com/getoutreach/devtel").Run()
		}()
	} else {
		go func() {
			//nolint:errcheck // Why: We don't care enough about the telemetry plugin to crash the whole process
			_ = exec.Command(devspace, "update", "plugin", "devtel").Run()
		}()
	}

	return devspace, nil
}

// getImageRegistry returns the image registry for the app
// It returns the dev registry (either devenv.local or one configured for use with remote cluster)
func (a *App) getImageRegistry(ctx context.Context) (registry string, err error) {
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
	devRegistry, err := a.getImageRegistry(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get image registry")
	}

	imageSource := "local"
	registry := devRegistry
	if !a.Local {
		imageSource = "remote"
		registry = a.box.DeveloperEnvironmentConfig.ImageRegistry
	}

	binPath, err := os.Executable()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get devenv executable path")
	}

	vars := []string{
		fmt.Sprintf("DEPLOY_TO_DEV_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_BIN=%s", binPath),
		fmt.Sprintf("DEVENV_VERSION=%s", app.Version),
		fmt.Sprintf("DEVENV_DEPLOY_VERSION=%s", a.Version),
		fmt.Sprintf("DEVENV_DEPLOY_IMAGE_SOURCE=%s", imageSource),
		fmt.Sprintf("DEVENV_DEPLOY_IMAGE_REGISTRY=%s", registry),
		fmt.Sprintf("DEVENV_DEPLOY_DEV_IMAGE_REGISTRY=%s", devRegistry),
		fmt.Sprintf("DEVENV_DEPLOY_BOX_IMAGE_REGISTRY=%s", a.box.DeveloperEnvironmentConfig.ImageRegistry),
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

	devspace, err := ensureDevspace(a.log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to ensure devspace is installed")
	}
	vars = append(vars, fmt.Sprintf("DEVENV_DEVSPACE_BIN=%s", devspace))

	return vars, nil
}

// commandEnvLegacyOverrides returns the environment variables that should be set for legacy (pre-devspace) commands
func (a *App) commandEnvLegacyOverrides() []string {
	vars := []string{
		fmt.Sprintf("DEVENV_DEPLOY_IMAGE_REGISTRY=%s", a.box.DeveloperEnvironmentConfig.ImageRegistry),
		fmt.Sprintf("DEVENV_DEPLOY_DEV_IMAGE_REGISTRY=%s", a.box.DeveloperEnvironmentConfig.ImageRegistry),
		fmt.Sprintf("DEVENV_DEPLOY_BOX_IMAGE_REGISTRY=%s", a.box.DeveloperEnvironmentConfig.ImageRegistry),
	}
	return vars
}

// commandBuilderOptions contains options for creating exec.Cmd to run either a devspace or fallback command
type commandBuilderOptions struct {
	environmentVariabes []string

	// this config top level key has to be defined in devspace.yaml
	requiredConfig string

	// args to pass to the devspace command
	devspaceArgs []string

	// If one of these exists, we don't invoke devspace.
	fallbackCommandPaths []string

	// args to pass to the fallback command
	fallbackCommandArgs []string
}

// command returns the exec.Cmd to run the devspace (or fallback) command
func (a *App) command(ctx context.Context, opts *commandBuilderOptions) (*exec.Cmd, error) {
	// We can grab the env vars here, we'll need them in almost every case.
	vars, err := a.commandEnv(ctx)
	if err != nil {
		return nil, err
	}
	vars = append(vars, opts.environmentVariabes...)

	cmd, err := a.overrideCommand(ctx, opts, vars)
	if err != nil {
		return nil, err
	}
	if cmd != nil {
		return cmd, nil
	}

	cmd, err = a.devspaceCommand(ctx, opts, vars)
	if err != nil {
		return nil, err
	}
	if cmd != nil {
		return cmd, nil
	}

	return nil, fmt.Errorf("no fallback script or devspace.yaml found for the application")
}

// overrideCommand returns the exec.Cmd to run the override script
func (a *App) overrideCommand(ctx context.Context, opts *commandBuilderOptions, vars []string) (*exec.Cmd, error) {
	// We check whether there's an override script for the deployment.
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

	return nil, nil
}

// devspaceCommand returns the exec.Cmd to run the devspace command
func (a *App) devspaceCommand(ctx context.Context, opts *commandBuilderOptions, vars []string) (*exec.Cmd, error) {
	// We check whether there's a devspace.yaml file in the repository.
	var devspaceYamlPath string
	if _, err := os.Stat(filepath.Join(a.Path, "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, "devspace.yaml")
	} else if _, err := os.Stat(filepath.Join(a.Path, ".bootstrap", "devspace.yaml")); err == nil {
		devspaceYamlPath = filepath.Join(a.Path, ".bootstrap", "devspace.yaml")
	}

	if devspaceYamlPath == "" {
		return nil, fmt.Errorf("no fallback script or devspace.yaml found for the application")
	}

	// We do have to make sure devspace CLI is installed.
	devspace, err := ensureDevspace(a.log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to ensure devspace is installed")
	}

	if err := a.clusterTypeSupported(ctx, devspace, devspaceYamlPath, vars); err != nil {
		return nil, err
	}

	if err := a.devspaceConfigured(ctx, opts, devspace, devspaceYamlPath, vars); err != nil {
		return nil, err
	}

	args := opts.devspaceArgs
	args = append(args, "--config", devspaceYamlPath)
	// We know ahead of time what namespace bootstrap apps deploy to. so we can use that.
	if a.Type == TypeBootstrap {
		args = append(args,
			"--namespace", fmt.Sprintf("%s--bento1a", a.RepositoryName),
			"--no-warn",
		)
	}

	a.log.Infof("Running %s devspace %s", strings.Join(vars, " "), strings.Join(args, " "))
	cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create devspace command")
	}
	cmd.Env = append(cmd.Env, vars...)

	return cmd, nil
}

// devspaceConfigured checks whether the devspace.yaml has the required config
func (a *App) devspaceConfigured(
	ctx context.Context, opts *commandBuilderOptions, devspace, devspaceYamlPath string, vars []string) error {
	// We check whether the devspace has requiredConfig configured.
	// We assume individual profiles don't add dev configs. If they do, this won't work.
	cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path, devspace, "print", "--skip-info", "--config", devspaceYamlPath)
	if err != nil {
		return errors.Wrap(err, "failed to create devspace print command")
	}
	cmd.Env = append(cmd.Env, vars...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Print(string(out))
		return errors.Wrap(err, "failed to run devspace print command")
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		fmt.Print(string(out))
		return errors.Wrap(err, "failed to parse devspace print output")
	}

	if _, ok := cfg[opts.requiredConfig]; !ok {
		return fmt.Errorf("devspace.yaml is missing required config %s", opts.requiredConfig)
	}

	return nil
}

// clusterTypeSupported checks whether devspace is configured to work with KiND clusters
func (a *App) clusterTypeSupported(ctx context.Context, devspaceBin, devspaceConfigPath string, envVars []string) error {
	if !a.Local || a.kr.Type != kubernetesruntime.RuntimeTypeLocal || a.Type != TypeBootstrap {
		return nil
	}

	// For KiND and devspace, a kind profile must be configured.
	// We can skip the check if app is not built locally.
	cmd, err := cmdutil.CreateKubernetesCommand(ctx, a.Path,
		devspaceBin, "list", "profiles", "--disable-profile-activation", "--config", devspaceConfigPath)
	if err != nil {
		return errors.Wrap(err, "failed to create devspace print command")
	}
	cmd.Env = append(cmd.Env, envVars...)

	// We cannot use print command here, because it strips the profiles.
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

	return nil
}
