#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Spike S3 oracle + CLI probe replay. Rewrites golden/<scenario>.server.txt
# (kubectl stdout+stderr, verbatim), golden/passing-mutated.mutated.yaml (the
# Deployment as the cluster stored it after Kyverno mutated it), cli/out/*.txt
# (kyverno CLI stdout+stderr, verbatim) and prints cold-start timings.
#
# Oracle used to record the goldens: kind context kind-fathom-oracle, server
# v1.37.0, kubectl client v1.36.4, Kyverno chart kyverno/kyverno 3.9.1
# (appVersion v1.19.1), Kyverno CLI v1.19.1 at /home/luk/go/bin/kyverno
# (sha256 dfa1ffe747e43d0d5a34cbc676ff96ecafdf7ef979eee1c6d9d6606e3613c138).
# The CLI is always run with `env -i` so nothing from the caller's environment
# (KUBECONFIG, HOME, proxies) reaches it; every input is an explicit file.
set -u
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CTX="${FATHOM_ORACLE_CONTEXT:-kind-fathom-oracle}"
KYVERNO="${FATHOM_KYVERNO_BIN:-/home/luk/go/bin/kyverno}"
CHART_VERSION=3.9.1            # appVersion v1.19.1
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/s3-kyverno.XXXXXX")"
k() { kubectl --context "$CTX" "$@"; }
ky() { env -i HOME="$SCRATCH/home" PATH=/usr/bin:/bin "$KYVERNO" "$@"; }
mkdir -p "$SCRATCH/home" "$T/golden" "$T/cli/out"
cd "$T"

# --- 1. Kyverno in the oracle cluster ---------------------------------------
helm --kube-context "$CTX" repo add kyverno https://kyverno.github.io/kyverno/ >/dev/null 2>&1 || true
helm --kube-context "$CTX" repo update kyverno >/dev/null
# PolicyExceptions are off by default in the chart (admission controller runs
# with --enablePolicyException=false); (e) needs them on for namespace s3.
helm --kube-context "$CTX" upgrade --install kyverno kyverno/kyverno -n kyverno --create-namespace \
  --version "$CHART_VERSION" \
  --set admissionController.replicas=1 \
  --set admissionController.container.resources.requests.cpu=50m \
  --set admissionController.container.resources.requests.memory=64Mi \
  --set backgroundController.resources.requests.cpu=20m \
  --set backgroundController.resources.requests.memory=32Mi \
  --set cleanupController.resources.requests.cpu=20m \
  --set cleanupController.resources.requests.memory=32Mi \
  --set reportsController.resources.requests.cpu=20m \
  --set reportsController.resources.requests.memory=32Mi \
  --set features.policyExceptions.enabled=true \
  --set features.policyExceptions.namespace=s3 >/dev/null
k -n kyverno rollout status deploy/kyverno-admission-controller --timeout=300s
helm --kube-context "$CTX" list -n kyverno
k api-resources --api-group=policies.kyverno.io 2>/dev/null | grep -E '^NAME|validatingpolicies'

# --- 2. snapshot side data + policies (a)..(e) -------------------------------
k apply -f snapshot/namespace.yaml -f snapshot/cm-allowed-teams.yaml -f snapshot/cm-replica-limits.yaml -f snapshot/rbac-argocd.yaml
k apply -f policies/a-require-team-label.yaml -f policies/b-add-default-securitycontext.yaml \
        -f policies/c-restrict-protected-deployer.yaml -f policies/d-vap-replica-limit.yaml \
        -f policies/e-polex-legacy-app.yaml
for p in require-team-label add-default-securitycontext restrict-protected-deployer; do
  k wait --for=condition=Ready "cpol/$p" --timeout=120s
done
sleep 3 # webhook reconfiguration after the policies turn Ready

# --- 3. server verdicts -------------------------------------------------------
AS=(--as=system:serviceaccount:argocd:argocd-application-controller
    --as-group=system:serviceaccounts:argocd --as-group=system:serviceaccounts --as-group=system:authenticated)
dry() { # dry <golden name> [kubectl apply flags...]; stdout+stderr kept verbatim
  local name="$1"; shift
  k apply --dry-run=server "$@" >"golden/$name.server.txt" 2>&1
  echo "$name: exit=$?"
}
k -n s3 delete deploy web-passing --ignore-not-found >/dev/null   # fresh CREATE for the mutation golden
dry violating-team         -f objects/violating-team.yaml            # denied by (a), message from the ConfigMap
dry excused                -f objects/excused.yaml                   # legacy-app, excused by (e)
dry protected-admin        -f objects/protected.yaml                 # denied by (c): kubernetes-admin
dry protected-argocd       "${AS[@]}" -f objects/protected.yaml     # allowed: impersonated argocd SA
dry vap-violating          -f objects/vap-violating.yaml             # denied by (d): apiserver VAP with paramRef
dry passing-mutated-dryrun -f objects/passing-mutated.yaml           # allowed
k apply -f objects/passing-mutated.yaml >golden/passing-mutated.server.txt 2>&1; echo "passing-mutated (real apply): exit=$?"
k -n s3 get deploy web-passing -o yaml >golden/passing-mutated.mutated.yaml
k auth whoami -o yaml >snapshot/identity-admin.yaml
k auth whoami "${AS[@]}" -o yaml >snapshot/identity-argocd.yaml

# (f) new-style ValidatingPolicy: applied on its own so the goldens above stay
# ClusterPolicy-only; removed again afterwards.
k apply -f policies/f-vpol-require-team-label.yaml
k wait --for=jsonpath='{.status.conditionStatus.ready}'=true vpol/vpol-require-team-label --timeout=120s
sleep 3
# Two validating webhooks (validate.kyverno.svc-fail for the ClusterPolicy and
# vpol.validate.kyverno.svc-fail for the ValidatingPolicy) both deny this object;
# the apiserver calls them in parallel and reports whichever fails first, so this
# golden alternates between the two texts (the other one is kept as
# golden/vpol-violating-team.alt.server.txt).
dry vpol-violating-team -f objects/violating-team.yaml
dry vpol-excused        -f objects/excused.yaml          # (e) does not cover the vpol: denied by vpol
dry vpol-passing        -f objects/passing-mutated.yaml
k delete -f policies/f-vpol-require-team-label.yaml

# --- 4. CLI probes ------------------------------------------------------------
run() { # run <case> <kyverno args...>; stdout+stderr kept verbatim in cli/out/<case>.txt
  local name="$1"; shift
  ky "$@" >"cli/out/$name.txt" 2>&1
  echo "$name: exit=$? :: kyverno $*"
}
P=policies; O=objects; SN=snapshot
# (i)..(v)
run i-pass                  apply $P/a-require-team-label.yaml --resource $O/passing-mutated.yaml --resource $SN/cm-allowed-teams.yaml
run i-pass-values-report    apply $P/a-require-team-label.yaml --resource $O/passing-mutated.yaml --values-file cli/values.yaml --policy-report
run ii-fail                 apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --resource $SN/cm-allowed-teams.yaml
run iii-fail-report         apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --resource $SN/cm-allowed-teams.yaml --policy-report
run iii-fail-report-json    apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --resource $SN/cm-allowed-teams.yaml --policy-report --output-format json
run iv-error-missing-cm     apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml
run iv-error-missing-cm-detailed apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --detailed-results
ky apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml -v 4 >cli/out/iv-error-missing-cm-v4.txt 2>&1; echo "iv-error-missing-cm-v4: exit=$?"
OUT="$(mktemp -d "$SCRATCH/mutated-out.XXXX")"
run v-mutate-out            apply $P/b-add-default-securitycontext.yaml --resource $O/passing-mutated.yaml -o "$OUT"
cp "$OUT/web-passing-mutated.yaml" cli/out/web-passing-mutated.cli.yaml
# (vi) ConfigMap context for the JMESPath ClusterPolicy
run vi-cpol-ctx-resource    apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --resource $SN/cm-allowed-teams.yaml
run vi-cpol-ctx-values      apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --values-file cli/values.yaml
run vi-cpol-ctx-contextfile apply $P/a-require-team-label.yaml --resource $O/violating-team.yaml --context-file cli/context.yaml
# (vii) ConfigMap context for the CEL ValidatingPolicy
run vii-vpol-ctx-resource    apply $P/f-vpol-require-team-label.yaml --resource $O/violating-team.yaml --resource $SN/cm-allowed-teams.yaml
run vii-vpol-ctx-values      apply $P/f-vpol-require-team-label.yaml --resource $O/violating-team.yaml --values-file cli/values.yaml
run vii-vpol-ctx-contextfile apply $P/f-vpol-require-team-label.yaml --resource $O/violating-team.yaml --context-file cli/context.yaml
run vii-vpol-ctx-none        apply $P/f-vpol-require-team-label.yaml --resource $O/violating-team.yaml
run vii-vpol-pass-contextfile apply $P/f-vpol-require-team-label.yaml --resource $O/passing-mutated.yaml --context-file cli/context.yaml
run vii-vpol-fail-report     apply $P/f-vpol-require-team-label.yaml --resource $O/violating-team.yaml --context-file cli/context.yaml --policy-report
# (viii) --userinfo
run viii-userinfo-none      apply $P/c-restrict-protected-deployer.yaml --resource $O/protected.yaml
run viii-userinfo-admin     apply $P/c-restrict-protected-deployer.yaml --resource $O/protected.yaml --userinfo cli/userinfo-admin.yaml
run viii-userinfo-argocd    apply $P/c-restrict-protected-deployer.yaml --resource $O/protected.yaml --userinfo userinfo.yaml
# (ix) VAP + --parameter-resource
run ix-vap-param            apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --parameter-resource $SN/cm-replica-limits.yaml
run ix-vap-param-values     apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --parameter-resource $SN/cm-replica-limits.yaml --values-file cli/values.yaml
run ix-vap-param-values-report apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --parameter-resource $SN/cm-replica-limits.yaml --values-file cli/values.yaml --policy-report
run ix-vap-param-pass       apply $P/d-vap-replica-limit.yaml --resource $O/passing-mutated.yaml --parameter-resource $SN/cm-replica-limits.yaml
run ix-vap-param-pass-values apply $P/d-vap-replica-limit.yaml --resource $O/passing-mutated.yaml --parameter-resource $SN/cm-replica-limits.yaml --values-file cli/values.yaml
run ix-vap-param-nsresource apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --resource $SN/namespace.yaml --parameter-resource $SN/cm-replica-limits.yaml
run ix-vap-param-nsresource-report apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --resource $SN/namespace.yaml --parameter-resource $SN/cm-replica-limits.yaml --policy-report
run ix-vap-noparam          apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml
run ix-vap-noparam-values   apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --values-file cli/values.yaml
run ix-vap-noparam-values-report apply $P/d-vap-replica-limit.yaml --resource $O/vap-violating.yaml --values-file cli/values.yaml --policy-report
# (x) PolicyException
run x-polex                 apply $P/a-require-team-label.yaml --resource $O/excused.yaml --resource $SN/cm-allowed-teams.yaml --exception $P/e-polex-legacy-app.yaml
run x-polex-none            apply $P/a-require-team-label.yaml --resource $O/excused.yaml --resource $SN/cm-allowed-teams.yaml
run x-polex-report          apply $P/a-require-team-label.yaml --resource $O/excused.yaml --values-file cli/values.yaml --exception $P/e-polex-legacy-app.yaml --policy-report
# (xi) does one invocation validate the mutated object?
run xi-twopass-validate-only        apply cli/probe-require-nonroot.yaml --resource $O/passing-mutated.yaml
run xi-twopass-mutate-then-validate apply $P/b-add-default-securitycontext.yaml cli/probe-require-nonroot.yaml --resource $O/passing-mutated.yaml
# (xii) the whole chain with every side file
CHAIN=($P/a-require-team-label.yaml $P/b-add-default-securitycontext.yaml $P/c-restrict-protected-deployer.yaml
       $P/d-vap-replica-limit.yaml $P/f-vpol-require-team-label.yaml
       --resource $O/violating-team.yaml --resource $O/passing-mutated.yaml --resource $O/excused.yaml
       --resource $O/protected.yaml --resource $O/vap-violating.yaml
       --values-file cli/values.yaml --context-file cli/context.yaml --userinfo cli/userinfo-admin.yaml
       --parameter-resource $SN/cm-replica-limits.yaml --exception $P/e-polex-legacy-app.yaml)
OUT="$(mktemp -d "$SCRATCH/chain-out.XXXX")"
run xii-full-chain          apply "${CHAIN[@]}" -o "$OUT"
run xii-full-chain-report   apply "${CHAIN[@]}" --policy-report

# --- 5. cold start ------------------------------------------------------------
for i in 1 2 3 4 5; do
  s=$(date +%s%N)
  ky apply $P/a-require-team-label.yaml --resource $O/passing-mutated.yaml --values-file cli/values.yaml >/dev/null 2>&1
  rc=$?; e=$(date +%s%N)
  echo "coldstart run$i: $(( (e - s) / 1000000 )) ms exit=$rc"
done
echo "scratch: $SCRATCH"
