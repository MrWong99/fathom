# S4 result: PASS

Date: 2026-09-18. Oracle: kind `kind-fathom-oracle`, server v1.37.0, kubectl
client v1.36.4, namespace `s4`. Ports from kubernetes tag v1.37.0 to
`k8s.io/api/core/v1` and `k8s.io/api/rbac/v1`; staging modules at v0.37.0, no
`replace`, no `k8s.io/kubernetes`, `CGO_ENABLED=0 go build ./...` passes.

The criterion has three parts. Judged strictly:

| Part | Result |
|---|---|
| Golden test against `--dry-run=server` on kind | **Met.** 16 of 16 scenarios identical offline against the recorded goldens: every denial text equal to the server's `StatusError` text after stripping only kubectl's `Error from server (Forbidden): error when creating "<file>": ` wrapper; the four admitted objects identical on every leaf LimitRanger owns and a strict subset of the server's diff elsewhere. 23 of 23 `kubectl auth can-i` verdicts identical. Live replay against the cluster as it is now (phase B, three pods) with the live LimitRange and quota status: 15 of 16 text-identical plus `q-scope-high` by set match (the server's single message is one of the offline `CheckAll` messages; `compareDenial` accepts that only when the primary text differs, so the live headline is not 16 exact matches), 23 of 23 can-i. Two scenarios (`lr-pod-level-cpu`, `lr-pod-level-hugepages`) were added on review the same day, after the first cut's Pod-level sums were found to drift from upstream for pod-level hugepages ("Drift found and fixed" below). |
| Port size recorded | **Met.** Table below: 1,965 ported Apache-2.0 LOC from 2,318 upstream LOC, plus 322 LOC of fathom-owned glue (340 with the shared `gates`). |
| Per-minor refresh task budgeted | **Met.** Recipe with commands and a 2.5 to 4 h budget below. |

## Versions

| Component | Version |
|---|---|
| Go | `go1.27.1-X:nodwarf5 linux/amd64`; `CGO_ENABLED=0 go build ./...` and `go vet ./...` verified (`-race` needs cgo, run separately) |
| k8s.io/api, apimachinery, apiserver, client-go, component-base, component-helpers | v0.37.0 (`go list -m all`); k8s.io/utils v0.0.0-20260626114624-be93311217bd |
| Upstream sources | kubernetes tag v1.37.0: `plugin/pkg/admission/limitranger/{admission,interfaces}.go`, `pkg/quota/v1/evaluator/core/{pods,persistent_volume_claims,services,registry}.go`, `pkg/apis/core/v1/helper/helpers.go`, `pkg/apis/core/v1/helper/qos/qos.go`, `pkg/scheduler/util/utils.go`, `pkg/registry/rbac/validation/rule.go`, `plugin/pkg/auth/authorizer/rbac/rbac.go`, `pkg/apis/rbac/v1/evaluation_helpers.go`, `pkg/features/kube_features.go` (gate defaults); all ten files plus `LICENSE` were read from a local extract of the tag, the recipe below re-fetches the same set |
| kind | context `kind-fathom-oracle`, server `v1.37.0` (`testdata/golden/server-version.txt`) |
| kubectl | client v1.36.4 |
| Feature gates pinned in `port/gates` (defaults at v1.37.0) | PodLevelResources beta on (1.34), PodLevelResourcesFixKubeletQOSClass beta on (1.37), InPlacePodVerticalScaling GA locked (1.35), VolumeAttributesClass GA locked (1.36), RecoverVolumeExpansionFailure GA locked (1.34) |

## Scenarios: server vs offline

Server text is `testdata/golden/<name>.server.txt` after the wrapper strip;
offline text is `chain.Admit` on the same object against
`testdata/snapshot/` (the LimitRange file and the quota status the scenario
was captured with; `lr-pod-level-hugepages` names `limitrange-hugepages.yaml`). "Live" is `TestOracleLive` on 2026-09-18 against the current
cluster state (phase B) with the live LimitRange and quota status.

| Scenario | Server | Offline | Identical | Live |
|---|---|---|---|---|
| lr-defaults | admitted; set requests cpu 100m/memory 128Mi, limits cpu 200m/memory 256Mi, annotation `LimitRanger plugin set: cpu, memory request for container app; cpu, memory limit for container app` | admitted; the same 5 leaves, same annotation | yes (offline diff = server diff on LimitRanger leaves; server adds 33 leaves from defaulting, ServiceAccount, Priority, DefaultTolerationSeconds) | identical (both denied by `pods=3` in phase B) |
| lr-request-only | admitted; set limits cpu 200m/memory 256Mi only, requests kept 150m/100Mi, annotation `... cpu, memory limit for container app` | admitted; the same 3 leaves | yes | identical (phase B denial) |
| lr-over-max | `pods "lr-over-max" is forbidden: maximum cpu usage per Container is 1, but limit is 1500m` | same | yes | identical |
| lr-under-min | `pods "lr-under-min" is forbidden: minimum cpu usage per Container is 50m, but request is 20m` | same | yes | identical |
| lr-ratio | `pods "lr-ratio" is forbidden: cpu max limit to request ratio per Container is 4, but provided ratio is 5.000000` | same | yes | identical |
| lr-pod-max | `pods "lr-pod-max" is forbidden: maximum cpu usage per Pod is 2, but limit is 3` | same | yes | identical |
| lr-pod-level-cpu | `pods "lr-pod-level-cpu" is forbidden: maximum cpu usage per Pod is 2, but limit is 3` (one container with cpu 1, `spec.resources` cpu 3: the pod-level value is what the Pod max sees) | same | yes | identical |
| lr-pod-level-hugepages | admitted under `limitrange-hugepages.yaml` (Pod max `hugepages-2Mi: 64Mi`; container sum 32Mi, `spec.resources` 128Mi: the server ignores hugepages at pod level) | admitted; 0 leaves changed | yes (server adds the same 33 defaulting leaves) | identical (phase B denial by `pods=3` on both sides; the live LimitRange has no hugepages max) |
| pvc-min | `persistentvolumeclaims "pvc-min" is forbidden: minimum storage usage per PersistentVolumeClaim is 1Gi, but request is 500Mi` | same | yes | identical |
| pvc-max | `persistentvolumeclaims "pvc-max" is forbidden: maximum storage usage per PersistentVolumeClaim is 20Gi, but request is 30Gi` | same | yes | identical |
| q-cpu-exceeded | `pods "q-cpu-exceeded" is forbidden: exceeded quota: s4-quota, requested: requests.cpu=700m, used: requests.cpu=600m, limited: requests.cpu=1` | same | yes | identical (phase B: `requested: pods=1,requests.cpu=700m, used: pods=3,requests.cpu=700m, limited: pods=3,requests.cpu=1` on both sides) |
| q-pvc-storage | `persistentvolumeclaims "q-pvc-storage" is forbidden: exceeded quota: s4-quota, requested: requests.storage=6Gi, used: requests.storage=5Gi, limited: requests.storage=10Gi` | same | yes | identical |
| q-lb | `services "q-lb" is forbidden: exceeded quota: s4-quota, requested: services.loadbalancers=1, used: services.loadbalancers=1, limited: services.loadbalancers=1` | same | yes | identical |
| q-scope-high | `pods "q-scope-high" is forbidden: exceeded quota: s4-quota-high, requested: pods=1, used: pods=1, limited: pods=1` | same | yes (phase A: only `s4-quota-high` is exceeded, exact text) | **set match, not text-identical**: in phase B both quotas are exceeded; the server reported `s4-quota-high`, offline reports `s4-quota` first (by name) and `CheckAll` lists both, and `compareDenial` passes because the server's text is one of the offline texts |
| q-ok | admitted; no LimitRanger annotation (all resources explicit) | admitted; 0 leaves changed | yes (server adds the same 33 defaulting leaves) | identical (phase B denial) |
| q-pods-exceeded | `pods "q-pods-exceeded" is forbidden: exceeded quota: s4-quota, requested: pods=1, used: pods=3, limited: pods=3` | same | yes | identical |

Chain order note: every scenario runs LimitRanger mutate, LimitRanger
validate, then ResourceQuota, with the quota present even for the
`limitrange` scenarios (the oracle captured those before the quota existed);
the outcome is the same because LimitRanger denies first and the allowed
ones fit phase A.

LimitRange note: `lr-pod-level-hugepages` is captured under
`snapshot/limitrange-hugepages.yaml` (the shared LimitRange plus a Pod max on
`hugepages-2Mi`), which `commands.sh` applies for that one dry-run and then
restores, because upstream's `maxConstraint` denies every pod that has no
limit for a resource the Pod max names (`maximum hugepages-2Mi usage per Pod
is 64Mi.  No limit is specified`); left in place it denies every other pod
scenario, which the first rerun of the script showed.

**Server-side non-determinism, measured.** Eight consecutive live
`--dry-run=server` of `q-scope-high` in phase B reported `s4-quota` six times
and `s4-quota-high` twice at first capture, and seven to one on a rerun the
same day; the split is illustrative (a sample of map iteration order), the
finding is that both names occur for the same request. The quota admission plugin lists the namespace's
quotas from its informer cache (`resource_access.go`, `List(labels.Everything())`
over an indexer map) and `CheckRequest` returns at the first exceeded quota,
so which quota a user sees when several are exceeded is not a function of
the request. fathom sorts quotas by name and reports every exceeded quota
(`quota.CheckAll`, one finding per quota); calibration compares the
server's message against that set.

**Usage functions vs the quota controller.** `TestRecomputeMatchesControllerStatus`
feeds the real objects of each phase through the ported `Usage` functions
(`quota.CalculateUsage`, as the controller does) and reproduces every
`status.used` entry the controller wrote for both quotas in both phases
(`limits.cpu` 700m/900m, `requests.memory` 640Mi/768Mi, `pods` 2/3,
`s4-quota-high` `pods` 1 via the PriorityClass scope, `requests.storage`
5Gi, `services.loadbalancers` 1); `count/configmaps` is excluded because the
snapshot does not hold `kube-root-ca.crt`.

## RBAC: kubectl auth can-i vs offline (subject `system:serviceaccount:s4:deployer`)

The offline user.Info carries the groups `system:serviceaccounts`,
`system:serviceaccounts:s4`, `system:authenticated`. kubectl sends the
kubeconfig namespace even for cluster-scoped checks; RBAC reads the
namespace only to select RoleBindings, so the empty namespace used offline
gives the same verdicts (confirmed by the live replay). 23 of 23 identical,
offline and live.

| # | Check | kubectl | offline | identical |
|---|---|---|---|---|
| 0 | create deployments -n s4 | yes | yes (RoleBinding deployer-binding/s4 of Role deployer-role) | yes |
| 1 | list deployments -n s4 | yes | yes | yes |
| 2 | delete deployments -n s4 | no | no | yes |
| 3 | get deployments -n default | no | no | yes |
| 4 | get deployments --subresource=scale -n s4 | no | no (`deployments` does not match `deployments/scale`) | yes |
| 5 | update deployments --subresource=scale -n s4 | no | no | yes |
| 6 | get deployments/scale -n s4 (kubectl: resourceName `scale`) | yes | yes | yes |
| 7 | get configmaps -n s4 | yes | yes | yes |
| 8 | get configmaps/app-config -n s4 | yes | yes | yes |
| 9 | update configmaps/app-config -n s4 | yes | yes (resourceNames rule) | yes |
| 10 | update configmaps/other-config -n s4 | no | no | yes |
| 11 | update configmaps -n s4 (unnamed) | no | no | yes |
| 12 | get secrets -n s4 | no | no | yes |
| 13 | get secrets/app-config -n s4 | no | no | yes |
| 14 | delete services -n s4 | yes | yes (`*` verb) | yes |
| 15 | patch services -n s4 | yes | yes | yes |
| 16 | get services -n default | no | no | yes |
| 17 | get nodes | yes | yes (ClusterRoleBinding s4-view-nodes-binding) | yes |
| 18 | list nodes | yes | yes | yes |
| 19 | delete nodes | no | no | yes |
| 20 | get nodes/fathom-oracle-control-plane | yes | yes | yes |
| 21 | list namespaces | yes | yes (aggregated ClusterRole s4-aggregate, rules as exported) | yes |
| 22 | get pods -n s4 | no | no | yes |

Aggregated ClusterRoles need no offline aggregation: the controller-manager
writes the aggregated rules into `.rules` and a snapshot exports them that
way. A snapshot taken before the controller ran (or with `rules: []` from a
GitOps repo instead of the cluster) would under-report, which is a
snapshot-freshness question, not a port question.

## Port size

LOC = non-blank lines from the `package` line onward (license header and the
`fathom:` change block excluded), same rule on both sides.

| Port | Upstream file(s) at v1.37.0 | Upstream LOC | Ported LOC (Apache-2.0) | Internal-type substitutions | Dropped | Staging helpers used | fathom glue (MIT) |
|---|---|---|---|---|---|---|---|
| limitranger | `plugin/pkg/admission/limitranger/admission.go`, `interfaces.go` | 611 + 19 = 630 | 491 + 19 = 510 | 56 `corev1.*` identifier uses in the ported file, each an `api.*` use upstream (14 `ResourceList`, 10 `LimitRange`, 10 `ResourceName`, 7 `Pod`, 4 `ResourceRequirements`, 2 `LimitTypeContainer`, 2 `PersistentVolumeClaim`, `Container`, `LimitTypePersistentVolumeClaim`, `LimitTypePod`, `ResourceCPU`, `ResourceMemory`; 2 `Kind` -> `corev1.SchemeGroupVersion.WithKind(...).GroupKind()`); 1 gate read -> `gates.PodLevelResources` | informer/lister/client, LRU + singleflight live lookup, `Register`, `SetExternalKube*`, `ValidateInitialization` (~90 LOC); the container-aggregation bodies of the local `podRequests`/`podLimits` copy and `addResourceList`/`maxResourceList` (~75 LOC); their pod-level override loops, `supportedPodLevelResources` and `isSupportedPodLevelResource` are kept ("Drift found and fixed") | `k8s.io/component-helpers/resource.PodRequests/PodLimits` for the container aggregation only (`ExcludeOverhead: true`, `UseStatusResources: false`, `SkipPodLevelResources: true`), `k8s.io/apiserver/pkg/admission` (`Handler`, `Attributes`, `NewForbidden`), apimachinery `resource`, `meta`, `utilerrors`, `sets` | `snapshot.go` 36 (`SnapshotRanges`, `SnapshotRangesFile`) |
| quota | `pkg/quota/v1/evaluator/core/pods.go`, `persistent_volume_claims.go`, `services.go`, `registry.go`; `pkg/apis/core/v1/helper/qos/qos.go`; 4 functions of `pkg/apis/core/v1/helper/helpers.go` (40); `IsDRAExtendedResourceName` of `pkg/scheduler/util/utils.go` (3) | 444 + 293 + 141 + 66 + 97 + 40 + 3 = 1,084 | 435 + 280 + 136 + 67 + 96 = 1,014 | 3 converter arms removed (`*api.Pod`, `*api.PersistentVolumeClaim`, `*api.Service`, 2 lines each); 7 gate reads -> `gates.*`; 4 `helper.`/`qos.`/`schedutil.` calls -> local; the 5 disabled-gate else branches deleted | `registry.go` (ResourceClaim evaluator, DRA gates, device-class informer cache) replaced by 37 LOC | `k8s.io/apiserver/pkg/quota/v1` (`Add`, `Contains`, `ContainsPrefix`, `Intersection`, `ResourceNames`, `ToSet`, `Evaluator`, `UsageStatsOptions`, `Registry`, `CalculateUsage`), `.../quota/v1/generic` (`CalculateUsageStats`, `ListResourceUsingListerFunc`, `Matches`, `MatchesNoScopeFunc`, `ObjectCountQuotaResourceNameFor`, `NewObjectCountEvaluator`, `NewRegistry`), `component-helpers/resource` (`PodRequests`, `PodLimits`, `IsPodLevelResourcesSet`), `component-helpers/storage/volume.GetPersistentVolumeClaimClass`, `k8s.io/apiserver/pkg/admission/plugin/resourcequota.CheckRequest` (the admission-time check and its message: 0 LOC ported) | `registry.go` 37, `check.go` 138 (`Check`, `CheckAll`, `LoadQuotas`, `RecomputeUsage`) |
| rbac | `pkg/registry/rbac/validation/rule.go`; `plugin/pkg/auth/authorizer/rbac/rbac.go`; `pkg/apis/rbac/v1/evaluation_helpers.go` | 307 + 187 + 110 = 604 | 271 + 94 + 76 = 441 | 0 type substitutions (already `rbacv1`); 5 `rbacv1helpers.*Matches` calls -> local copies | `ConfirmNoEscalation` and `CompactRules` (rule.go, 36 LOC) and `CompactString`, `SortableRuleSlice` plus the `fmt` import (evaluation_helpers.go, 34 LOC): the RBAC-write escalation checks; klog denial log, `RBACAuthorizer.RulesFor` adapter, client-go lister adapters (rbac.go, 93 LOC) | `k8s.io/apiserver/pkg/authentication/serviceaccount` (`MatchesUsername`, `SplitUsername`, `MakeGroupNames`), `.../authentication/user`, `.../authorization/authorizer` (`Attributes`, `AttributesRecord`, `Decision`, `ConditionsAwareDecisionFromParts`); `component-helpers/auth/rbac/validation.Covers` is not needed once `ConfirmNoEscalation` is out | `snapshot.go` 111 (loader, `UserInfo`, `CanI`; with `StaticRoles` in rule.go this is the "~150-line rule resolver" of design 3.3 Z1) |
| **total** | | **2,318** | **1,965** | | | | **322** = 36 + 37 + 138 + 111 (340 with the shared `gates` 18; not counted: `port/snapshot` 168 and `port/chain` 80 are the spike's stand-ins for `internal/snapshot` and `internal/admit`, `port/oracle` 254 is test tooling) |

Design C2-M4 withdrew the earlier LOC figure without recording a number;
this table is the first measured one. Upstream comments are kept verbatim
(they make the per-minor diff apply cleanly), and `rbac.go` plus the five
matchers are counted although design 3.1 names only `rule.go`.

## Drift found and fixed (review, 2026-09-18)

The first cut delegated the Pod-level `min`/`max` sums entirely to
`component-helpers/resource.PodRequests/PodLimits` and claimed they "equal
upstream's local copy". They do not for pod-level hugepages: upstream's
`isSupportedPodLevelResource` (admission.go, the deleted local copy) accepts
only `cpu` and `memory`, while `component-helpers@v0.37.0`
`IsSupportedPodLevelResource` also matches `hugepages-*`. A Pod with
`spec.resources` hugepages above a `type: Pod` max whose container sum is
below it is admitted by the server and was denied by the port. No scenario
exercised `spec.resources` at all (upstream's `admission_test.go` does).

Fix: `podRequests`/`podLimits` keep upstream's signature, override loop and
`supportedPodLevelResources` set; only the container aggregation
(`addResourceList`/`maxResourceList` sidecar formula) is `component-helpers`
with `SkipPodLevelResources: true`. Evidence: the two new oracle scenarios
(`lr-pod-level-cpu` denied on the pod-level value, `lr-pod-level-hugepages`
admitted on the container sum), `TestPodLevelResources` (five cases) and
`TestPodLimitsDiffersFromComponentHelpers`, which fails the day
`component-helpers` stops overriding hugepages so the local override can be
dropped. The `UseDRANodeAllocatableResourceClaimStatus` path of
`component-helpers` is off by default and is not a drift.

## Per-minor refresh recipe

Budget: **2.5 to 4 hours per minor** for all three ports, measured against
the 1.32 to 1.37 churn of these files (a few tens of lines per minor in
`pods.go`/`persistent_volume_claims.go`, mostly new gates and DRA;
`limitranger/admission.go` ~150 lines in 1.32 to 1.34 for pod-level
resources, none since; `services.go`, `rule.go`, `rbac.go`,
`evaluation_helpers.go` unchanged for years). Breakdown: fetch and diff
0.25 h; limitranger 0.5 to 1 h; quota 1 to 1.5 h (collapsing new gate
branches, checking whether an inlined helper moved into component-helpers,
which would delete our copy); rbac 0.25 h; gates 0.25 h; oracle re-capture
on a kind node image of the new minor and golden diff 0.5 to 1 h.

```sh
# 1. fetch both tags (upstream sources only; nothing is imported)
OLD=v1.37.0 NEW=v1.38.0 W=/tmp/k8s-refresh && mkdir -p $W && cd $W
FILES='kubernetes-*/plugin/pkg/admission/limitranger/*.go
kubernetes-*/pkg/quota/v1/evaluator/core/*.go
kubernetes-*/pkg/apis/core/v1/helper/helpers.go
kubernetes-*/pkg/apis/core/v1/helper/qos/qos.go
kubernetes-*/pkg/scheduler/util/utils.go
kubernetes-*/pkg/registry/rbac/validation/rule.go
kubernetes-*/plugin/pkg/auth/authorizer/rbac/rbac.go
kubernetes-*/pkg/apis/rbac/v1/evaluation_helpers.go
kubernetes-*/pkg/features/kube_features.go
kubernetes-*/LICENSE'
for T in $OLD $NEW; do
  curl -fsSL https://github.com/kubernetes/kubernetes/archive/refs/tags/$T.tar.gz \
    | tar xz --wildcards $FILES
done
# 2. what changed upstream, per ported file
for f in plugin/pkg/admission/limitranger/admission.go \
         pkg/quota/v1/evaluator/core/pods.go \
         pkg/quota/v1/evaluator/core/persistent_volume_claims.go \
         pkg/quota/v1/evaluator/core/services.go \
         pkg/quota/v1/evaluator/core/registry.go \
         pkg/apis/core/v1/helper/qos/qos.go \
         pkg/registry/rbac/validation/rule.go \
         plugin/pkg/auth/authorizer/rbac/rbac.go \
         pkg/apis/rbac/v1/evaluation_helpers.go; do
  diff -u kubernetes-${OLD#v}/$f kubernetes-${NEW#v}/$f > $W/$(basename $f).diff
done
# helpers we inlined: only the four functions matter
diff -u kubernetes-${OLD#v}/pkg/apis/core/v1/helper/helpers.go kubernetes-${NEW#v}/pkg/apis/core/v1/helper/helpers.go \
  | grep -A20 'IsExtendedResourceName\|IsNativeResource\|ScopedResourceSelectorRequirementsAsSelector'
# gates the ports pin: new defaults
for g in PodLevelResources PodLevelResourcesFixKubeletQOSClass InPlacePodVerticalScaling VolumeAttributesClass RecoverVolumeExpansionFailure; do
  grep -n "^	$g: {" -A6 kubernetes-${NEW#v}/pkg/features/kube_features.go
done
# 3. apply the upstream hunks onto the ports (re-typing keeps line structure)
cd $FATHOM/internal/admit/port   # spikes/s4-ports/port in this spike
patch -p1 --dry-run limitranger/admission.go < $W/admission.go.diff   # then without --dry-run
patch -p1 --dry-run quota/pods.go < $W/pods.go.diff                   # ... and so on
# hunks that fail are the fathom: blocks; re-apply the mechanical re-typing on them
sed -i -E 's/\bapi\.(ResourceList|ResourceName|ResourceRequirements|Pod|Container|PersistentVolumeClaim|ContainerRestartPolicyAlways|ResourceCPU|ResourceMemory)\b/corev1.\1/g' limitranger/admission.go
sed -i -E 's/(utilfeature|feature)\.DefaultFeatureGate\.Enabled\((k8sfeatures|features)\.([A-Za-z]+)\)/gates.\3/g' quota/*.go limitranger/admission.go
# 4. bump the staging modules and rebuild
go get k8s.io/api@v0.38.0 k8s.io/apimachinery@v0.38.0 k8s.io/apiserver@v0.38.0 \
       k8s.io/client-go@v0.38.0 k8s.io/component-helpers@v0.38.0 k8s.io/component-base@v0.38.0
go mod tidy && CGO_ENABLED=0 go build ./... && go vet ./...
# 5. re-capture the oracle on the new minor and rerun the goldens
#    (the old oracle is still there: delete it, or create fathom-oracle-$NEW
#    and point FATHOM_ORACLE_CONTEXT at it; the s1..s3 spikes share the cluster)
kind delete cluster --name fathom-oracle
kind create cluster --name fathom-oracle --image kindest/node:$NEW
spikes/s4-ports/testdata/commands.sh        # rewrites golden/ and snapshot/
go test -race -count=1 ./...                # TestGoldens, TestOracleLive, TestCanI*
# 6. update port/gates/gates.go, NOTICES (tag), RESULT port-size table
```

Rules that keep the diff small: keep upstream ordering, names and comments;
put every local change in a `fathom:` marked block; never reformat upstream
code; when a `fathom:` block collides with an upstream hunk, prefer
re-dropping (the informer plumbing, converter arms, gate else branches) over
merging.

## What it changes in the design

- **3.1 (`internal/admit/port` list).** Confirmed as `limitranger`, `quota`
  and `rbacrules` with the files above; add the small shared pieces this
  spike needed: `port/gates` (one file of pinned gate constants with the
  upstream minor they came from) and the four inlined core/v1 helpers plus
  `qos.go` inside `port/quota`. The `mapcompile` entry is S2's. `NOTICES`
  entries as in this spike's `NOTICES`; the Apache license text ships next
  to it. The design's "LOC figure withdrawn" (C2-M4) can be restated:
  1,965 ported LOC plus 322 glue (340 with `gates`) for the three ports.
- **3.3 M-stage (LimitRanger defaults as Info diffs).** Works as designed:
  the mutation is exactly the leaves the server writes
  (`spec.containers[*].resources.{requests,limits}.{cpu,memory}`,
  `spec.initContainers[*]...`, the `kubernetes.io/limit-ranger` annotation),
  so each leaf becomes one Info finding with the annotation text as message.
  The other 33 leaves the server adds are defaulting and other mutators
  (ServiceAccount token volume, Priority, DefaultTolerationSeconds) and
  belong to their own M-stage entries, not to LimitRanger. One detail to
  keep: the Pod-level sums use `component-helpers` for the container
  aggregation only (`ExcludeOverhead`, `UseStatusResources=false`,
  `SkipPodLevelResources=true`) with upstream's own pod-level override on
  top, which accepts cpu and memory only; delegating the override to
  `component-helpers` (whose `IsSupportedPodLevelResource` also matches
  `hugepages-*`) denies a pod the server admits (`lr-pod-level-hugepages`).
- **3.3 V-stage (two-number quota).** The admission-time number is
  `resourcequota.CheckRequest` from staging over the ported evaluators; no
  arithmetic or message is fathom's. Two additions: (a) quotas are checked
  by name and **every** exceeded quota is a finding (`CheckAll`), because the
  server's single message is non-deterministic when several are exceeded
  (6:2 and 7:1 over two samples of eight runs); the calibration oracle
  definition in 3.4 should say "the server's message is one of fathom's" for
  that case, and a calibration report should count such a match separately
  from a text-identical one, as the table above does. (b)
  `RecomputeUsage` (`quota.CalculateUsage` over the ported `Usage`
  functions) reproduces the controller's `status.used` from the `full`
  slice, which is what the old-revision subtraction and the rollout-peak
  number need; the storage-class-scoped PVC resources appear only once
  DefaultStorageClass has run, as 3.3 already says.
- **3.3 Z1 (RBAC).** `RulesAllow`/`RuleAllows` plus the resolver behave like
  `kubectl auth can-i` on all 23 checks including `resourceNames`,
  subresources, `*` verbs, cluster-scoped bindings and an aggregated
  ClusterRole. The resolver is upstream's `StaticRoles` (45 LOC) plus a
  111-line loader, within the "~150-line" estimate. The snapshot must be
  the cluster's export (aggregated rules materialised), and the identity
  fathom feeds it must include the ServiceAccount groups.
- **10.1 S4.** Done, PASS; the row can also record that the quota ports
  plug into the staging admission function unchanged.
- **`MatchingScopes` needs a port: yes.** The method is on the staging
  `quota.Evaluator` interface and the staging admission plugin calls it, but
  every real implementation (`podMatchesScopeFunc`, `pvcMatchesScopeFunc`,
  `IsTerminating`, `isBestEffort`, PriorityClass selector matching,
  cross-namespace affinity) and its helper
  `ScopedResourceSelectorRequirementsAsSelector` live only in
  `k8s.io/kubernetes`; they are ported inside `pods.go` and
  `persistent_volume_claims.go` (services and object counts return empty
  in staging already). `TestPodMatchingScopes` covers In/NotIn/Exists/
  DoesNotExist on PriorityClass, BestEffort, Terminating; the scoped quota
  scenario and the recomputed `s4-quota-high` usage exercise it against the
  server.
- Not in scope, recorded: the ResourceClaim (DRA) evaluator and
  `count/<resource>.<group>` evaluators for arbitrary kinds (the generic
  object-count evaluator is in staging; the registry only needs the GVR
  list from discovery), Pod `resize` and PVC `status` update paths
  (`Handles` keeps them, the chain only creates), `LimitedResources`
  admission configuration (passed as nil).

## Files

- `port/limitranger/{admission,interfaces,snapshot}.go`, `port/quota/{pods,persistent_volume_claims,services,helpers,qos,registry,check}.go`, `port/rbac/{rbac,rule,evaluation_helpers,snapshot}.go`, `port/gates/gates.go`, `port/snapshot/{load,attributes,lister}.go`, `port/chain/chain.go`, `port/oracle/oracle.go`
- tests: `port/chain/chain_test.go` (goldens + live), `port/limitranger/admission_test.go`, `port/quota/quota_test.go`, `port/rbac/rbac_test.go`, `port/snapshot/load_test.go`
- `testdata/`: `commands.sh`, `manifest.json`, `objects/`, `golden/`, `snapshot/` (`limitrange-hugepages.yaml` is the LimitRange of `lr-pod-level-hugepages` only)
- `NOTICES`, `LICENSE.apache-2.0`, `README.md`, this file
