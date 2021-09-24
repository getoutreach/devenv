package context

import (
	gocontext "context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
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

func NewCmdContext(log logrus.FieldLogger) *cli.Command {
	o := NewOptions(log)

	return &cli.Command{
		Name:            "context",
		Aliases:         []string{"c"},
		Usage:           "Change which devenv you're currently using",
		SkipFlagParsing: true,
		Action: func(c *cli.Context) error {
			o.Args = c.Args().Slice()
			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx gocontext.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	runtimes := kubernetesruntime.GetEnabledRuntimes(b)

	clusters := make([]*kubernetesruntime.RuntimeCluster, 0)
	for _, r := range runtimes {
		r.Configure(o.log, b)
		if err := r.PreCreate(ctx); err != nil {
			o.log.WithError(err).Warnf("Failed to setup runtime %s, skipping", r.GetConfig().Name)
			continue
		}

		newClusters, err := r.GetClusters(ctx)
		if err != nil {
			o.log.WithError(err).Warnf("Failed to get clusters from runtime %s, skipping", r.GetConfig().Name)
			continue
		}

		clusters = append(clusters, newClusters...)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tRUNTIME")

	for _, c := range clusters {
		fmt.Fprintln(w, c.Name+"\t"+c.RuntimeName)
	}

	w.Flush()

	return nil
}
