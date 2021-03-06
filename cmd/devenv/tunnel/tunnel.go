package tunnel

import (
	"context"
	"time"

	localapp "github.com/getoutreach/devenv/cmd/devenv/local-app"
	"github.com/getoutreach/devenv/internal/alert"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetestunnelruntime"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/getoutreach/gobox/pkg/box"
	localizerapi "github.com/getoutreach/localizer/api"
	"github.com/getoutreach/localizer/pkg/localizer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//nolint:gochecknoglobals
var (
	tunnelLongDesc = `
		Tunnel uses localizer to create port-forwards into your Kubernetes cluster on your local-machine.

		These tunnels are then hooked up to DNS via your /etc/hosts file, 
		which points to aliased ip addresses in the 127.0.0.1/8 space.
	`
	tunnelExample = `
		# Grant local access to Kubernetes Services running inside of the 
		# developer environment
		devenv tunnel
	`
)

type Options struct {
	log logrus.FieldLogger

	LocalApps []string
}

func NewOptions(log logrus.FieldLogger) *Options {
	return &Options{
		log: log,
	}
}

func NewCmdTunnel(log logrus.FieldLogger) *cli.Command {
	o := NewOptions(log)

	return &cli.Command{
		Name:        "tunnel",
		Usage:       "Grant local access to Kubernetes Services",
		Description: cmdutil.NewDescription(tunnelLongDesc, tunnelExample),
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "local-app",
				Usage: "Specify an application to run through local-app",
			},
		},
		Action: func(c *cli.Context) error {
			cmdutil.CLIStringSliceToStringSlice(c.StringSlice("local-app"), &o.LocalApps)
			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error { //nolint:funlen
	p, err := kubernetestunnelruntime.EnsureLocalizer(o.log)
	if err != nil {
		return err
	}

	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	if _, err2 := devenvutil.EnsureDevenvRunning(ctx, conf, b); err2 != nil {
		return err2
	}

	done := make(chan struct{})
	go func(ctx context.Context) {
		// Sleep for 20 seconds the first time before checking to alert for permissions.
		async.Sleep(ctx, time.Second*20)

		for ctx.Err() == nil {
			alert.Alert("Your tunnel needs elevated permissions.")

			select {
			case <-ctx.Done():
			case <-done:
				return
			case <-time.After(time.Second * 60):
				// Sleep for 60 seconds between each alert after the first one.
				continue
			}
		}
	}(ctx)

	// Preemptively ask for sudo to prevent input mangaling with o.LocalApps
	o.log.Info("You may get a sudo prompt, this is so localizer can create tunnels")
	err = cmdutil.RunKubernetesCommand(ctx, "", true, "sudo", "echo", "Hello, world!")
	if err != nil {
		return errors.Wrap(err, "failed to get sudo")
	}
	close(done)

	if localizer.IsRunning() {
		// We block on the connection, so only try for 2 seconds before moving on. This should
		// be fine if localizer is actually running because its communicating over the local
		// network.
		gCtx, cancel := context.WithTimeout(ctx, time.Second*2)

		client, closer, err := localizer.Connect(gCtx,
			grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())) //nolint:govet // Why: It's okay to shadow the error here.
		if err != nil {
			cancel()
			//nolint:lll // Why: Not much we can do here
			o.log.Info("detected localizer socket, but could not connect to localizer. try the following and then rerun:\n\tsudo kill $(pgrep localizer)\n\tsudo rm -f /var/run/localizer.sock")
			return errors.Wrap(err, "connect to localizer client to kill stale connection")
		}

		defer closer()
		cancel()

		if _, err := client.Kill(ctx, &localizerapi.Empty{}); err != nil {
			return errors.Wrap(err, "kill stale localizer connectin")
		}

		// Wait for that stale connection to actually be gone before continuing.
		for ctx.Err() == nil && localizer.IsRunning() {
			async.Sleep(ctx, time.Second*1)
		}
	}

	localizerErrCh := make(chan error)
	go func() {
		// sudo hacks, -E here is just "forward environment"
		localizerErrCh <- cmdutil.RunKubernetesCommand(ctx, "", false, "sudo", "-E", p)
	}()

	// wait for localizer to be up
	for ctx.Err() == nil && !localizer.IsRunning() {
		async.Sleep(ctx, time.Second*2)
	}

	for _, a := range o.LocalApps {
		la := localapp.NewOptions(o.log)
		la.AppName = a

		o.log.Infof("Running 'devenv local-app %s'", a)
		if err := la.Run(ctx); err != nil { //nolint:govet // Why: Shadowing err on purpose
			o.log.WithField("app.name", a).WithError(err).Error("failed to create local-app")
		}
	}

	// Wait for localizer to stop
	return <-localizerErrCh
}
