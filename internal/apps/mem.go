// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file implements an in memory storage
// interface for apps

package apps

import "context"

// InMemoryClient is an in memory client that implements
// the apps.Interface interface for usage in unit testing.
type InMemoryClient struct {
	apps map[string]App
}

// NewInMemory returns an apps.Interface satisfying client
// that uses an in memory store. Suitable for unit testing
func NewInMemory(defaultApps []App) *InMemoryClient {
	apps := make(map[string]App)
	for i := range defaultApps {
		app := &defaultApps[i]
		apps[app.Name] = *app
	}

	return &InMemoryClient{apps}
}

// List returns all known apps
func (i *InMemoryClient) List(_ context.Context) ([]App, error) {
	apps := make([]App, 0)
	for _, a := range i.apps {
		apps = append(apps, a)
	}
	return apps, nil
}

// Get returns an application, if it exists
func (i *InMemoryClient) Get(_ context.Context, name string) (App, error) {
	if app, ok := i.apps[name]; ok {
		return app, nil
	}

	return App{}, ErrNotFound
}

// Set sets the state of a deployed application
func (i *InMemoryClient) Set(_ context.Context, a *App) error {
	i.apps[a.Name] = *a
	return nil
}

// Delete deletes an application
func (i *InMemoryClient) Delete(_ context.Context, name string) error {
	delete(i.apps, name)
	return nil
}
