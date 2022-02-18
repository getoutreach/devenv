local ok = import 'kubernetes/outreach.libsonnet';
local namespace = 'devenv';
local name = 'snapshot';

local all = {
  role: ok.Role(name, namespace=namespace) {
    rules: [
      {
        apiGroups: [''],
        resources: ['configmaps'],
        verbs: ['create'],
      },
    ],
  },
  svc_acct: ok.ServiceAccount(name, namespace) {},
  rolebinding: ok.RoleBinding(name, namespace=namespace) {
    roleRef_:: $.role,
    subjects_:: [$.svc_acct],
  },
};

ok.List() {
  items_:: all,
}
