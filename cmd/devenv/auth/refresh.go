package auth

import (
	"context"

	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
)

//nolint:gochecknoglobals
var (
	refresh = `
		Refresh ensures Vault operator has valid credentials for creating/updating secrets.
	`
	refreshExample = `
		# Reauthenticate Vault operator
		devenv auth refresh
	`
)

// RefreshOptions the options for the auth refresh command.
type RefreshOptions struct {
	log logrus.FieldLogger
	k   kubernetes.Interface

	App string
}

// NewRefreshOptions creates a new instance of the auth refresh options.
func NewRefreshOptions(log logrus.FieldLogger) (*RefreshOptions, error) {
	k, _, err := kube.GetKubeClientWithConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return &RefreshOptions{
		log: log,
		k:   k,
	}, nil
}

// NewCmdAuthRefresh returns a new instance of the auth refresh command.
func NewCmdAuthRefresh(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "refresh",
		Usage:       "Refresh Vault token used by devenv vault operator",
		Description: cmdutil.NewDescription(refresh, refreshExample),
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			o, err := NewRefreshOptions(log)
			if err != nil {
				return err
			}

			return o.Run(c.Context)
		},
	}
}

// Run executes the auth refresh command.
func (o *RefreshOptions) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	_, err = devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	return nil
}
