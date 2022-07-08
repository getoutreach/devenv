# Devspace.sh

DevSpace is an open-source developer tool for Kubernetes that lets you develop and deploy cloud-native software faster.

devenv uses devspace internally for

* deploying services to devenv cluster, including building images, compiling jsonnet, applying manifests
* managing dev pods in cluster, including port forwarding, file sync, interactive terminal into the dev pods

## Devspace Integration into Devenv

We don't want developers at Outreach to have to manage lots of different tools on their own, debug why they don't work or whether they are holding them wrong. For this reason we integrate `devspace` into `devenv` as an implementation detail.

### apps deploy

* Check if `./scripts/deploy-to-dev.sh` exists. If it does, call it with `deploy` argument.
    * Add `DEVENV_IMAGE_REGISTRY` env variable
    * Add `DEPLOY_TO_DEV_VERSION` env variable
    * Add `DEVENV_APP_VERSION` env variable
    * Add `DEVENV_KIND` env variable
    * Add `DEVENV_APPNAME` env variable
    * `KUBECONFIG` set to dev-environment
* Otherwise check whether `devspace.yaml` or `./bootstrap/devspace.yaml` exists. If it does, check whether it has the `deployments` config.
    * Run `devspace deploy --namespace <app namespace>` with
        * `DEVENV_IMAGE_REGISTRY` env variable
        * `DEPLOY_TO_DEV_VERSION` env variable
        * `DEVENV_APP_VERSION` env variable
        * `DEVENV_KIND` env variable
        * `DEVENV_APPNAME` env variable
        * `KUBECONFIG` set to dev-environment
* Otherwise deploy not supported.

- [ ] Can we rename `./scripts/deploy-to-dev.sh` to ./scripts/devenv-apps-deploy.sh?

- [ ] When running against a loft cluster, create a Buildkit builder pod before running `devspace deploy`. The builder pod should have mounted package cache volume.

### apps delete

* Check if `./scripts/deploy-to-dev.sh` exists. If it does, call it with `delete` argument.
    * Add `DEVENV_IMAGE_REGISTRY` env variable
    * Add `DEPLOY_TO_DEV_VERSION` env variable
    * Add `DEVENV_APP_VERSION` env variable
    * Add `DEVENV_KIND` env variable
    * Add `DEVENV_APPNAME` env variable
    * `KUBECONFIG` set to dev-environment
* Otherwise check whether `devspace.yaml` or `./bootstrap/devspace.yaml` exists. If it does, check whether it has the `deployments` config.
    * Run `devspace purge --namespace <app namespace>` with
        * `DEVENV_IMAGE_REGISTRY` env variable
        * `DEPLOY_TO_DEV_VERSION` env variable
        * `DEVENV_APP_VERSION` env variable
        * `DEVENV_KIND` env variable
        * `DEVENV_APPNAME` env variable
        * `KUBECONFIG` set to dev-environment
* Otherwise delete not supported.

### apps dev

Supports multi-entry point applications using --service flag.

* Check if `./scripts/devenv-apps-dev.sh` exists. If it does, call it with `start` argument.
    * Add `DEVENV_IMAGE_REGISTRY` env variable
    * Add `DEPLOY_TO_DEV_VERSION` env variable
    * Add `DEVENV_APP_VERSION` env variable
    * Add `DEVENV_KIND` env variable
    * Add `DEVENV_APPNAME` env variable
    * Add `DEVENV_SERVICE` env variable
    * Add `KUBECONFIG` set to dev-environment
* Otherwise check whether `devspace.yaml` or `./bootstrap/devspace.yaml` exists. If it does, check whether it has the `dev` config.
    * Run `devspace dev --namespace <app namespace> [--profile <service>]` with
        * `DEVENV_IMAGE_REGISTRY` env variable
        * `DEPLOY_TO_DEV_VERSION` env variable
        * `DEVENV_APP_VERSION` env variable
        * `DEVENV_KIND` env variable
        * `DEVENV_SERVICE` env variable
        * `DEVENV_APPNAME` env variable
        * `KUBECONFIG` set to dev-environment
* Otherwise dev not supported.

- [ ] When running against a loft cluster, create a Buildkit builder pod before running `devspace dev`. It relies on `devspace deploy` internally. The builder pod should have mounted package cache volume.

### apps dev stop

Supports multi-entry point applications using --service flag.

* Check if `./scripts/devenv-apps-dev.sh` exists. If it does, call it with `stop` argument.
    * Add `DEVENV_IMAGE_REGISTRY` env variable
    * Add `DEPLOY_TO_DEV_VERSION` env variable
    * Add `DEVENV_APP_VERSION` env variable
    * Add `DEVENV_KIND` env variable
    * Add `DEVENV_APPNAME` env variable
    * Add `DEVENV_SERVICE` env variable
    * Add `KUBECONFIG` set to dev-environment
* Otherwise check whether `devspace.yaml` or `./bootstrap/devspace.yaml` exists. If it does, check whether it has the `dev` config.
    * Run `devspace reset pods --namespace <app namespace> [--profile <service>]` with
        * `DEVENV_IMAGE_REGISTRY` env variable
        * `DEPLOY_TO_DEV_VERSION` env variable
        * `DEVENV_APP_VERSION` env variable
        * `DEVENV_KIND` env variable
        * `DEVENV_APPNAME` env variable
        * `DEVENV_SERVICE` env variable
        * `KUBECONFIG` set to dev-environment
* Otherwise dev stop not supported.

### apps dev terminal

check whether `devspace.yaml` or `./bootstrap/devspace.yaml` exists. If it does, check whether it has the `dev` config.

* Run `devspace enter --namespace <app namespace> [--profile $DEVENV_SERVICE]` with
    * `DEVENV_IMAGE_REGISTRY` env variable
    * `DEPLOY_TO_DEV_VERSION` env variable
    * `DEVENV_APP_VERSION` env variable
    * `DEVENV_KIND` env variable
    * `DEVENV_SERVICE` env variable
    * `DEVENV_APPNAME` env variable
    * `KUBECONFIG` set to dev-environment
* Otherwise dev terminal not supported.
