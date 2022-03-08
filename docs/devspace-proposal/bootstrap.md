# Devenv & Bootstrap Apps

This doc will describes different functionality provided by `devenv`, what it does, how it works and in some cases discuses alternative approaches considered.

Two kinds of dev-environment Kubernetes clusters are supported:

- KiND, running on developer machine, in a Docker container
- loft, running in cloud

## `apps deploy`

This command (as the name suggests) deploys application into dev-environment cluster. It can do so either from source code, or based on a pre-build version from github repo.

### Deploy Pre-built Apps

- uses images built by CI
- clones github repo for manifests, uses kubecfg to build and apply them to dev-environment cluster

### Deploy from Source Code

The deployment mechanism is a bit different between KiND and loft clusters because of how can the images be made available to the cluster. Both variants use Buildkit to build the images, the difference is in where the Buildkit runs. Both variants also use devspace CLI to orchestrate deployment under the hood.

`devenv` passes type of cluster into devspace in a variable `DEVENV_KIND=(kind|loft)`. Based on which type of cluster we are deploying into, devspace config uses profiles to set

- image build mechanism (local vs in-cluster)
- whether to push the image to the registry, and whether to do kind load docker-image

#### Deploy into KiND Cluster

1. `devenv` calls `devspace deploy` with the right Kubernetes context, namespace, and `IMAGE_REGISTRY=outreach.local`.Devspace executes the rest of the process.
1. `devspace` runs Buildkit locally. Tag it as outreach.local/<appName>:<random_version>. Load image into KiND in a post-build devspace hook.
1. Build jsonnet (devspace calls a script from devbase, `build-jsonnet.sh`) in a pre-deploy hook. Then it uses `kubectl` to apply the pre-built manifests. (Note: We need to ensure the right `kubectl` version is used, the one provided with `devenv`.)

> We should probably expose loading image/pushing it to dev registry through devenv. Right now, custom deployment scripts use hacks to get a devenv version of kind binary to load the image.

#### Deploy into loft Cluster

1. `devenv` calls `devspace deploy` with the right Kubernetes context, namespace, and `IMAGE_REGISTRY=$(devenv registry get)`.
1. (optimization) `devenv` ensures there's a Buildkit pod with mounted local volume for caching dependencies.
1. `devspace` sets up credentials for dev registry, so the image can be both pushed and pulled
1. `devspace` runs Buildkit remotely in-cluster. This requires docker context to be sent to cluster, `.dockerignore` is important to minimize amount of data being sent. The image is pushed to dev's image registry.
1. Build jsonnet (`devspace` calls a script from devbase, `build-jsonnet.sh`) in a pre-deploy hook. Then it uses `kubectl` to apply the pre-built manifests. (Note: We need to ensure the right `kubectl` version is used, the one provided with `devenv`.)

## `apps dev`

`devenv` uses `devspace` with the right Kubernetes context and namespace to replace application pods with a dev pod.

> This functionality is the same regardless of type of cluster being used.

1. `devspace` deploys the pre-built app (this ensures the infra, services, ingress, ..., exists, but avoids building the image from source, this saves time)
1. `devspace` scales down the application deployment based on common labels (image selectors are much more fragile)
1. `devspace` starts a new pod with same configuration (env variables, secrets, mounts) like the application pods with following changes
   - based on CI image
   - added label to mark it as dev container
   - removed resource limits
   - mounted local volume for caching packages
   - added `DEV_EMAIL` env variable
   - added `GH_TOKEN` env variable
   - added `NPM_TOKEN` env variable
   - added `VERSION` env variable
   - set up credentials from env variables to be used for pulling packages (npm, golang, gems)
   - command tails a log file (this is where application will `tee` logs so they land in Datadog)
1. `devspace` syncs local source code into the dev container (and keeps syncing them as long as the session is open)
1. `devspace` forwards ports (8000 for metrics, 8080 for HTTP, 5000 for gRPC, **TBD** for debugger)
1. `devspace` opens an interactive terminal into that pod

> Note: make commands understand application is running in dev container and `tee` application logs into the file that is tailed for Datadog logs.

#### `apps dev` source code sync alternative

Instead of sync of source code and build in dev container, sync built binaries.

Pros:

- no need to be able to build the app in the container (no credentials for package managers, no special custom build tools per app)

Cons:

- binaries are **much** bigger compared to changed source files, so sync on slower networks will have more failures, will take longer

### `apps dev stop`

`devenv` uses `devspace` to reset the application to deployed state.

1. scales deployment back up
1. stops port forwarding
1. closes interactive terminals

### `apps dev terminal`

Open interactive terminal session into application dev container.

> Checks whether application is in dev mode (the dev container is deployed instead of application) and uses `devspace enter` to open an interactive terminal to the dev container.
