# Devenv & Client

This doc describes how to use devenv for client development.

- [ ] Q: Do we want to use devenv for stand-alone mode as well? The benefit would be simplification of dev server startup code. No need to manage certificates from client code. It would mean devenv reverse proxy might "run" without the kubernetes cluster behind it.

## Why Special Case for Client?

Performance.

The client build is fairly slow (compared to bootstrap services for example). In this case, the machine dev builds the code on matters a lot. Shared nodes for dev containers are good enough perf-wise for building a service, but significantly slower than the M1 macs for building JS.

Here are some numbers for comparison (clean build, excluding installation of packages):

|                             | Devspace            | Intel              | M1 Mac             |
| --------------------------- | ------------------- | ------------------ | ------------------ |
| General output time         | 14 mins, 16.84 secs | 6 mins, 53.92 secs | 4 mins, 10.23 secs |
| modules with no loaders     | 12 mins, 54.39 secs | 52.12 secs         | 3 mins, 44.41 secs |
| ts-loader                   | 12 mins, 31.99 secs | 5 mins, 54.47 secs | 3 mins, 35.15 secs |
| source-map-loader           | 1 min, 21.42 secs   | 51.86 secs         | 27.26 secs         |
| file-loader                 | 2.25 secs           | 0.969 secs         | 0.72 secs          |
| css-loader                  | 0.413 secs          | 0.155 secs         | 0.13 secs          |
| html-webpack-plugin         | 0.164 secs          | 0.089 secs         | 0.06 secs          |
| style-loader and css-loader | 0.067 secs          | 0.033 secs         | 0.018 secs         |

Given these numbers, developing clients in regular dev containers is not feasible. The options are 

* provide special dev containers running on faster nodes (not feasible due to cost, the machines need lots of cores and large premium storage to be fast)
* reverse port forwarding into cluster and then through ingress to local box again (round trip slows things down on request, port forwarding may be unstable)
* reverse proxy built into `devenv` decides where to route traffic (cluster vs localhost, and possibly `web-master.outreach-staging.com` so we can skip the whole deployment to dev-environment altogether)

## `apps deploy`

This command (as the name suggests) deploys application into dev-environment cluster. It can do so either from source code, or based on a pre-build version from github repo.

### Current State

#### `apps deploy client`

Run legacy `scripts/deploy-to-dev.sh` script to deploy `client` to dev-environment.

The script creates an ingress for `developer.outreach.io` that points to an external service (`web-master.outreach-staging.com`).

#### `apps deploy` (from source)

Not supported

### Deploy Pre-built `client`

> This doesn't need to change. I think.

Run legacy `scripts/deploy-to-dev.sh` script to deploy `client` to dev-environment.

The script creates an ingress for `developer.outreach.io` that points to an external service (`web-master.outreach-staging.com`).

- [ ] Remove the localizer orca-proxy pod from client deployment.

### Deploy `client` from Source Code

The deployment mechanism is a bit different between KiND and loft clusters because of how can the images be made available to the cluster. Both variants use Buildkit to build the images, the difference is in where the Buildkit runs. Both variants also use devspace CLI to orchestrate deployment under the hood.

`devenv` passes `$DEPLOY_TO_DEV_VERSION` set to `local` when deploying from source.

`devenv` passes type of cluster into devspace (or `scripts/deploy-to-dev.sh`) in a variable `DEVENV_KIND=(kind|loft)`. Based on which type of cluster we are deploying into, devspace config uses profiles to set

- image build mechanism (local vs in-cluster)
- whether to push the image to the registry, and whether to do kind load docker-image

`devenv` passes `$DEVENV_IMAGE_REGISTRY` variable to devspace (or `scripts/deploy-to-dev.sh`)

#### Deploy into KiND Cluster

Run legacy `scripts/deploy-to-dev.sh` to deploy client to dev-environment. The script in this case uses `devspace deploy` to orchestrate next steps.

> Using `devspace deploy` instead of doing it from script is useful in `apps dev` as that is managed by `devspace` and depends on `deploy`.

1. build `$DEVENV_IMAGE_REGISTRY/orca:<rand_str>` image using Buildkit,
1. load image into KiND in a post-build hook,
1. build `deployments/orca/full.jsonnet` to `deployments/full.yaml`
1. replace server images in the built yaml and apply it

#### Deploy into loft Cluster

Run legacy `scripts/deploy-to-dev.sh` to deploy client to dev-environment. The script in this case uses `devspace deploy` to orchestrate next steps.

> Using `devspace deploy` instead of doing it from script is useful in `apps dev` as that is managed by `devspace` and depends on `deploy`.

1. build `$DEVENV_IMAGE_REGISTRY/orca:<rand_str>` image using Buildkit,
1. push image to dev's registry,
1. build `deployments/orca/full.jsonnet` to `deployments/full.yaml`
1. replace server images in the built yaml and apply it

## `apps dev`

Run `scripts/devenv-apps-dev.sh start` to

* register `developer.outreach.io` with `devenv` proxy and send the traffic to `http://127.0.0.1:8080`.
* start webpack-dev-server

> This functionality is the same regardless of type of cluster being used.

### `apps dev stop`

Run `scripts/devenv-apps-dev.sh stop` to

* unregister `developer.outreach.io` with `devenv`

> This will run when the `scripts/devenv-apps-dev.sh start` is stopped as well. This will allow for the reverse proxy to run in a separate process.

### `apps dev terminal`

There is no `dev` config in devspace so terminal is not supported.

# Reverse Proxy

Routing traffic dynamically based on current devenv state (whether `client` is deployed into cluster, or running locally). By default, all the traffic goes to cluster (which devenv knows where to find it). 

## `proxy register --host <handled hostname> --target <where the traffic goes>`

> Requires sudo to to edit /etc/hosts (it may contain that hostname under non-127.0.0.1 IP, or not contain it at all)

* Adds new mapping to `~/.local/dev-environment/proxy/mappings.json`. When mapping for host exists, it gets overwritten.
* [Based on how we want to implement this] Sends a request to `devenv` control plane to add the new mapping.

## `proxy remove --host <handled hostname>`

* Removes the mapping from  `~/.local/dev-environment/proxy/mappings.json`
* [Based on how we want to implement this] Sends a request to `devenv` control plane to remove the mapping.

> Requires sudo to to edit `/etc/hosts`

## `proxy start`

* Reads a config file from `~/.local/dev-environment/proxy/mappings.json` with existing registrations.
* Edit `/etc/hosts` to send mappings to `127.0.0.1`.
* Starts the reverse proxy (listening on port 443).
* Listens on chages to mappings (either watching the file, or an internal control plane endpoint that can be called from register/unregister).

> Requires sudo to to edit `/etc/hosts` and listen on port 443

## `devenv` Daemon

`devevn` runs on dev machine as a daemon. This lets us run all the traffic through the revese proxy, switch clusters without having to change `/etc/hosts`, show devs error messages when devenv status isn't quiet right. They don't need to worry whether the tunnel/proxy is running or not. Should include status icon in menu bar.

We can produce notifications when something happens (alert worthy for example). Or in the morning, with nudge to update apps in devenv.

## Dev DNS Server

> We may or may not want this. It's non-trivial to set up in a way that works across different operating systems. This is semi-serious suggestion. It would probably aleviate some of our name resolution pains.

This removes the need to edit `/etc/hosts` all the time, enables wildcard resolution (*.outreach-dev.com, for example, or *.outreach.local)

It's possible to set up programmatically in a way that even MacOS accepts it. VPNs do it, so can we.
