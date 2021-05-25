package stop

import (
	"context"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/worker"
	olog "github.com/getoutreach/gobox/pkg/log"
	"github.com/getoutreach/gobox/pkg/trace"
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

	d dockerclient.APIClient
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	return &Options{
		log: log,
		d:   d,
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

func (o *Options) StopContainers(ctx context.Context, containers []string) error {
	ctx = trace.StartCall(ctx, "stop.RemoveContainers")
	defer trace.EndCall(ctx)

	containersInf := make([]interface{}, len(containers))
	for i, cont := range containers {
		containersInf[i] = cont
	}

	timeout := time.Duration(0)
	_, err := worker.ProcessArray(ctx, containersInf, func(ctx context.Context, data interface{}) (interface{}, error) {
		cont := data.(string)
		ctx = trace.StartCall(ctx, "docker.ContainerStop", olog.F{"container": cont})
		defer trace.EndCall(ctx)

		err := o.d.ContainerStop(ctx, cont, &timeout)
		if err != nil && !dockerclient.IsErrNotFound(err) {
			err = trace.SetCallStatus(ctx, err)
			return nil, err
		}

		return nil, nil
	})

	return err
}

func (o *Options) Run(ctx context.Context) error {
	o.log.Info("Stopping Developer Environment ...")
	err := o.StopContainers(ctx, []string{
		"k3s",
		containerruntime.ContainerName,

		// older containers
		"proxy",
		"proxy-http",
		"proxy-https",

		// new proxy containers
		"proxy-6443",
		"proxy-443",
		"proxy-80",
	})
	if err != nil {
		return err
	}

	o.log.Info("Developer Environment stopped successfully")

	return nil
}
