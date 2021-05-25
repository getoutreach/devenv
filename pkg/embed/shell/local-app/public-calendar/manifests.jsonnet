local ok = import 'kubernetes/outreach.libsonnet';

local all = {
  service: ok._Object('v1', 'Service', 'calclient-devproxy', namespace='clicktrack--bento1a') {
    spec: {
      selector: {
        app: 'calclient-devproxy',
      },
      ports: [{
        protocol: 'TCP',
        port: 3030,
        targetPort: self.port,
      }],
    },
  },
  ingress: ok.Ingress('clicktrack-devenv', 'clicktrack--bento1a') {
    spec+: {
      rules: [
        {
          host: domain,
          http: {
            paths: [
              {
                backend: {
                  serviceName: 'clicktrack',
                  servicePort: 'http',
                },
              },
              {
                path: '/c',
                backend: {
                  serviceName: 'calclient-devproxy',
                  servicePort: 3030,
                },
              },
              {
                path: '/calendar',
                backend: {
                  serviceName: 'calclient-devproxy',
                  servicePort: 3030,
                },
              },
            ],
          },
        }
        for domain in ['outreach.outreach-content-domain-test.com', 'clicktrack.outreach-dev.com']
      ],
    },
  },
};

all
