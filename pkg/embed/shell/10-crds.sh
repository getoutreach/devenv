#!/usr/bin/env bash
set -e

# Things that we want to allow to be created in the dev-environment, but don't neccessarily use / provide
# should be added here. As an example, we provide VPA CRDs to allow resources to be created
# but then essentially "noop" them.
CRD_URLS=(
  https://raw.githubusercontent.com/kubernetes/autoscaler/vertical-pod-autoscaler-0.6.3/vertical-pod-autoscaler/deploy/vpa-{,beta2-}crd.yaml
)

for url in "${CRD_URLS[@]}"; do
  kubectl apply --validate=false -f "$url" >/dev/null || echo "Warning: failed to create CRDs, cluster creation may fail"
done
