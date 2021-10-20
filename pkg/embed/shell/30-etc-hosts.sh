#!/usr/bin/env bash
#
# run something after the cluster is created
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

# shellcheck source=./lib/os.sh
source "$DIR/lib/os.sh"

IFS=" " read -r -a domains <<<"$(kubectl --context=dev-environment get ingress --all-namespaces -o jsonpath='{.items[*].spec.rules[*].host}')"

hostsFile="/etc/hosts"
if [[ $OS == "windows" ]]; then
  # TODO: What if the host drive isn't C:?
  hostsFile="/mnt/c/Windows/System32/drivers/etc/hosts"
fi

INGRESS_CONTROLLER_IP=$1
if [[ -z $INGRESS_CONTROLLER_IP ]]; then
  # shellcheck disable=SC2016
  echo 'Error: Missing argv $1, ingress controller IP'
  exit 1
fi

# We can't modify the file in Docker, which we use in CI, so for now
# just write to that w/o the one-time sudo optimization.
if [[ -z $CI ]]; then
  tempFile=$(mktemp)
  cp "$hostsFile" "$tempFile"
else
  tempFile="$hostsFile"
fi

echo "Configuring /etc/hosts to point to ingress controller at $INGRESS_CONTROLLER_IP"

modified=false

# Check if the file doesn't end with a newline
if [[ "$(tail -c 1 /etc/hosts | wc -l)" -eq 0 ]]; then
  if [[ -z $CI ]]; then
    echo "" >"$tempFile"
  else
    sudo bash -c "echo '' > '$tempFile'"
  fi
  modified=true
fi

for domain in "${domains[@]}"; do
  # If we have an entry for the domain that isn't the IP we expect, then remove the line
  # and mark that we modified the file
  if grep "$domain" "$hostsFile" | grep -v "$INGRESS_CONTROLLER_IP" >/dev/null 2>&1; then
    if [[ -z $CI ]]; then
      # Why: We need to use a subshell to avoid a temporary file
      # shellcheck disable=SC2005
      echo "$(grep -v "$domain" "$tempFile")" >"$tempFile"
    else
      sudo bash -c "echo '\$(grep -v \"$domain\" \"$tempFile\")' > '$tempFile'"
    fi
    modified=true
  fi

  # If we don't already have the entry in our hosts file, add it and mark that we modified the file.
  if ! grep "$domain" "$hostsFile" | grep "$INGRESS_CONTROLLER_IP" >/dev/null 2>&1; then
    if [[ -z $CI ]]; then
      echo "$INGRESS_CONTROLLER_IP $domain" >>"$tempFile"
    else
      sudo bash -c "echo '$INGRESS_CONTROLLER_IP $domain' >>'$tempFile'"
    fi
    modified=true
  fi
done

# Only replace the file when it's been updated.
if [[ $modified == "true" ]] && [[ -z $CI ]]; then
  # To minimize the number of sudo / UAC calls we have to
  # move the hosts file to replace the existing one
  if [[ $OS == "windows" ]]; then
    powershell.exe -c \
      "Start-Process -Verb runAs powershell.exe -ArgumentList '-c Move-Item -Force -Path \"$(wslpath -w "$tempFile")\" -Destination \"$(wslpath -w "$hostsFile")\"'"
  else
    echo "Updating $hostsFile, password prompt (if present) is for sudo access"
    sudo mv "$tempFile" "$hostsFile"
    rm "$tempFile" >/dev/null 2>&1 || true
  fi
fi
