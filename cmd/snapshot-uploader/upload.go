package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Why: just using for digest checking
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/pkg/snapshot"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type localSnapshot struct {
	Digest string `yaml:"digest"`
}

type SnapshotUploader struct {
	conf *snapshot.Config

	source *minio.Client
	dest   *minio.Client
	log    logrus.FieldLogger

	downloadedFile *os.File
}

type step func(context.Context) error

// StartFromEnv reads configuration from the environment
// and starts an upload
func (s *SnapshotUploader) StartFromEnv(ctx context.Context, log logrus.FieldLogger) error {
	conf := &snapshot.Config{}
	if err := json.Unmarshal([]byte(os.Getenv("CONFIG")), &conf); err != nil {
		return errors.Wrap(err, "failed to parse config from CONFIG")
	}
	s.conf = conf
	s.log = log

	steps := []step{s.CreateClients, s.Prepare, s.DownloadFile, s.UploadArchiveContents}
	for _, fn := range steps {
		err := fn(ctx)
		if err != nil {
			fnName := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
			return errors.Wrapf(err, "failed to run step %s", fnName)
		}
	}

	return nil
}

// CreateClients creates the S3 clients for our dest and source
func (s *SnapshotUploader) CreateClients(ctx context.Context) error {
	var err error
	s.source, err = minio.New(s.conf.Source.S3Host, &minio.Options{
		Creds:  credentials.NewStaticV4(s.conf.Source.AWSAccessKey, s.conf.Source.AWSSecretKey, s.conf.Source.AWSSessionToken),
		Secure: true,
		Region: s.conf.Source.Region,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create source s3 client")
	}

	s.dest, err = minio.New(s.conf.Dest.S3Host, &minio.Options{
		Creds:  credentials.NewStaticV4(s.conf.Dest.AWSAccessKey, s.conf.Dest.AWSSecretKey, s.conf.Dest.AWSSessionToken),
		Secure: false,
		Region: s.conf.Dest.Region,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create dest s3 client")
	}

	return nil
}

// Prepare checks if a snapshot needs to be downloaded or not
// and otherwise prepares the dest to receive a snapshot.
func (s *SnapshotUploader) Prepare(ctx context.Context) error {
	if currentResp, err := s.dest.GetObject(ctx, s.conf.Dest.Bucket, "current.yaml", minio.GetObjectOptions{}); err == nil {
		var current *localSnapshot
		err = yaml.NewDecoder(currentResp).Decode(&current)
		if err == nil {
			if current.Digest == s.conf.Source.Digest {
				s.log.Info("Using already downloaded snapshot")
				return nil
			}
		}
	}

	s.log.Info("preparing local storage for snapshot")
	for obj := range s.dest.ListObjects(ctx, s.conf.Dest.Bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Key == "" {
			continue
		}

		s.log.WithField("key", obj.Key).Info("removing old snapshot file")
		err2 := s.dest.RemoveObject(ctx, s.conf.Dest.Bucket, obj.Key, minio.RemoveObjectOptions{})
		if err2 != nil {
			s.log.WithError(err2).WithField("key", obj.Key).Warn("failed to remove old snapshot key")
		}
	}

	return nil
}

// DownloadFile downloads a file from a given URL and returns the path to it
func (s *SnapshotUploader) DownloadFile(ctx context.Context) error { //nolint:funlen
	s.log.Info("Starting download")
	obj, err := s.source.GetObject(ctx, s.conf.Source.Bucket, s.conf.Source.Key, minio.GetObjectOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to fetch the latest snapshot information")
	}
	defer obj.Close()

	tmpFile, err := os.CreateTemp("", "devenv-snapshot-*")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary file")
	}

	tmpFile.Close()           //nolint:errcheck // Why: Best effort
	os.Remove(tmpFile.Name()) //nolint:errcheck // Why: Best effort

	err = os.MkdirAll(filepath.Dir(tmpFile.Name()), 0755)
	if err != nil {
		return errors.Wrap(err, "failed to create temporary directory")
	}

	f, err := os.Create(tmpFile.Name())
	if err != nil {
		return errors.Wrap(err, "failed to create temporary file")
	}

	digest := md5.New() //nolint:gosec // Why: we're just checking the digest
	_, err = io.Copy(io.MultiWriter(f, digest), obj)
	f.Close()
	if err != nil {
		return errors.Wrap(err, "failed to write file")
	}
	s.log.Info("Finished download snapshot")

	gotMD5 := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	if gotMD5 != s.conf.Source.Digest {
		return fmt.Errorf("downloaded snapshot failed checksum validation")
	}

	f, err = os.Open(tmpFile.Name())
	if err != nil {
		return errors.Wrap(err, "failed to open temporary file")
	}
	s.downloadedFile = f

	return nil
}

// UploadArchiveContents uploads a given archive's contents into
// the configured destination bucket.
func (s *SnapshotUploader) UploadArchiveContents(ctx context.Context) error {
	s.log.Info("Extracting snapshot into minio bucket")
	tarReader := tar.NewReader(s.downloadedFile)
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
			_, err := s.dest.PutObject(ctx, s.conf.Dest.Bucket,
				fileName, tarReader, header.Size, minio.PutObjectOptions{
					SendContentMd5: true,
				})
			if err != nil {
				return errors.Wrapf(err, "failed to upload file '%s'", fileName)
			}
		}
	}
	s.log.Info("Finished extracting snapshot")

	s.log.Info("Writing snapshot state to minio")
	defer s.log.Info("Finished writing snapshot state")
	currentYaml, err := yaml.Marshal(localSnapshot{
		Digest: s.conf.Source.Digest,
	})
	if err != nil {
		return err
	}
	currentSnapshot := bytes.NewReader(currentYaml)
	_, err = s.dest.PutObject(ctx, s.conf.Dest.Bucket, "current.yaml", currentSnapshot, currentSnapshot.Size(), minio.PutObjectOptions{})
	return errors.Wrap(err, "failed to set current snapshot")
}
