# TODOs

- [ ] Pass `$DEVENV_*` env variables to `devspace`, `scripts/deploy-to-dev.sh`, and `./scripts/devenv-apps-dev.sh`
- [ ] Implement `devenv apps dev` (and `./scripts/devenv-apps-dev.sh`)
- [ ] Implement `devenv apps dev stop`
- [ ] Implement `devenv apps dev terminal`
- [ ] Implement `devenv apps deploy` using `devspace deploy` (notice how it doesn't take path for deploying from source code. It uses repo root instead)

# Bootstrap

- [ ] Add image_registry var to bootstrap jsonnet files. It should only apply to app specific images. Not common ones, like temporal, busybox, etc.
- [ ] Move `devspace.yaml` to `devbase` for bootstrap services. (Use $DEVENV_APPNAME instead of template from bootstrap for setting the right APP)
- [ ] `devenv` should set devspace configuration file explicitly. If <repo_root>/devspace.yaml available use it, otherwise fallback to `<repo_root>/.bootstrap/devspace.yaml`.
- [ ] Set up `.dockerignore` for bootstrap images.
- [ ] Add loading images into KiND cluster in post-build hook.
- [ ] When building docker image for loft dev-environment, pre-create Buildkit pod.
- [ ] Set up remote debugging in dev container.
- [ ] Send logs from dev container to Datadog. (This most likely needs to be done in devbase devspace.yaml file as the start command for the dev container)

# Flagship

- [ ] Add support for deploying flagship from source code. (This comes after bootstrap, as it can borrow heavily from the devspace config work done there)
- [ ] Add support for dev of flagship server
- [ ] Add support for dev of flagship worker
- [ ] Implement `devenv apps dev terminal` for multiple services.
- [ ] Add support for dev of flagship kafka-worker. (This might not be needed?)
- [ ] Add support for dev of flagship console. (This might not be needed?)

# Client

- [ ] Add support for deploy client form source 
- [ ] Remove the localizer orca-proxy pod from client deployment.
- [ ] Validate reverse port forwarding through `devspace` for client dev
- [ ] Implement `devenv apps dev` support for client
