// Package snapshot stores shared code between devenv and
// the snapshot-uploader
package snapshot

import "github.com/getoutreach/gobox/pkg/box"

// S3Config is configuration for accessing an object, or path
// in S3.
type S3Config struct {
	// AWSAccessKey is the access key to use
	AWSAccessKey string `json:"aws_access_key"`

	// AWSSecretKey is the secret key to use
	AWSSecretKey string `json:"aws_secret_key"`

	// AWSSessionToken is the session token to use
	AWSSessionToken string `json:"aws_session_token,omitempty"`

	// S3Host is the host to use for connecting to S3
	S3Host string `json:"s3_host"`

	// Bucket is the bucket to use
	Bucket string `json:"s3_bucket"`

	// Region is the region of this bucket
	Region string `json:"region"`

	// Key is the key to use when accessing S3, either an object
	// or a path depending on the expected input.
	Key string `json:"s3_key"`

	// SnapshotTarget is the target snapshot to use if in source, if in dest
	// ignored.
	SnapshotTarget string `json:"snapshot_target,omitempty"`

	// SnapshotChannel is the target channel to use if in source, if in dest
	// ignored.
	SnapshotChannel box.SnapshotLockChannel `json:"snapshot_channel,omitempty"`

	// Digest is an optional digest to use when validating an object
	Digest string `json:"s3_md5_hash,omitempty"`
}

type Config struct {
	// Source is the configuration for downloading the snapshot
	Source *S3Config `json:"source"`

	// Dest is the configuration for extracting the snapshot
	Dest *S3Config `json:"dest"`
}
