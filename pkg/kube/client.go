package kube

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetKubeConfig() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".outreach", "kubeconfig.yaml"), nil
}

// GetKubeClient returns a Kubernetes client
func GetKubeClient() (kubernetes.Interface, error) {
	k, _, err := GetKubeClientWithConfig()
	return k, err
}

func GetKubeClientWithConfig() (kubernetes.Interface, *rest.Config, error) {
	var config *rest.Config
	var err error

	if config, err = rest.InClusterConfig(); err != nil {
		kubeConfPath, err := GetKubeConfig() //nolint:govet // Error shadowing is OK...
		if err != nil {
			return nil, nil, err
		}

		lr := clientcmd.NewDefaultClientConfigLoadingRules()
		lr.ExplicitPath = kubeConfPath
		apiconfig, err := lr.Load()
		if err != nil {
			return nil, nil, err
		}

		ccc := clientcmd.NewDefaultClientConfig(*apiconfig, &clientcmd.ConfigOverrides{})

		config, err = ccc.ClientConfig()
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to get kubernetes client config")
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return client, config, nil
}
