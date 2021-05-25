package cmdutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"github.com/sirupsen/logrus"
)

func getFileFromArchive(r io.Reader, filename string) (io.Reader, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			if header.Name != filename {
				continue
			}

			return tarReader, nil
		}
	}

	return nil, fmt.Errorf("failed to find file '%s' in downloaded archive", filename)
}

func createWritableFile(execPath string) (*os.File, error) {
	f, err := os.Create(execPath)
	if err != nil {
		return nil, err
	}

	return f, nil
}

func downloadArchive(resp *http.Response, execPath, filename string) error {
	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"downloading",
	)

	memStorage := bytes.NewBuffer([]byte{})
	_, err := io.Copy(io.MultiWriter(memStorage, bar), resp.Body)
	if err != nil && err != io.EOF {
		return err
	}

	memFile, err := getFileFromArchive(memStorage, filename)
	if err != nil {
		return err
	}

	f, err := os.Create(execPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, memFile)
	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

func downloadPureFile(resp *http.Response, execPath string) error {
	f, err := createWritableFile(execPath)
	if err != nil {
		return err
	}
	defer f.Close()

	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"downloading",
	)
	_, err = io.Copy(io.MultiWriter(f, bar), resp.Body)
	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

// EnsureBinary downloads a binary if it's not found, based on the name of the binary
// otherwise it returns the path to it.
func EnsureBinary(log logrus.FieldLogger, name, downloadDesc, downloadURL, archiveFileName string) (string, error) { //nolint:funlen
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// TODO: We need to figure out where to store these paths we use.
	sourceDir := filepath.Join(homeDir, ".local", "dev-environment", ".deps")
	execPath := filepath.Join(sourceDir, name)

	// TODO: better support for other archives in the future
	isArchive := false
	if strings.HasSuffix(downloadURL, ".tar.gz") {
		isArchive = true
	}

	// if it already exists, then we just return it
	if _, err2 := os.Stat(execPath); err2 == nil {
		return execPath, nil
	}

	err = os.MkdirAll(sourceDir, 0755)
	if err != nil {
		return "", errors.Wrap(err, "failed to make dependency directory")
	}

	// this is called on failure
	cleanup := func() {
		os.Remove(execPath)
	}

	if downloadDesc == "" {
		downloadDesc = name
	}

	log.Infof("Downloading %s", downloadDesc)
	resp, err := http.Get(downloadURL) //nolint:gosec // We're OK with arbitrary URLs here.
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("got unexpected status code: %v", resp.StatusCode)
	}

	if isArchive {
		err = downloadArchive(resp, execPath, archiveFileName)
	} else {
		err = downloadPureFile(resp, execPath)
	}
	if err != nil {
		cleanup()
		return "", err
	}

	err = os.Chmod(execPath, 0755)
	if err != nil {
		cleanup()
		return "", err
	}

	return execPath, nil
}
