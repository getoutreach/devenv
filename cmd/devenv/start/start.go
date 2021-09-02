package start

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
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

	cont, err := o.d.ContainerInspect(ctx, containerruntime.ContainerName)
	if dockerclient.IsErrNotFound(err) {
		if _, err = o.d.ContainerInspect(ctx, "k3s"); err == nil {
			o.log.Info("Please destroy and reprovision your cluster. This will greatly increase the stability.")
			return fmt.Errorf("found older kubernetes runtime environment (k3s)")
		}

		o.log.Info("Hint: Try running 'devenv provision'")
		return fmt.Errorf("developer environment not found")
	} else if err != nil {
		return err
	}

	if cont.State.Running {
		return fmt.Errorf("developer environment is already started")
	}

	o.log.Info("Starting Developer Environment")
	err = o.d.ContainerStart(ctx, cont.ID, types.ContainerStartOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to start developer environment")
	}

	sopt, err := status.NewOptions(o.log)
	if err != nil {
		return err
	}

	o.log.Info("Waiting for developer environment to be up ...")
	err = devenvutil.WaitForDevenv(ctx, sopt, o.log)
	if err != nil {
		return err
	}

	if b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		if err := vault.EnsureLoggedIn(ctx, o.log, b, o.k); err != nil {
			return errors.Wrap(err, "failed to refresh vault authentication")
		}
	}

	o.log.Info("Developer Environment has started (note: services may take longer to start)")
	return nil
}
