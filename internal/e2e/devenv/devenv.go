// Package devenv implements helpers for e2e tests in
// interacting with a devenv.
package devenv

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	"github.com/getoutreach/devenv/cmd/devenv/provision"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	dockerclient "github.com/docker/docker/client"
)

// ProvisionOpts are arguments to pass to a devenv provision
type ProvisionOpts struct {
	// SnapshotTarget is the target snapshot to deploy
	// if empty, defaults to none.
	SnapshotTarget string

	// Apps is a list of apps to deploy to this devenv
	Apps []string

	// Box is the box config to use when provisioning the devenv
	// this is required. Note: Do not use the host box. Using it may
	// result in inconsistent test runs.
	Box *box.Config
}

// Logger is the logger that should be used for tests
var Logger = &logrus.Logger{Out: os.Stderr, Level: logrus.DebugLevel, ReportCaller: true,
	Formatter: &logrus.TextFormatter{ForceColors: true}}

// DestroyDevenv destroys a devenv
func DestroyDevenv(ctx context.Context) error {
	var dopts *destroy.Options
	var err error

	if dopts, err = destroy.NewOptions(Logger); err == nil {
		if err = dopts.Run(ctx); err == nil {
			return nil
		}
	}

	return err
}

// ProvisionDevenv creates a devenv based on the following options
// and returns a function to destroy it.
//nolint:gocritic,revive // Why: We're OK not naming these
func ProvisionDevenv(t *testing.T, ctx context.Context, opts *ProvisionOpts) func() {
	t.Log("Provisioning devenv")
	popts, err := provision.NewOptions(Logger, opts.Box)
	if err != nil {
		t.Error(errors.Wrap(err, "failed to create devenv provision options"))
		return nil
	}

	popts.KubernetesRuntime, err = kubernetesruntime.GetRuntime("kind")
	if err != nil {
		t.Error(errors.Wrap(err, "failed to load kubernetes runtime"))
		return nil
	}

	if opts.SnapshotTarget == "" {
		popts.Base = true
	} else {
		popts.SnapshotTarget = opts.SnapshotTarget
	}

	if opts.Apps != nil {
		popts.DeployApps = opts.Apps
	}

	cleanupFn := func() {
		if err := DestroyDevenv(ctx); err != nil {
			t.Logf("WARNING: Failed to cleanup devenv: %v\n", err)
		}
	}

	// cleanup before provisioning
	t.Log("Attempting to cleanup older devenv, can ignore warning below")
	cleanupFn()

	if err := popts.Run(ctx); err != nil {
		t.Error(errors.Wrapf(err, "failed to provision devenv with provided options: %v", opts))
		return cleanupFn
	}

	return cleanupFn
}

// execOSOut is a helper to use os.Stdout/Err in a exec.Cmd
func execOSOut(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// UseSnapshotStorage creates a docker backed snapshot storage that can be used
// for testing. Configures the provided box config to use it.
//nolint:gocritic, revive // Why: t is first param in test helpers
func UseSnapshotStorage(t *testing.T, ctx context.Context, b *box.Config) (func(), error) {
	t.Log("Setting up snapshot storage infrastructure")
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	minioImage := "minio/minio:RELEASE.2022-02-05T04-40-59Z"
	minioContainerName := "dev-environment-minio-test"

	if err := execOSOut(exec.CommandContext(ctx, "docker", "pull", minioImage)).Run(); err != nil {
		return nil, errors.Wrap(err, "failed to pull minio image")
	}

	// Kind does some magic[1] to create a network to ensure that we use a stable IP address
	// across restarts. Instead of duplicating that logic here, which could cause problems,
	// we just limit this to only being able to be ran after a provision has ocurred once,
	// which, at the time, is done with the first test.
	//
	// [1]: https://github.com/getoutreach/kind/commit/59a4b10b08c59809f51333addad0004c95d1c908
	kindNetwork, err := d.NetworkInspect(ctx, "kind", types.NetworkInspectOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to find kind network, test must be ran after a provision")
	}

	cleanupFn := func() {
		err := d.ContainerRemove(ctx, minioContainerName,
			types.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		if err != nil {
			t.Logf("WARNING: Failed to remove minio container: %v", err)
		}
	}

	// cleanup before attempting to create
	t.Log("Attempting to cleanup older minio container, can ignore warning below")
	cleanupFn()

	_, err = d.ContainerCreate(ctx, &container.Config{
		Env: []string{
			"MINIO_ACCESS_KEY=" + "ACCESS_KEY",
			"MINIO_SECRET_KEY=" + "SECRET_KEY",
		},
		Image:      minioImage,
		Entrypoint: []string{"/usr/bin/env", "sh", "-c"},
		Cmd: []string{
			// Minio uses directories for buckets, so if we make
			// a directory on init, it'll make a bucket with that name
			"mkdir -p /data/snapshots && exec minio server /data",
		},
	}, &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name:              "always",
			MaximumRetryCount: 0,
		},
		PortBindings: nat.PortMap{
			"9000/tcp": []nat.PortBinding{
				// We use the minio port documented in our port-allocation docs:
				// https://outreach-io.atlassian.net/wiki/spaces/EN/pages/1433993221
				{
					HostIP:   "127.0.0.1",
					HostPort: "61003",
				},
			},
		},
	}, nil, nil, minioContainerName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create minio container")
	}

	if err := d.ContainerStart(ctx, minioContainerName, types.ContainerStartOptions{}); err != nil {
		return cleanupFn, errors.Wrap(err, "failed to start minio container")
	}

	// HACK: 172.18.0.0/16 -> 172.18.0.10
	ipAddr := strings.TrimSuffix(kindNetwork.IPAM.Config[0].Subnet, ".0/16") + ".10"

	b.DeveloperEnvironmentConfig.SnapshotConfig = box.SnapshotConfig{
		// Note: This assumes that you're accessing from inside the cluster.
		// endpoint will need to be modified if attempting to publish from outside.
		Endpoint: "http://" + ipAddr + ":9000",
		Region:   "us-east-1", // default minio region
		Bucket:   "snapshots",
	}

	err = execOSOut(exec.CommandContext(ctx, "docker", "network", "connect", "kind", minioContainerName, "--ip", ipAddr)).Run()
	return cleanupFn, errors.Wrap(err, "failed to pull minio image")
}
