local ok = import '../libs.libsonnet';
local name = 'nginx-ingress';

local cluster_name = std.extVar('cluster_name');
local cluster_type = std.extVar('cluster_type');

local clusterTypeConf = {
  // local is exposed via a nodeport to be accessible on the host
  'local': {
    controller+: {
      service+: {
        type: 'NodePort',
        annotations: {
          'devenv.outreach.io/local-ip': '127.0.0.1',
          'kubectl.kubernetes.io/last-applied-configuration': '{"apiVersion":"v1","kind":"Service","metadata":{"name":"nginx-ingress-ingress-nginx-controller","namespace":"nginx-ingress"},"spec":{"ports":[{"name":"http","nodePort":32080,"port":80,"protocol":"TCP","targetPort":"http"},{"name":"https","nodePort":32443,"port":443,"protocol":"TCP","targetPort":"https"}]}}',
        },
        nodePorts: {
          http: 32080,
          https: 32443,
        },
      },
    },
  },
  remote: {},
};

local manifests = ok.HelmChart('ingress-nginx') {
  namespace:: name,
  version:: '4.0.17',
  repo:: 'https://kubernetes.github.io/ingress-nginx',
  values:: {
    controller: {
      watchIngressWithoutClass: true,
      admissionWebhooks: {
        enabled: false,
      },
    },
  } + clusterTypeConf[cluster_type],
};

manifests
