#!/bin/bash
# Stand-in for the GitHub runner's bin/installdependencies.sh in the host
# smoke test. It does what the official script does on Fedora, which the
# podman machine VM runs: as root, install the runner's system dependencies
# with dnf.
if [ "$(id -u)" -ne 0 ]; then
  echo 'Need to run with sudo privilege'
  exit 1
fi
dnf install -y lttng-ust openssl-libs krb5-libs zlib libicu || {
  echo "'dnf' failed with exit code '$?'"
  exit 1
}
