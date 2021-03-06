# devenv

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/getoutreach/devenv)
[![Generated via Bootstrap](https://img.shields.io/badge/Outreach-Bootstrap-%235951ff)](https://github.com/getoutreach/bootstrap)
[![Coverage Status](https://coveralls.io/repos/github/getoutreach/devenv/badge.svg?branch=main)](https://coveralls.io/github//getoutreach/devenv?branch=main)

Kubernetes Developer Environment Platform

## Contributing

Please read the [CONTRIBUTING.md](CONTRIBUTING.md) document for guidelines on developing and contributing changes.

## High-level Overview

<!--- Block(overview) -->

[System Requirements](docs/system-requirements.md) |
[Lifecycle](docs/lifecycle.md) |
[Interacting with Services](docs/interacting-with-services.md) |

## Getting Started

1. Download the [latest release](https://github.com/getoutreach/devenv/releases/latest) for your platform and install the `devenv` binary to `/usr/local/bin/`:

```bash
tar xvf devenv_**_**.tar.gz
mv devenv /usr/local/bin/

# Linux/WSL2 optional: allow your user to update the devenv
sudo chown $(id -u):$(id -g) $(command -v devenv)
```

2. **(macOS only)** Ensure the `devenv` binary is authorized to run.

```bash
xattr -c $(command -v devenv)
```

3. Follow the instructions for your platform in the [detailed system requirements docs](docs/system-requirements.md)

### Defining a Box

TODO. See [gobox/pkg/box](https://github.com/getoutreach/gobox) for the spec.

### Creating the Developer Environment

To create a developer environment, run:

```bash
devenv provision
```

Next there's a manual step that you'll need to do. You'll need to add a `KUBECONFIG` environment variable, this can be done by adding the line below to your shellrc (generally `~/.zshrc` or `~/.zsh_profile` or `~/.bashrc`):

NOTE: For Outreach developers this step is already completed by [orc](https://github.com/getoutreach/orc).

```bash
# Add the dev-environment to our kube config
export KUBECONFIG="$HOME/.kube/config:$HOME/.outreach/kubeconfig.yaml"
```

You now have a developer environment provisioned!

## FAQ

### Using different drivers

The `devenv` supports different kubernetes runtime drivers, below are the instructions for each driver

#### KinD

This should work out of the box!

#### Loft

You will need to create a loft instance, and set it in your `box.yaml`: TODO

You will need to create a vcluster template named `devenv` with the following contents:

```yaml
storage:
  size: 50Gi

syncer:
  # Don't sync ingresses, our ingress controller will handle this, and get its own IP address
  # so we can address it via /etc/hosts
  extraArgs: ["--disable-sync-resources=ingresses"]

# This allows metrics-server to function properly
rbac:
  clusterRole:
    create: true
```

<!--- EndBlock(overview) -->
