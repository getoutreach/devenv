//go:build or_e2e
// +build or_e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getoutreach/devenv/cmd/devenv/snapshot"
	"github.com/getoutreach/devenv/internal/e2e/devenv"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
)

// sets cd to root of repo
//nolint:gochecknoinits // Why: Easier then calling all the time.
func init() {
	_, filename, _, _ := runtime.Caller(0) //nolint:dogsled // Why: This is OK?
	dir := filepath.Join(filepath.Dir(filename), "..", "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}
}

func getVaultURL() string {
	if os.Getenv("CI") != "" {
		return "https://vault-dev.outreach.cloud"
	}

	return "https://vault.outreach.cloud"
}

var defaultProvisionArgs = devenv.ProvisionOpts{
	Box: &box.Config{
		Org: "getoutreach",
		DeveloperEnvironmentConfig: box.DeveloperEnvironmentConfig{
			VaultConfig: box.VaultConfig{
				Enabled:    true,
				AuthMethod: "oidc",
				Address:    getVaultURL(),
			},
			ImagePullSecret: "dev/devenv/image-pull-secret",
			ImageRegistry:   "gcr.io/outreach-docker",
			RuntimeConfig: box.DeveloperEnvironmentRuntimeConfig{
				EnabledRuntimes: []string{
					"kind",
				},
			},
		},
	},
}

func TestCanProvisionDevenv(t *testing.T) {
	devenv.ProvisionDevenv(t, context.Background(), &defaultProvisionArgs)()
}

func TestCanProvisionSnapshotDevenv(t *testing.T) {
	provisionArgs := defaultProvisionArgs
	ctx := context.Background()

	devenv.ProvisionDevenv(t, ctx, &provisionArgs)()

	cleanupFn, err := devenv.UseSnapshotStorage(t, ctx, provisionArgs.Box)
	if cleanupFn != nil {
		defer cleanupFn()
	}
	if err != nil {
		t.Error(errors.Wrap(err, "failed to create snapshot storage for test"))
		return
	}

	generateBox := *provisionArgs.Box
	generateBox.DeveloperEnvironmentConfig.SnapshotConfig.Endpoint = "http://127.0.0.1:61003"
	snapshotOpts, err := snapshot.NewOptions(devenv.Logger, &generateBox)
	if err != nil {
		t.Error(errors.Wrap(err, "failed to create a devenv snapshot option set"))
		return
	}

	os.Setenv("AWS_ACCESS_KEY_ID", "ACCESS_KEY")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET_KEY")

	t.Log("Generating snapshot")
	if err := snapshotOpts.Generate(ctx, &box.SnapshotGenerateConfig{
		Targets: map[string]*box.SnapshotTarget{
			"default": {
				Command: `echo "今日は!"`,
			},
		},
	}, false, box.SnapshotLockChannelStable); err != nil {
		t.Error(errors.Wrap(err, "failed to generate a snapshot from provisioned devenv"))
		return
	}

	t.Log("Destroying snapshot generation intermediate devenv")
	if err := devenv.DestroyDevenv(ctx, provisionArgs.Box); err != nil {
		t.Error(errors.Wrap(err, "failed to destroy devenv after snapshot generation"))
		return
	}

	provisionArgs.SnapshotTarget = "default"

	t.Log("Generating devenv using generated snapshot")
	devenv.ProvisionDevenv(t, context.Background(), &provisionArgs)()
}
