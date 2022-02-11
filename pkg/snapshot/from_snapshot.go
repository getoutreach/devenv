// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file contains functions for provisioning
// from a snapshot.
// Note: This needs to be cleaned up.
package snapshot

import (
	"context"
	"fmt"
	"time"

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

type Options struct {
	log logrus.FieldLogger
	k   kubernetes.Interface
	r   *rest.Config
	b   *box.Config
	vc  veleroclient.Interface
}

func NewOptions(log logrus.FieldLogger, b *box.Config) (*Options, error) {
	k, conf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		log.WithError(err).Warn("failed to create kubernetes client")
	}

	opts := &Options{
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

func (o *Options) GetSnapshot(ctx context.Context, snapshotName string) (*velerov1api.Backup, error) {
	if o.vc == nil {
		return nil, fmt.Errorf("velero client not set")
	}

	return o.vc.VeleroV1().Backups(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
}

func (o *Options) deleteExistingRestore(ctx context.Context, snapshotName string) error {
	restore, err := o.vc.VeleroV1().Restores(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
	if err == nil {
		if restore.Status.Phase == velerov1api.RestorePhaseInProgress {
			return fmt.Errorf("existing restore is in progress, refusing to create new restore")
		}
		o.log.Info("Deleting previous completed restore")
		err = o.vc.VeleroV1().Restores(SnapshotNamespace).Delete(ctx, snapshotName, metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to delete existing restore")
		}

		o.log.Info("Waiting for delete to finish ...")
		ticker := time.NewTicker(5 * time.Second)
	loop:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				_, err = o.vc.VeleroV1().Restores(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
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

func (o *Options) RestoreSnapshot(ctx context.Context, snapshotName string, liveRestore bool) error { //nolint:funlen
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if snapshotName == "" {
		return fmt.Errorf("missing snapshot name")
	}

	if _, err := o.GetSnapshot(ctx, snapshotName); err != nil {
		return err
	}

	if err := o.deleteExistingRestore(ctx, snapshotName); err != nil {
		return err
	}

	if _, err := o.vc.VeleroV1().Restores(SnapshotNamespace).Create(ctx, &velerov1api.Restore{
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
	restoreInformer := velerov1.NewRestoreInformer(o.vc, SnapshotNamespace, 0, nil)
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

	o.log.Info("Waiting for snapshot restore operation to complete ...")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case restore, ok := <-updates:
			if !ok {
				return fmt.Errorf("failed to watch restore operation")
			}
			if restore.Status.Phase != velerov1api.RestorePhaseNew && restore.Status.Phase != velerov1api.RestorePhaseInProgress {
				o.log.Infof("Snapshot restore finished with status: %v", restore.Status.Phase)
				return nil
			}
		}
	}
}

// CreateBackupStorage creates a backup storage location
func (o *Options) CreateBackupStorage(ctx context.Context, name, bucket string) error {
	_, err := o.vc.VeleroV1().BackupStorageLocations(SnapshotNamespace).Create(ctx, &velerov1api.BackupStorageLocation{
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
