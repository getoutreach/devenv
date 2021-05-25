local ok = import 'kubernetes/kube.libsonnet';
local namespaces = [
  'cert-manager',
  'nginx-ingress',
  'velero',
  'devenv',
  'monitoring',
  'vault-secrets-operator',

  // This is outreach specific, but also going to be used
  // by things one day. So we keep it here for now.
  'bento1a',
];

local items = {
  prune_images: ok.CronJob('prune-images', 'devenv') {
    spec+: {
      concurrencyPolicy: 'Forbid',
      schedule: '0 * * * *',
      jobTemplate+: {
        spec+: {
          template+: {
            spec+: {
              containers_:: {
                default: ok.Container('default') {
                  image: 'gcr.io/outreach-docker/kindest/node:v1.20.2',
                  command: ['/bin/bash', '-c', 'crictl rmi --prune'],
                  volumeMounts_+:: {
                    'containerd-socket': {
                      mountPath: '/var/run/containerd/containerd.sock',
                    },
                  },
                  securityContext: {
                    privileged: true,
                  },
                },
              },
              volumes+: [{
                name: 'containerd-socket',
                hostPath: {
                  path: '/var/run/containerd/containerd.sock',
                  type: 'Socket',
                },
              }],
            },
          },
        },
      },
    },
  },
};

ok.FilteredList() {
  items_+:: items + {
    [n + '_namespace']: ok.Namespace(n)
    for n in namespaces
  },
}
