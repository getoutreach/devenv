// Copyright 2022 Outreach Corporation. All Rights Reserved.

package shim

import (
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestAddKubectl(t *testing.T) {
	tempDir := t.TempDir()

	err := AddKubectl(WithShimDir(tempDir))
	assert.NilError(t, err)

	p, err := exec.LookPath("kubectl")
	assert.NilError(t, err)

	assert.Assert(t, p == filepath.Join(tempDir, "kubectl"))
}
