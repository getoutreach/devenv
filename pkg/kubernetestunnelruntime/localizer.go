package kubernetestunnelruntime

import (
	"runtime"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/sirupsen/logrus"
)

//nolint:gochecknoglobals
var (
	LocalizerVersion     = "v1.14.3"
	LocalizerDownloadURL = "https://github.com/getoutreach/localizer/releases/download/" +
		LocalizerVersion + "/localizer_" + strings.TrimPrefix(LocalizerVersion, "v") + "_" +
		runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
)

// EnsureLocalizer ensures that localizer exists and returns
// the location of the binary. Note: this outputs text
// if localizer is being downloaded
func EnsureLocalizer(log logrus.FieldLogger) (string, error) { //nolint:funlen
	log.WithField("version", LocalizerVersion).WithField("url", LocalizerDownloadURL).Info("using localizer")
	return cmdutil.EnsureBinary(log, "localizer-"+LocalizerVersion, "Kubernetes Tunnel Runtime (localizer)", LocalizerDownloadURL, "localizer")
}
