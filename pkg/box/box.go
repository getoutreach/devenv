package box

import "time"

type SnapshotLockChannel string

const (
	SnapshotLockChannelStable SnapshotLockChannel = "stable"
	SnapshotLockChannelRC     SnapshotLockChannel = "rc"
)

type DeveloperEnvironmentConfig struct {
	// SnapshotConfig is the snapshot configuration for the devenv
	SnapshotConfig *SnapshotConfig `yaml:"snapshots"`

	// VaultConfig denotes how to talk to Vault
	VaultConfig *VaultConfig `yaml:"vault"`

	// ImagePullSecret is a path to credentials used to pull images with
	// currently the only supported value is a vault key path with
	// VaultEnabled being true
	ImagePullSecret string `yaml:"imagePullSecret"`

	// ImageRegistry is the registry to use for detecting your apps
	// e.g. gcr.io/outreach-docker
	ImageRegistry string `yaml:"imageRegistry"`
}

type VaultConfig struct {
	// Enabled determines if we should setup vault or not
	Enabled bool `yaml:"enabled"`

	// AuthMethod is the method to talk to vault, e.g. oidc
	AuthMethod string `yaml:"authMethod"`

	// Address is the URL to talk to Vault
	Address string `yaml:"address"`
}

type SnapshotConfig struct {
	// Endpoint is the S3 compatible endpoint to fetch a snapshot from
	Endpoint string `yaml:"endpoint"`

	// Region is the region to use for this bucket
	Region string `yaml:"region"`

	// Bucket is the bucket that the snapshots are in
	Bucket string `yaml:"bucket"`

	// DefaultName is the default name (snapshot) to use, e.g. flagship
	DefaultName string `yaml:"defaultName"`

	// ReadAWSRole is the role to use, if set, for saml2aws for RO access
	ReadAWSRole string `yaml:"readAWSRole"`

	// WriteAWSRole is the role to use, if set, for saml2aws for RW access
	WriteAWSRole string `yaml:"writeAWSRole"`
}

type Config struct {
	// Org is the Github org for this box, e.g. getoutreach
	Org string `yaml:"org"`

	// DeveloperEnvironmentConfig is the configuration for the developer environment for this box
	DeveloperEnvironmentConfig *DeveloperEnvironmentConfig `yaml:"devenv"`
}

// Storage is a wrapper type used for storing the box configuration
type Storage struct {
	Config *Config `yaml:"config"`

	LastUpdated time.Time `yaml:"lastUpdated"`
	StorageURL  string    `yaml:"storageURL"`
}

// NewConfig makes a full initialized Config
func NewConfig() *Config {
	return &Config{
		DeveloperEnvironmentConfig: &DeveloperEnvironmentConfig{
			SnapshotConfig: &SnapshotConfig{},
		},
	}
}

// SnapshotTarget is the defn for a generated snapshot
type SnapshotTarget struct {
	// Command is the command to be run to generate this snapshot,
	// note that a devenv is already provisioned and accessible at this
	// stage of the generation process
	Command string `yaml:"command"`

	// PostRestore is a path to a yaml file that contains pre-rendered manifests
	// These manifests will be ran through a special go-template that allows
	// injecting information like the current user / git email.
	PostRestore string `yaml:"post_restore"`

	// DeployApps is an array of applications to deploy via deploy-app
	// before running the Command specified.
	DeployApps []string `yaml:"deploy_apps"`

	// ReadyAddress is a URL to ping before marking the devenv as ready
	ReadyAddress string `yaml:"readyAddress"`
}

type SnapshotGenerateConfig struct {
	// Targets are all of the snapshots that can be generated. The key equates
	// the name of the generated snapshot
	Targets map[string]*SnapshotTarget `yaml:"targets"`
}

// SnapshotLockTarget is a generated snapshot and metadata on it
type SnapshotLockTarget struct {
	// Digest is a MD5 base64 encoded digest of the archive
	Digest string `yaml:"digest"`

	// Key is the key that this snapshot is stored at, note that the bucket is
	// not set or determined here and instead come from the snapshotconfig
	URI string `yaml:"key"`

	// Config is the config used to generate this snapshot
	Config *SnapshotTarget

	// VeleroBackupName is the name of this snapshot. This is used to invoke velero
	// commands. It should not be used for uniqueness constraints.
	VeleroBackupName string `yaml:"veleroBackupName"`
}

type SnapshotLockListItem struct {
	// Digest is a MD5 base64 encoded digest of the archive
	Digest string `yaml:"digest"`

	// Key is the key that this snapshot is stored at, note that the bucket is
	// not set or determined here and instead come from the snapshotconfig
	URI string `yaml:"key"`

	// Config is the config used to generate this snapshot
	Config *SnapshotTarget

	// VeleroBackupName is the name of this snapshot. This is used to invoke velero
	// commands. It should not be used for uniqueness constraints.
	VeleroBackupName string `yaml:"veleroBackupName"`
}
type SnapshotLockList struct {
	// Snapshots is a channel separated list of snapshots for a given target
	Snapshots map[SnapshotLockChannel][]*SnapshotLockListItem `yaml:"snapshots"`
}

// SnapshotLock is an manifest of all the available snapshots
type SnapshotLock struct {
	Version     int                            `yaml:"version"`
	GeneratedAt time.Time                      `yaml:"generatedAt"`
	Targets     map[string]*SnapshotLockTarget `yaml:"targets"`

	TargetsV2 map[string]*SnapshotLockList `yaml:"targets_v2"`
}
