// Package kubernetesruntime stores kubernetes cluster
// providers which implement a common interface for interaction
// with them.
package kubernetesruntime

import (
	"context"
	"errors"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd/api"
)

var (
	ErrNotFound = errors.New("runtime not found")
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

	Type RuntimeType
}

// RuntimeStatus is the status of a given runtime
type RuntimeStatus struct {
	status.Status
}

type Runtime interface {
	GetConfig() RuntimeConfig
	Status(context.Context, logrus.FieldLogger) RuntimeStatus
	Create(context.Context, logrus.FieldLogger) error
	Destroy(context.Context, logrus.FieldLogger) error
	GetKubeConfig(context.Context) (*api.Config, error)
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

func GetRuntimes() []Runtime {
	return runtimes
}
