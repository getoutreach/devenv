package provision

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/getoutreach/devenv/internal/vault"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/embed"
	"github.com/pkg/errors"
)

func (o *Options) deployStage(ctx context.Context, stage int) error {
	dir, err := o.extractEmbed(ctx)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	stageDir := filepath.Join(dir, "manifests", fmt.Sprintf("stage-%d", stage))

	files, err := os.ReadDir(stageDir)
	if err != nil {
		return errors.Wrap(err, "failed to list files in extracted embed dir")
	}

	o.log.WithField("stage", stage).Info("Deploying Stage")
	for _, f := range files {
		//nolint:govet // Why: we're OK shadowing err
		o.log.WithField("manifest", f.Name()).Info("Deploying Manifest")
		err := cmdutil.RunKubernetesCommand(ctx, stageDir, true, "kubecfg",
			"--jurl", "https://raw.githubusercontent.com/getoutreach/jsonnet-libs/master", "update", f.Name())
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *Options) deployVaultSecretsOperator(ctx context.Context) error {
	dir, err := o.extractEmbed(ctx)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	o.log.Info("Deploying vault-secrets-operator")

	return cmdutil.RunKubernetesCommand(ctx, dir, true, "kubecfg",
		"--jurl", "https://raw.githubusercontent.com/getoutreach/jsonnet-libs/master", "update", "--ext-str",
		fmt.Sprintf("vault_addr=%s", o.b.DeveloperEnvironmentConfig.VaultConfig.Address),
		"manifests/vault/vault-secrets-operator.jsonnet")
}

func (o *Options) deployStages(ctx context.Context, stages int) error {
	// e.g. stages 3
	// stage 0, 1, 2
	for i := 0; i != (stages + 1); i++ {
		if err := o.deployStage(ctx, i); err != nil {
			return errors.Wrapf(err, "failed to deploy stage %d", i)
		}
	}

	return nil
}

// extractEmbed wraps embed.ExtractAllToTempDir but handles cleaning up the dir
// if failed
func (o *Options) extractEmbed(ctx context.Context) (string, error) {
	dir, err := embed.ExtractAllToTempDir(ctx)
	if err != nil {
		if dir != "" {
			//nolint:errcheck
			os.RemoveAll(dir)
		}
		return "", err
	}

	return dir, err
}

func (o *Options) ensureImagePull(ctx context.Context) error {
	if !o.b.DeveloperEnvironmentConfig.VaultConfig.Enabled {
		return nil
	}

	if o.b.DeveloperEnvironmentConfig.ImagePullSecret == "" {
		return nil
	}

	// We need to take the user's key and inject data after the KV store, e.g.
	// dev/devenv/image-pull-secret becomes dev/data/devenv/...
	paths := strings.Split(o.b.DeveloperEnvironmentConfig.ImagePullSecret, "/")
	secretPath := strings.Join(append([]string{paths[0], "data"}, paths[1:]...), "/")

	storagePath := filepath.Join(o.homeDir, imagePullSecretPath)
	if _, err := os.Stat(storagePath); err == nil {
		// we already have it, so exit
		return nil
	}

	o.log.WithField("secretPath", secretPath).Info("Fetching image pull secret via Vault")
	if err := vault.EnsureLoggedIn(ctx, o.log, o.b, nil); err != nil {
		return errors.Wrap(err, "failed to login to vault")
	}

	v, err := vault.NewClient(ctx, o.b)
	if err != nil {
		return errors.Wrap(err, "failed to create vault client")
	}

	sec, err := v.Logical().Read(secretPath)
	if err != nil {
		return errors.Wrap(err, "failed to read image pull secret from Vault")
	}

	imageSecret := sec.Data["data"].(map[string]interface{})["secret"].(string)

	err = os.MkdirAll(filepath.Dir(storagePath), 0755)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(storagePath, []byte(imageSecret), 0600)
}
