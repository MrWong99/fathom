#!/usr/bin/env bash
# S8 import-degradation spike: create three importer identities on the kind
# oracle (context kind-fathom-oracle, server v1.37.0) and probe, layer by layer
# (design section 3.5), what each may read. Writes:
#   kubeconfig/<tier>.yaml   long-lived (8h) ServiceAccount token kubeconfigs
#   probe.json               every can-i and real request with the server's answer
# Re-runnable: everything is `kubectl apply`; nothing outside namespace s8 is
# created and nothing is deleted. The cluster state from S1..S4 (namespaces
# s1..s4, spike.fathom.dev CRDs, VAP/MAP, Kyverno) is the realistic cluster
# being imported.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CTX="${CTX:-kind-fathom-oracle}"
K="kubectl --context $CTX"
TIERS=(discovery-only namespaced-read cluster-read)
declare -A SA=([discovery-only]=fathom-import-discovery [namespaced-read]=fathom-import-namespaced [cluster-read]=fathom-import-cluster)
TOKEN_DURATION="${TOKEN_DURATION:-8h}"

# ------------------------------------------------------------------ 1. RBAC
echo "== apply RBAC"
$K apply -f "$HERE/rbac/namespace.yaml"
for tier in "${TIERS[@]}"; do
  $K apply -f "$HERE/rbac/$tier/"
done

# ------------------------------------------------------------------ 2. kubeconfigs
echo "== kubeconfigs"
SERVER="$($K config view --minify --raw -o jsonpath='{.clusters[0].cluster.server}')"
CA_B64="$($K config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
mkdir -p "$HERE/kubeconfig"
for tier in "${TIERS[@]}"; do
  sa="${SA[$tier]}"
  tok="$($K -n s8 create token "$sa" --duration="$TOKEN_DURATION")"
  cat > "$HERE/kubeconfig/$tier.yaml" <<YAML
# fathom S8 importer identity: tier "$tier", ServiceAccount s8/$sa.
# Token minted with: kubectl -n s8 create token $sa --duration=$TOKEN_DURATION
# Kind-local throwaway cluster only; the token expires and the CA is kind's.
apiVersion: v1
kind: Config
current-context: fathom-import-$tier
clusters:
  - name: $CTX
    cluster:
      server: $SERVER
      certificate-authority-data: $CA_B64
users:
  - name: s8/$sa
    user:
      token: $tok
contexts:
  - name: fathom-import-$tier
    context:
      cluster: $CTX
      user: s8/$sa
      namespace: s8
YAML
done

# ------------------------------------------------------------------ 3. probes
echo "== probes"
NDJSON="$(mktemp)"
trap 'rm -f "$NDJSON"' EXIT

# rec TIER LAYER KIND SUBLAYER -- CMD...   (KIND = can-i | real)
rec() {
  local tier="$1" layer="$2" kind="$3" sub="$4"; shift 4; [ "$1" = "--" ] && shift
  local kc="$HERE/kubeconfig/$tier.yaml"
  local out rc
  out="$(kubectl --kubeconfig "$kc" "$@" 2>&1)"; rc=$?
  local allowed=false
  if [ "$kind" = can-i ]; then
    # kubectl prints "Warning: resource ... is not namespace scoped" before the
    # verdict when the kubeconfig context carries a namespace; the verdict is the
    # last non-warning line and the exit code is 0 for yes, 1 for no.
    local verdict; verdict="$(printf '%s\n' "$out" | grep -v '^Warning:' | tail -n 1)"
    [ $rc -eq 0 ] && [ "$verdict" = yes ] && allowed=true
  else
    [ $rc -eq 0 ] && allowed=true
  fi
  # status distinguishes an RBAC denial from a resource the server does not serve
  local status=forbidden
  if $allowed; then status=allowed
  elif printf '%s' "$out" | grep -qE "doesn't have a resource type|NotFound|could not find the requested resource"; then status=not-served
  fi
  # keep the payload short: first 8 lines / 1200 bytes
  local short; short="$(printf '%s' "$out" | head -n 8 | cut -c1-1200)"
  jq -cn --arg tier "$tier" --arg layer "$layer" --arg kind "$kind" --arg sub "$sub" \
     --arg cmd "kubectl --kubeconfig kubeconfig/$tier.yaml $*" --argjson rc "$rc" \
     --argjson allowed "$allowed" --arg status "$status" --arg out "$short" \
     '{tier:$tier,layer:$layer,sublayer:$sub,kind:$kind,command:$cmd,exitCode:$rc,allowed:$allowed,status:$status,output:$out}' >> "$NDJSON"
}

# ssar TIER LAYER SUB GROUP RESOURCE VERB   -- raw SelfSubjectAccessReview (no discovery involved)
ssar() {
  local tier="$1" layer="$2" sub="$3" group="$4" resource="$5" verb="$6"
  local kc="$HERE/kubeconfig/$tier.yaml"
  local body out rc allowed=false status=forbidden
  body="$(jq -cn --arg g "$group" --arg r "$resource" --arg v "$verb" \
    '{apiVersion:"authorization.k8s.io/v1",kind:"SelfSubjectAccessReview",spec:{resourceAttributes:{group:$g,resource:$r,verb:$v}}}')"
  out="$(printf '%s' "$body" | kubectl --kubeconfig "$kc" create --raw /apis/authorization.k8s.io/v1/selfsubjectaccessreviews -f - 2>&1)"; rc=$?
  [ $rc -eq 0 ] && [ "$(printf '%s' "$out" | jq -r '.status.allowed' 2>/dev/null)" = true ] && { allowed=true; status=allowed; }
  local short; short="$(printf '%s' "$out" | jq -c '.status' 2>/dev/null || printf '%s' "$out" | head -n 3)"
  jq -cn --arg tier "$tier" --arg layer "$layer" --arg kind ssar --arg sub "$sub" \
     --arg cmd "kubectl --kubeconfig kubeconfig/$tier.yaml create --raw /apis/authorization.k8s.io/v1/selfsubjectaccessreviews -f - <<< '$body'" \
     --argjson rc "$rc" --argjson allowed "$allowed" --arg status "$status" --arg out "$short" \
     '{tier:$tier,layer:$layer,sublayer:$sub,kind:$kind,command:$cmd,exitCode:$rc,allowed:$allowed,status:$status,output:$out}' >> "$NDJSON"
}

# both TIER LAYER SUB -- CAN-I-ARGS... ++ REAL-ARGS...
both() {
  local tier="$1" layer="$2" sub="$3"; shift 3; [ "$1" = "--" ] && shift
  local cani=() real=() seen=0 a
  for a in "$@"; do
    if [ "$a" = "++" ]; then seen=1; continue; fi
    if [ $seen -eq 0 ]; then cani+=("$a"); else real+=("$a"); fi
  done
  rec "$tier" "$layer" can-i "$sub" -- auth can-i "${cani[@]}"
  rec "$tier" "$layer" real  "$sub" -- "${real[@]}"
}

for tier in "${TIERS[@]}"; do
  echo "-- $tier"
  # identity
  rec "$tier" identity real whoami -- auth whoami -o json
  rec "$tier" identity real selfsubjectrulesreview -- auth can-i --list --namespace s4

  # discovery/  [schema]
  both "$tier" discovery apis            -- get /apis          ++ get --raw /apis
  both "$tier" discovery aggregated      -- get /api           ++ get --raw /api
  both "$tier" discovery version         -- get /version       ++ version -o json
  # openapi/  [schema]
  both "$tier" openapi v3-index          -- get /openapi/v3    ++ get --raw /openapi/v3
  both "$tier" openapi v3-group          -- get /openapi/v3/apis/apps/v1 ++ get --raw /openapi/v3/apis/apps/v1
  # crds/  [schema]
  both "$tier" crds list                 -- list customresourcedefinitions.apiextensions.k8s.io ++ get crd widgets.spike.fathom.dev -o name
  # catalogs/names  [schema]  -- needs list on the class objects; discovery alone cannot yield names
  both "$tier" catalogs/names storageclasses  -- list storageclasses.storage.k8s.io    ++ get storageclasses -o name
  both "$tier" catalogs/names ingressclasses  -- list ingressclasses.networking.k8s.io ++ get ingressclasses -o name
  both "$tier" catalogs/names priorityclasses -- list priorityclasses.scheduling.k8s.io ++ get priorityclasses -o name
  both "$tier" catalogs/names runtimeclasses  -- list runtimeclasses.node.k8s.io       ++ get runtimeclasses -o name
  # catalogs/  full objects  [policy]
  both "$tier" catalogs/full storageclasses -- get storageclasses.storage.k8s.io ++ get storageclasses -o json
  both "$tier" catalogs/full csidrivers     -- list csidrivers.storage.k8s.io    ++ get csidrivers -o json
  # admission/  VAP/MAP + bindings (admissionregistration.k8s.io/v1)  [policy]
  both "$tier" admission/policies vap  -- list validatingadmissionpolicies.admissionregistration.k8s.io        ++ get validatingadmissionpolicies -o name
  both "$tier" admission/policies vapb -- list validatingadmissionpolicybindings.admissionregistration.k8s.io ++ get validatingadmissionpolicybindings -o name
  both "$tier" admission/policies map  -- list mutatingadmissionpolicies.admissionregistration.k8s.io          ++ get mutatingadmissionpolicies -o name
  both "$tier" admission/policies mapb -- list mutatingadmissionpolicybindings.admissionregistration.k8s.io   ++ get mutatingadmissionpolicybindings -o name
  rec  "$tier" admission/policies real map-v1beta1-fallback -- get --raw /apis/admissionregistration.k8s.io/v1beta1/mutatingadmissionpolicies
  # admission/params/  paramKind objects (ConfigMaps in the binding's namespace) [full by default]
  both "$tier" admission/params s3-replica-limits -- get configmaps -n s3 ++ get configmap replica-limits -n s3 -o name
  both "$tier" admission/params s2-vap-limits     -- get configmaps -n s2 ++ get configmap vap-limits -n s2 -o name
  # admission/webhooks/  [policy]
  both "$tier" admission/webhooks validating -- list validatingwebhookconfigurations.admissionregistration.k8s.io ++ get validatingwebhookconfigurations -o name
  both "$tier" admission/webhooks mutating   -- list mutatingwebhookconfigurations.admissionregistration.k8s.io   ++ get mutatingwebhookconfigurations -o name
  # engines/  Kyverno  [policy]
  both "$tier" engines clusterpolicies          -- list clusterpolicies.kyverno.io                  ++ get clusterpolicies.kyverno.io -o name
  both "$tier" engines validatingpolicies       -- list validatingpolicies.policies.kyverno.io      ++ get validatingpolicies.policies.kyverno.io -o name
  both "$tier" engines policyexceptions-v2      -- list policyexceptions.kyverno.io -A              ++ get policyexceptions.kyverno.io -A -o name
  both "$tier" engines policyexceptions-cel     -- list policyexceptions.policies.kyverno.io -A     ++ get policyexceptions.policies.kyverno.io -A -o name
  both "$tier" engines policies-namespaced-s4   -- list policies.kyverno.io -n s4                   ++ get policies.kyverno.io -n s4 -o name
  # namespaces/<ns>/  [full]
  both "$tier" namespaces list            -- list namespaces                     ++ get namespaces -o name
  both "$tier" namespaces get-s4-labels   -- get namespaces/s4                   ++ get namespace s4 -o jsonpath='{.metadata.labels}'
  both "$tier" namespaces get-s8-labels   -- get namespaces/s8                   ++ get namespace s8 -o jsonpath='{.metadata.labels}'
  both "$tier" namespaces get-s2-labels   -- get namespaces/s2                   ++ get namespace s2 -o jsonpath='{.metadata.labels}'
  both "$tier" namespaces quota-s4        -- list resourcequotas -n s4           ++ get resourcequotas -n s4 -o jsonpath='{range .items[*]}{.metadata.name}={.status.used}{"\n"}{end}'
  both "$tier" namespaces limitrange-s4   -- list limitranges -n s4              ++ get limitranges -n s4 -o name
  both "$tier" namespaces netpol-s4       -- list networkpolicies.networking.k8s.io -n s4 ++ get networkpolicies -n s4 -o name
  both "$tier" namespaces quota-s2-other  -- list resourcequotas -n s2           ++ get resourcequotas -n s2 -o name
  # rbac/  [full]
  both "$tier" rbac clusterroles        -- list clusterroles.rbac.authorization.k8s.io        ++ get clusterroles fathom-import-cluster-read -o name
  both "$tier" rbac clusterrolebindings -- list clusterrolebindings.rbac.authorization.k8s.io ++ get clusterrolebindings fathom-import-cluster-read -o name
  both "$tier" rbac roles-s4            -- list roles.rbac.authorization.k8s.io -n s4         ++ get roles -n s4 -o name
  both "$tier" rbac rolebindings-s4     -- list rolebindings.rbac.authorization.k8s.io -n s4  ++ get rolebindings -n s4 -o name
  # inventory/  [full]
  both "$tier" inventory configmaps-s4      -- list configmaps -n s4       ++ get configmaps -n s4 -o name
  both "$tier" inventory secrets-names-s4   -- list secrets -n s4          ++ get secrets -n s4 -o name
  both "$tier" inventory secrets-names-s8   -- list secrets -n s8          ++ get secrets -n s8 -o name
  both "$tier" inventory serviceaccounts-s4 -- list serviceaccounts -n s4  ++ get serviceaccounts -n s4 -o name
  both "$tier" inventory services-s4        -- list services -n s4         ++ get services -n s4 -o name
  both "$tier" inventory ingresses-s4       -- list ingresses.networking.k8s.io -n s4 ++ get ingresses -n s4 -o name
  both "$tier" inventory deployments-s4     -- list deployments.apps -n s4 ++ get deployments -n s4 -o name
  both "$tier" inventory pods-s4            -- list pods -n s4             ++ get pods -n s4 -o name
  both "$tier" inventory configmaps-all     -- list configmaps -A          ++ get configmaps -A -o name
  # gitops/  [full]  -- neither CRD group is installed on kind
  both "$tier" gitops argocd-applications -- list applications.argoproj.io -A            ++ get applications.argoproj.io -A -o name
  both "$tier" gitops flux-helmreleases   -- list helmreleases.helm.toolkit.fluxcd.io -A ++ get helmreleases.helm.toolkit.fluxcd.io -A -o name
  # kubectl can-i answers "no" for a resource discovery cannot resolve; a raw
  # SelfSubjectAccessReview shows what RBAC alone would say for the same tuple.
  ssar "$tier" gitops argocd-applications-ssar argoproj.io applications list
  ssar "$tier" gitops flux-helmreleases-ssar helm.toolkit.fluxcd.io helmreleases list
  # nodes/  [full]
  both "$tier" nodes list -- list nodes ++ get nodes -o jsonpath='{range .items[*]}{.metadata.name} cpu={.status.allocatable.cpu}{"\n"}{end}'
done

# ------------------------------------------------------------------ 4. assemble probe.json
ADMREG_VERSIONS="$($K get --raw /apis/admissionregistration.k8s.io | jq -c '[.versions[].version]')"
SERVER_VERSION="$($K version -o json | jq -r .serverVersion.gitVersion)"
CLIENT_VERSION="$($K version -o json | jq -r .clientVersion.gitVersion)"
NODE_IMAGE="$($K get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}')"
V1BETA1_RC=0; V1BETA1_OUT="$($K get --raw /apis/admissionregistration.k8s.io/v1beta1 2>&1)" || V1BETA1_RC=$?
GITOPS_CRDS="$($K get crd -o name | grep -E 'argoproj.io|fluxcd.io' | jq -R . | jq -sc .)"

jq -n \
  --arg ctx "$CTX" --arg server "$SERVER" --arg sv "$SERVER_VERSION" --arg cv "$CLIENT_VERSION" --arg kubelet "$NODE_IMAGE" \
  --argjson admreg "$ADMREG_VERSIONS" --arg v1b1 "$V1BETA1_OUT" --argjson v1b1rc "$V1BETA1_RC" --argjson gitops "$GITOPS_CRDS" \
  --arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --slurpfile results "$NDJSON" \
  '{
     schemaVersion: "s8-probe/1",
     capturedAt: $date,
     cluster: {
       context: $ctx, server: $server, serverVersion: $sv, kubectlVersion: $cv, kubeletVersion: $kubelet,
       platform: "kind (local); no EKS/AKS/OpenShift cluster was available for this spike",
       admissionregistrationServedVersions: $admreg,
       featureState: { mapApiVersion: "v1" },
       mapV1beta1Fallback: { testable: false, request: "GET /apis/admissionregistration.k8s.io/v1beta1", exitCode: $v1b1rc, output: $v1b1 },
       gitopsCRDsInstalled: $gitops
     },
     tiers: {
       "discovery-only": { serviceAccount: "s8/fathom-import-discovery", bindings: "none beyond the default system:discovery, system:basic-user, system:public-info-viewer ClusterRoleBindings for system:authenticated" },
       "namespaced-read": { serviceAccount: "s8/fathom-import-namespaced", bindings: "Role fathom-import-namespaced-read in s8 and s4 (no secrets, no namespace list); ClusterRole fathom-import-namespaced-read-namespaces (get namespaces resourceNames s4,s8)" },
       "cluster-read":    { serviceAccount: "s8/fathom-import-cluster", bindings: "ClusterRole fathom-import-cluster-read (get/list/watch on every layer, no secrets); ClusterRole fathom-import-secrets-optional exists but is NOT bound" }
     },
     results: $results,
     summary: ($results | group_by(.tier) | map({
        key: .[0].tier,
        value: (group_by(.layer) | map({key: .[0].layer, value: {
            allowed: (map(select(.allowed)) | length),
            denied:  (map(select(.allowed|not)) | length),
            deniedSublayers: (map(select(.allowed|not) | .sublayer) | unique)
        }}) | from_entries)
     }) | from_entries)
   }' > "$HERE/probe.json"
echo "wrote $HERE/probe.json ($(jq '.results|length' "$HERE/probe.json") records)"
