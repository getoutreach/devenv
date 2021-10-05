package destroy

import (
	"context"
	"fmt"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
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
	b   *box.Config

	// Options
	CurrentClusterName    string
	RemoveImageCache      bool
	RemoveSnapshotStorage bool
	KubernetesRuntime     kubernetesruntime.Runtime
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	b, err := box.LoadBox()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read box config")
	}

	conf, err := config.LoadConfig(context.TODO())
	if err != nil {
		return nil, errors.Wrap(err, "failed to read devenv config")
	}

	runtimeName, clusterName := conf.ParseContext()
	if clusterName == "" {
		return nil, fmt.Errorf("invalid clusterName, was currentcontext set in devenv config?")
	}

	r, err := kubernetesruntime.GetRuntimeFromContext(conf, b)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get runtime from context, was the runtime '%s' enabled?", runtimeName)
	}

	r.Configure(log, b)

	return &Options{
		log: log,
		d:   d,
		b:   b,

		// Defaults
		CurrentClusterName: clusterName,
		KubernetesRuntime:  r,
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
	if o.CurrentClusterName != o.KubernetesRuntime.GetConfig().ClusterName {
		return fmt.Errorf("cannot delete clusters that don't belong to us")
	}

	o.log.WithField("runtime", o.KubernetesRuntime.GetConfig().Name).
		Infof("Destroying devenv '%s'", o.CurrentClusterName)

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
