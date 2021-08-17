package kubernetestunnelruntime

import (
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/sirupsen/logrus"
)

//nolint:gochecknoglobals
var (
	LocalizerVersion     = "v1.12.0"
	LocalizerDownloadURL = "https://github.com/getoutreach/localizer/releases/download/" +
		LocalizerVersion + "/localizer_" + strings.TrimPrefix(LocalizerVersion, "v") + "_" +
		runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
)

// EnsureLocalizer ensures that localizer exists and returns
// the location of the binary. Note: this outputs text
// if localizer is being downloaded
func EnsureLocalizer(log logrus.FieldLogger) (string, error) { //nolint:funlen
	return cmdutil.EnsureBinary(log, "localizer-"+LocalizerVersion, "Kubernetes Tunnel Runtime (localizer)", LocalizerDownloadURL, "localizer")
}
