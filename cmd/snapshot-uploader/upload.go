package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

	"github.com/getoutreach/devenv/internal/snapshot"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type localSnapshot struct {
	Digest string `yaml:"digest"`
}

type SnapshotUploader struct {
	conf *snapshot.Config

	source *minio.Client
	dest   *minio.Client
	log    logrus.FieldLogger
	k      kubernetes.Interface

	// set after fetched
	snapshot *box.SnapshotLockListItem

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

	// Default to S3 if no endpoint
	if conf.Source.S3Host == "" {
		conf.Source.S3Host = "https://s3.amazonaws.com"
	}

	if conf.Dest.S3Host == "" {
		conf.Dest.S3Host = "https://s3.amazonaws.com"
	}

	s.conf = conf
	s.log = log

	steps := []step{s.CreateClients, s.Discover, s.Prepare, s.DownloadFile,
		s.UploadArchiveContents, s.ExtractPostRestore}
	for _, fn := range steps {
		err := fn(ctx)
		if err != nil {
			fnName := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
			return errors.Wrapf(err, "failed to run step %s", fnName)
		}
	}

	return nil
}

// removeProtocol removes http+s from a given URL
func removeProtocol(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
}

// CreateClients creates the S3 clients for our dest and source
func (s *SnapshotUploader) CreateClients(ctx context.Context) error {
	s.log.Info("Creating snapshot clients")
	var err error

	// create source options
	sourceOpts := &minio.Options{
		Creds:  credentials.NewStaticV4(s.conf.Source.AWSAccessKey, s.conf.Source.AWSSecretKey, s.conf.Source.AWSSessionToken),
		Region: s.conf.Source.Region,
	}
	if strings.HasPrefix(s.conf.Source.S3Host, "https") {
		sourceOpts.Secure = true
	}
	s.conf.Source.S3Host = removeProtocol(s.conf.Source.S3Host)

	// create dest options
	destOpts := &minio.Options{
		Creds:  credentials.NewStaticV4(s.conf.Dest.AWSAccessKey, s.conf.Dest.AWSSecretKey, s.conf.Dest.AWSSessionToken),
		Secure: false,
		Region: s.conf.Dest.Region,
	}
	if strings.HasPrefix(s.conf.Dest.S3Host, "https") {
		destOpts.Secure = true
	}
	s.conf.Dest.S3Host = removeProtocol(s.conf.Dest.S3Host)

	// create the clients
	s.source, err = minio.New(s.conf.Source.S3Host, sourceOpts)
	if err != nil {
		return errors.Wrap(err, "failed to create source s3 client")
	}

	s.dest, err = minio.New(s.conf.Dest.S3Host, destOpts)
	if err != nil {
		return errors.Wrap(err, "failed to create dest s3 client")
	}

	s.k, err = kube.GetKubeClient()
	if err != nil {
		return errors.Wrap(err, "failed to create kubernetes client")
	}
	return nil
}

// Discover implements snapshot discovery. This finds the latest snapshot
// from a given S3 bucket.
func (s *SnapshotUploader) Discover(ctx context.Context) error {
	if s.conf.Source.Key != "" {
		s.log.Info("Using snapshot at %s", s.conf.Source.Key)
		return nil
	}

	s.log.Info("Discovering snapshots")
	resp, err := s.source.GetObject(ctx, s.conf.Source.Bucket,
		"automated-snapshots/v2/latest.yaml", minio.GetObjectOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to fetch the latest snapshot information")
	}
	defer resp.Close()

	var lockfile *box.SnapshotLock
	err = yaml.NewDecoder(resp).Decode(&lockfile)
	if err != nil {
		return errors.Wrap(err, "failed to parse remote snapshot lockfile")
	}

	target := s.conf.Source.SnapshotTarget
	channel := s.conf.Source.SnapshotChannel

	if _, ok := lockfile.TargetsV2[target]; !ok {
		return fmt.Errorf("unknown snapshot target '%s'", s.conf.Source.SnapshotTarget)
	}

	if _, ok := lockfile.TargetsV2[target].Snapshots[channel]; !ok {
		return fmt.Errorf("unknown snapshot channel '%s'", channel)
	}

	if len(lockfile.TargetsV2[target].Snapshots[channel]) == 0 {
		return fmt.Errorf("no snapshots found for channel '%s'", channel)
	}

	// 0-index is the latest
	snapshotTarget := lockfile.TargetsV2[target].Snapshots[channel][0]

	targetJSON, err := json.Marshal(snapshotTarget)
	if err != nil {
		s.log.Infof("Using snapshot: %v", snapshotTarget)
	} else {
		s.log.Infof("Using snapshot: %s", targetJSON)
	}

	s.snapshot = snapshotTarget
	s.conf.Source.Key = snapshotTarget.URI
	s.conf.Source.Digest = snapshotTarget.Digest

	return nil
}

// Prepare checks if a snapshot needs to be downloaded or not
// and otherwise prepares the dest to receive a snapshot.
func (s *SnapshotUploader) Prepare(ctx context.Context) error {
	s.log.Info("Getting current snapshot information")
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

	s.log.Info("Preparing local storage for snapshot")
	for obj := range s.dest.ListObjects(ctx, s.conf.Dest.Bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Key == "" {
			continue
		}

		s.log.WithField("key", obj.Key).Info("Removing old snapshot file")
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

	err = os.MkdirAll(filepath.Dir(tmpFile.Name()), 0o755)
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

func (s *SnapshotUploader) getPostRestoreManifests(ctx context.Context, postRestorePath string) ([]byte, error) {
	// compress because of 1MB limit, like Helm does.
	s.log.Info("Compressing post-restore manifests")
	obj, err := s.dest.GetObject(ctx, s.conf.Dest.Bucket, postRestorePath, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil
	}
	defer obj.Close()

	var postRestoreBuf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&postRestoreBuf, gzip.BestCompression)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gzip writer")
	}

	if _, err := io.Copy(gzw, obj); err != nil {
		return nil, errors.Wrap(err, "failed to compress post-restore manifests")
	}

	if err := gzw.Close(); err != nil {
		return nil, errors.Wrap(err, "failed to flush gzip writer")
	}

	s.log.Info("Finished compressing post-restore manifests")
	return postRestoreBuf.Bytes(), nil
}

// ExtractPostRestore extracts the post-restore manifests out of S3 and inserts
// them into Kubernetes.
func (s *SnapshotUploader) ExtractPostRestore(ctx context.Context) error {
	s.log.Info("Generating snapshot state in Kubernetes")

	postRestorePath := strings.TrimPrefix(s.snapshot.Config.PostRestore, "./")
	postRestoreManifests, err := s.getPostRestoreManifests(ctx, postRestorePath)
	if err != nil {
		return errors.Wrap(err, "failed to get post-restore manifests")
	}

	serializedSnapshot, err := json.Marshal(s.snapshot)
	if err != nil {
		return errors.Wrap(err, "failed to serialize snapshot")
	}

	data := map[string]string{
		"snapshot.json": string(serializedSnapshot),
	}

	if postRestoreManifests != nil {
		data["post-restore.yaml"] = base64.StdEncoding.EncodeToString(postRestoreManifests)

		s.log.Info("Cleaning up post-restore artifacts")
		//nolint:errcheck // Why: best effort
		s.dest.RemoveObject(ctx, s.conf.Dest.Bucket, postRestorePath, minio.RemoveObjectOptions{})
	}

	s.log.Info("Creating 'snapshot' configmap")
	_, err = s.k.CoreV1().ConfigMaps("devenv").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snapshot",
		},
		Data: data,
	}, metav1.CreateOptions{})
	if err != nil {
		s.log.WithError(err).Error("Failed to create snapshot configmap")
		return errors.Wrap(err, "failed to create snapshot configmap")
	}

	s.log.Info("Finished setting up Kubernetes snapshot state")
	return nil
}
