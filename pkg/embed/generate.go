package embed

import (
	"context"
	goembed "embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// Manifests contains the devenv of the manifests
//go:embed manifests/*
var Manifests goembed.FS

// Config contains configuration of the devenv that is static
//go:embed config/*
var Config goembed.FS

// Shell contains all of the shell scripts used
//go:embed shell/*
var Shell goembed.FS

func MustRead(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}

	return b
}

// ExtractToDir extracts an embed.FS to a given directory
func ExtractToDir(efs *goembed.FS, dir string) error {
	return fs.WalkDir(efs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := efs.Open(p)
		if err != nil {
			return errors.Wrap(err, "failed to access embedded file")
		}
		defer f.Close()

		tempFileDir := filepath.Join(dir, filepath.Dir(p))
		err = os.MkdirAll(tempFileDir, 0755)
		if err != nil {
			return errors.Wrap(err, "failed to create directory for embedded file")
		}

		nf, err := os.Create(filepath.Join(tempFileDir, filepath.Base(p)))
		if err != nil {
			return errors.Wrap(err, "failed to create temporary file")
		}
		defer nf.Close()

		//nolint:gocritic // Why: This is an octal friendly package
		err = nf.Chmod(0777) // Can't access orig file perms? :'(
		if err != nil {
			return errors.Wrap(err, "failed to chmod temporary file")
		}

		_, err = io.Copy(nf, f)
		return errors.Wrap(err, "failed to write embedded file")
	})
}

// ExtractAllToTempDir extracts all embedded files into a temporary directory
// allowing usage of them with shell scripts / external commands.
// The extracted files match the embedded setup
func ExtractAllToTempDir(ctx context.Context) (string, error) {
	// Use os.CreateTemp to get a non-allocated file name for usage as
	// a temp dir
	f, err := os.CreateTemp("", "devenv-*")
	if err != nil {
		return "", err
	}
	tempDir := f.Name()
	//nolint:errcheck // Why: best effort
	f.Close()

	err = os.Remove(tempDir)
	if err != nil {
		return tempDir, err
	}

	err = os.MkdirAll(tempDir, 0755)
	if err != nil {
		return tempDir, err
	}

	// Extract all the filesystems
	filesystems := []*goembed.FS{
		&Shell,
		&Manifests,
		&Config,
	}

	for _, input := range filesystems {
		err2 := ExtractToDir(input, tempDir)
		if err2 != nil {
			return tempDir, err2
		}
	}

	return tempDir, nil
}
