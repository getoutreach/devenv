local ok = import './libs.libsonnet';
local name = 'vault-secrets-operator';
local vault_addr = std.extVar('vault_addr');

local mockSecret = ok.Secret(name, namespace=name) {
  data:: {
    VAULT_TOKEN: '',
    VAULT_TOKEN_LEASE_DURATION: '',
  },
};

local manifests = ok.HelmChart(name) {
  namespace:: name,
  version:: '1.14.3',
  repo:: 'https://ricoberger.github.io/helm-charts',
  values:: {
    environmentVars: ok.envList({
      VAULT_TOKEN: ok.SecretKeyRef(mockSecret, 'VAULT_TOKEN'),
      VAULT_TOKEN_LEASE_DURATION: ok.SecretKeyRef(mockSecret, 'VAULT_TOKEN_LEASE_DURATION'),
    }),
    vault: {
      // TODO: Get this from box.Config
      address: vault_addr,
    },
  },
};

manifests
