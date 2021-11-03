// Package kubernetesruntime stores kubernetes cluster
// providers which implement a common interface for interaction
// with them.
package kubernetesruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd/api"
)

var (
	ErrNotFound   = errors.New("runtime not found")
	ErrNotRunning = errors.New("no runtime is running")
)

// RuntimeType dictates what type of runtime this kubernetes runtime
// implements.
type RuntimeType string

const (
	// RuntimeTypeLocal is a local kubernetes cluster that
	// runs directly on this computer, e.g. in a Docker for X
	// compatible VM or directly on the host via Docker.
	RuntimeTypeLocal RuntimeType = "local"

	// RunetimeTypeRemote is a remote kubernetes cluster
	// that has no connection other than APIServer with
	// this PC.
	RuntimeTypeRemote RuntimeType = "remote"
)

// RuntimeConfig is a config returned by a runtim
type RuntimeConfig struct {
	// Name is the name of this runtime
	Name string

	// Type is the type of runtime this is
	Type RuntimeType

	// ClusterName is the name of the cluster this runtime creates
	ClusterName string
}

// RuntimeCluster is a cluster that is currently provisioned / accessible by a given
// runtime.
type RuntimeCluster struct {
	// RuntimeName is the name of the runtime that created this cluster
	RuntimeName string

	// Name is the name of this cluster.
	Name string

	// KubeConfig is the kubeconfig that allows access to this cluster.
	KubeConfig *api.Config
}

// RuntimeStatus is the status of a given runtime
type RuntimeStatus struct {
	status.Status
}

// Runtime is the Kubernetes Runtime interface that all
// runtimes should implement.
type Runtime interface {
	// GetConfig returns the configuration of a runtime
	GetConfig() RuntimeConfig

	// Status returns the status of a given runtime.
	Status(context.Context) RuntimeStatus

	// Create creates a new Kubernetes cluster using this runtime
	Create(context.Context) error

	// Destroy destroys a kubernetes cluster from this runtime
	Destroy(context.Context) error

	// Stop stops (sleeps) a kubernetes cluster from this runtime
	Stop(context.Context) error

	// Start starts (awakens) a kubernetes cluster from this runtime
	Start(context.Context) error

	// PreCreate is ran before creating a kubernetes cluster, useful
	// for implementing pre-requirements.
	PreCreate(context.Context) error

	// Configure is ran first to configure the runtime with it's
	// dependencies.
	Configure(logrus.FieldLogger, *box.Config)

	// GetKubeConfig returns the kube conf for the active cluster
	// created by this runtime.
	GetKubeConfig(context.Context) (*api.Config, error)

	// GetClusters returns all clusters currently accessible by this runtime
	GetClusters(context.Context) ([]*RuntimeCluster, error)
}

var runtimes = []Runtime{NewLoftRuntime(), NewKindRuntime()}

// GetRuntime returns a runtime by name, if not found
// nil is returned
func GetRuntime(name string) (Runtime, error) {
	for _, r := range runtimes {
		if r.GetConfig().Name == name {
			return r, nil
		}
	}

	return nil, ErrNotFound
}

// GetRuntimes returns all registered runtimes. Generally
// GetEnabledRuntimes should be used over this.
func GetRuntimes() []Runtime {
	return runtimes
}

// GetEnabledRuntimes returns a list of enabled runtimes
// based on a given box configuration
func GetEnabledRuntimes(b *box.Config) []Runtime {
	selectedRuntimes := make([]Runtime, 0)
	for _, r := range runtimes {
		for _, enabled := range b.DeveloperEnvironmentConfig.RuntimeConfig.EnabledRuntimes {
			if enabled == r.GetConfig().Name {
				selectedRuntimes = append(selectedRuntimes, r)
			}
		}
	}
	return selectedRuntimes
}

// GetRuntimeFromContext returns the runtime from the current config.Context
func GetRuntimeFromContext(conf *config.Config, b *box.Config) (Runtime, error) {
	if conf == nil {
		return nil, fmt.Errorf("no config provided")
	}

	if conf.CurrentContext == "" {
		return nil, fmt.Errorf("no context was set in the config")
	}

	runtime, _ := conf.ParseContext()
	if runtime == "" {
		return nil, fmt.Errorf("failed to parse context")
	}

	runtimes := GetEnabledRuntimes(b)
	for _, r := range runtimes {
		if r.GetConfig().Name == runtime {
			return r, nil
		}
	}

	return nil, fmt.Errorf("failed to find enabled runtime named '%s'", runtime)
}
