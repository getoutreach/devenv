package start

import (
	"context"
	"io"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
)

//nolint:gochecknoglobals
var (
	startLongDesc = `
		Start restarts your Kubernetes leader node, which kicks off launching your developer environment.
	`
	startExample = `
		# Start your already provisioned developer environment
		devenv start
	`
)

type Options struct {
	log logrus.FieldLogger
	d   dockerclient.APIClient
	k   kubernetes.Interface
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	k, err := kube.GetKubeClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return &Options{
		log: log,
		d:   d,
		k:   k,
	}, nil
}

func NewCmdStart(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "start",
		Usage:       "Start your already provisioned developer environment",
		Description: cmdutil.NewDescription(startLongDesc, startExample),
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

// Run runs the start command. This is nolint'd for now until we
// rewrite the rest of this. Then it makes more sense to split this
// out into functions.
func (o *Options) Run(ctx context.Context) error { //nolint:funlen
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
	kr.Configure(o.log, b)

	if err := kr.PreCreate(ctx); err != nil {
		return err
	}

	o.log.Info("Starting Developer Environment")
	if err := kr.Start(ctx); err != nil {
		return errors.Wrap(err, "failed to start developer environment")
	}
	o.log.Info("Started Developer Environment")

	o.log.Info("Waiting for Kubernetes to be accessible ...")
	sopt, err := status.NewOptions(o.log)
	if err != nil {
		return err
	}

	// Wait for the devenv to be "ready" so that we can create a Kubernetes client that works
	noopLogger := logrus.New()
	noopLogger.Out = io.Discard
	err = devenvutil.WaitForDevenv(ctx, sopt, noopLogger)
	if err != nil {
		return err
	}

	k, err := kube.GetKubeClient()
	if err != nil {
		return err
	}

	if err := devenvutil.WaitForAllPodsToBeReady(ctx, k, o.log); err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	return nil
}
