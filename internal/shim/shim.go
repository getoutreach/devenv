// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Package shim is used for creating devenv specific command replacements.
package shim

type Options struct {
	dir string
}

func WithShimDir(dir string) func(*Options) {
	return func(opts *Options) {
		opts.dir = dir
	}
}
