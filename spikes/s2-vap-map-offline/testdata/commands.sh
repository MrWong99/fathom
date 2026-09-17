#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Spike S2 oracle: runs every fixture against the kind cluster with
# --dry-run=server and captures the verbatim server answer into golden/.
# Re-runnable; only touches namespace s2 and the s2/vap-*/map-* policies.
set -u
cd "$(dirname "$0")"
CTX=${KUBE_CONTEXT:-kind-fathom-oracle}
K="kubectl --context $CTX"

echo "== server"
$K version
$K api-resources --api-group=admissionregistration.k8s.io
$K get --raw /apis/admissionregistration.k8s.io

echo "== namespace s2 (environment=prod team=shop)"
$K create namespace s2 --dry-run=client -o yaml | $K apply -f -
$K label namespace s2 environment=prod team=shop --overwrite

echo "== params and policies"
$K apply -f params/vap-limits.yaml
$K apply -f policies/vap-deny.yaml
$K apply -f policies/vap-warn.yaml
$K apply -f policies/vap-param-missing.yaml
$K apply -f policies/map-apply.yaml
$K apply -f policies/map-jsonpatch.yaml
$K apply -f policies/map-reinvoke.yaml

echo "== wait: VAP status.observedGeneration and typeChecking"
$K wait --for=jsonpath='{.status.observedGeneration}'=1 --timeout=60s validatingadmissionpolicy/vap-deny
$K get validatingadmissionpolicy vap-deny -o jsonpath='{.status}{"\n"}'

# retry <n> <grep-pattern> <cmd...>: rerun cmd until its output matches pattern
# (the apiserver policy informers take a moment; verdicts stabilise in seconds).
retry() {
  local n=$1 pat=$2; shift 2
  local i out
  for i in $(seq 1 "$n"); do
    out=$("$@" 2>&1)
    if grep -q -- "$pat" <<<"$out"; then return 0; fi
    sleep 1
  done
  echo "retry: '$pat' not seen after $n attempts; last output:" >&2
  echo "$out" >&2
  return 1
}
echo "== wait: verdicts stable"
retry 30 'exceeds maxReplicas'  $K apply --dry-run=server -f objects/vap-deny-violation.yaml
retry 30 'Warning:'             $K apply --dry-run=server -f objects/vap-warn.yaml
retry 30 'no params found'      $K apply --dry-run=server -f objects/vap-param-missing.yaml
retry 30 'fathom.dev/mutated-by' $K apply --dry-run=server -o yaml -f objects/map-apply.yaml
retry 30 'fathom.dev/patched-by' $K apply --dry-run=server -o yaml -f objects/map-jsonpatch.yaml
retry 30 'tier-seen: backend'   $K apply --dry-run=server -o yaml -f objects/map-reinvoke.yaml

# capture <scenario> [apply|create]: verbatim stdout+stderr into golden/<scenario>.server.txt,
# exit code into golden/<scenario>.exit; for mutations also -o yaml into golden/<scenario>.mutated.yaml.
capture() {
  local s=$1 verb=${2:-apply} obj=objects/${3:-$1}.yaml
  $K "$verb" --dry-run=server -f "$obj" >"golden/$s.server.txt" 2>&1
  echo $? >"golden/$s.exit"
  echo "-- $s (exit $(cat golden/$s.exit))"; cat "golden/$s.server.txt"
}
mutated() {
  local s=$1 verb=${2:-apply} obj=objects/${3:-$1}.yaml
  $K "$verb" --dry-run=server -o yaml -f "$obj" >"golden/$s.mutated.yaml" 2>"golden/$s.mutated.stderr.txt"
  [ -s "golden/$s.mutated.stderr.txt" ] || rm -f "golden/$s.mutated.stderr.txt"
}

echo "== capture"
capture vap-deny-violation
capture vap-deny-pass
capture vap-deny-excluded
capture vap-warn
capture vap-param-missing
capture map-apply;      mutated map-apply
capture map-jsonpatch;  mutated map-jsonpatch
# kubectl create sends no last-applied annotation, so the JSONPatch takes the
# "annotations absent" branch; kubectl apply (client-side) takes the other one.
capture map-jsonpatch-create create map-jsonpatch; mutated map-jsonpatch-create create map-jsonpatch
capture map-reinvoke;   mutated map-reinvoke

echo "== openapi documents (layout of kubectl-validate openapiclient.NewLocalSchemaFiles)"
mkdir -p openapi/api openapi/apis/apps openapi/apis/admissionregistration.k8s.io
$K get --raw /openapi/v3 > openapi/index.json
$K get --raw /openapi/v3/api/v1 > openapi/api/v1.json
$K get --raw /openapi/v3/apis/apps/v1 > openapi/apis/apps/v1.json
$K get --raw /openapi/v3/apis/admissionregistration.k8s.io/v1 > openapi/apis/admissionregistration.k8s.io/v1.json

echo "== snapshot (namespace, params, policies as served)"
$K get namespace s2 -o yaml > snapshot/namespace-s2.yaml
$K get configmap vap-limits -n s2 -o yaml > snapshot/configmap-vap-limits.yaml
$K get validatingadmissionpolicies -o yaml > snapshot/validatingadmissionpolicies.yaml
$K get validatingadmissionpolicybindings -o yaml > snapshot/validatingadmissionpolicybindings.yaml
$K get mutatingadmissionpolicies -o yaml > snapshot/mutatingadmissionpolicies.yaml
$K get mutatingadmissionpolicybindings -o yaml > snapshot/mutatingadmissionpolicybindings.yaml
$K auth whoami -o yaml > snapshot/whoami.yaml
echo "== done"
