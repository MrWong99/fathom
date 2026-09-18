#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Spike S7 fixture capture replay. Re-captures the cluster-derived layers of
# testdata/snapshot/ from the kind oracle (read-only: nothing is applied, the
# authored objects are only checked with --dry-run=server) and rebuilds the
# derived files (CRD layer via fetch-crds.sh, app render via app/render.sh).
#
# Oracle used to record the fixtures: kind context kind-fathom-oracle, server
# v1.37.0 (kindest/node v1.37.0), kubectl client v1.36.4, helm v4.3.0, Kyverno
# CLI v1.19.1 at /home/luk/go/bin/kyverno
# (sha256 dfa1ffe747e43d0d5a34cbc676ff96ecafdf7ef979eee1c6d9d6606e3613c138).
# Machine: 13th Gen Intel Core i9-13900K, 32 logical CPUs, cpufreq governor
# powersave, Linux 7.2.6 (cachyos).
set -euo pipefail
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CTX="${FATHOM_ORACLE_CONTEXT:-kind-fathom-oracle}"
S="$T/snapshot"
k() { kubectl --context "$CTX" "$@"; }
cd "$T"

echo "== server"
k version -o json | jq -r '.serverVersion.gitVersion' | tee "$S/server-version.txt"

echo "== openapi (layout of kubectl-validate openapiclient.NewLocalSchemaFiles: /api/<v>.json, /apis/<g>/<v>.json)"
mkdir -p "$S/openapi/api" "$S/openapi/apis"
k get --raw /openapi/v3 > "$S/openapi/index.json"
for gv in api/v1 apis/apps/v1 apis/batch/v1 apis/networking.k8s.io/v1 apis/policy/v1 apis/admissionregistration.k8s.io/v1; do
  mkdir -p "$S/openapi/$(dirname "$gv")"
  k get --raw "/openapi/v3/$gv" > "$S/openapi/$gv.json"
done

echo "== discovery (aggregated APIGroupDiscoveryList v2, via kubectl proxy so the Accept header can be set)"
PORT=18477
k proxy --port="$PORT" --address=127.0.0.1 >/dev/null 2>&1 &
PROXY=$!
trap 'kill $PROXY 2>/dev/null || true' EXIT
for i in $(seq 1 50); do curl -fs "http://127.0.0.1:$PORT/version" >/dev/null 2>&1 && break; sleep 0.2; done
ACCEPT='application/json;g=apidiscovery.k8s.io;v=v2;as=APIGroupDiscoveryList'
curl -fsS -H "Accept: $ACCEPT" "http://127.0.0.1:$PORT/api"  > "$S/discovery/api.json"
curl -fsS -H "Accept: $ACCEPT" "http://127.0.0.1:$PORT/apis" > "$S/discovery/apis.json"
kill $PROXY; wait $PROXY 2>/dev/null || true; trap - EXIT
jq -r '.kind' "$S/discovery/api.json" "$S/discovery/apis.json"

echo "== catalogs (names + full objects; the cluster has only kind's defaults)"
k get storageclasses  -o yaml > "$S/catalogs/storageclasses.yaml"
k get priorityclasses -o yaml > "$S/catalogs/priorityclasses.yaml"
k get ingressclasses  -o yaml > "$S/catalogs/ingressclasses.yaml"
k get runtimeclasses  -o yaml > "$S/catalogs/runtimeclasses.yaml"
k get csidrivers      -o yaml > "$S/catalogs/csidrivers.yaml"

echo "== authored objects: schema check only (--dry-run=server, nothing persisted; s7 never exists on the oracle)"
k apply --dry-run=server -f "$S/namespaces/s7/namespace.yaml"
# namespaced objects need the namespace to exist for a server dry run, so they
# are checked against namespace s7 rewritten to 'default' on the fly.
for f in "$S/namespaces/s7/limitrange.yaml" "$S/namespaces/s7/resourcequota.yaml" "$S/rbac/role.yaml" "$S/rbac/rolebinding.yaml" "$S/rbac/serviceaccount.yaml"; do
  sed 's/^  namespace: s7$/  namespace: default/' "$f" | k apply --dry-run=server -f -
done
k apply --dry-run=server -f "$S/admission/validatingadmissionpolicies.yaml" -f "$S/admission/validatingadmissionpolicybindings.yaml" \
                         -f "$S/admission/mutatingadmissionpolicies.yaml" -f "$S/admission/mutatingadmissionpolicybindings.yaml"

echo "== derived layers"
"$T/fetch-crds.sh"
"$T/app/render.sh"
echo "== kyverno policies parse check (CLI, no cluster)"
KYVERNO="${FATHOM_KYVERNO_BIN:-/home/luk/go/bin/kyverno}"
env -i HOME=/tmp PATH=/usr/bin:/bin "$KYVERNO" apply "$S/engines/kyverno" --resource "$T/app/rendered.yaml" --exception "$S/engines/kyverno/polex-redis-metrics.yaml" --exception "$S/engines/kyverno/polex-rabbitmq-cluster.yaml" -t 2>&1 | tail -3 || true
