local cluster = import '../cluster.libsonnet';
local ok = import '../libs.libsonnet';
local name = 'velero';

local podsPath = if cluster.type == 'local' then
  '/var/lib/kubelet/pods'
else
  '/var/lib/loft/%s/kubelet/pods' % cluster.name;

local manifests = ok.HelmChart(name) {
  namespace:: name,
  version:: '2.27.3',
  repo:: 'https://vmware-tanzu.github.io/helm-charts',
  values:: {
    image: {
      repository: 'velero/velero',
      tag: 'v1.7.1',
      pullPolicy: 'IfNotPresent',
    },
    resources: {
      requests: {
        cpu: '0',
        memory: '0',
      },
      limits: {
        cpu: '0',
        memory: '0',
      },
    },
    kubectl: {
      // Use a docker image that supports multi-arch kubectl.
      // Remove when https://github.com/vmware-tanzu/helm-charts/issues/339
      // is closed.
      image: {
        // See Dockerfile.velero-kubectl
        repository: 'jaredallard/velero-kubectl',
        tag: 'v1.23.3',
      },
    },
    initContainers: [
      {
        name: 'velero-plugin-for-aws',
        image: 'velero/velero-plugin-for-aws:v1.3.0',
        imagePullPolicy: 'IfNotPresent',
        volumeMounts: [
          {
            mountPath: '/target',
            name: 'plugins',
          },
        ],
      },
    ],
    configuration: {
      provider: 'aws',
      backupStorageLocation: {
        bucket: 'velero',
        config: {
          region: 'minio',
          s3ForcePathStyle: 'true',
          s3Url: 'http://minio.minio:9000',
        },
      },
      volumeSnapshotLocation: {
        config: {
          region: 'minio',
          s3ForcePathStyle: 'true',
          s3Url: 'http://minio.minio:9000',
        },
      },
    },
    credentials: {
      useSecret: true,
      secretContents: {
        cloud: '[default]\naws_access_key_id = minioaccess\naws_secret_access_key = miniosecret\n',
      },
    },
    deployRestic: true,
    restic: {
      resources: {
        requests: {
          cpu: '0',
          memory: '0',
        },
        limits: {
          cpu: '0',
          memory: '0',
        },
      },
      podVolumePath: podsPath,
    },
  },
};

manifests
