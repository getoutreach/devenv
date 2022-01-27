package top

import (
	"context"
	"os"
	"os/exec"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// !!!
// !!! NOTE: This is deprecated. It doesn't work for all types of devenv.
// !!!

//nolint:gochecknoglobals
var (
	topLongDesc = `
	Htop for devenv
	`
	topExample = `
		# Htop for devenv
		devenv top
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

func NewCmdTop(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "top",
		Usage:       "DEPRECATED: htop for devenv",
		Hidden:      true,
		Description: cmdutil.NewDescription(topLongDesc, topExample),
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

func (o *Options) runContainerTop(ctx context.Context) error {
	args := []string{"run", "--pid=host", "--rm", "-it", "alpine",
		"sh", "-c", "apk add --no-cache htop; htop"}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (o *Options) Run(ctx context.Context) error {
	return o.runContainerTop(ctx)
}
