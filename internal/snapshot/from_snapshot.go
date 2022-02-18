// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file contains functions for provisioning
// from a snapshot.
// Note: This needs to be cleaned up.
package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/getoutreach/devenv/internal/apps"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	veleroclient "github.com/vmware-tanzu/velero/pkg/generated/clientset/versioned"
	velerov1 "github.com/vmware-tanzu/velero/pkg/generated/informers/externalversions/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/util/boolptr"
)

const (
	SnapshotNamespace = "velero"
)

// Manager contains logic for handling snapshots in a developer environment.
type Manager struct {
	log logrus.FieldLogger
	k   kubernetes.Interface
	r   *rest.Config
	b   *box.Config
	vc  veleroclient.Interface
}

// NewManager creates a fully initialized Manager instance
func NewManager(log logrus.FieldLogger, b *box.Config) (*Manager, error) {
	k, conf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		log.WithError(err).Warn("failed to create kubernetes client")
	}

	opts := &Manager{
		log: log,
		b:   b,
	}

	// If we made a kubernetes client, create the other clients that rely on it
	if k != nil {
		var err error
		opts.k = k
		opts.r = conf

		opts.vc, err = veleroclient.NewForConfig(conf)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create snapshot client")
		}
	}

	return opts, nil
}

// RetrieveVeleroBackup returns a backup from a velero store.
func (m *Manager) RetrieveVeleroBackup(ctx context.Context, snapshotName string) (*velerov1api.Backup, error) {
	if m.vc == nil {
		return nil, fmt.Errorf("velero client not set")
	}

	return m.vc.VeleroV1().Backups(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
}

// deleteExistingRestore deletes an existing restore object
func (m *Manager) deleteExistingRestore(ctx context.Context, snapshotName string) error {
	restore, err := m.vc.VeleroV1().Restores(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
	if err == nil {
		if restore.Status.Phase == velerov1api.RestorePhaseInProgress {
			return fmt.Errorf("existing restore is in progress, refusing to create new restore")
		}
		m.log.Info("Deleting previous completed restore")
		err = m.vc.VeleroV1().Restores(SnapshotNamespace).Delete(ctx, snapshotName, metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to delete existing restore")
		}

		m.log.Info("Waiting for delete to finish ...")
		ticker := time.NewTicker(5 * time.Second)
	loop:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				_, err = m.vc.VeleroV1().Restores(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
				if kerrors.IsNotFound(err) {
					break loop
				} else if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Restore restores a given snapshot into an environment.
func (m *Manager) Restore(ctx context.Context, snapshotName string) error { //nolint:funlen
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if snapshotName == "" {
		return fmt.Errorf("missing snapshot name")
	}

	if _, err := m.RetrieveVeleroBackup(ctx, snapshotName); err != nil {
		return err
	}

	if err := m.deleteExistingRestore(ctx, snapshotName); err != nil {
		return err
	}

	appsClient := apps.NewKubernetesConfigmapClient(m.k, "")
	beforeApps, err := appsClient.List(ctx)
	if err != nil {
		m.log.WithError(err).
			Warn("failed to preserve apps configmap state before restore, apps information may be invalid")
		beforeApps = []apps.App{}
	}

	if err := appsClient.Reset(ctx); err != nil {
		m.log.WithError(err).Warn("failed to reset apps configmap, apps information may be invalid")
	}

	if _, err := m.vc.VeleroV1().Restores(SnapshotNamespace).Create(ctx, &velerov1api.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: SnapshotNamespace,
			Name:      snapshotName,
		},
		Spec: velerov1api.RestoreSpec{
			BackupName:              snapshotName,
			RestorePVs:              boolptr.True(),
			IncludeClusterResources: boolptr.True(),
			PreserveNodePorts:       boolptr.True(),

			// TODO(DTSS-829): This should be moved into the generation framework
			ExcludedNamespaces: []string{
				"nginx-ingress",
				"kube-system",
				"cert-manager",
				"nginx-ingress",
				"velero",
				"minio",
				"vault-secrets-operator",
				"local-path-storage",
				"monitoring",
				"resourcer--bento1a",
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		return err
	}

	updates := make(chan *velerov1api.Restore)
	restoreInformer := velerov1.NewRestoreInformer(m.vc, SnapshotNamespace, 0, nil)
	restoreInformer.AddEventHandler( //nolint:dupl
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				restore, ok := obj.(*velerov1api.Restore)
				if !ok {
					return false
				}
				return restore.Name == snapshotName
			},
			Handler: cache.ResourceEventHandlerFuncs{
				UpdateFunc: func(_, obj interface{}) {
					restore, ok := obj.(*velerov1api.Restore)
					if !ok {
						return
					}
					updates <- restore
				},
				DeleteFunc: func(obj interface{}) {
					restore, ok := obj.(*velerov1api.Restore)
					if !ok {
						return
					}
					updates <- restore
				},
			},
		},
	)
	go restoreInformer.Run(ctx.Done())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case restore, ok := <-updates:
			if !ok {
				return fmt.Errorf("failed to watch restore operation")
			}
			if restore.Status.Phase != velerov1api.RestorePhaseNew && restore.Status.Phase != velerov1api.RestorePhaseInProgress {
				m.log.Infof("Snapshot restore finished with status: %v", restore.Status.Phase)

				if newApps, err := appsClient.List(ctx); err != nil {
					m.log.Infof("Snapshot created %d applications", len(newApps))
				}

				// iterate over old apps that we had before the restore, adding them
				// to the current state of the world. Note: we don't override versions
				// because velero doesn't replace existing resources
				for i := range beforeApps {
					a := &beforeApps[i]

					if _, err := appsClient.Get(ctx, a.Name); err == nil {
						continue
					}

					appsClient.Set(ctx, a) //nolint:errcheck // Why: best effort
				}

				return nil
			}
		}
	}
}

// CreateBackupStorage creates a backup storage location
func (m *Manager) CreateBackupStorage(ctx context.Context, name, bucket string) error {
	_, err := m.vc.VeleroV1().BackupStorageLocations(SnapshotNamespace).Create(ctx, &velerov1api.BackupStorageLocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: velerov1api.BackupStorageLocationSpec{
			Provider: "aws",
			StorageType: velerov1api.StorageType{
				ObjectStorage: &velerov1api.ObjectStorageLocation{
					Bucket: bucket,
				},
			},
			Config: map[string]string{
				"region":           "minio",
				"s3ForcePathStyle": "true",
				"s3Url":            "http://minio.minio:9000",
			},
		},
	}, metav1.CreateOptions{})
	return err
}
