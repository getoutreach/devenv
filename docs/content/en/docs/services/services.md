---
title : "Interacting with Services"
description: "How to interact with services in a devenv"
date: 2022-05-09
lastmod: 2022-05-09
draft: false
images: []
---

## Deploying a Service

To deploy a service into your developer environment, run `devenv apps deploy <appName>`.

**Note**: By default tags are used for deployments, if present. Otherwise the latest commit is used. If you wish to ignore tags and use the latest commit instead set the topic `release-type-commits` on your repository. Though note this is unsupported and only provided for projects that haven't yet moved to tags.

### Deploying a Specific Revision

To deploy a specific revision of a service, run `devenv apps deploy <appName@CommitOrTag>`.

### Deploying Local Changes

To deploy your application into Kubernetes locally, run `devenv apps deploy .`.

## Updating Services

There are two commands that can update an application in your developer environment, depending on the version you want.

### Updating to the Latest Version

`devenv apps update <appName>` for a single application, `devenv apps update` to update all applications.

## Running a Local Service

If you want to run any code locally that needs to pretend it's inside the cluster, you will need to
use our tunnel command.

```bash
devenv tunnel
```

### Exposing Your Local Service to the Developer Environment

If you have a service running happily in the dev environment that you want to start a
develop/build/test iteration cycle on locally, you can use `devenv local-app` to start a tunnel
from Kubernetes to your local service. Run the following to substitute the Kubernetes-deployed service:

```bash
devenv local-app [serviceName]
```

**Note**: `serviceName` is generally the name of the repository of the service you want to switch
**Note**: If your service is not a Bootstrap application, you _may_ need to supply `--namespace <namespace>`.

#### Mapping a port

By default, the local port and the remote port are the same. If you need to expose a different local port for the remote port, please use `--port <local port>:<remote port>`. For example, use `--port 8080:80` to expose the local port 8080 as port 80 in the dev environment.
