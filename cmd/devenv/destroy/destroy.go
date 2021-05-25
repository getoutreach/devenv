package destroy

import (
	"context"

	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/devenv/pkg/snapshoter"
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

			return o.Run(c.Context)
		},
	}
}

func (o *Options) Run(ctx context.Context) error {
	_, err := kubernetesruntime.EnsureKind(o.log)
	if err != nil {
		o.log.Errorf("failed to download container runtime")
		return err
	}

	o.log.Info("Destroying devenv ...")
	if err := kubernetesruntime.ResetKind(ctx, o.log); err != nil {
		o.log.WithError(err).Warn("failed to remove kind cluster")
	}

	if o.RemoveImageCache {
		o.log.Info("Removing Kubernetes Docker image cache ...")
		err := o.d.VolumeRemove(ctx, containerruntime.ContainerName+"-containerd", false)
		if err != nil && !dockerclient.IsErrNotFound(err) {
			return errors.Wrap(err, "failed to remove image volume")
		}
	}

	if o.RemoveSnapshotStorage {
		err := o.d.ContainerStop(ctx, snapshoter.MinioContainerName, nil)
		if err != nil && !dockerclient.IsErrNotFound(err) {
			return errors.Wrap(err, "failed to stop local snapshot storage")
		}

		err = o.d.ContainerRemove(ctx, snapshoter.MinioContainerName, types.ContainerRemoveOptions{
			Force: true,
		})
		if err != nil && !dockerclient.IsErrNotFound(err) {
			return errors.Wrap(err, "failed to remove local snapshot storage")
		}

		err = o.d.VolumeRemove(ctx, snapshoter.MinioContainerName, true)
		if err != nil && !dockerclient.IsErrNotFound(err) {
			return errors.Wrap(err, "failed to remove local snapshot storage data")
		}
	}

	o.log.Info("Finished successfully")
	return nil
}
