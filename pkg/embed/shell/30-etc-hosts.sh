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

# We can't modify the file in Docker, which we use in CI, so for now
# just write to that w/o the one-time sudo optimization.
if [[ -z $CI ]]; then
  cp "$hostsFile" "$tempFile"
  tempFile=$(mktemp)
else
  tempFile="$hostsFile"
fi

modified=false
for domain in "${domains[@]}"; do
  if ! grep "$domain" "$hostsFile" >/dev/null 2>&1; then
    echo " ADD $domain"
    echo "127.0.0.1 $domain" >>"$tempFile"
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
