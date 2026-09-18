#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Fetch the S7 300-CRD snapshot layer from upstream release artifacts and split
# it into snapshot/crds/<crd name>.yaml (one served apiextensions.k8s.io/v1 CRD
# per file, verbatim). Sources and tags are the table below; SOURCES.md is the
# human record. Re-running starts from an empty crds/ directory so the set is
# reproducible; downloads are cached in $CACHE.
set -euo pipefail
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CACHE="${S7_CRD_CACHE:-$(mktemp -d "${TMPDIR:-/tmp}/s7-crds.XXXXXX")}"
OUT="$T/snapshot/crds"
mkdir -p "$CACHE"
# fresh directory, no recursive force delete: move the old one aside
if [ -d "$OUT" ]; then mv "$OUT" "$CACHE/crds.old.$(date +%s)"; fi
mkdir -p "$OUT"

fetch() { # fetch <label> <url>...  -> downloads then splits
  local label="$1"; shift
  local files=()
  for u in "$@"; do
    local f="$CACHE/$label-$(basename "$u" | tr -c 'A-Za-z0-9._-' '_')"
    [ -s "$f" ] || curl -fsSL --retry 3 -o "$f" "$u"
    files+=("$f")
  done
  python3 "$T/split-crds.py" "$OUT" "$label" "${files[@]}"
}
ghdir() { # ghdir <repo> <ref> <path> -> raw URLs of *.yaml in that directory
  curl -fsSL "https://api.github.com/repos/$1/contents/$3?ref=$2" \
    | jq -r '.[] | select(.name | endswith(".yaml")) | .download_url'
}

fetch cert-manager@v1.21.2 https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.crds.yaml
fetch gateway-api-standard@v1.6.2 https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.2/standard-install.yaml
fetch gateway-api-experimental@v1.6.2 https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.2/experimental-install.yaml
fetch prometheus-operator@v0.94.0 https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.94.0/bundle.yaml
fetch argo-cd@v3.5.3 \
  https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.3/manifests/crds/application-crd.yaml \
  https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.3/manifests/crds/applicationset-crd.yaml \
  https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.3/manifests/crds/appproject-crd.yaml
fetch argo-workflows@v3.7.18 $(ghdir argoproj/argo-workflows v3.7.18 manifests/base/crds/full)
fetch argo-rollouts@v1.10.0 https://github.com/argoproj/argo-rollouts/releases/download/v1.10.0/install.yaml
fetch flux2@v2.9.5 https://github.com/fluxcd/flux2/releases/download/v2.9.5/install.yaml
fetch crossplane@v1.20.13 $(ghdir crossplane/crossplane v1.20.13 cluster/crds)
fetch external-secrets@v0.20.4 https://raw.githubusercontent.com/external-secrets/external-secrets/v0.20.4/deploy/crds/bundle.yaml
fetch kyverno@v1.19.1 https://github.com/kyverno/kyverno/releases/download/v1.19.1/install.yaml
fetch cilium@v1.20.2 $(ghdir cilium/cilium v1.20.2 pkg/k8s/apis/cilium.io/client/crds/v2) $(ghdir cilium/cilium v1.20.2 pkg/k8s/apis/cilium.io/client/crds/v2alpha1)
fetch istio@1.31.0 https://raw.githubusercontent.com/istio/istio/1.31.0/manifests/charts/base/files/crd-all.gen.yaml
fetch knative-serving@v1.23.0 https://github.com/knative/serving/releases/download/knative-v1.23.0/serving-crds.yaml
fetch knative-eventing@v1.23.0 https://github.com/knative/eventing/releases/download/knative-v1.23.0/eventing-crds.yaml
fetch tekton-pipeline@v1.16.0 https://github.com/tektoncd/pipeline/releases/download/v1.16.0/release.yaml
fetch keda@v2.20.2 https://github.com/kedacore/keda/releases/download/v2.20.2/keda-2.20.2-crds.yaml
fetch velero@v1.18.2 $(ghdir vmware-tanzu/velero v1.18.2 config/crd/v1/bases) $(ghdir vmware-tanzu/velero v1.18.2 config/crd/v2alpha1/bases)
fetch cloudnative-pg@v1.30.0 https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.30/releases/cnpg-1.30.0.yaml
fetch strimzi@0.51.0 https://github.com/strimzi/strimzi-kafka-operator/releases/download/0.51.0/strimzi-crds-0.51.0.yaml
fetch longhorn@v1.12.1 https://raw.githubusercontent.com/longhorn/longhorn/v1.12.1/deploy/longhorn.yaml
fetch cluster-api@v1.14.2 \
  https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.2/core-components.yaml \
  https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.2/bootstrap-components.yaml \
  https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.2/control-plane-components.yaml
fetch metallb@v0.16.1 https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml
fetch traefik@v3.7.13 https://raw.githubusercontent.com/traefik/traefik/v3.7.13/docs/content/reference/dynamic-configuration/kubernetes-crd-definition-v1.yml
fetch opentelemetry-operator@v0.159.0 https://github.com/open-telemetry/opentelemetry-operator/releases/download/v0.159.0/opentelemetry-operator.yaml
# reserve (not needed for the 300 target): rook@v1.20.7 (21 CRDs, 10 with CEL)
#fetch rook@v1.20.7 https://raw.githubusercontent.com/rook/rook/v1.20.7/deploy/examples/crds.yaml
# reserve (not needed for the 300 target): kueue@v0.19.5 (11 CRDs, 9 with CEL)
#fetch kueue@v0.19.5 https://github.com/kubernetes-sigs/kueue/releases/download/v0.19.5/manifests.yaml

echo "total CRDs: $(ls "$OUT"/*.yaml | wc -l)"
echo "with x-kubernetes-validations: $(grep -l 'x-kubernetes-validations' "$OUT"/*.yaml | wc -l)"
