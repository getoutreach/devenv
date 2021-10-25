package localapp

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	deployapp "github.com/getoutreach/devenv/cmd/devenv/deploy-app"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/embed"
	"github.com/getoutreach/devenv/pkg/kubernetestunnelruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/localizer/pkg/localizer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	localAppLongDesc = `
		local-app creates a tunnel into your developer environment that receives traffic from Kubernetes and tunnels it to your local application instance
	`
	localAppExample = `
		# Point a Kubernetes Service at your local application instance
		devenv local-app <appName>

		# Tunnel a Kubernetes Service in another namespace
		devenv local-app --namespace <namespace> <appName>
	`
)

const (
	DefaultNamespace = "bento1a"
)

type Options struct {
	log logrus.FieldLogger

	AppName   string
	Namespace string

	// CreateManifests is a path to manifests
	// that should be created before running the KFR
	// but also removed after running --stop
	// Note: This should never be user facing.
	// The path should be relative to the
	// extracted source temp directory
	CreateManifests string

	// OriginalManifests are one to re-apply after a local-app
	// has finished.
	// Note: This should never be user facing.
	// The path should be relative to the
	// extracted source temp directory
	OriginalManifests string

	// Stop designates if this command is meant to stop
	// an active forward
	Stop bool

	Ports map[uint64]uint64
}

func NewOptions(log logrus.FieldLogger) *Options {
	return &Options{
		log:   log,
		Ports: make(map[uint64]uint64),
	}
}

func NewCmdLocalApp(log logrus.FieldLogger) *cli.Command { //nolint:funlen
	o := NewOptions(log)

	return &cli.Command{
		Name:        "local-app",
		Usage:       "Point a Kubernetes Service at a local-application",
		Description: cmdutil.NewDescription(localAppLongDesc, localAppExample),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "namespace",
				Usage:       "Namespace your application resides in",
				Destination: &o.Namespace,
			},
			&cli.BoolFlag{
				Name:        "stop",
				Usage:       "Stop forwarding an application",
				Destination: &o.Stop,
			},
			&cli.StringSliceFlag{
				Name:  "port",
				Usage: "port to expose locally, can be repeated",
			},
		},
		Action: func(c *cli.Context) error {
			argsLen := c.Args().Len()
			if argsLen < 1 {
				return fmt.Errorf("missing appName argument")
			} else if argsLen > 1 {
				return fmt.Errorf("expected only one argument, got %d", argsLen)
			}

			o.AppName = c.Args().First()

			for _, pstr := range c.StringSlice("port") {
				split := strings.Split(pstr, ":")

				// validate that we only got two ports
				if len(split) != 2 {
					return fmt.Errorf("expected format srcPort:destPort, got '%s'", pstr)
				}

				srcPort, err := strconv.ParseUint(split[0], 10, 0)
				if err != nil {
					return fmt.Errorf("failed to parse %s as a port", split[0])
				}

				destPort, err := strconv.ParseUint(split[1], 10, 0)
				if err != nil {
					return fmt.Errorf("failed to parse %s as a port", split[1])
				}

				if _, ok := o.Ports[srcPort]; ok {
					return fmt.Errorf("port %v is already mapped to %v", srcPort, o.Ports[srcPort])
				}

				o.Ports[srcPort] = destPort
			}

			if c.Bool("not-bootstrap") {
				o.Namespace = DefaultNamespace
			}

			return o.Run(c.Context)
		},
	}
}

func (o *Options) handleSpecialCases() {
	switch o.AppName {
	case "accounts", "outreach-accounts": //nolint:goconst // Why: no
		// IDEA: Remove on next major release.
		if o.AppName == "accounts" {
			o.log.Warn("accounts as an appname is DEPRECATED. Use outreach-accounts instead")
		}

		o.Namespace = "outreach-accounts"
		o.AppName = "outreach-accounts"

	case "flagship", "flagship-server", "outreach": //nolint:goconst // Why: No
		// IDEA: Remove on next major release.
		if o.AppName == "flagship" || o.AppName == "flagship-server" {
			o.log.Warn("flagship/flagship-server as an appname is DEPRECATED. Use outreach instead")
		}

		o.Namespace = DefaultNamespace
		o.AppName = "outreach"

	// Special cases for UI related services.
	case "flagship-client":
		o.AppName = "clientron"
		o.Ports = map[uint64]uint64{
			4202: 8080,
		}
	case "orca", "client", "orca-proxy":
		// IDEA: Remove on next major release.
		if o.AppName == "orca" || o.AppName == "orca-proxy" {
			o.log.Warn("orca/orca-proxy as an appname is DEPRECATED. Use outreach instead")
		}

		o.Namespace = DefaultNamespace
		o.AppName = "client"
		o.CreateManifests = "shell/local-app/orca/manifests.yaml"
	case "outlook":
		o.Namespace = DefaultNamespace
		o.AppName = "outlook-proxy"
		o.CreateManifests = "shell/local-app/outlook/manifests.yaml"
	case "public-calendar":
		o.Namespace = "clicktrack--bento1a"
		o.AppName = "calclient-devproxy"
		o.CreateManifests = "shell/local-app/public-calendar/manifests.jsonnet"
		o.OriginalManifests = "shell/local-app/public-calendar/original.jsonnet"
	}
}

func (o *Options) Run(ctx context.Context) error { //nolint:funlen
	o.handleSpecialCases()

	if o.Namespace == "" {
		o.Namespace = fmt.Sprintf("%s--bento1a", o.AppName)
	}

	localizerPath, err := kubernetestunnelruntime.EnsureLocalizer(o.log)
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

	_, err = devenvutil.EnsureDevenvRunning(ctx, conf, b)
	if err != nil {
		return err
	}

	if !localizer.IsRunning() {
		o.log.Error("Did you run 'devenv tunnel'?")
		return fmt.Errorf("failed to find running kubernetes tunnel runtime")
	}

	// Build the argv for localizer
	args := []string{}
	// map ports to the argv
	for srcPort, destPort := range o.Ports {
		args = append(args, "--map", fmt.Sprintf("%d:%d", srcPort, destPort))
	}
	if o.Stop {
		args = append(args, "--stop")
	}

	// append the namespace/service args
	args = append(args, o.Namespace+"/"+o.AppName)

	dir, err := embed.ExtractAllToTempDir(ctx)
	if err != nil {
		if dir != "" {
			//nolint:errcheck
			os.RemoveAll(dir)
		}
		return err
	}

	// Create manifests if told to do so, and we're not --stop
	if !o.Stop && o.CreateManifests != "" {
		o.log.Info("Creating pre-requisite manifests ...")
		err2 := cmdutil.RunKubernetesCommand(ctx, dir, false, "kubecfg",
			"--jurl", "https://raw.githubusercontent.com/getoutreach/jsonnet-libs/master",
			"update", o.CreateManifests)
		if err2 != nil {
			return errors.Wrap(err, "failed to create bundled manifests")
		}
	}

	args = append([]string{"expose"}, args...)
	err = cmdutil.RunKubernetesCommand(ctx, "", false, localizerPath, args...)
	if err != nil {
		return err
	}

	// Delete the manifests, if set for this command and we're --stop
	if o.Stop && o.CreateManifests != "" {
		o.log.Info("Removing pre-requisite manifests ...")
		err2 := cmdutil.RunKubernetesCommand(ctx, dir, false, "kubecfg",
			"--jurl", "https://raw.githubusercontent.com/getoutreach/jsonnet-libs/master",
			"delete", o.CreateManifests)
		if err2 != nil {
			o.log.WithError(err2).Warn("failed to delete helper manifests")
		}

		// Until we can parse the manifests and wait for their deletions
		// we have to wait an arbitrary amount of time :(
		time.Sleep(15 * time.Second)

		// re-apply original manifests, if we have any
		if o.OriginalManifests != "" {
			o.log.Info("Re-applying original manifests")
			err3 := cmdutil.RunKubernetesCommand(ctx, dir, false, "kubecfg",
				"--jurl", "https://raw.githubusercontent.com/getoutreach/jsonnet-libs/master",
				"update", o.OriginalManifests)
			if err3 != nil {
				o.log.WithError(err3).Warn("failed to delete helper manifests")
			}
		} else {
			// Otherwise we redeploy the application to remove whatever mutations may have occurred
			// by o.CreateManifests
			dopts, err := deployapp.NewOptions(o.log)
			if err != nil {
				return err
			}
			dopts.App = o.AppName
		}
	}

	// If we're not --stop then tell the user how to stop their
	// application. We use raw os.Args to closely match their provided
	// input instead of reconstructing it.
	if !o.Stop {
		o.log.Infof("To stop forwarding your application, run 'devenv local-app --stop %s'", strings.Join(os.Args[2:], " "))
	}

	return nil
}
