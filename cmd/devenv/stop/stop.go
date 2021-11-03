package stop

import (
	"context"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	stopLongDesc = `
		Stop stops your developer environment. This includes your Kubernetes leader node, and the containers it created.
	`
	stopExample = `
		# Stop your running developer environment
		devenv stop
	`
)

type Options struct {
	log logrus.FieldLogger
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	return &Options{
		log: log,
	}, nil
}

func NewCmdStop(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "stop",
		Usage:       "Stop your running developer environment",
		Description: cmdutil.NewDescription(stopLongDesc, stopExample),
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

	kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}

	o.log.Info("Stopping Developer Environment ...")
	if err := kr.Stop(ctx); err != nil {
		return errors.Wrap(err, "failed to stop developer environment")
	}
	o.log.Info("Developer Environment stopped successfully")

	return nil
}
