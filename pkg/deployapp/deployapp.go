package deployapp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/sshhelper"
	"github.com/getoutreach/gobox/pkg/trace"
	dockerparser "github.com/novln/docker-parser"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var validRepoReg = regexp.MustCompile(`^([A-Za-z_\-.])+$`)

var repoCachePath = filepath.Join(".outreach", ".cache", "dev-environment", "deploy-app-v2")

type Type string

const (
	TypeBootstrap Type = "bootstrap"
	TypeLegacy    Type = "legacy"

	DeleteJobAnnotation = "outreach.io/db-migration-delete"
)

type App struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// Type is the type of application this is
	Type Type

	// Path, if set, is the path that should be used to deploy this application
	// this will be used over the github repository
	Path string

	// Local is wether this app was downloaded or is local
	Local bool

	// RepositoryName is the repository name for this application
	RepositoryName string

	// Version is the version of this application that should be deployed.
	// This is only used if RepositoryName is set and being used. This has no
	// effect when Path is set.
	Version string
}

// Run deploys an application by name, to the devenv.
func Run(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, conf *rest.Config, appNameOrPath string) error {
	version := ""
	versionSplit := strings.SplitN(appNameOrPath, "@", 2)
	if len(versionSplit) == 2 {
		appNameOrPath = versionSplit[0]
		version = versionSplit[1]
	}

	// if not a valid Github repository name or is a current directory or lower directory reference, then
	// run as local
	app := &App{
		k:              k,
		conf:           conf,
		Version:        version,
		RepositoryName: appNameOrPath,
	}
	if !validRepoReg.MatchString(appNameOrPath) || appNameOrPath == "." || appNameOrPath == ".." {
		app.Path = appNameOrPath
		app.Local = true
		app.RepositoryName = filepath.Base(appNameOrPath)

		if version != "" {
			return fmt.Errorf("when deploying a local-app a version must not be set")
		}
	}

	fields := logrus.Fields{
		"app.name": app.RepositoryName,
	}
	if app.Version != "" {
		fields["app.version"] = app.Version
	}
	app.log = log.WithFields(fields)
	return app.Deploy(ctx)
}

//nolint:gocritic // Why: Not naming these seems fine.
func (a *App) downloadRepository(ctx context.Context, repo string) (func(), string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return func() {}, "", err
	}

	// on macOS we seem to lose contents of temp directories, so now we need to do this
	tempDir := filepath.Join(homeDir, repoCachePath, repo, time.Now().Format(time.RFC3339Nano))
	cleanupFn := func() {
		os.RemoveAll(tempDir)
	}
	err = os.MkdirAll(tempDir, 0755)
	if err != nil {
		return cleanupFn, tempDir, err
	}

	args := []string{"clone", "git@github.com:getoutreach/" + a.RepositoryName, tempDir}
	if a.Version != "" {
		args = append(args, "--branch", a.Version, "--depth", "1")
	}

	a.log.Info("Fetching Application")
	//nolint:gosec // Why: We're using git here because of it's ability to better handle mixed input
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(b))
		return cleanupFn, tempDir, err
	}

	cmd = exec.CommandContext(ctx, "git", "describe", "--tags")
	cmd.Dir = tempDir
	b, err = cmd.Output()
	if err == nil {
		ver := strings.TrimSpace(string(b))
		if ver != a.Version {
			a.log.WithField("app.version", ver).Info("Detected potential application version")
		}
	}
	return cleanupFn, tempDir, nil
}

// deployLegacy attempts to deploy an application by running
// the file at ./scripts/deploy-to-dev.sh
func (a *App) deployLegacy(ctx context.Context) error {
	return cmdutil.RunKubernetesCommand(ctx, a.Path, true, "./scripts/deploy-to-dev.sh", "update")
}

func (a *App) deployBootstrap(ctx context.Context) error { //nolint:funlen
	b, err := ioutil.ReadFile(filepath.Join(a.Path, "service.yaml"))
	if err != nil {
		return errors.Wrap(err, "failed to read service.yaml")
	}

	// conf is a partial of the configuration file used to bootstrap services
	// internally at outreach. The entirety of that repository used to carry
	// out that process will soon be open-sourced and the type of this variable
	// will be concrete.
	var conf struct {
		Name string `yaml:"name"`
	}

	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return errors.Wrap(err, "failed to parse service.yaml")
	}
	a.RepositoryName = conf.Name
	a.log = a.log.WithField("app.name", a.RepositoryName)

	// Only build a docker image if we're not using the latest version
	// or if we're in local mode
	builtDockerImage := false
	if a.Version != "" || a.Local {
		err = a.buildDockerImage(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to build image")
		}
		builtDockerImage = true
	}

	a.log.Info("Deploying Application into Kubernetes")

	deployScript := "./scripts/deploy-to-dev.sh"
	deployScriptArgs := []string{"update"}

	// Cheap way of detecting bootstrap v6 w/o importing bootstrap.lock
	if _, err := os.Stat(filepath.Join(a.Path, "scripts", "shell-wrapper.sh")); err == nil { //nolint:govet // Why: we're OK shadowing err
		deployScript = "./scripts/shell-wrapper.sh"
		deployScriptArgs = append([]string{"deploy-to-dev.sh"}, deployScriptArgs...)
	}

	err = cmdutil.RunKubernetesCommand(ctx, a.Path, true, deployScript, deployScriptArgs...)
	if err != nil {
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
	repositoryPath := a.Path

	// Download the repository.
	if repositoryPath == "" {
		cleanup, tempDir, err := a.downloadRepository(ctx, a.RepositoryName)
		defer cleanup()
		if err != nil {
			return err
		}
		repositoryPath = tempDir
	}

	// set a.Path to the repository path, this lets the deploy logic not need
	// to worry about the source code
	a.Path = repositoryPath

	serviceYamlPath := filepath.Join(a.Path, "service.yaml")
	deployScriptPath := filepath.Join(a.Path, "scripts", "deploy-to-dev.sh")
	if _, err := os.Stat(serviceYamlPath); err == nil {
		a.Type = TypeBootstrap
	} else if _, err := os.Stat(serviceYamlPath); err == nil {
		a.Type = TypeLegacy
	} else {
		return fmt.Errorf("failed to determine application type, no %s or %s", serviceYamlPath, deployScriptPath)
	}

	// Delete all jobs with a db-migration annotation. Namespaces aren't the same per type
	// so calculate them
	namespaces := make([]string, 0)
	switch a.Type {
	case TypeBootstrap, TypeLegacy:
		namespaces = append(namespaces, a.RepositoryName, a.RepositoryName+"--bento1a")
	}
	err := devenvutil.DeleteObjects(ctx, a.log, a.k, a.conf, devenvutil.DeleteObjectsObjects{
		Namespaces: namespaces,
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
		return a.deployBootstrap(ctx)
	case TypeLegacy:
		return a.deployLegacy(ctx)
	}

	return fmt.Errorf("unknown application type %s", a.Type)
}
