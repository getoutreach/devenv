package snapshot

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Why: just using for digest checking
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	"github.com/getoutreach/devenv/cmd/devenv/provision"
	devenvaws "github.com/getoutreach/devenv/pkg/aws"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	noncmdsnapshot "github.com/getoutreach/devenv/pkg/snapshot"
	"github.com/getoutreach/devenv/pkg/snapshoter"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	veleroclient "github.com/vmware-tanzu/velero/pkg/generated/clientset/versioned"
	velerov1 "github.com/vmware-tanzu/velero/pkg/generated/informers/externalversions/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/util/boolptr"
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
	log logrus.FieldLogger
	k   kubernetes.Interface
	r   *rest.Config
	d   dockerclient.APIClient
	b   *box.Config
	vc  veleroclient.Interface
}

func NewOptions(log logrus.FieldLogger, b *box.Config) (*Options, error) {
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
		b:   b,
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
	}

	return opts, nil
}

func NewCmdSnapshot(log logrus.FieldLogger) *cli.Command { //nolint:funlen
	return &cli.Command{
		Name:        "snapshot",
		Usage:       "Manage snapshots of your developer environment",
		Description: cmdutil.NewDescription(snapshotLongDesc, helpersExample),
		Subcommands: []*cli.Command{
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
					b, err := box.LoadBox()
					if err != nil {
						return errors.Wrap(err, "failed to load box configuration")
					}

					o, err := NewOptions(log, b)
					if err != nil {
						return err
					}

					byt, err := os.ReadFile("snapshots.yaml")
					if err != nil {
						return err
					}

					var s *box.SnapshotGenerateConfig
					err = yaml.Unmarshal(byt, &s)
					if err != nil {
						return err
					}

					return o.Generate(c.Context, s, c.Bool("skip-upload"), box.SnapshotLockChannel(c.String("channel")))
				},
			},
		},
	}
}

// awsEndpointResolver is a stub aws.EndpointResolver that returns a static
// endpoint.
type awsEndpointResolver struct {
	endpoint string
	region   string
}

func (a *awsEndpointResolver) ResolveEndpoint(_, _ string) (aws.Endpoint, error) {
	return aws.Endpoint{
		PartitionID:       "aws",
		URL:               a.endpoint,
		HostnameImmutable: true,
		SigningRegion:     a.region,
	}, nil
}

func (o *Options) Generate(ctx context.Context, s *box.SnapshotGenerateConfig,
	skipUpload bool, channel box.SnapshotLockChannel) error { //nolint:funlen
	o.log.WithField("snapshots", len(s.Targets)).Info("Generating Snapshots")

	copts := devenvaws.DefaultCredentialOptions()
	if o.b.DeveloperEnvironmentConfig.SnapshotConfig.WriteAWSRole != "" {
		copts.Role = o.b.DeveloperEnvironmentConfig.SnapshotConfig.WriteAWSRole
	}
	copts.Log = o.log
	err := devenvaws.EnsureValidCredentials(ctx, copts)
	if err != nil {
		return errors.Wrap(err, "failed to get necessary permissions")
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region))
	if err != nil {
		return err
	}

	// use a custom endpoint if provided
	if endpoint := o.b.DeveloperEnvironmentConfig.SnapshotConfig.Endpoint; endpoint != "" {
		cfg.EndpointResolver = &awsEndpointResolver{ //nolint:staticcheck // Why: using new one doesn't work?
			endpoint: endpoint,
			region:   cfg.Region,
		}
	}

	o.log.WithField("region", cfg.Region).WithField("endpoint", endpoint).Info("s3 config")

	s3c := s3.NewFromConfig(cfg)

	lockfile := &box.SnapshotLock{}
	resp, err := s3c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:    aws.String("automated-snapshots/v2/latest.yaml"),
	})
	if err == nil {
		defer resp.Body.Close()
		err = yaml.NewDecoder(resp.Body).Decode(&lockfile)
		if err != nil {
			return errors.Wrap(err, "failed to parse remote snapshot lockfile")
		}
	} else {
		o.log.WithError(err).
			Warn("Failed to fetch existing remote snapshot lockfile, will generate a new one")
	}

	if lockfile.TargetsV2 == nil {
		lockfile.TargetsV2 = make(map[string]*box.SnapshotLockList)
	}

	for name, t := range s.Targets {
		//nolint:govet // Why: We're OK shadowing err
		itm, err := o.generateSnapshot(ctx, s3c, name, t, skipUpload)
		if err != nil {
			return err
		}

		if _, ok := lockfile.TargetsV2[name]; !ok {
			lockfile.TargetsV2[name] = &box.SnapshotLockList{}
		}

		if lockfile.TargetsV2[name].Snapshots == nil {
			lockfile.TargetsV2[name].Snapshots = make(map[box.SnapshotLockChannel][]*box.SnapshotLockListItem)
		}

		if _, ok := lockfile.TargetsV2[name].Snapshots[channel]; !ok {
			lockfile.TargetsV2[name].Snapshots[channel] = make([]*box.SnapshotLockListItem, 0)
		}

		// Make this the latest version
		lockfile.TargetsV2[name].Snapshots[channel] = append(
			[]*box.SnapshotLockListItem{itm}, lockfile.TargetsV2[name].Snapshots[channel]...,
		)
	}

	// Don't generate a lock if we're not uploading
	if skipUpload {
		return nil
	}

	lockfile.GeneratedAt = time.Now().UTC()

	byt, err := yaml.Marshal(lockfile)
	if err != nil {
		return err
	}

	_, err = s3c.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:    aws.String(filepath.Join("automated-snapshots", "v2", "latest.yaml")),
		Body:   bytes.NewReader(byt),
	})
	return err
}

//nolint:funlen,gocritic
func (o *Options) uploadSnapshot(ctx context.Context, s3c *s3.Client,
	name string, t *box.SnapshotTarget) (string, string, error) {
	tmpFile, err := os.CreateTemp("", "snapshot-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmpFile.Name())

	hash := md5.New() //nolint:gosec // Why: We're just creating a digest
	tw := tar.NewWriter(io.MultiWriter(tmpFile, hash))

	o.k, err = kube.GetKubeClient()
	if err != nil {
		return "", "", err
	}

	mc, err := snapshoter.NewSnapshotBackend(ctx, o.r, o.k)
	if err != nil {
		return "", "", err
	}

	o.log.Info("creating tar archive")
	for obj := range mc.ListObjects(ctx, noncmdsnapshot.SnapshotNamespace, minio.ListObjectsOptions{Recursive: true}) {
		// Skip empty keys
		if strings.EqualFold(obj.Key, "") {
			continue
		}

		sObj, err := mc.GetObject(ctx, noncmdsnapshot.SnapshotNamespace, obj.Key, minio.GetObjectOptions{}) //nolint:govet
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get object from local S3")
		}

		info, err := sObj.Stat()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to stat object")
		}

		err = tw.WriteHeader(&tar.Header{
			Typeflag:   tar.TypeReg,
			Name:       info.Key,
			Size:       info.Size,
			Mode:       0o755,
			ModTime:    info.LastModified,
			AccessTime: info.LastModified,
			ChangeTime: info.LastModified,
		})
		if err != nil {
			return "", "", errors.Wrap(err, "failed to write tar header")
		}

		_, err = io.Copy(tw, sObj)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to download object from local S3")
		}
	}

	// If we have post-restore manifests, then include them in the archive at a well-known
	// path for post-processing on runtime
	if t.PostRestore != "" {
		f, err := os.Open(t.PostRestore) //nolint:govet // Why: We're OK shadowing err.
		if err != nil {
			return "", "", errors.Wrap(err, "failed to open post-restore file")
		}

		inf, err := f.Stat()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to stat post-restore file")
		}

		header, err := tar.FileInfoHeader(inf, "")
		if err != nil {
			return "", "", errors.Wrap(err, "failed to create tar header")
		}
		header.Name = "post-restore/manifests.yaml"

		err = tw.WriteHeader(header)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to write tar header")
		}

		_, err = io.Copy(tw, f)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to write post-restore file to archive")
		}
	}

	if err := tw.Close(); err != nil { //nolint:govet // Why: we're OK shadowing err
		return "", "", err
	}
	if err := tmpFile.Close(); err != nil { //nolint:govet // Why: we're OK shadowing err
		return "", "", err
	}

	hashStr := base64.StdEncoding.EncodeToString(hash.Sum(nil))
	key := filepath.Join("automated-snapshots", "v2", name, strconv.Itoa(int(time.Now().UTC().UnixNano()))+".tar")

	tmpFile, err = os.Open(tmpFile.Name())
	if err != nil {
		return "", "", err
	}

	obj := &s3.PutObjectInput{
		Bucket:     &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:        &key,
		Body:       tmpFile,
		ContentMD5: &hashStr,
	}

	o.log.WithField("bucket", *obj.Bucket).WithField("key", *obj.Key).Info("uploading tar archive")
	_, err = s3c.PutObject(ctx, obj)
	if err != nil {
		return "", "", err
	}

	return hashStr, key, nil
}

//nolint:funlen
func (o *Options) generateSnapshot(ctx context.Context, s3c *s3.Client,
	name string, t *box.SnapshotTarget, skipUpload bool) (*box.SnapshotLockListItem, error) {
	o.log.WithField("snapshot", name).Info("Generating Snapshot")

	destroyOpts, err := destroy.NewOptions(o.log, o.b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create destroy command")
	}
	destroyOpts.Run(ctx) //nolint:errcheck

	os.Setenv("DEVENV_SNAPSHOT_GENERATION", "true") //nolint:errcheck

	popts, err := provision.NewOptions(o.log, o.b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create options for provision")
	}

	popts.KubernetesRuntime, err = kubernetesruntime.GetRuntime("kind")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kind runtime")
	}
	popts.Base = true

	if err := popts.Run(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to provision devenv")
	}

	if len(t.DeployApps) != 0 {
		o.log.Info("Deploying applications into devenv")
		for _, app := range t.DeployApps {
			o.log.WithField("application", app).Info("Deploying application")
			cmd := exec.CommandContext(ctx, os.Args[0], "--skip-update", "deploy-app", app) //nolint:gosec
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil { //nolint:govet // Why: We're OK shadowing err.
				return nil, errors.Wrap(err, "failed to deploy application")
			}
		}
	}

	if t.Command != "" {
		o.log.Info("Running snapshot generation command")
		err = cmdutil.RunKubernetesCommand(ctx, "", false, "/bin/bash", "-c", t.Command)
		if err != nil {
			return nil, errors.Wrap(err, "failed to run snapshot supplied command")
		}
	}

	if len(t.PostDeployApps) != 0 {
		o.log.Info("Deploying applications into devenv")
		for _, app := range t.PostDeployApps {
			o.log.WithField("application", app).Info("Deploying application")
			cmd := exec.CommandContext(ctx, os.Args[0], "--skip-update", "deploy-app", app) //nolint:gosec
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil { //nolint:govet // Why: We're OK shadowing err.
				return nil, errors.Wrap(err, "failed to deploy application")
			}
		}
	}

	// Need to create a new Kubernetes client that uses the new cluster
	o, err = NewOptions(o.log, o.b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new clients")
	}

	err = devenvutil.WaitForAllPodsToBeReady(ctx, o.k, o.log)
	if err != nil {
		return nil, err
	}

	veleroBackupName, err := o.CreateSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	hash := "unknown"
	key := "unknown"
	if !skipUpload {
		hash, key, err = o.uploadSnapshot(ctx, s3c, name, t)
		if err != nil {
			return nil, errors.Wrap(err, "failed to upload snapshot")
		}
	}

	return &box.SnapshotLockListItem{
		Digest:           hash,
		URI:              key,
		Config:           t,
		VeleroBackupName: veleroBackupName,
	}, nil
}

func (o *Options) CreateSnapshot(ctx context.Context) (string, error) { //nolint:funlen
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	updates := make(chan *velerov1api.Backup)
	backupInformer := velerov1.NewBackupInformer(o.vc, noncmdsnapshot.SnapshotNamespace, 0, nil)

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

	_, err := o.vc.VeleroV1().Backups(noncmdsnapshot.SnapshotNamespace).Create(ctx, &velerov1api.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name: backupName,
		},
		Spec: velerov1api.BackupSpec{
			// Don't include velero, we need to install it before the backup. Skip minio because it's the snapshot backend
			ExcludedNamespaces: []string{"velero", "minio"},
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
