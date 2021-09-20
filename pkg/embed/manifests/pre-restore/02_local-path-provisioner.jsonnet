local cluster = import '../cluster.libsonnet';
local ok = import '../libs.libsonnet';
local namespace = 'local-path-storage';

local items = {
  configmap: ok.ConfigMap('local-path-config', namespace) {
    data: {
      'config.json': std.manifestJsonEx({
        nodePathMap: [{
          node: 'DEFAULT_PATH_FOR_NON_LISTED_NODES',
          paths: ['/var/local-path-provisioner'],
        }],
      }, '  '),
      setup: |||
        #!/bin/sh
        while getopts "m:s:p:" opt
        do
            case $opt in
                p)
                absolutePath=$OPTARG
                ;;
                s)
                sizeInBytes=$OPTARG
                ;;
                m)
                volMode=$OPTARG
                ;;
            esac
        done
        mkdir -m 0777 -p ${absolutePath}
      |||,
      teardown: |||
        #!/bin/sh
        while getopts "m:s:p:" opt
        do
            case $opt in
                p)
                absolutePath=$OPTARG
                ;;
                s)
                sizeInBytes=$OPTARG
                ;;
                m)
                volMode=$OPTARG
                ;;
            esac
        done

        rm -rf ${absolutePath}
      |||,
      'helperPod.yaml': std.manifestYamlDoc({
        apiVersion: 'v1',
        kind: 'Pod',
        metadata: {
          name: 'helper-pod'
        },
        spec: {
          containers: [
            {
              name: 'helper-pod',
              image: 'busybox'
            },
          ],
        },
      }),
    },
  },
  deployment: ok.Deployment('local-path-provisioner', namespace) {
    spec+: {
      strategy: {
        type: 'Recreate',
      },
      template+: {
        spec+: {
          nodeSelector: {
            'kubernetes.io/os': 'linux',
          },
          tolerations: [{
            key: 'node-role.kubernetes.io/master',
            operator: 'Equal',
            effect: 'NoSchedule',
          }],
          serviceAccountName: 'local-path-provisioner-service-account',
          containers: [
            ok.Container('local-path-provisioner') {
              image: 'gcr.io/outreach-docker/dev-tooling-team/local-path-provisioner:v0.0.19-outreach.1',
              imagePullPolicy: 'IfNotPresent',
              command: [
                'local-path-provisioner',
                '--debug',
                'start',
                '--helper-image',
                'k8s.gcr.io/build-image/debian-base:v2.1.0',
                '--config',
                '/etc/config/config.json',
              ],
              volumeMounts_+:: {
                'config-volume': {
                  mountPath: '/etc/config',
                },
              },
              env_+:: {
                POD_NAMESPACE: ok.FieldRef('metadata.namespace'),
              },
            },
          ],
          volumes_+:: {
            'config-volume': ok.ConfigMapVolume(items.configmap),
          },
        },
      },
    },
  },
  clusterrole: ok.ClusterRole('local-path-provisioner-role') {
    rules: [
      {
        apiGroups: [''],
        resources: [
          'nodes',
          'persistentvolumeclaims',
          'configmaps',
        ],
        verbs: ['get', 'list', 'watch'],
      },
      {
        apiGroups: [''],
        resources: [
          'endpoints',
          'persistentvolumes',
          'pods',
        ],
        verbs: ['*'],
      },
      {
        apiGroups: [''],
        resources: ['events'],
        verbs: ['create', 'patch'],
      },
      {
        apiGroups: ['storage.k8s.io'],
        resources: ['storageclasses'],
        verbs: ['get', 'list', 'watch'],
      },
    ],
  },
};

ok.FilteredList() {
  items_:: if cluster.type == 'local' then items else {},
}
