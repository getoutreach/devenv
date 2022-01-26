// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: Contains tests of the apps package

package apps_test

import (
	"context"
	"testing"

	"github.com/getoutreach/devenv/internal/apps"
	"gotest.tools/v3/assert"
)

// TestBasicFlow tests the basic functionality of the in memory
// client as well as the interface
func TestBasicFlow(t *testing.T) {
	ctx := context.Background()

	var c apps.Interface = apps.NewInMemory(nil)

	foundApps, err := c.List(ctx)
	assert.NilError(t, err, "expected List() to not error")
	assert.Equal(t, len(foundApps), 0, "expected List() to be empty")

	setApp := &apps.App{Name: "my-cool-app", Version: "v1.0.0"}
	err = c.Set(ctx, setApp)
	assert.NilError(t, err, "expected Set() to not error")

	foundApps, err = c.List(ctx)
	assert.NilError(t, err, "expected List() to not error")
	assert.Equal(t, len(foundApps), 1, "expected List() to be 1")

	foundApp, err := c.Get(ctx, setApp.Name)
	assert.NilError(t, err, "expected Get() to not error")
	assert.DeepEqual(t, foundApp, *setApp)

	assert.NilError(t, c.Delete(ctx, setApp.Name), "expected Delete() to not error")

	_, err = c.Get(ctx, setApp.Name)
	assert.Error(t, err, apps.ErrNotFound.Error(), "expected Get() to error after Delete()")
}
