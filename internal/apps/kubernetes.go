// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file implements a Kubernetes configmap
// store for the apps.Interface interface.

package apps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubernetesConfigmapClient is a Kubernetes backed client
// that implements the apps.Interface interface for storing
// information about apps.
type KubernetesConfigmapClient struct {
	k             kubernetes.Interface
	namespace     string
	configmapName string
}

// NewKubernetesConfigmapClient returns an initialized
// KubernetesConfigmapClient/ If namespace is not set it
// is defaulted to "devenv"
func NewKubernetesConfigmapClient(k kubernetes.Interface, namespace string) *KubernetesConfigmapClient {
	if namespace == "" {
		namespace = "devenv"
	}

	return &KubernetesConfigmapClient{k, namespace, "apps"}
}

// parseConfigmap reads the configmap from Kubernetes. If not found
// an empty map is returned.
func (k *KubernetesConfigmapClient) parseConfigmap(ctx context.Context) (map[string]App, error) {
	c, err := k.k.CoreV1().ConfigMaps(k.namespace).Get(ctx, k.configmapName, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return make(map[string]App), nil
	} else if err != nil {
		return nil, err
	}

	apps := make(map[string]App)
	for appName, content := range c.Data {
		var app App
		if err := json.NewDecoder(strings.NewReader(content)).Decode(&app); err != nil {
			return nil, errors.Wrapf(err, "failed to read apps entry '%s'", appName)
		}

		apps[app.Name] = app
	}

	return apps, nil
}

// serializeConfigmap takes the provided apps and serializes it into a configmap
func (k *KubernetesConfigmapClient) serializeConfigmap(ctx context.Context, apps map[string]App) error {
	serializedData := make(map[string]string)
	for _, a := range apps {
		b, err := json.Marshal(a)
		if err != nil {
			return errors.Wrapf(err, "failed to encode '%s' data to json", a.Name)
		}
		serializedData[a.Name] = string(b)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: k.configmapName,
		},
		Data: serializedData,
	}

	exists := false
	if _, err := k.k.CoreV1().ConfigMaps(k.namespace).Get(ctx, cm.Name, metav1.GetOptions{}); err == nil {
		exists = true
	}

	var err error
	if exists {
		_, err = k.k.CoreV1().ConfigMaps(k.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	} else {
		_, err = k.k.CoreV1().ConfigMaps(k.namespace).Create(ctx, cm, metav1.CreateOptions{})
	}

	return err
}

// List returns all known apps
func (k *KubernetesConfigmapClient) List(ctx context.Context) ([]App, error) {
	apps, err := k.parseConfigmap(ctx)
	if err != nil {
		return nil, err
	}

	// convert from map[string]App -> []App
	appsList := make([]App, 0)
	for _, a := range apps {
		appsList = append(appsList, a)
	}
	return appsList, nil
}

// Get returns an application, if it exists
func (k *KubernetesConfigmapClient) Get(ctx context.Context, name string) (App, error) {
	apps, err := k.parseConfigmap(ctx)
	if err != nil {
		return App{}, err
	}

	if app, ok := apps[name]; ok {
		return app, nil
	}
	return App{}, ErrNotFound
}

// Set sets the state of a deployed application
func (k *KubernetesConfigmapClient) Set(ctx context.Context, a *App) error {
	apps, err := k.parseConfigmap(ctx)
	if err != nil {
		return err
	}
	apps[a.Name] = *a
	return k.serializeConfigmap(ctx, apps)
}

// Delete deletes an application, if it exists
func (k *KubernetesConfigmapClient) Delete(ctx context.Context, name string) error {
	apps, err := k.parseConfigmap(ctx)
	if err != nil {
		return err
	}

	if _, ok := apps[name]; ok {
		delete(apps, name)
		return k.serializeConfigmap(ctx, apps)
	}

	return ErrNotFound
}
