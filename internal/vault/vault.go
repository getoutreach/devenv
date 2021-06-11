package vault

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/getoutreach/devenv/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	vault "github.com/hashicorp/vault/api"
)

func EnsureLoggedIn(ctx context.Context, log logrus.FieldLogger, b *box.Config, k kubernetes.Interface) error {
	// Check if we need to issue a new token
	err := exec.CommandContext(ctx, "vault", "token", "lookup").Run()
	if err != nil {
		// We did, so issue a new token using our authentication method
		//nolint:gosec // Why: Gotta do what you've gotta do :'(
		cmd := exec.CommandContext(ctx, "vault", "login", "-method", b.DeveloperEnvironmentConfig.VaultConfig.AuthMethod)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return errors.Wrap(err, "failed to run vault login")
		}
	}

	// If we have a Kubernetes client, attempt to add our new credentials into the
	// environment
	if k != nil {
		err2 := refreshKubernetesAuth(ctx, k)
		if err2 != nil {
			return err2
		}
	}

	return nil
}

func refreshKubernetesAuth(ctx context.Context, k kubernetes.Interface) error { //nolint:funlen
	secretName := "vault-secrets-operator"
	exists := true

	token, err := exec.CommandContext(ctx, "vault", "print", "token").CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to get vault token: %s", string(token))
	}

	_, err = k.CoreV1().Secrets(secretName).Get(ctx, secretName, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		exists = false
	} else if err != nil {
		return errors.Wrap(err, "failed to access vault secret")
	}

	if exists {
		//nolint:govet // Why: We're OK shadowing err
		err := k.CoreV1().Secrets(secretName).Delete(ctx, secretName, metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to delete vault secret in devenv")
		}
	}

	_, err = k.CoreV1().Secrets(secretName).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"VAULT_TOKEN":                strings.TrimSpace(string(token)),
			"VAULT_TOKEN_LEASE_DURATION": "43200",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to create vault secret")
	}

	pods, err := k.CoreV1().Pods(secretName).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to list vault-secret-operator pods")
	}

	for i := range pods.Items {
		p := &pods.Items[i]
		//nolint:govet // Why: We're OK shadowing err
		err := k.CoreV1().Pods(secretName).Delete(ctx, p.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to delete vault-secret-operator pod")
		}
	}

	return nil
}

func NewClient(ctx context.Context, b *box.Config) (*vault.Client, error) {
	vconf := vault.DefaultConfig()
	vconf.Address = b.DeveloperEnvironmentConfig.VaultConfig.Address

	v, err := vault.NewClient(vconf)
	if err != nil {
		return nil, err
	}

	token, err := exec.CommandContext(ctx, "vault", "print", "token").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get vault token: %s", string(token))
	}

	v.SetToken(strings.TrimSpace(string(token)))

	return v, nil
}
