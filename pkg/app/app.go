package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/google/go-github/v42/github"
	"golang.org/x/oauth2"

	githubauth "github.com/getoutreach/gobox/pkg/cli/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// IDEA(pelisy): Do we want to redo Type? Given we support more than 1 fallback/override script kinda invalidates it.

var validRepoReg = regexp.MustCompile(`^([A-Za-z_\-.])+$`)

var repoCachePath = filepath.Join(".outreach", ".cache", "dev-environment", "deploy-app-v2")

type Type string

const (
	TypeBootstrap Type = "bootstrap"
	TypeDevspace  Type = "devspace"
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
		app.Version = "local"
		app.RepositoryName = filepath.Base(appNameOrPath)

		if !filepath.IsAbs(app.Path) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, errors.Wrap(err, "failed to get current working dir")
			}

			app.Path, err = filepath.Abs(filepath.Join(cwd, appNameOrPath))
			if err != nil {
				return nil, errors.Wrap(err, "failed to convert relative app path to absolute")
			}
		}

		if version != "" {
			return nil, fmt.Errorf("when deploying a local-app a version must not be set")
		}
	}

	// Handle local applications
	if app.Local {
		if err := app.determineTypeLocal(ctx); err != nil {
			return nil, errors.Wrap(err, "determine repository type")
		}

		// we can check the name early when local
		if err := app.determineRepositoryName(); err != nil {
			return nil, errors.Wrap(err, "determine repository name")
		}

		app.log = log.WithField("app.name", app.RepositoryName).
			WithField("app.type", app.Type)

		return &app, nil
	}

	// Remote applications logic here
	if err := app.determineTypeRemote(ctx); err != nil {
		return nil, errors.Wrap(err, "determine repository type")
	}

	app.log = log.WithField("app.name", app.RepositoryName).
		WithField("app.type", app.Type)

	// Find the latest version if not set, or resolve the provided version
	if app.Version == "" {
		if err := app.detectVersion(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to determine application version")
		}
	} else {
		// If we had a provided version, attempt to
		// resolve the version in case it's a branch
		if err := app.resolveVersion(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to resolve application version")
		}
	}
	app.log = app.log.WithField("app.version", app.Version)

	// Download the repository if it doesn't already exist on disk.
	if app.Path == "" {
		cleanup, err := app.downloadRepository(ctx, app.RepositoryName)
		app.cleanupFn = cleanup
		if err != nil {
			return nil, err
		}
	}

	if err := app.determineRepositoryName(); err != nil {
		return nil, errors.Wrap(err, "determine repository name")
	}

	return &app, nil
}

// detectVersion determines the latest version of a repository by three criteria
// - topic "release-type-commits" is set. Uses the latest commit
// - 0 tags on the repository. Uses the latest commit.
// - Otherwise it uses the latest release (tag) on the repository
func (a *App) detectVersion(ctx context.Context) error {
	token, err := githubauth.GetToken()
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

	// Note: This doesn't resolve the latest _tag_ but the latest Github release. This is a
	// requirement for us for now. (no tags w/o releases)
	rel, _, err := gh.Repositories.GetLatestRelease(ctx, a.box.Org, a.RepositoryName)
	if err != nil {
		return errors.Wrapf(err, "failed to lookup repository %s/%s", a.box.Org, a.RepositoryName)
	}

	a.Version = *rel.TagName
	return nil
}

// resolveVersion attempts to determine if a version is a tag or branch
func (a *App) resolveVersion(ctx context.Context) error {
	token, err := githubauth.GetToken()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve github token")
	}

	gh := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: string(token)},
	)))

	// check if it's a valid tag
	_, _, err = gh.Git.GetRef(ctx, a.box.Org, a.RepositoryName, "tags/"+a.Version)
	if err == nil {
		return nil
	}

	// if the version wasn't a tag and we're a bootstrapped service we're sad.
	if a.Type == TypeBootstrap {
		return fmt.Errorf("Pointing at a commit/branch for bootstrap services is unsupported, use a tag instead")
	}

	// check if it's a valid commit
	_, _, err = gh.Git.GetCommit(ctx, a.box.Org, a.RepositoryName, a.Version)
	if err == nil {
		return nil
	}

	// lookup branch
	br, _, err := gh.Repositories.GetBranch(ctx, a.box.Org, a.RepositoryName, a.Version, true)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve the latest commit on branch %s/%s@%s", a.box.Org, a.RepositoryName, a.Version)
	}
	a.Version = br.GetCommit().GetSHA()

	a.log.Warn("Pointing at a branch doesn't currently use the version of the application, use devspace instead")

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

	if err := os.MkdirAll(tempDir, 0o755); err != nil { //nolint:govet // Why: We're okay with shadowing the error.
		return cleanup, err
	}

	a.log.Info("Fetching application")

	//nolint:gosec // Why: On purpose
	cmd := exec.CommandContext(ctx, "git", "clone", fmt.Sprintf("git@github.com:%s/%s", a.box.Org, a.RepositoryName), tempDir)
	if b, err := cmd.CombinedOutput(); err != nil {
		return cleanup, errors.Wrapf(err, "failed to shallow clone repository: %s", string(b))
	}

	//nolint:gosec // Why: On purpose
	cmd = exec.CommandContext(ctx, "git", "checkout", a.Version)
	cmd.Dir = tempDir
	if b, err := cmd.CombinedOutput(); err != nil {
		return cleanup, errors.Wrapf(err, "failed to checkout given ref: %s", string(b))
	}

	return cleanup, nil
}

// determineTypeLocal determines the type of a local application
func (a *App) determineTypeLocal(_ context.Context) error {
	fileExists := func(path string) bool {
		parts := strings.Split(path, "/")
		_, err := os.Stat(filepath.Join(parts...))
		return err == nil
	}

	return a.determineType(fileExists)
}

// determineType determines the type of repository a service is
func (a *App) determineTypeRemote(ctx context.Context) error {
	token, err := githubauth.GetToken()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve github token")
	}

	gh := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: string(token)},
	)))

	if _, _, err := gh.Repositories.Get(ctx, a.box.Org, a.RepositoryName); err != nil {
		return errors.Wrap(err, "failed to check if repository exists")
	}

	fileExists := func(path string) bool {
		versionOpts := &github.RepositoryContentGetOptions{
			Ref: a.Version,
		}
		_, _, _, err = gh.Repositories.GetContents(ctx, a.box.Org, a.RepositoryName, path, versionOpts)

		return err == nil
	}

	return a.determineType(fileExists)
}

func (a *App) determineType(fileExists func(string) bool) error {
	if fileExists("bootstrap.lock") {
		// All bootstrap services are set up for use with devspace but there are more rules
		// applicable just to bootstrap services.
		a.Type = TypeBootstrap
		return nil
	}

	if fileExists("scripts/deploy-to-dev.sh") {
		a.Type = TypeLegacy
		return nil
	}
	if fileExists("scripts/devenv-apps-dev.sh") {
		a.Type = TypeLegacy
		return nil
	}

	return fmt.Errorf(
		"%s doesn't appear to support being deployed via the devenv, please contact application owners",
		a.RepositoryName,
	)
}

// determineRepository name determines a repositories name from the path
// or from a service.yaml
func (a *App) determineRepositoryName() error {
	// Determine the name via the basename for legacy applications
	if a.Type != TypeBootstrap {
		if !a.Local { // If not local, use the provided name
			return nil
		}

		a.RepositoryName = filepath.Base(a.Path)
		return nil
	}

	// read the repository's service.yaml for bootstrap applications
	b, err := os.ReadFile(filepath.Join(a.Path, "service.yaml"))
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
