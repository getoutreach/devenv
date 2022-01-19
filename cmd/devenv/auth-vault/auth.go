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
	"k8s.io/client-go/rest"
)

//nolint:gochecknoglobals
var (
	authVault = `
		auth-vault ensures Vault operator has valid credentials for creating/updating secrets.
	`
	authVaultExample = `
		# Reauthenticate Vault operator
		devenv auth-vault
	`
)

type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	App string
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	k, conf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return &Options{
		k:    k,
		conf: conf,
		log:  log,
	}, nil
}

func NewCmdAuthVault(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "auth-vault",
		Usage:       "Reauthenticate Vault operator",
		Description: cmdutil.NewDescription(authVault, authVaultExample),
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
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
