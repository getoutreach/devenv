package app

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/google/go-github/v42/github"
	"golang.org/x/oauth2"
	"golang.org/x/tools/go/vcs"

	// TODO(jaredallard): Move this into gobox
	githubauth "github.com/getoutreach/stencil/pkg/extensions/github"
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
	log        logrus.FieldLogger
	k          kubernetes.Interface
	appsClient apps.Interface
	conf       *rest.Config
	box        *box.Config
	kr         *kubernetesruntime.RuntimeConfig

	// cleanupFn is called to cleanup the downloaded files, if applicable
	cleanupFn func()

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

// NewApp creates a new App for interaction with in a devenv
func NewApp(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, b *box.Config, conf *rest.Config,
	appNameOrPath string, kr *kubernetesruntime.RuntimeConfig) (*App, error) {
	version := ""
	versionSplit := strings.SplitN(appNameOrPath, "@", 2)

	if len(versionSplit) == 2 {
		appNameOrPath = versionSplit[0]
		version = versionSplit[1]
	}

	app := App{
		k:              k,
		box:            b,
		appsClient:     apps.NewKubernetesConfigmapClient(k, ""),
		conf:           conf,
		kr:             kr,
		Version:        version,
		RepositoryName: appNameOrPath,
	}

	// if not a valid Github repository name or is a current directory or lower directory reference, then
	// run as local
	if !validRepoReg.MatchString(appNameOrPath) || appNameOrPath == "." || appNameOrPath == ".." {
		app.Path = appNameOrPath
		app.Local = true
		app.RepositoryName = filepath.Base(appNameOrPath)

		if version != "" {
			return nil, fmt.Errorf("when deploying a local-app a version must not be set")
		}

		if err := app.determineRepositoryName(); err != nil {
			return nil, errors.Wrap(err, "determine repository name")
		}
	}
	app.log = log.WithField("app.name", app.RepositoryName)

	// Download the repository if it doesn't already exist on disk.
	if app.Path == "" {
		cleanup, err := app.downloadRepository(ctx, app.RepositoryName)
		app.cleanupFn = cleanup
		if err != nil {
			return nil, err
		}
	}

	if app.Version == "" {
		if err := app.detectVersion(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to determine application version")
		}
	}

	if err := app.determineType(); err != nil {
		return nil, errors.Wrap(err, "determine repository type")
	}

	if err := app.determineRepositoryName(); err != nil {
		return nil, errors.Wrap(err, "determine repository name")
	}

	app.log = app.log.WithField("app.version", app.Version)

	return &app, nil
}

// detectVersion determines the latest version of a repository by three criteria"
// - topic "release-type-commits" is set. Uses the latest commit
// - 0 tags on the repository. Uses the latest commit.
// - Otherwise it uses the latest release (tag) on the repository
func (a *App) detectVersion(ctx context.Context) error {
	token, err := githubauth.GetGHToken()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve github token")
	}

	gh := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: string(token)},
	)))

	repo, _, err := gh.Repositories.Get(ctx, a.box.Org, a.RepositoryName)
	if err != nil {
		return errors.Wrapf(err, "failed to lookup repository %s/%s", a.box.Org, a.RepositoryName)
	}

	useCommit := false
	for _, topic := range repo.Topics {
		if topic == "release-type-commits" {
			useCommit = true
			break
		}
	}

	tags, _, err := gh.Repositories.ListTags(ctx, a.box.Org, a.RepositoryName, &github.ListOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to list tags on repository %s/%s", a.box.Org, a.RepositoryName)
	}
	if len(tags) == 0 {
		useCommit = true
	}

	if useCommit {
		if repo.DefaultBranch == nil || *repo.DefaultBranch == "" {
			repo.DefaultBranch = github.String("master")
		}

		br, _, err := gh.Repositories.GetBranch(ctx, a.box.Org, a.RepositoryName, *repo.DefaultBranch, true)
		if err != nil {
			return errors.Wrapf(err, "failed to resolve the latest commit on repository %s/%s@%s", a.box.Org, a.RepositoryName, *repo.DefaultBranch)
		}

		a.Version = br.GetCommit().GetSHA()
		return nil
	}

	rel, _, err := gh.Repositories.GetLatestRelease(ctx, a.box.Org, a.RepositoryName)
	if err != nil {
		return errors.Wrapf(err, "failed to lookup repository %s/%s", a.box.Org, a.RepositoryName)
	}

	a.Version = *rel.TagName
	return nil
}

// downloadRepository downloads the repository of our application
func (a *App) downloadRepository(ctx context.Context, repo string) (cleanup func(), err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return func() {}, err
	}

	// on macOS we seem to lose contents of temp directories, so now we need to do this
	tempDir := filepath.Join(homeDir, repoCachePath, repo, time.Now().Format(time.RFC3339Nano))
	// Set the path of the app to the downloaded repository in the temporary directory.
	a.Path = tempDir
	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil { //nolint:govet // Why: We're okay with shadowing the error.
		return cleanup, err
	}

	if a.Version == "" {
		if err := a.detectVersion(ctx); err != nil {
			return cleanup, errors.Wrapf(err, "failed to find latest version of %s", a.RepositoryName)
		}
	}

	root, err := vcs.RepoRootForImportPath("github.com/"+path.Join(a.box.Org, a.RepositoryName), false)
	if err != nil {
		return cleanup, errors.Wrap(err, "failed to setup vcs client")
	}

	a.log.Info("Fetching Application")
	return cleanup, root.VCS.CreateAtRev(tempDir, root.Repo, a.Version)
}

// determineType determines the type of repository a service is
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

// determineRepository name determines a repositories name from the path
// or from a service.yaml
func (a *App) determineRepositoryName() error {
	// Determine the name via the basename for legacy applications
	if a.Type != TypeBootstrap {
		if filepath.IsAbs(a.Path) {
			a.RepositoryName = filepath.Base(a.Path)
			return nil
		}

		// can't resolve names of relative paths at this point
		return fmt.Errorf("failed to resolve application's name")
	}

	// ready the repository's service.yaml for bootstrap applications
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

// Close cleans up all resources of this application
// outside of the application itself.
func (a *App) Close() error {
	if a.cleanupFn != nil {
		a.cleanupFn()
	}

	return nil
}
