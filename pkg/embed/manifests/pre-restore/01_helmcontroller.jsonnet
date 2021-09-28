local cluster = import '../cluster.libsonnet';
local ok = import '../libs.libsonnet';
local namespace = 'kube-system';

local items = {
  crd_helmcharts: {
    apiVersion: 'apiextensions.k8s.io/v1beta1',
    kind: 'CustomResourceDefinition',
    metadata: {
      name: 'helmcharts.helm.cattle.io',
      namespace: 'kube-system',
    },
    spec: {
      group: 'helm.cattle.io',
      version: 'v1',
      additionalPrinterColumns: [
        {
          name: 'Job',
          type: 'string',
          description: 'Job associated with updates to this chart',
          JSONPath: '.status.jobName',
        },
        {
          name: 'Chart',
          type: 'string',
          description: 'Helm Chart name',
          JSONPath: '.spec.chart',
        },
        {
          name: 'TargetNamespace',
          type: 'string',
          description: 'Helm Chart target namespace',
          JSONPath: '.spec.targetNamespace',
        },
        {
          name: 'Version',
          type: 'string',
          description: 'Helm Chart version',
          JSONPath: '.spec.version',
        },
        {
          name: 'Repo',
          type: 'string',
          description: 'Helm Chart repository URL',
          JSONPath: '.spec.repo',
        },
        {
          name: 'HelmVersion',
          type: 'string',
          description: 'Helm version used to manage the selected chart',
          JSONPath: '.spec.helmVersion',
        },
        {
          name: 'Bootstrap',
          type: 'boolean',
          description: 'True if this is chart is needed to bootstrap the cluster',
          JSONPath: '.spec.bootstrap',
        },
      ],
      names: {
        kind: 'HelmChart',
        plural: 'helmcharts',
        singular: 'helmchart',
      },
      scope: 'Namespaced',
    },
  },
  crd_helmchartconfigs: {
    apiVersion: 'apiextensions.k8s.io/v1beta1',
    kind: 'CustomResourceDefinition',
    metadata: {
      name: 'helmchartconfigs.helm.cattle.io',
      namespace: 'kube-system',
    },
    spec: {
      group: 'helm.cattle.io',
      version: 'v1',
      names: {
        kind: 'HelmChartConfig',
        plural: 'helmchartconfigs',
        singular: 'helmchartconfig',
      },
      scope: 'Namespaced',
    },
  },
  deployment: {
    apiVersion: 'apps/v1',
    kind: 'Deployment',
    metadata: {
      name: 'helm-controller',
      namespace: 'kube-system',
      labels: {
        app: 'helm-controller',
      },
    },
    spec: {
      replicas: 1,
      selector: {
        matchLabels: {
          app: 'helm-controller',
        },
      },
      template: {
        metadata: {
          labels: {
            app: 'helm-controller',
          },
        },
        spec: {
          containers: [
            {
              name: 'helm-controller',
              image: 'rancher/helm-controller:v0.10.6',
              command: [
                'helm-controller',
              ],
            },
          ],
        },
      },
    },
  },
  clusterrole: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'ClusterRole',
    metadata: {
      annotations: {
        'rbac.authorization.kubernetes.io/autoupdate': 'true',
      },
      labels: {
        'kubernetes.io/bootstrapping': 'rbac-defaults',
      },
      name: 'helm-controller',
      namespace: 'kube-system',
    },
    rules: [
      {
        apiGroups: [
          '*',
        ],
        resources: [
          '*',
        ],
        verbs: [
          '*',
        ],
      },
      {
        nonResourceURLs: [
          '*',
        ],
        verbs: [
          '*',
        ],
      },
    ],
  },
  clusterrolebinding: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'ClusterRoleBinding',
    metadata: {
      name: 'helm-controller',
      namespace: 'kube-system',
    },
    roleRef: {
      apiGroup: 'rbac.authorization.k8s.io',
      kind: 'ClusterRole',
      name: 'helm-controller',
    },
    subjects: [
      {
        apiGroup: 'rbac.authorization.k8s.io',
        kind: 'User',
        name: 'system:serviceaccount:kube-system:default',
      },
    ],
  },
};

ok.FilteredList() {
  // HACK: This should actually check if the runtime _type_ is k3s, but we don't
  // provide that information at the moment.
  items_:: if cluster.type == 'local' then items else {},
}
