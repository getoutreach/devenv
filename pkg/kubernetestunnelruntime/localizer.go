package kubernetestunnelruntime

import (
	"os"
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/sirupsen/logrus"
)

//nolint:gochecknoglobals
var (
	LocalizerVersion     = "v1.8.2"
	LocalizerDownloadURL = "https://github.com/jaredallard/localizer/releases/download/" +
		LocalizerVersion + "/localizer_" + strings.TrimPrefix(LocalizerVersion, "v") + "_" +
		runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"

	LocalizerSock = "/var/run/localizer.sock"
)

// EnsureLocalizer ensures that localizer exists and returns
// the location of the binary. Note: this outputs text
// if localizer is being downloaded
func EnsureLocalizer(log logrus.FieldLogger) (string, error) { //nolint:funlen
	return cmdutil.EnsureBinary(log, "localizer-"+LocalizerVersion, "Kubernetes Tunnel Runtime (localizer)", LocalizerDownloadURL, "localizer")
}

func IsLocalizerRunning() bool {
	if _, err := os.Stat(LocalizerSock); err != nil {
		return false
	}

	return true
}
