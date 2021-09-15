package provision

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Why: We're just doing digest checking
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cenkalti/backoff/v4"
	"github.com/getoutreach/devenv/pkg/snapshoter"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"gopkg.in/yaml.v2"
)

type localSnapshot struct {
	Name     string                    `yaml:"name"`
	Metadata *box.SnapshotLockListItem `yaml:"metadata"`
}

// fetchSnapshot finds the latest snapshot by name
// downloads it, stages it into the restore bucket, then returns the config.
// It's stored in it's own local S3 bucket because restic isn't namespaced
// which is used to store volume contents.
func (o *Options) fetchSnapshot(ctx context.Context) (*box.SnapshotLockListItem, error) { //nolint:funlen
	bucketName := snapshotLocalBucket

	m, err := snapshoter.NewSnapshotBackend(ctx, o.r, o.k)
	if err != nil {
		return nil, err
	}

	for ctx.Err() == nil {
		exists, err := m.BucketExists(ctx, bucketName) //nolint:govet // Why: We're OK shadowing err
		if exists {
			break
		}

		o.log.WithError(err).Info("Waiting for snapshot infra to be ready")
		async.Sleep(ctx, 10*time.Second)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region))
	if err != nil {
		return nil, errors.Wrap(err, "unable to load SDK config")
	}

	s3client := s3.NewFromConfig(cfg)
	resp, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:    aws.String("automated-snapshots/v2/latest.yaml"),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the latest snapshot information")
	}
	defer resp.Body.Close()

	var lockfile *box.SnapshotLock
	err = yaml.NewDecoder(resp.Body).Decode(&lockfile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse remote snapshot lockfile")
	}

	if _, ok := lockfile.TargetsV2[o.SnapshotTarget]; !ok {
		return nil, fmt.Errorf("unknown snapshot target '%s'", o.SnapshotTarget)
	}

	if _, ok := lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel]; !ok {
		return nil, fmt.Errorf("unknown snapshot channel '%s'", o.SnapshotChannel)
	}

	if len(lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel]) == 0 {
		return nil, fmt.Errorf("no snapshots found for channel '%s'", o.SnapshotChannel)
	}

	latestSnapshotFile := lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel][0]

	if currentResp, err2 := m.GetObject(ctx, bucketName, "current.yaml", minio.GetObjectOptions{}); err2 == nil {
		var current *localSnapshot
		err2 = yaml.NewDecoder(currentResp).Decode(&current)
		if err2 == nil {
			if current.Name == o.SnapshotTarget && current.Metadata.Digest == latestSnapshotFile.Digest {
				o.log.Info("Using already downloaded snapshot")
				return current.Metadata, nil
			}
		}
	}

	// If we're at this point, ensure that the bucket we want is empty
	o.log.Info("preparing local storage for snapshot")
	for obj := range m.ListObjects(ctx, bucketName, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Key == "" {
			continue
		}

		o.log.WithField("key", obj.Key).Debug("removing old snapshot file")
		err2 := m.RemoveObject(ctx, bucketName, obj.Key, minio.RemoveObjectOptions{})
		if err2 != nil {
			o.log.WithError(err2).WithField("key", obj.Key).Warn("failed to remove old snapshot key")
		}
	}

	return latestSnapshotFile, o.uploadFilesFromArchive(ctx, m, bucketName, latestSnapshotFile, s3client)
}

func (o *Options) downloadArchive(ctx context.Context, snapshot *box.SnapshotLockListItem, s3client *s3.Client) (*os.File, error) { //nolint:funlen
	obj, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:    &snapshot.URI,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the latest snapshot information")
	}
	defer obj.Body.Close()

	bar := progressbar.DefaultBytes(
		obj.ContentLength,
		"downloading snapshot",
	)

	tmpFile, err := os.CreateTemp("", "devenv-snapshot-*")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temporary file")
	}

	tmpFile.Close()           //nolint:errcheck // Why: Best effort
	os.Remove(tmpFile.Name()) //nolint:errcheck // Why: Best effort

	err = os.MkdirAll(filepath.Dir(tmpFile.Name()), 0755)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temporary directory")
	}

	f, err := os.Create(tmpFile.Name())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temporary file")
	}

	digest := md5.New() //nolint:gosec // Why: we're just checking the digest

	_, err = io.Copy(io.MultiWriter(bar, f, digest), obj.Body)
	f.Close()
	if err != nil {
		return nil, errors.Wrap(err, "failed to write file")
	}

	gotMD5 := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	if gotMD5 != snapshot.Digest {
		return nil, fmt.Errorf("downloaded snapshot failed checksum validation")
	}

	f, err = os.Open(tmpFile.Name())
	if err != nil {
		return nil, errors.Wrap(err, "failed to open temporary file")
	}

	return f, err
}

// uploadFilesFromArchive uploads the files from a given tar.xz archive
// into our local S3 bucket
func (o *Options) uploadFilesFromArchive(ctx context.Context, m *snapshoter.SnapshotBackend, bucketName string, snapshot *box.SnapshotLockListItem, s3client *s3.Client) error { //nolint:funlen
	t := backoff.WithMaxRetries(backoff.WithContext(backoff.NewExponentialBackOff(), ctx), 5)

	var f *os.File
	for {
		var err error
		f, err = o.downloadArchive(ctx, snapshot, s3client)
		if err == nil {
			break
		}

		waitTime := t.NextBackOff()
		if waitTime == backoff.Stop { // this is hit when max attempts or context is canceled
			return fmt.Errorf("reached maximum attempts")
		}
		o.log.WithError(err).Warnf("failed to download archive, waiting to try again: %s", waitTime)

		time.Sleep(waitTime)
	}

	info, err := f.Stat()
	if err != nil {
		return err
	}

	bar := progressbar.DefaultBytes(
		info.Size(),
		"extracting snapshot",
	)

	tarReader := tar.NewReader(io.TeeReader(f, bar))

	for {
		header, err := tarReader.Next() //nolint:govet // Why: OK shadowing err
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "failed to read tar header")
		}

		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			fileName := strings.TrimPrefix(header.Name, "./")
			o.log.WithField("fileName", fileName).Debug("extracting and uploading to local bucket file")
			_, err := m.PutObject(ctx, bucketName,
				fileName, tarReader, header.Size, minio.PutObjectOptions{
					SendContentMd5: true,
				})
			if err != nil {
				return errors.Wrapf(err, "failed to upload file '%s'", fileName)
			}
		}
	}

	currentYaml, err := yaml.Marshal(localSnapshot{
		Name:     o.SnapshotTarget,
		Metadata: snapshot,
	})
	if err != nil {
		return err
	}
	currentSnapshot := bytes.NewReader(currentYaml)

	_, err = m.PutObject(ctx, bucketName, "current.yaml", currentSnapshot, currentSnapshot.Size(), minio.PutObjectOptions{})
	m.Close()
	return errors.Wrap(err, "failed to set current snapshot")
}
