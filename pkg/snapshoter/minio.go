package snapshoter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

const (
	MinioContainerName      = "minio-developer-environment"
	MinioAccessKey          = "minioaccess"
	MinioSecretKey          = "miniosecret"
	MinioSnapshotBucketName = "velero"
	MinioDockerImage        = "minio/minio:RELEASE.2021-05-18T00-53-28Z"
)

func CreateMinioClient() (*minio.Client, error) {
	m, err := minio.New("127.0.0.1:61002", &minio.Options{
		Creds:  credentials.NewStaticV4(MinioAccessKey, MinioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create minio client")
	}

	return m, nil
}

// Ensure creates a Minio container if it doesn't exist, ensures that
// it's running, and that it's connected to the kubernetes runtime network
func Ensure(ctx context.Context, d dockerclient.APIClient, log logrus.FieldLogger) error { //nolint:funlen,gocyclo
	cont, err := d.ContainerInspect(ctx, MinioContainerName)
	if dockerclient.IsErrNotFound(err) {
		// if not found, then we create it
		if cont, err = createMinioContainer(ctx, d, log); err != nil {
			return err
		}
	} else if err != nil {
		return errors.Wrap(err, "failed to determine snapshot storage container status")
	}

	// If we're connected to a network that no longer exists, remove the container
	// we can't disconnect from a non-existent network :(
	if contNet, ok := cont.NetworkSettings.Networks[containerruntime.ContainerNetwork]; ok {
		netw, err := d.NetworkInspect(ctx, contNet.NetworkID, types.NetworkInspectOptions{}) //nolint:govet // Why: We're OK shadowing err
		if dockerclient.IsErrNotFound(err) {
			err = d.ContainerRemove(ctx, cont.ID, types.ContainerRemoveOptions{
				Force: true,
			})
			if err != nil {
				return errors.Wrap(err, "failed to remove minio")
			}

			if cont, err = createMinioContainer(ctx, d, log); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		// check and see if the network needs to be migrated
		// While kind handles this, we need to remove the network and container
		// to allow kind to actually create the new network
		if netw.Labels["io.x-k8s.kind.network-custom"] != "true" {
			err = d.ContainerRemove(ctx, MinioContainerName, types.ContainerRemoveOptions{
				Force: true,
			})
			if err != nil {
				return errors.Wrap(err, "failed to update minio")
			}

			err := d.NetworkRemove(ctx, containerruntime.ContainerNetwork) //nolint:govet // Why: We're OK shadowing err
			if err != nil {
				return errors.Wrap(err, "failed to trigger migration of kind network")
			}

			if cont, err = createMinioContainer(ctx, d, log); err != nil {
				return err
			}
		}
	}

	// Update the docker container if the image is out-of-date
	if cont.Config.Image != MinioDockerImage {
		log.Info("Updating Snapshot Storage Container")
		if _, _, err2 := d.ImageInspectWithRaw(ctx, MinioDockerImage); err2 != nil {
			log.Info("Pulling Snapshot Storage Container Image ...")
			err2 = devenvutil.RunKubernetesCommand(ctx, "", "docker", "pull", MinioDockerImage)
			if err != nil {
				return errors.Wrap(err2, "failed to get pull snapshot storage container image")
			}
		}

		d.ContainerStop(ctx, MinioContainerName, nil) //nolint:errcheck
		err2 := d.ContainerRemove(ctx, MinioContainerName, types.ContainerRemoveOptions{
			Force: true,
		})
		if err2 != nil {
			return errors.Wrap(err2, "failed to update minio")
		}

		if cont, err = createMinioContainer(ctx, d, log); err != nil {
			return err
		}
	}

	// This handles ensuring it's running, but also if we just created it
	switch cont.State.Status {
	case "exited", "dead", "created":
		err = errors.Wrap(
			d.ContainerStart(ctx, MinioContainerName, types.ContainerStartOptions{}),
			"failed to start snapshot storage container",
		)
		if err != nil {
			return err
		}

		log.Info("Waiting for snapshot local-storage to be ready")
		t := time.NewTicker(5 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}

			resp, err2 := http.Get("http://127.0.0.1:61002/minio/health/live")
			if err2 == nil {
				resp.Body.Close()

				// if 200, exit
				if resp.StatusCode == http.StatusOK {
					break
				}

				// not 200, so modify the error
				err2 = fmt.Errorf("got status %s", resp.Status)
			}

			log.WithError(err2).Info("Snapshot local-storage is not ready")
		}
		log.Info("Snapshot local-storage is ready")
	}
	if err != nil {
		return err
	}

	// if it's not attached to the kind network, then attach it, but only if we found the network
	// the network can exist only if the kubernetes runtime has ran before.
	if _, err := d.NetworkInspect(ctx, containerruntime.ContainerNetwork, types.NetworkInspectOptions{}); err == nil {
		if _, ok := cont.NetworkSettings.Networks[containerruntime.ContainerNetwork]; !ok {
			err = d.NetworkConnect(ctx, containerruntime.ContainerNetwork, MinioContainerName, &network.EndpointSettings{})
			if err != nil {
				return errors.Wrap(err, "failed to connect snapshot storage to kubernetes runtime network")
			}
		}
	}

	return nil
}

// createMinioContainer creates the minio docker container
func createMinioContainer(ctx context.Context, d dockerclient.APIClient, log logrus.FieldLogger) (types.ContainerJSON, error) {
	if _, _, err := d.ImageInspectWithRaw(ctx, MinioDockerImage); err != nil {
		log.Info("Pulling Snapshot Storage Container Image ...")
		err = devenvutil.RunKubernetesCommand(ctx, "", "docker", "pull", MinioDockerImage)
		if err != nil {
			return types.ContainerJSON{}, errors.Wrap(err, "failed to get pull snapshot storage container image")
		}
	}

	log.Info("Starting snapshot local-storage")
	_, err := d.ContainerCreate(ctx, &container.Config{
		Env: []string{
			"MINIO_ACCESS_KEY=" + MinioAccessKey,
			"MINIO_SECRET_KEY=" + MinioSecretKey,
		},
		Image:      MinioDockerImage,
		Entrypoint: []string{"/usr/bin/env", "sh", "-c"},
		Cmd: []string{
			// Minio uses directories for buckets, so if we make
			// a directory on init, it'll make a bucket with that name
			fmt.Sprintf("mkdir -p /data/%s /data/%s-restore && exec minio server /data", MinioSnapshotBucketName, MinioSnapshotBucketName),
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
					HostPort: "61002",
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: MinioContainerName,
				Target: "/data",
			},
		},
	}, nil, nil, MinioContainerName)
	if err != nil {
		return types.ContainerJSON{}, errors.Wrap(err, "failed to create snapshot storage container")
	}

	cont, err := d.ContainerInspect(ctx, MinioContainerName)
	return cont, err
}

func ListSnapshots(ctx context.Context) ([]*velerov1api.Backup, error) {
	// Initialize minio client object.
	m, err := minio.New("127.0.0.1:61002", &minio.Options{
		Creds:  credentials.NewStaticV4(MinioAccessKey, MinioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	backups := make([]*velerov1api.Backup, 0)
	for o := range m.ListObjects(ctx, MinioSnapshotBucketName, minio.ListObjectsOptions{Prefix: "backups/", Recursive: true}) {
		if o.Err != nil {
			return nil, err
		}

		// We only care about the backup definition
		if filepath.Base(o.Key) != "velero-backup.json" {
			continue
		}

		f, err := m.GetObject(ctx, MinioSnapshotBucketName, o.Key, minio.GetObjectOptions{})
		if err != nil {
			return nil, err
		}

		var backup *velerov1api.Backup
		d := json.NewDecoder(f)
		err = d.Decode(&backup)
		if err != nil {
			return nil, err
		}

		backups = append(backups, backup)
	}

	return backups, nil
}
