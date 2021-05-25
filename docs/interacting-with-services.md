# Interacting with Services

This pages goes into detail about how to interact with services.

- [Interacting with Services](#interacting-with-services)
  * [Deploying a Service](#deploying-a-service)
    + [Deploying a Specific Revision](#deploying-a-specific-revision)
    + [Deploying Local Changes](#deploying-local-changes)
  * [Updating Services](#updating-services)
    + [Updating to the Latest Version](#updating-to-the-latest-version)
    + [Deploying a Specific Version](#deploying-a-specific-version)
  * [Running a Local Service](#running-a-local-service)
    + [Exposing Your Local Service to the Developer Environment](#exposing-your-local-service-to-the-developer-environment)
      - [Mapping a port](#mapping-a-port)
---

## Deploying a Service

To deploy a service into your developer environment, run `devenv deploy-app <appName>`. 

### Deploying a Specific Revision

To deploy a specific revision of a service, run `devenv deploy-app <appName@revision>`. 

**Note**: When deploying a specific revision, the Docker image used will correspond to whatever
is in your image cache, _not_ the Docker image for that revision. If this matters, please use follow [Deploying Local Changes](#deploying-local-changes).

### Deploying Local Changes

To deploy your application into Kubernetes locally, run `devenv deploy-app --local .`. Press `y` when prompted
to build a Docker image.

## Updating Services

There are two commands that can update an application in your developer environment, depending on the version you want.

### Updating to the Latest Version

`devenv update-app [namespace]` for a single application, `devenv update-apps` to update all applications.

### Deploying a Specific Version

Clone the repository of the application you'd like to update, checkout a branch or tag, and run:

```
devenv deploy-app --local .
```

When prompted to build a docker image or not, enter `y` if you want to update the application running, or `n` if you want to
just update the Kubernetes manifests. If you aren't sure, generally you want to enter `y`.

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
