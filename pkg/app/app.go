package app

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

	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
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
	kr   *kubernetesruntime.RuntimeConfig

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

func NewApp(log logrus.FieldLogger, k kubernetes.Interface, conf *rest.Config, appNameOrPath string, kr *kubernetesruntime.RuntimeConfig) (*App, error) {
	version := ""
	versionSplit := strings.SplitN(appNameOrPath, "@", 2)

	if len(versionSplit) == 2 {
		appNameOrPath = versionSplit[0]
		version = versionSplit[1]
	}

	// if not a valid Github repository name or is a current directory or lower directory reference, then
	// run as local
	app := App{
		k:              k,
		conf:           conf,
		kr:             kr,
		Version:        version,
		RepositoryName: appNameOrPath,
	}

	if !validRepoReg.MatchString(appNameOrPath) || appNameOrPath == "." || appNameOrPath == ".." {
		app.Path = appNameOrPath
		app.Local = true
		app.RepositoryName = filepath.Base(appNameOrPath)

		if version != "" {
			return nil, fmt.Errorf("when deploying a local-app a version must not be set")
		}
	}

	fields := logrus.Fields{
		"app.name": app.RepositoryName,
	}
	if app.Version != "" {
		fields["app.version"] = app.Version
	}
	app.log = log.WithFields(fields)

	return &app, nil
}

func (a *App) downloadRepository(ctx context.Context, repo string) (cleanup func(), err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return func() {}, err
	}

	// on macOS we seem to lose contents of temp directories, so now we need to do this
	tempDir := filepath.Join(homeDir, repoCachePath, repo, time.Now().Format(time.RFC3339Nano))
	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil { //nolint:govet // Why: We're okay with shadowing the error.
		return cleanup, err
	}

	args := []string{"clone", "git@github.com:getoutreach/" + a.RepositoryName, tempDir}
	if a.Version != "" {
		args = append(args, "--branch", a.Version, "--depth", "1")
	}

	a.log.Info("Fetching Application")

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // Why: We're using git here because of it's ability to better handle mixed input
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(b))
		return cleanup, err
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

	// Set the path of the app to the downloaded repository in the temporary directory.
	a.Path = tempDir

	return cleanup, nil
}

func (a *App) determineType() error {
	serviceYamlPath := filepath.Join(a.Path, "service.yaml")
	deployScriptPath := filepath.Join(a.Path, "scripts", "deploy-to-dev.sh")

	if _, err := os.Stat(serviceYamlPath); err == nil {
		a.Type = TypeBootstrap
	} else if _, err := os.Stat(deployScriptPath); err == nil {
		a.Type = TypeLegacy
	} else {
		return fmt.Errorf("failed to determine application type, no %s or %s", serviceYamlPath, deployScriptPath)
	}

	return nil
}

func (a *App) determineRepositoryName() error {
	if a.Type != TypeBootstrap {
		if a.Path != "" && a.Path != "." && a.Path != ".." && a.Path != "../" {
			a.RepositoryName = filepath.Base(a.Path)
			return nil
		}

		return errors.New("could not determine repository name")
	}

	b, err := ioutil.ReadFile(filepath.Join(a.Path, "service.yaml"))
	if err != nil {
		return errors.Wrap(err, "failed to read service.yaml")
	}

	// conf is a partial of the configuration file for services configured with
	// bootstrap (stencil).
	var conf struct {
		Name string `yaml:"name"`
	}

	if err = yaml.Unmarshal(b, &conf); err != nil {
		return errors.Wrap(err, "failed to parse service.yaml")
	}

	a.RepositoryName = conf.Name
	return nil
}
