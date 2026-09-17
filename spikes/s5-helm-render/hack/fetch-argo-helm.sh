#!/usr/bin/env sh
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Fetch the exact Helm binary Argo CD bundles at a given tag and verify it
# against Argo's own checksum file. Prints the path of the helm binary.
#   hack/fetch-argo-helm.sh v3.5.3 /some/dir
set -eu
tag="${1:?argo-cd tag, e.g. v3.5.3}"
out="${2:?output directory}"
raw="https://raw.githubusercontent.com/argoproj/argo-cd/${tag}"
ver="$(curl -sSL "${raw}/hack/tool-versions.sh" | sed -n 's/^helm4_version=//p')"
[ -n "$ver" ] || { echo "no helm4_version in ${tag}/hack/tool-versions.sh" >&2; exit 1; }
mkdir -p "$out" && cd "$out"
tgz="helm-v${ver}-linux-amd64.tar.gz"
[ -f "$tgz" ] || curl -sSL -o "$tgz" "https://get.helm.sh/${tgz}"
curl -sSL -o "${tgz}.sha256" "${raw}/hack/installers/checksums/${tgz}.sha256"
sha256sum -c "${tgz}.sha256" >&2
tar xzf "$tgz"
echo "$(pwd)/linux-amd64/helm"
