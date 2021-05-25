local ok = import 'kubernetes/outreach.libsonnet';

// This copies https://github.com/getoutreach/clicktrack/blob/master/deployments/clicktrack/clicktrack.override.jsonnet, sadly.
local all = {
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
                  serviceName: 'calendarproxy',
                  servicePort: 'http',
                },
              },
              {
                path: '/calendar',
                backend: {
                  serviceName: 'calendarproxy',
                  servicePort: 'http',
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
