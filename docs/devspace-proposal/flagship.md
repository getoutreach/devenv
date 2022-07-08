# Devenv & Flagship

This doc describes different functionality provided by `devenv`, what it does, how it works and in some cases discuses alternative approaches considered.

Two kinds of dev-environment Kubernetes clusters are supported:

- KiND, running on developer machine, in a Docker container
- loft, running in cloud

## `apps deploy`

This command (as the name suggests) deploys application into dev-environment cluster. It can do so either from source code, or based on a pre-build version from github repo.

### Current State

#### `apps deploy outreach`

1. Delete db-migration pods
1. Find latest image version
1. Run legacy `scripts/deploy-to-dev.sh` script to deploy `outreach` to dev-environment.
    1. Apply helm chart manifests
    1. `devenv deploy-app` pre-seeding dependencies (outreach-accounts, socketron, outreach-templating-service)
    1. Create `aws-credentials` secret in `bento1a`
    1. Wait for helm-chart deps to be up and running
    1. Deploy ElasticSearch
    1. Deploy `outreach` services (flagship-server, flagship-worker, flagship-scheduler, flagship-kafka-worker, server-console)
    1. `devenv deploy-app` required dependencies (olis, mint, giraffe, authnidentity, client)
    1. `kubecfg update` migrations

#### `apps deploy` (from source)

> I'm not certain this actually works now, I'm inclined to say it doesn't. Not just with loft, but also  the KiND cluster.

1. Delete db-migration pods
1. Uses `local` as deployed version. 
1. Run legacy `scripts/deploy-to-dev.sh` script to deploy flagship/server to dev-environment. 
    1. Scale down all flagship deployments (flagship-scheduler flagship-server flagship-worker server-console)
    1. Build docker image locally and load it to KiND dev-environment.
    1. [This is missing right now] update deployments with new image tag.
    1. Scale the deployments back up.

> Note: Notice how it's not deploying any of the dependencies, or databases, or migrations.

### Deploy Pre-built Flagship

> This doesn't need to change. I think.

- uses images built by CI

1. Clone GitHub repo.
1. Delete db-migration pods.
1. Find latest image version.
1. Run legacy `scripts/deploy-to-dev.sh` script to deploy `outreach` to dev-environment.
    1. Apply helm chart manifests
    1. `devenv deploy-app` pre-seeding dependencies (outreach-accounts, socketron, outreach-templating-service)
    1. Create `aws-credentials` secret in `bento1a`
    1. Wait for helm-chart deps to be up and running
    1. Deploy ElasticSearch
    1. Deploy `outreach` services (flagship-server, flagship-worker, flagship-scheduler, flagship-kafka-worker, server-console)
    1. `devenv deploy-app` required dependencies (olis, mint, giraffe, authnidentity, client)
    1. `kubecfg update` migrations

> Note: We need to ensure this is idempotent

### Deploy Flagship from Source Code

The deployment mechanism is a bit different between KiND and loft clusters because of how can the images be made available to the cluster. Both variants use Buildkit to build the images, the difference is in where the Buildkit runs. Both variants also use devspace CLI to orchestrate deployment under the hood.

`devenv` passes `$DEPLOY_TO_DEV_VERSION` set to `local` when deploying from source.

`devenv` passes type of cluster into devspace (or `scripts/deploy-to-dev.sh`) in a variable `DEVENV_KIND=(kind|loft)`. Based on which type of cluster we are deploying into, devspace config uses profiles to set

- image build mechanism (local vs in-cluster)
- whether to push the image to the registry, and whether to do kind load docker-image

`devenv` passes `$DEVENV_IMAGE_REGISTRY` variable to devspace (or `scripts/deploy-to-dev.sh`) 

#### Deploy into KiND Cluster

1. Delete db-migration pods.
1. Run legacy `scripts/deploy-to-dev.sh` script to deploy `outreach` to dev-environment.
    1. Apply helm chart manifests
    1. `devenv deploy-app` pre-seeding dependencies (outreach-accounts, socketron, outreach-templating-service)
    1. Create `aws-credentials` secret in `bento1a`
    1. Wait for helm-chart deps to be up and running
    1. Deploy ElasticSearch
    1. `devspace deploy` to 
        1. build `$DEVENV_IMAGE_REGISTRY/flagship/server:<rand_str>` image using Buildkit,
        1. load image into KiND in a post-build hook,
        1. build `deployments/outreach/outreach.jsonnet` to `deployments/outreach.yaml`
        1. replace server images in the built yaml and apply it
    1. `devenv deploy-app` required dependencies (olis, mint, giraffe, authnidentity, client)
    1. `kubecfg update` migrations

#### Deploy into loft Cluster

1. Delete db-migration pods.
1. Run legacy `scripts/deploy-to-dev.sh` script to deploy `outreach` to dev-environment.
    1. Apply helm chart manifests
    1. `devenv deploy-app` pre-seeding dependencies (outreach-accounts, socketron, outreach-templating-service)
    1. Create `aws-credentials` secret in `bento1a`
    1. Wait for helm-chart deps to be up and running
    1. Deploy ElasticSearch
    1. `devspace deploy` to 
        1. build `$DEVENV_IMAGE_REGISTRY/flagship/server:<rand_str>` image using Buildkit,
        1. push image to dev's registry,
        1. build `deployments/outreach/outreach.jsonnet` to `deployments/outreach.yaml`
        1. replace server images in the built yaml and apply it
    1. `devenv deploy-app` required dependencies (olis, mint, giraffe, authnidentity, client)
    1. `kubecfg update` migrations

## `apps dev [--service server|worker|kafka-worker|console]`

`devenv` uses `devspace` with the right Kubernetes context and namespace to replace service pods with a dev pod.

__Not everything will be deployed for dev mode. It assumes flagship has been deployed before (either from source or pre-built).__

If `--service` isn't provided, devenv lists available profiles.

> This functionality is the same regardless of type of cluster being used.

1. `devenv deploy` runs as part of dev to setup local images
    1. build `$DEVENV_IMAGE_REGISTRY/flagship/server:<rand_str>` image using Buildkit,
    1. push image to dev's registry,
    1. build `deployments/outreach/outreach.jsonnet` to `deployments/outreach.yaml`
    1. replace server images in the built yaml and apply it
1. Build dev image with installed gems to speed up startup (this runs in parallel with building the local server image)
1. Based on profile (flagship-server, flagship-worker, flagship-scheduler, flagship-kafka-worker, server-console) `devspace` scales down the service deployment based on common labels (image selectors are much more fragile)
1. `devspace` starts a new pod with same configuration (env variables, secrets, mounts) like the service pods with following changes
   - based on CI image
   - added label to mark it as dev container
   - removed resource limits
   - mounted local volume for caching packages
   - added `DEV_EMAIL` env variable
   - added `GH_TOKEN` env variable
   - added `NPM_TOKEN` env variable
   - added `PUMA_BIND` env variable
   - set up credentials from env variables to be used for pulling packages (npm, golang, gems)
   - command tails a log file (this is where application will `tee` logs so they land in Datadog)
1. `devspace` syncs local source code into the dev container (and keeps syncing them as long as the session is open)
1. `devspace` forwards ports (3000 for server)
1. `devspace` opens an interactive terminal into that pod

> Note: make commands understand service is running in dev container and `tee` service logs into the file that is tailed for Datadog logs.

### `apps dev stop`

`devenv` uses `devspace` to reset the service (or more) to deployed state.

1. scales deployment back up
1. stops port forwarding
1. closes interactive terminals

### `apps dev terminal [--service server|worker|kafka-worker|console]`

Open interactive terminal session into service dev container.

> Checks whether service is in dev mode (the dev container is deployed instead of service) and uses `devspace enter` to open an interactive terminal to the dev container.

If more than one of services in dev mode and not explicitly set, devenv provides a list of profiles to pick from.
