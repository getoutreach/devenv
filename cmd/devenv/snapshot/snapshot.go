package snapshot

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/devenv/pkg/worker"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
	apixv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
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

//nolint:gochecknoglobals
var (
	snapshotLongDesc = `
		Manage snapshots of your developer environment.
	`
	helpersExample = `
		# Create a snapshot
		devenv snapshot create

		# Delete a snapshot
		devenv snapshot delete <date>

		# Restore a snapshot to a existing cluster
		devenv snapshot restore <date>
	`
)

type Options struct {
	log  logrus.FieldLogger
	k    kubernetes.Interface
	r    *rest.Config
	d    dockerclient.APIClient
	vc   veleroclient.Interface
	apix apixv1client.Interface
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	k, conf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		log.WithError(err).Warn("failed to create kubernetes client")
	}

	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	opts := &Options{
		log: log,
		d:   d,
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

		opts.apix, err = apixv1client.NewForConfig(conf)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create apix client")
		}
	}

	return opts, nil
}

func NewCmdSnapshot(log logrus.FieldLogger) *cli.Command { //nolint:funlen
	var o *Options
	return &cli.Command{
		Name:        "snapshot",
		Usage:       "Manage snapshots of your developer environment",
		Description: cmdutil.NewDescription(snapshotLongDesc, helpersExample),
		Before: func(c *cli.Context) error {
			var err error
			o, err = NewOptions(log)
			return err
		},
		Subcommands: []*cli.Command{
			{
				Name:        "create",
				Description: "Create a new snapshot of your developer environment. Deprecated: Use generate instead.",
				Hidden:      true,
				Usage:       "devenv snapshot create",
				Action: func(c *cli.Context) error {
					if err := devenvutil.EnsureDevenvRunning(c.Context); err != nil {
						return err
					}
					_, err := o.CreateSnapshot(c.Context)
					return err
				},
			},
			{
				Name:        "restore",
				Description: "Restore an existing snapshot of your developer environment",
				Usage:       "devenv snapshot restore <name>",
				Action: func(c *cli.Context) error {
					if err := devenvutil.EnsureDevenvRunning(c.Context); err != nil {
						log.Info("If you're looking to provision an environment with a snapshot, try 'devenv provision --snapshot <name>'")
						return err
					}
					return o.RestoreSnapshot(c.Context, c.Args().First(), true)
				},
			},
			{
				Name:        "delete",
				Description: "Delete an existing snapshot of your developer environment",
				Usage:       "devenv snapshot delete",
				Action: func(c *cli.Context) error {
					if err := devenvutil.EnsureDevenvRunning(c.Context); err != nil {
						return err
					}
					return o.DeleteSnapshot(c.Context, c.Args().First())
				},
			},
			{
				Name:        "generate",
				Description: "Generate a snapshot from a snapshot definition",
				Usage:       "devenv snapshot generate",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "skip-upload",
						Usage: "Generate a snapshot, but don't upload it",
					},
					&cli.StringFlag{
						Name:  "channel",
						Value: string(box.SnapshotLockChannelRC),
						Usage: "Which channel this snapshot should be uploaded to",
					},
				},
				Action: func(c *cli.Context) error {
					b, err := ioutil.ReadFile("snapshots.yaml")
					if err != nil {
						return err
					}

					var s *box.SnapshotGenerateConfig
					err = yaml.Unmarshal(b, &s)
					if err != nil {
						return err
					}

					return o.Generate(c.Context, s, c.Bool("skip-upload"), box.SnapshotLockChannel(c.String("channel")))
				},
			},
		},
	}
}

func (o *Options) DeleteSnapshot(ctx context.Context, snapshotName string) error {
	if o.vc == nil {
		return fmt.Errorf("velero client not set")
	}

	if snapshotName == "" {
		return fmt.Errorf("missing snapshot name")
	}

	_, err := o.GetSnapshot(ctx, snapshotName)
	if err != nil {
		return err
	}

	_, err = o.vc.VeleroV1().DeleteBackupRequests(SnapshotNamespace).Create(ctx, &velerov1api.DeleteBackupRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: snapshotName + "-",
		},
		Spec: velerov1api.DeleteBackupRequestSpec{
			BackupName: snapshotName,
		},
	}, metav1.CreateOptions{})

	return err
}

func (o *Options) GetSnapshot(ctx context.Context, snapshotName string) (*velerov1api.Backup, error) {
	if o.vc == nil {
		return nil, fmt.Errorf("velero client not set")
	}

	return o.vc.VeleroV1().Backups(SnapshotNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
}

func (o *Options) deleteNamespaces(ctx context.Context) error { //nolint:funlen
	var namespaces []interface{}
	cont := ""
	for {
		l, err := o.k.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
			Continue: cont,
		})
		if err != nil {
			return err
		}
		cont = l.Continue

		for i := range l.Items {
			namespaces = append(namespaces, &l.Items[i])
		}

		if l.Continue == "" {
			break
		}
	}

	if _, err := worker.ProcessArray(ctx, namespaces, func(ctx context.Context, itm interface{}) (interface{}, error) {
		n := itm.(*corev1.Namespace)

		// skip some namespaces
		switch n.Name {
		case "default", "kube-system", "velero", "kube-public", "kube-node-lease", "nginx-ingress", "local-path-storage":
			return nil, nil
		}

		log := o.log.WithField("namespace", n.Name)
		log.Info("deleting namespace")
		err := o.k.CoreV1().Namespaces().Delete(ctx, n.Name, metav1.DeleteOptions{})
		if err != nil {
			return nil, err
		}

		ticker := time.NewTicker(5 * time.Second)
	loop:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
				_, err = o.k.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
				if kerrors.IsNotFound(err) {
					break loop
				} else if err != nil {
					return nil, err
				}

				log.Info("waiting for namespace to delete ...")
			}
		}

		log.Info("deleted namespace")
		return nil, nil
	}); err != nil {
		return err
	}

	return nil
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

	if liveRestore {
		o.log.Warn("THIS WILL DELETE ALL EXISTING DATA IN YOUR CLUSTER FROM BEFORE THE SNAPSHOT. PROCEED?")
		proceed, err := cmdutil.GetYesOrNoInput(ctx)
		if err != nil {
			return err
		}

		if !proceed {
			return fmt.Errorf("denied to proceed")
		}
	}

	if _, err := o.GetSnapshot(ctx, snapshotName); err != nil {
		return err
	}

	if err := o.deleteExistingRestore(ctx, snapshotName); err != nil {
		return err
	}

	if liveRestore {
		if err := o.deleteNamespaces(ctx); err != nil {
			return err
		}
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

func (o *Options) CreateSnapshot(ctx context.Context) (string, error) { //nolint:funlen
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	updates := make(chan *velerov1api.Backup)
	backupInformer := velerov1.NewBackupInformer(o.vc, SnapshotNamespace, 0, nil)

	// Create DNS1133 compliant backup name.
	backupName := strings.ToLower(
		strings.ReplaceAll(time.Now().Format(time.RFC3339), ":", "-"),
	)

	backupInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				backup, ok := obj.(*velerov1api.Backup)
				if !ok {
					return false
				}
				return backup.Name == backupName
			},
			Handler: cache.ResourceEventHandlerFuncs{
				UpdateFunc: func(_, obj interface{}) {
					backup, ok := obj.(*velerov1api.Backup)
					if !ok {
						return
					}
					updates <- backup
				},
				DeleteFunc: func(obj interface{}) {
					backup, ok := obj.(*velerov1api.Backup)
					if !ok {
						return
					}
					updates <- backup
				},
			},
		},
	)
	go backupInformer.Run(ctx.Done())

	_, err := o.vc.VeleroV1().Backups(SnapshotNamespace).Create(ctx, &velerov1api.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name: backupName,
		},
		Spec: velerov1api.BackupSpec{
			// Don't include velero, we need to install it before the backup
			ExcludedNamespaces: []string{"velero"},
			// Skip helm chart resources, since they've already been rendered at
			// this point.
			ExcludedResources:       []string{"HelmChart"},
			SnapshotVolumes:         boolptr.True(),
			DefaultVolumesToRestic:  boolptr.True(),
			IncludeClusterResources: boolptr.True(),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	o.log.Info("Waiting for snapshot to finish being created...")

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case backup, ok := <-updates:
			if !ok {
				return "", fmt.Errorf("failed to create snapshot")
			}

			if backup.Status.Phase != velerov1api.BackupPhaseNew && backup.Status.Phase != velerov1api.BackupPhaseInProgress {
				o.log.Infof("Created snapshot finished with status: %s", backup.Status.Phase)
				return backupName, nil
			}
		}
	}
}
