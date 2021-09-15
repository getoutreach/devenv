local originalOk = import 'kubernetes/kube.libsonnet';

local ok = originalOk {
  // TODO: Probably want to put this in jsonnet-libs
  // HelmChart creates a helm chart in the dev-environment
  HelmChart(name, namespace='kube-system'): ok._Object('helm.cattle.io/v1', 'HelmChart', name=name, namespace=namespace) {
    local this = self,

    chart:: name,
    version:: error 'helmchart version required',
    repo:: error 'helmchart repo required',
    namespace:: namespace,
    values:: {},

    spec+: {
      helmVersion: 'v3',
      chart: this.chart,
      version: this.version,
      repo: this.repo,
      targetNamespace: this.namespace,
      valuesContent: std.manifestYamlDoc(this.values),
    },
  },

  // VaultSecret creates a VaultSecret in the devenv
  VaultSecret(name, namespace): ok._Object('ricoberger.de/v1alpha1', 'VaultSecret', name=name, namespace=namespace) {
    spec+: {
      type: 'Opaque',
      path: error 'vaultsecret path is required',
    },
  },
};

ok
