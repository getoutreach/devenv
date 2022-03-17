// Package registry implements the registry devenv command
package registry

import (
	"context"
	"fmt"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	longDesc = `
		Registry provides tools for working with development docker registries if they are enabled
	`
)

// Options holds the options for the registry command
type Options struct {
	log logrus.FieldLogger
}

// NewOptions creates a new Options instance for the registry command
func NewOptions(log logrus.FieldLogger) (*Options, error) {
	return &Options{
		log: log,
	}, nil
}

// NewCmdRegistry creates a new command for the registry subcommand
func NewCmdRegistry(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "registry",
		Usage:       "Commands to interact with development docker image registries",
		Description: cmdutil.NewDescription(longDesc, ""),
		Flags:       []cli.Flag{},
		Subcommands: []*cli.Command{
			{
				Name:  "get",
				Usage: "Get your developer environment docker image registry instance",
				Description: cmdutil.NewDescription(
					"Returns the URL/path for the closest docker image registry, or the one configured. The path is used to namespace docker images.",
					"",
				),
				Action: func(c *cli.Context) error {
					o, err := NewOptions(log)
					if err != nil {
						return err
					}

					return o.RunGet(c.Context)
				},
			},
		},
	}
}

// getDevenvName returns the name of the devenv in case it's a loft dev environment
func (o *Options) getDevenvName(ctx context.Context, b *box.Config) (string, error) {
	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to load config")
	}

	var devenvName string
	if kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b); err == nil {
		krConf := kr.GetConfig()
		if krConf.Type != kubernetesruntime.RuntimeTypeRemote {
			return "", fmt.Errorf("This command is only supported for cloud devenvs")
		}
		if krConf.Name != "loft" {
			return "", fmt.Errorf("This command is only supported for loft environments (cloud devenvs)")
		}

		devenvName = kr.GetConfig().ClusterName
	} else {
		return "", errors.Wrap(err, "failed to lookup current devenv to validate is cloud/loft")
	}

	return devenvName, nil
}

// RunGet runs the get command
func (o *Options) RunGet(ctx context.Context) error { //nolint:funlen // Why: acceptable size atm
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	devenvName, err := o.getDevenvName(ctx, b)
	if err != nil {
		return err
	}

	imageRegistry, err := apps.DevImageRegistry(ctx, o.log, b, devenvName)
	if err != nil {
		return err
	}

	// print out the endpoint based on the templated path output + base endpoint
	// from box
	fmt.Println(imageRegistry)
	return nil
}
