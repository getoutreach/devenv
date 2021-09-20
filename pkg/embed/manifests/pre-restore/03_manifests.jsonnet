local ok = import 'kubernetes/kube.libsonnet';
local namespaces = [
  'cert-manager',
  'nginx-ingress',
  'velero',
  'devenv',
  'monitoring',
  'minio',
  'vault-secrets-operator',

  // This is outreach specific, but also going to be used
  // by things one day. So we keep it here for now.
  'bento1a',
];

ok.FilteredList() {
  items_+:: {
    [n + '_namespace']: ok.Namespace(n)
    for n in namespaces
  },
}
