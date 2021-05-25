package kubectl

import (
	"context"
	"math/rand"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/component-base/logs"
	"k8s.io/kubectl/pkg/cmd"
)

type Options struct {
	log logrus.FieldLogger

	Args []string
}

func NewOptions(log logrus.FieldLogger) *Options {
	return &Options{
		log: log,
	}
}

func NewCmdKubectl(log logrus.FieldLogger) *cli.Command {
	o := NewOptions(log)

	return &cli.Command{
		Name:            "kubectl",
		Aliases:         []string{"k"},
		Usage:           "Run kubectl commands in your local developer environment",
		SkipFlagParsing: true,
		Action: func(c *cli.Context) error {
			o.Args = c.Args().Slice()
			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
	rand.Seed(time.Now().UnixNano())

	command := cmd.NewDefaultKubectlCommand()
	command.SetArgs(append([]string{"--context", "dev-environment"}, os.Args[2:]...))

	logs.InitLogs()
	defer logs.FlushLogs()

	return command.ExecuteContext(ctx)
}
