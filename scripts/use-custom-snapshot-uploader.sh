#!/usr/bin/env bash
# Builds a snapshot-uploader cmd and injects it into a local kind environment
# for testing. Then provisions an environment using it.
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

KIND_BIN="$HOME/.local/dev-environment/.deps/kind-$(grep -m1 KindVersion "$DIR/../pkg/kubernetesruntime/kind.go" | awk '{ print $3 }' | tr -d '"')"
KIND_IMAGE="$(yq -r '.nodes[0].image' "$DIR/../pkg/embed/config/kind.yaml")"

# shellcheck source=../.bootstrap/shell/lib/logging.sh
source "$DIR/../.bootstrap/shell/lib/logging.sh"

if ! docker ps >/dev/null 2>&1; then
  fatal "Error: docker must be running"
fi

if [[ $1 == "--help" ]] || [[ $1 == "-h" ]]; then
  echo "USAGE: $(basename "$0") [PROVISION ARGS...]"
  exit 0
fi

info "KIND Information:"
info_sub "Binary: $(basename "$KIND_BIN")"
info_sub "Image: $KIND_IMAGE"

info "Building devenv"
make

info "Creating intermediate devenv to inject image into"
echo "currentContext: kind:dev-environment" >"$HOME/.config/devenv/config.yaml"
"$DIR/../bin/devenv" destroy
"$KIND_BIN" create cluster \
  --image "$KIND_IMAGE" \
  --name dev-environment

info_sub "Building docker image"
make docker-build-dev

info_sub "Loading docker image into cache"
"$KIND_BIN" load docker-image --name dev-environment \
  "gcr.io/outreach-docker/devenv:$(make version)"

info_sub "Cleaning up environment"
"$DIR/../bin/devenv" destroy
