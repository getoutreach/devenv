# Outreach Kubernetes Developer Environment

[System Requirements](system-requirements.md) |
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
xattr -c $(type -P devenv)
```

3. Follow the instructions for your platform in the [detailed system requirements docs](docs/system-requirements.md)

### Defining a Box


### Creating the Developer Environment
 
To create a developer environment, run:

```bash
devenv provision --base
```

Next there's a manual step that you'll need to do. You'll need to add a `KUBECONFIG` environment variable, this can be done by adding the line below to your shellrc (generally `~/.zshrc` or `~/.zsh_profile` or `~/.bashrc`):

```bash
# Add the dev-environment to our kube config
export KUBECONFIG="$HOME/.kube/config:$HOME/.outreach/kubeconfig.yaml"
```

You now have a developer environment provisioned!
