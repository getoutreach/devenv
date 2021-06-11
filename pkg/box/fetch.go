package box

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/getoutreach/gobox/pkg/sshhelper"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var (
	BoxConfigPath = ".outreach/.config/box"
	BoxConfigFile = "box.yaml"
)

func getBoxPath() (string, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get user homedir")
	}

	return filepath.Join(homedir, BoxConfigPath, BoxConfigFile), nil
}

// LoadBox loads the default box or returns an error
func LoadBox() (*Config, error) {
	s, err := LoadBoxStorage()
	if err != nil {
		return nil, err
	}

	ApplyEnvOverrides(s.Config)

	return s.Config, nil
}

// ApplyEnvOverrides overrides a box configuration based on env vars.
// This should really only be used for things that need to be overridden
// on runtime, e.g. CI
func ApplyEnvOverrides(s *Config) {
	if vaultAddr := os.Getenv("VAULT_ADDR"); vaultAddr != "" {
		s.DeveloperEnvironmentConfig.VaultConfig.Address = vaultAddr
	}
}

func LoadBoxStorage() (*Storage, error) {
	confPath, err := getBoxPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(confPath)
	if err != nil {
		return nil, err
	}

	var s *Storage
	err = yaml.NewDecoder(f).Decode(&s)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// EnsureBox loads a box if it already exists, or prompts the user for the box
// if not found. If it exists, remote is querired periodically for a new version
func EnsureBox(ctx context.Context, defaults []string, log logrus.FieldLogger) (*Config, error) {
	s, err := LoadBoxStorage()
	if os.IsNotExist(err) {
		err = InitializeBox(ctx, defaults)
		if err != nil {
			return nil, err
		}

		return LoadBox()
	} else if err != nil {
		return nil, err
	}

	diff := time.Now().UTC().Sub(s.LastUpdated)
	if diff < (30 * time.Minute) { // if last updated wasn't time interval, skip update
		return s.Config, nil
	}

	log.Info("Refreshing box configuration")
	// past the time interval, refresh the config
	c, err := downloadBox(ctx, s.StorageURL)
	if err != nil {
		return nil, err
	}

	s.Config = c

	err = saveBox(ctx, s)
	if err != nil {
		return nil, err
	}

	return s.Config, nil
}

func downloadBox(ctx context.Context, gitRepo string) (*Config, error) {
	a := sshhelper.GetSSHAgent()

	//nolint:errcheck // Why: Best effort and not worth bringing logger here
	_, err := sshhelper.LoadDefaultKey("github.com", a, &logrus.Logger{Out: io.Discard})
	if err != nil {
		return nil, errors.Wrap(err, "failed to load Github SSH key into in-memory keyring")
	}

	fs := memfs.New()
	_, err = git.CloneContext(ctx, memory.NewStorage(), fs, &git.CloneOptions{
		URL:   gitRepo,
		Auth:  sshhelper.NewExistingSSHAgentCallback(a),
		Depth: 1,
	})
	if err != nil {
		return nil, err
	}

	f, err := fs.Open(BoxConfigFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read box configuration file")
	}

	var c *Config
	return c, yaml.NewDecoder(f).Decode(&c) //nolint:gocritic
}

func saveBox(_ context.Context, s *Storage) error {
	s.LastUpdated = time.Now().UTC()

	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}

	confPath, err := getBoxPath()
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(confPath), 0755)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(confPath, b, 0600)
}

func InitializeBox(ctx context.Context, defaults []string) error {
	gitRepo := ""

	// TODO: This doesn't allow a user to put in their own input
	// but that can be fixed later
	err := survey.AskOne(&survey.Select{
		Message: "Please enter your box configuration location (default is for Outreach)",
		Help:    "This is the repository that contains your box.yaml and will be used for devenv configuration. Provided is the default for Outreach",
		Options: defaults,
	}, &gitRepo)
	if err != nil {
		return err
	}

	conf, err := downloadBox(ctx, gitRepo)
	if err != nil {
		return err
	}

	return saveBox(ctx, &Storage{
		StorageURL: gitRepo,
		Config:     conf,
	})
}
