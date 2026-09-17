#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# S4 oracle: replays every kubectl call that produced testdata/golden/ and
# testdata/snapshot/ against the kind oracle (context kind-fathom-oracle,
# server v1.37.0). Namespace s4 only. Real objects are left on the cluster so
# the quota status snapshot stays reproducible; re-running is idempotent
# (kubectl apply / create --dry-run=server).
#
# Usage: testdata/commands.sh            (run from anywhere)
set -uo pipefail

CTX=${FATHOM_ORACLE_CONTEXT:-kind-fathom-oracle}
NS=s4
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
G="$T/golden"
S="$T/snapshot"
O="objects"   # relative on purpose: the -f path is echoed in server error texts
K="kubectl --context $CTX"
mkdir -p "$G" "$S/rbac"
cd "$T" || exit 1

# dryrun <name> <file> [mutated]
#   Runs `kubectl apply --dry-run=server`, saves stderr verbatim to
#   golden/<name>.server.txt and, when the third argument is "mutated", the
#   server-mutated object (-o yaml) to golden/<name>.mutated.yaml.
#   Exit codes are appended to golden/exitcodes.txt.
dryrun() {
  local name=$1 file=$2 mode=${3:-}
  local out="$G/$name.mutated.yaml"
  if [ "$mode" = mutated ]; then
    $K apply --dry-run=server -f "$file" -o yaml >"$out" 2>"$G/$name.server.txt"
  else
    $K apply --dry-run=server -f "$file" >/dev/null 2>"$G/$name.server.txt"
  fi
  local rc=$?
  echo "$name $rc" >>"$G/exitcodes.txt"
  printf '%-18s rc=%d  %s\n' "$name" "$rc" "$(head -c 200 "$G/$name.server.txt" | tr '\n' ' ')"
}

# cani <verb> <resource[/name]> [--subresource=X] [-n ns]
#   NOTE kubectl semantics: "TYPE/NAME" is a resourceName check; a subresource
#   check needs --subresource=X. Verdicts append to golden/rbac-can-i.txt.
cani() {
  local verb=$1 resource=$2; shift 2
  local args=("$@")
  local verdict
  verdict=$($K auth can-i --as=system:serviceaccount:$NS:deployer "$verb" "$resource" "${args[@]}" 2>/dev/null)
  local rc=$?
  echo "verb=$verb resource=$resource args=[${args[*]}] verdict=$verdict rc=$rc" >>"$G/rbac-can-i.txt"
  printf '  can-i %-7s %-32s %-22s -> %s (rc=%d)\n' "$verb" "$resource" "${args[*]}" "$verdict" "$rc"
}

: >"$G/exitcodes.txt"
: >"$G/rbac-can-i.txt"

echo "== server version"
$K version -o yaml | sed -n '/serverVersion/,$p' | grep -E 'gitVersion' | tee "$G/server-version.txt"

echo "== 0. namespace + LimitRange"
$K apply -f "$S/namespace.yaml"
$K apply -f "$S/limitrange.yaml"

# re-run support: real-pod-b (ours, created in phase B) must not exist yet, or
# the quota (pods=3) would deny the allowed LimitRange scenarios on a replay.
$K delete pod real-pod-b -n $NS --ignore-not-found --now --wait=true
# ... and the quota controller must have seen it go, or the admitted scenarios
# below are denied by the stale pods=3/3 (first run: no quota yet, loop is a no-op).
for i in $(seq 1 30); do
  used=$($K get resourcequota s4-quota -n $NS -o jsonpath='{.status.used.pods}' 2>/dev/null)
  [ -z "$used" ] || [ "$used" = "2" ] && break
  sleep 1
done

echo "== 1. LimitRange scenarios (first run: before any quota exists)"
dryrun lr-defaults      "$O/lr-defaults.yaml"      mutated
dryrun lr-request-only  "$O/lr-request-only.yaml"  mutated
dryrun lr-over-max      "$O/lr-over-max.yaml"
dryrun lr-under-min     "$O/lr-under-min.yaml"
dryrun lr-ratio         "$O/lr-ratio.yaml"
dryrun lr-pod-max       "$O/lr-pod-max.yaml"
dryrun lr-pod-level-cpu "$O/lr-pod-level-cpu.yaml"

echo "== 1b. pod-level hugepages under the temporary LimitRange (Pod max hugepages-2Mi)"
$K apply -f "$S/limitrange-hugepages.yaml"
dryrun lr-pod-level-hugepages "$O/lr-pod-level-hugepages.yaml" mutated
$K apply -f "$S/limitrange.yaml"   # restore: every other pod has no hugepages limit
dryrun pvc-min          "$O/pvc-min.yaml"
dryrun pvc-max          "$O/pvc-max.yaml"

echo "== 2. PriorityClass + ResourceQuotas"
$K apply -f "$S/priorityclass.yaml"
$K apply -f "$S/quota.yaml"

echo "== 2a. real objects, phase A (2 pods: one normal, one high priority)"
$K apply -f "$O/real-pod-a.yaml"
$K apply -f "$O/real-pod-high.yaml"
$K apply -f "$O/real-pvc-a.yaml"
$K apply -f "$O/real-svc-lb.yaml"
# wait until the quota controller reports the consumption
for i in $(seq 1 30); do
  used=$($K get resourcequota s4-quota -n $NS -o jsonpath='{.status.used.pods}/{.status.used.persistentvolumeclaims}/{.status.used.services\.loadbalancers}')
  [ "$used" = "2/1/1" ] && break
  sleep 1
done
$K get resourcequota -n $NS -o yaml >"$S/quota-status.yaml"
echo "quota-status.yaml used: $($K get resourcequota s4-quota -n $NS -o jsonpath='{.status.used}')"

echo "== 2b. quota dry-runs against quota-status.yaml"
dryrun q-cpu-exceeded   "$O/q-cpu-exceeded.yaml"
dryrun q-pvc-storage    "$O/q-pvc-storage.yaml"
dryrun q-lb             "$O/q-lb.yaml"
dryrun q-scope-high     "$O/q-scope-high.yaml"
dryrun q-ok             "$O/q-ok.yaml"            mutated

echo "== 2c. real objects, phase B (3rd pod) -> quota-status-3pods.yaml"
$K apply -f "$O/real-pod-b.yaml"
for i in $(seq 1 30); do
  used=$($K get resourcequota s4-quota -n $NS -o jsonpath='{.status.used.pods}')
  [ "$used" = "3" ] && break
  sleep 1
done
$K get resourcequota -n $NS -o yaml >"$S/quota-status-3pods.yaml"
echo "quota-status-3pods.yaml used: $($K get resourcequota s4-quota -n $NS -o jsonpath='{.status.used}')"
dryrun q-pods-exceeded  "$O/q-pods-exceeded.yaml"

echo "== 3. RBAC"
$K apply -f "$S/rbac/fixture.yaml"
# wait for the aggregation controller to fill s4-aggregate
for i in $(seq 1 30); do
  n=$($K get clusterrole s4-aggregate -o jsonpath='{.rules}' | grep -c namespaces)
  [ "$n" -ge 1 ] && break
  sleep 1
done
$K get serviceaccount deployer -n $NS -o yaml >"$S/rbac/serviceaccount.yaml"
$K get role deployer-role -n $NS -o yaml >"$S/rbac/role.yaml"
$K get rolebinding deployer-binding -n $NS -o yaml >"$S/rbac/rolebinding.yaml"
$K get clusterrole s4-view-nodes s4-aggregate s4-view-namespaces -o yaml >"$S/rbac/clusterroles.yaml"
$K get clusterrolebinding s4-view-nodes-binding s4-aggregate-binding -o yaml >"$S/rbac/clusterrolebindings.yaml"

echo "== 3a. kubectl auth can-i as system:serviceaccount:s4:deployer"
cani create deployments                     -n $NS
cani list   deployments                     -n $NS
cani delete deployments                     -n $NS
cani get    deployments                     -n default
cani get    deployments  --subresource=scale -n $NS
cani update deployments  --subresource=scale -n $NS
cani get    deployments/scale               -n $NS
cani get    configmaps                      -n $NS
cani get    configmaps/app-config           -n $NS
cani update configmaps/app-config           -n $NS
cani update configmaps/other-config         -n $NS
cani update configmaps                      -n $NS
cani get    secrets                         -n $NS
cani get    secrets/app-config              -n $NS
cani delete services                        -n $NS
cani patch  services                        -n $NS
cani get    services                        -n default
cani get    nodes
cani list   nodes
cani delete nodes
cani get    nodes/fathom-oracle-control-plane
cani list   namespaces
cani get    pods                            -n $NS

echo "== done: goldens in $G, snapshot in $S"
