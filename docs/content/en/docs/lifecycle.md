---
title : "devenv lifcycle"
description: "Common commands when running the devenv and how to interact with one"
date: 2022-05-09
lastmod: 2022-05-09
draft: false
images: []
---

To destroy the developer environment, including data in databases:

```bash
devenv destroy
```

To stop the developer environment (which persists the state of the environment, unlike `devenv destroy`):

```bash
devenv stop
```

To restart the developer environment after stopping it:

```bash
devenv start
```

To initialize a new developer environment _(this exits immediately if it detects that a
dev-environment has already started to be provisioned)_:

```bash
devenv provision
```

To deploy additional services, use the `devenv apps deploy $APP_NAME` flag, where `$APP_NAME` is the name
of your service's GitHub repository. Every [`bootstrap` (coming soon!)](https://github.com/getoutreach/bootstrap)-created
service can be deployed into this environment without any extra configuration.

Run `devenv provision --help` for documentation on additional ways to customize the
provisioning process.
