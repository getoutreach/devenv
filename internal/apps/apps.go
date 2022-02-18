// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file implements the core of the apps logic
// storing, listing, and adding new applications to the current
// developer environment

// Package apps contains all logic for interacting with apps
// inside of a devenv. This entails discovering the versions
// of currently installed applications and adding a new
// application. This does NOT contain deployapp functionality
// which is currently in pkg/app instead.
package apps

import (
	"context"
	"errors"
	"time"
)

// This block contains typed errors
var (
	// ErrNotFound denotes that an application was not found
	ErrNotFound = errors.New("Application not found")
)

// Interface is an interface for interacting with apps in a devenv
type Interface interface {
	// List lists all known apps inside of a devenv
	List(ctx context.Context) ([]App, error)

	// Get returns information about an application if it exists in
	// a devenv.
	Get(ctx context.Context, name string) (App, error)

	// Set sets information about an application in the devenv
	Set(ctx context.Context, a *App) error

	// Delete deletes an application in the devenv
	Delete(ctx context.Context, name string) error

	// Reset deletes all application infomation and the underlying
	// datastore.
	Reset(ctx context.Context) error
}

// App is an application inside of a devenv
type App struct {
	// Name is the name of the application, this should be
	// 1:1 with the Github repository.
	Name string `json:"name" yaml:"name"`

	// Version is the currently deployed version of an application.
	// This should generally be semver and may be forced to be
	Version string `json:"version" yaml:"version"`

	// DeployedAt was when this app was deployed
	DeployedAt time.Time `json:"deployed_at" yaml:"deployedAt"`
}
