#!/usr/bin/env bash

if [[ $OSTYPE == "linux-gnu" ]]; then
  kernel_version="$(uname -r)"
  case $kernel_version in
  *-Microsoft | *-microsoft-standard)
    OS="windows"
    ;;
  *)
    OS="linux"
    ;;
  esac
elif [[ $OSTYPE == "darwin"* ]]; then
  # Why: Used in the scripts outside of this dir
  # shellcheck disable=SC2034
  OS="darwin"
else
  echo "Warning: failed to determine operating system, assuming linux based"

  # Why: Used in the scripts outside of this dir
  # shellcheck disable=SC2034
  OS=linux
fi

dev_kubectl() {
  kubectl --context=dev-environment "$@"
}

find_pod_metadata() {
  local label="$1"
  local value="$2"
  local namespace="$3"
  local metadataType="$4"
  local namespaceArgs

  if [[ -n $namespace ]]; then
    namespaceArgs="--namespace $namespace"
  else
    namespaceArgs="--all-namespaces"
  fi

  dev_kubectl get pods $namespaceArgs --selector="$label"="$value" --output=jsonpath="{.items[0].metadata.$metadataType}" 2>/dev/null
}

find_pod_name() {
  local label="$1"
  local value="$2"
  local namespace="$3"

  find_pod_metadata "$label" "$value" "$namespace" name
}

find_namespace() {
  local label="$1"
  local value="$2"

  find_pod_metadata "$label" "$value" "" namespace
}

infer_app_namespace() {
  local appName="$1"
  find_namespace app "$appName"
}

get_ready_pods() {
  local label="$1"
  local value="$2"
  local namespace="$3"

  pod="$(find_pod_name "$label" "$value" "$namespace")"
  if [[ -z $pod ]]; then
    # clear the previous
    if [[ -z $CI ]]; then
      tput cuu 2

      # We need two lines to match the output of kubectl get output
      echo "STATUS"
    fi

    dev_kubectl get pod -n "$namespace" -l"$label"="$value"
    return 1
  fi

  pod_status=$(dev_kubectl get pod -n "$namespace" "$pod" -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)

  # Check if pod is ready, if not, then we output the status
  if [[ $pod_status != "True" ]]; then
    if [[ -z $CI ]]; then
      tput cuu 2
    fi

    dev_kubectl get pod -n "$namespace" "$pod"
    return 1
  fi

  # pod is ready, return 0
  return 0
}

wait_for_app_start() {
  local value="$1"
  local appName="$2"
  local label="$3"

  if [[ -z $appName ]]; then
    appName="$1"
  fi

  if [[ -z $label ]]; then
    label="app"
  fi

  # We want to auto-detect namespaces, if possible.
  # shellcheck disable=SC2155
  local namespace="$4"

  if [[ -z $namespace ]]; then
    echo "finding '$appName'"
    if ! retry find_namespace "$label" "$value" >/dev/null; then
      echo "Warning: failed to find a namespace for '$appName', will not wait for it to be ready"
      return
    fi

    namespace=$(find_namespace "$label" "$value")

    if [[ -z $namespace ]]; then
      echo "Error: Failed to get namespace after we already found it..."
      exit 1
    fi
  fi

  echo "waiting for '$appName' to be available ..."

  if [[ -z $CI ]]; then
    echo -ne "\n\n"
  fi

  while ! get_ready_pods "$label" "$value" "$namespace"; do
    sleep 5
  done

  # was this all worth it? Probably not.
  if [[ -z $CI ]]; then
    tput cuu 2
    echo -ne "\033[K\n\033[K\n"
    tput cuu 3
  fi
  echo "waiting for '$appName' to be available ... done"
}

# retry a command x times, and then fails
retry() {
  local command="$1"
  local retries=10
  shift

  local i=1
  while [[ $i -ne $retries ]]; do
    if ! "$command" "$@"; then
      i=$((i + 1))
      delay=$((5 * i))
      echo " ... retrying in ${delay}s ($i/$retries) " >&2
      sleep "$delay"
    else
      break
    fi
  done

  if [[ $i -eq $retries ]]; then
    echo "Error: Failed to run '$command', hit maximum number of retries"
    return 1
  fi

  return 0
}
