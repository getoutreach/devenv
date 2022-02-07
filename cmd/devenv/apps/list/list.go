package list

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/getoutreach/devenv/internal/apps"
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
	listLongDesc = `
		Lists all currently deployed applications in your devenv
	`
	listExample = `
		# List all applications in your devenv
		devenv apps list

		# Return list of apps in json
		devenv apps list --output json
		devenv apps list -o json # Note: the space is required
	`
)

// Options are various options for the `apps deploy` command
type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	conf *rest.Config

	// Format is the foramt to output in
	// table or json
	Format string
}

// NewOptions create an initialized options struct for the `apps list` command
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

// NewCmd creates a new cli.Command for the `apps list` command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all deployed applications in your devenv",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Change the output format, valid options are: table, json",
				Value:   "table",
			},
		},
		Description: cmdutil.NewDescription(listLongDesc, listExample),
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}
			o.Format = c.String("output")
			return o.Run(c.Context)
		},
	}
}

// Run runs the `apps list` command
func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	if _, err := devenvutil.EnsureDevenvRunning(ctx, conf, b); err != nil {
		return err
	}

	appsClient := apps.NewKubernetesConfigmapClient(o.k, "")
	deployedApps, err := appsClient.List(ctx)
	if err != nil {
		return err
	}

	// alphabetical sort
	sort.Slice(deployedApps, func(i, j int) bool {
		return deployedApps[i].Name < deployedApps[j].Name
	})

	if o.Format == "table" {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "APP\tVERSION")
		for _, a := range deployedApps {
			fmt.Fprintln(w, a.Name+"\t"+a.Version)
		}
		return w.Flush()
	} else if o.Format == "json" {
		return json.NewEncoder(os.Stdout).Encode(deployedApps)
	}

	return fmt.Errorf("invalid format %s", o.Format)
}
