#!/usr/bin/env bash
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

# shellcheck source=./lib/os.sh
source "$DIR/lib/os.sh"

repoDir="$HOME/.local/dev-environment/.outreach-ca"

echo "checking local CA status"

if [[ ! -e "$repoDir/tls.crt" ]]; then
  # In case we got interrupted at somepoint, lets ensure
  # we have a clean base.
  rm -rf "$repoDir"

  echo "Generating CA"
  mkdir -p "$repoDir"
  pushd "$repoDir" >/dev/null 2>&1 || exit 1
  # fix for OSX
  cp /etc/ssl/openssl.cnf openssl.cnf
  cat >>openssl.cnf <<EOF
[ v3_ca ]
basicConstraints = critical,CA:TRUE
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer:always
EOF

  x509_subject="/C=US/ST=Washington/L=Seattle/O=Outreach.io/OU=Engineering/CN=Outreach Dev CA/emailAddress=dev@outreach.io"
  openssl genrsa -out "tls.key" 4096
  openssl req -x509 -new -nodes -key "tls.key" -sha256 -days 3650 -out "tls.crt" -subj "${x509_subject}" -extensions v3_ca -config openssl.cnf

  popd >/dev/null 2>&1 || exit 1

  # add the cert to our keychain
  if [[ $OS == "darwin" ]]; then
    echo "adding CA to keychain"
    sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain "$repoDir/tls.crt"
  elif [[ $OS == "linux" ]] || [[ $OS == "windows" ]]; then
    # This should work for WSL as well as Linux, but I haven't been able to verify.
    # Some sort of canary step here to be able to test if the cert actually was
    # installed correctly would be cool.
    sudo mkdir -p /usr/local/share/ca-certificates/outreach
    sudo cp "$repoDir/tls.crt" /usr/local/share/ca-certificates/outreach/dev-env-ca.crt

    # supposedly chromium snap will support ~/.pki, so add it to that as well, if it exists.
    if [[ -e "$HOME/.pki/nssdb" ]]; then
      certutil -d "sql:$HOME/.pki/nssdb" -D -n dev-env >/dev/null 2>&1 || true
      certutil -d "sql:$HOME/.pki/nssdb" -A -t "C,," -n "dev-env" -i "$repoDir/tls.crt" || true
    fi

    # support chromium snap
    if [[ -e "$HOME/snap/chromium/current/.pki/nssdb" ]]; then
      certutil -d "sql:$HOME/snap/chromium/current/.pki/nssdb" -D -n dev-env >/dev/null 2>&1 || true
      certutil -d "sql:$HOME/snap/chromium/current/.pki/nssdb" -A -t "C,," -n "dev-env" -i "$repoDir/tls.crt"
    fi

    if command -v update-ca-certificates >/dev/null; then
      sudo update-ca-certificates
    elif command -v trust >/dev/null; then
      sudo trust extract-compat
    else
      echo "Error: Failed to add CA to system store (failed to find compatible store)"
      exit 1
    fi
  fi

  if [[ $OS == "windows" ]]; then
    echo "Adding CA certificate to the Windows CA store (this will open a UAC prompt)"

    # This is a fancy way to elevate ourselves to the Admin user on windows
    # but with our own password. Typically "runas" would be used but apparently
    # that requires you to enter the "Administrator" password, which doesn't really
    # work given a) users don't know it, b) it's generally not set on OS install.
    powershell.exe -c "Start-Process -Verb runAs certutil.exe -ArgumentList '-addstore root $(wslpath -w "$HOME/.local/dev-environment/.outreach-ca/tls.crt")'"
  fi
fi

wait_for_app_start "webhook" "cert-manager" app cert-manager

echo "creating clusterissuer"
cat >/tmp/clusterissuer.yaml <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned
  namespace: cert-manager
spec:
  ca:
    secretName: ca
EOF

echo "uploading CA to kubernetes"
kubectl delete -n "cert-manager" secret generic ca >/dev/null 2>&1 || true
retry kubectl create -n "cert-manager" secret generic ca \
  --from-file="tls.crt=$repoDir/tls.crt" \
  --from-file="tls.key=$repoDir/tls.key"

# Delete an existing one, if it exists
kubectl delete -f /tmp/clusterissuer.yaml >/dev/null 2>&1 || true
retry kubectl create -f /tmp/clusterissuer.yaml

echo -e "waiting for ClusterIssuer to be ready ...\c"
until kubectl get clusterissuer "selfsigned" 2>&1 | grep True >/dev/null; do
  echo -n "."
  sleep 5
done
echo "done"
