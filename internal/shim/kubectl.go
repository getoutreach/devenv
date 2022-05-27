// Copyright 2022 Outreach Corporation. All Rights Reserved.

package shim

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

//go:embed kubectl.embed
var kubectlScript []byte

// AddKubectl creates a kubectl shell script in $HOME/.local/dev-environment/bin. This script executes
//
//		devenv --skip-update kubectl "$@"
//
// The function also adds the script to $PATH so it's picked by child processes.
func AddKubectl(opts ...Option) error {
	o := &Options{}
	o.apply(opts)

	devenvShimDir := o.dir
	if devenvShimDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(err, "failed get $HOME dir path")
		}

		devenvShimDir = filepath.Join(home, ".local", "dev-environment", "bin")
		err = os.MkdirAll(devenvShimDir, 0o755)
		if err != nil {
			return errors.Wrap(err, "failed to create $HOME/.local/dev-environment/bin dir")
		}
	}

	kubectlPath := filepath.Join(devenvShimDir, "kubectl")
	if _, err := os.Stat(kubectlPath); os.IsNotExist(err) {
		//nolint:gosec // Why: this needs to be an executable.
		if err := os.WriteFile(kubectlPath, kubectlScript, 0o744); err != nil {
			return errors.Wrapf(err, "failed to write to file %s", kubectlPath)
		}
	} else if err != nil {
		return errors.Wrapf(err, "failed to check if file %s exists", kubectlPath)
	}

	if err := os.Setenv("PATH", fmt.Sprintf("%s:%s", devenvShimDir, os.Getenv("PATH"))); err != nil {
		return errors.Wrap(err, "failed to add $HOME/.local/dev-environment/bin to $PATH")
	}

	return nil
}
