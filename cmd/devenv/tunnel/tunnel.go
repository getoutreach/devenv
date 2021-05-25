package tunnel

import (
	"context"
	"time"

	localapp "github.com/getoutreach/devenv/cmd/devenv/local-app"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetestunnelruntime"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	tunnelLongDesc = `
		Tunnel uses localizer to create port-forwards into your Kubernetes cluster on your local-machine.

		These tunnels are then hooked up to DNS via your /etc/hosts file, which points to aliased ip addresses in the 127.0.0.1/8 space.
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
			// DEPRECATED: Removing in the next major release.
			&cli.BoolFlag{
				Name:   "localizer",
				Hidden: true,
				Usage:  "use the experimental telepresence replacement (deprecated: localizer is the default now)",
			},
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

	if err2 := devenvutil.EnsureDevenvRunning(ctx); err != nil {
		return err2
	}

	// Preemptively ask for sudo to prevent input mangaling with o.LocalApps
	o.log.Info("You may get a sudo prompt, this is so localizer can create tunnels")
	err = cmdutil.RunKubernetesCommand(ctx, "", true, "sudo", "echo", "Hello, world!")
	if err != nil {
		return errors.Wrap(err, "failed to get sudo")
	}

	doneChan := make(chan struct{})
	go func() {
		// sudo hacks, -E here is just "forward environment"
		err = cmdutil.RunKubernetesCommand(ctx, "", false, "sudo", "-E", p)
		close(doneChan)
	}()

	// wait for localizer to be up
	tick := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}

		if !kubernetestunnelruntime.IsLocalizerRunning() {
			o.log.Info("waiting for localizer to be running")
			continue
		}

		break
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
	<-doneChan
	return err
}
