// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Package shim is used for creating devenv specific command replacements.
package shim

// Option functions set options for
type Option func(*Options)

// Options contain shim configuration
type Options struct {
	dir string
}

// apply uses Option funcs to set Options values
func (o *Options) apply(opts []Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithShimDir set shim directory option
func WithShimDir(dir string) Option {
	return func(opts *Options) {
		opts.dir = dir
	}
}
