package snapshot

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Why: Verifiying archives
	"encoding/base64"
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
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	devenvaws "github.com/getoutreach/devenv/pkg/aws"
	"github.com/getoutreach/devenv/pkg/box"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/snapshoter"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

func (o *Options) Generate(ctx context.Context, s *box.SnapshotGenerateConfig) error { //nolint:funlen
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	o.log.WithField("snapshots", len(s.Targets)).Info("Generating Snapshots")

	mc, err := minio.New("127.0.0.1:61002", &minio.Options{
		Creds:  credentials.NewStaticV4(snapshoter.MinioAccessKey, snapshoter.MinioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return err
	}

	// TODO: We need to allow this to be changed
	copts := devenvaws.DefaultCredentialOptions()
	if b.DeveloperEnvironmentConfig.SnapshotConfig.WriteAWSRole != "" {
		copts.Role = b.DeveloperEnvironmentConfig.SnapshotConfig.WriteAWSRole
	}
	copts.Log = o.log
	err = devenvaws.EnsureValidCredentials(ctx, copts)
	if err != nil {
		return errors.Wrap(err, "failed to get neccesssary permissions")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	cfg.Region = b.DeveloperEnvironmentConfig.SnapshotConfig.Region

	s3c := s3.NewFromConfig(cfg)

	generatedTargets := make(map[string]*box.SnapshotLockTarget)
	for name, t := range s.Targets {
		//nolint:govet // Why: We're OK shadowing err
		var err error
		generatedTargets[name], err = o.generateSnapshot(ctx, mc, s3c, name, t)
		if err != nil {
			return err
		}
	}

	lock := &box.SnapshotLock{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Targets:     generatedTargets,
	}

	byt, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}

	_, err = s3c.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("outreach-devenv-snapshots"),
		Key:    aws.String(filepath.Join("automated-snapshots", "v2", "latest.yaml")),
		Body:   bytes.NewReader(byt),
	})
	return err
}

func (o *Options) uploadSnapshot(ctx context.Context, mc *minio.Client, s3c *s3.Client, name string, t *box.SnapshotTarget) (string, string, error) { //nolint:funlen,gocritic
	tmpFile, err := os.CreateTemp("", "snapshot-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmpFile.Name())

	hash := md5.New() //nolint:gosec // Why: We're just creating a digest
	tw := tar.NewWriter(io.MultiWriter(tmpFile, hash))

	o.log.Info("creating tar archive")
	for obj := range mc.ListObjects(ctx, SnapshotNamespace, minio.ListObjectsOptions{Recursive: true}) {
		// Skip empty keys
		if strings.EqualFold(obj.Key, "") {
			continue
		}

		sObj, err := mc.GetObject(ctx, SnapshotNamespace, obj.Key, minio.GetObjectOptions{}) //nolint:govet
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
			Mode:       0755,
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

	o.log.Info("uploading tar archive")
	_, err = s3c.PutObject(ctx, &s3.PutObjectInput{
		Bucket:     aws.String("outreach-devenv-snapshots"),
		Key:        &key,
		Body:       tmpFile,
		ContentMD5: &hashStr,
	})
	if err != nil {
		return "", "", err
	}

	return hashStr, key, nil
}

//nolint:funlen
func (o *Options) generateSnapshot(ctx context.Context, mc *minio.Client, s3c *s3.Client,
	name string, t *box.SnapshotTarget) (*box.SnapshotLockTarget, error) {
	o.log.WithField("snapshot", name).Info("Generating Snapshot")

	destroyOpts, err := destroy.NewOptions(o.log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create destroy command")
	}

	destroyOpts.RemoveImageCache = true
	destroyOpts.RemoveSnapshotStorage = true
	destroyOpts.Run(ctx) //nolint:errcheck

	// using exec because of an import cycle, need to fix
	err = cmdutil.RunKubernetesCommand(ctx, "", false, os.Args[0], "provision", "--base")
	if err != nil {
		return nil, errors.Wrap(err, "failed to provision developer environment")
	}

	if len(t.DeployApps) != 0 {
		o.log.Info("Deploying applications into devenv")
		for _, app := range t.DeployApps {
			o.log.WithField("application", app).Info("Deploying application")
			cmd := exec.CommandContext(ctx, os.Args[0], "deploy-app", app) //nolint:gosec
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

	// Need to create a new Kubernetes client that uses the new cluster
	o, err = NewOptions(o.log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new clients")
	}

	// TODO: Velero gets really mad if you create a backup before it's ready
	// and will just hang. Need to write the code to actually wait for this instead of waiting 5 mins (usually way too long)
	o.log.Info("Waiting for snapshot infrastructure to be ready")
	time.Sleep(5 * time.Minute)

	veleroBackupName, err := o.CreateSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	hash, key, err := o.uploadSnapshot(ctx, mc, s3c, name, t)
	if err != nil {
		return nil, errors.Wrap(err, "failed to upload snapshot")
	}

	return &box.SnapshotLockTarget{
		Digest:           hash,
		URI:              key,
		Config:           t,
		VeleroBackupName: veleroBackupName,
	}, nil
}
