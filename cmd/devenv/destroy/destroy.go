package destroy

import (
	"context"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	destroyLongDesc = `
		destroy cleans up your developer environment. It's important to note that it doesn't clean up anything outside of Kubernetes.
	`
	destroyExample = `
		# Destroy the running developer environment
		devenv destroy
	`
)

type Options struct {
	log logrus.FieldLogger
	d   dockerclient.APIClient

	// Options
	RemoveImageCache      bool
	RemoveSnapshotStorage bool
	KubernetesRuntime     kubernetesruntime.Runtime
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

func NewCmdDestroy(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "destroy",
		Usage:       "Destroy the running developer environment",
		Description: cmdutil.NewDescription(destroyLongDesc, destroyExample),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "remove-image-cache",
				Usage: "cleanup the Kubernetes Docker image cache",
			},
			&cli.BoolFlag{
				Name:  "remove-snapshot-storage",
				Usage: "cleanup local snapshot storage",
			},
		},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}
			o.RemoveImageCache = c.Bool("remove-image-cache")
			o.RemoveSnapshotStorage = c.Bool("remove-snapshot-storage")
			b, err := box.LoadBox()
			if err != nil {
				return errors.Wrap(err, "failed to load box configuration")
			}

			// HACK: Right now we can't really tell which devenv was
			// running, so we destroy them all
			o.log.Info("Destroying devenv")
			for _, runtime := range kubernetesruntime.GetRuntimes() {
				runtime.Configure(o.log, b)

				o.KubernetesRuntime = runtime
				err := o.Run(c.Context) //nolint:govet // Why: shadowing err
				if err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
	// nolint:errcheck // Why: Failing to remove a cluster is OK.
	o.KubernetesRuntime.Destroy(ctx)

	if o.RemoveImageCache {
		if o.KubernetesRuntime.GetConfig().Type == kubernetesruntime.RuntimeTypeLocal {
			o.log.Info("Removing Kubernetes Docker image cache ...")
			err := o.d.VolumeRemove(ctx, containerruntime.ContainerName+"-containerd", false)
			if err != nil && !dockerclient.IsErrNotFound(err) {
				return errors.Wrap(err, "failed to remove image volume")
			}
		} else {
			o.log.Warn("--remove-image-cache has no effect on a remote kubernetes runtime")
		}
	}

	if o.RemoveSnapshotStorage {
		o.log.Warn("DEPRECATED: --remove-snapshot-storage no longer has any effect")
	}

	return nil
}
