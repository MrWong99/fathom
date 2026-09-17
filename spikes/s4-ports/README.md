# S4: port LimitRanger, quota evaluators and RBAC rules to external types

## Goal

Show that the three admission pieces fathom cannot import (they live in the
`k8s.io/kubernetes` module, which the product never depends on: CLAUDE.md,
design section 3.1) can be **ported** (copied and re-typed to
`k8s.io/api/core/v1` and `k8s.io/api/rbac/v1`, fed from a snapshot instead of
informers) rather than forked, and that the ports produce the server's
verdicts and messages byte for byte:

- **LimitRanger** mutate (`default`, `defaultRequest`; a limit is never copied
  from a request nor a request from a limit) and validate (`min`, `max`,
  `maxLimitRequestRatio`, Pod-level sums, PVC `min`/`max`);
- **ResourceQuota** usage evaluators for pods, PVCs and services plus the
  admission-time check `status.used + usage(obj) <= status.hard` for every
  quota in the namespace, honouring `scopes`/`scopeSelector`;
- **RBAC** `RulesAllow`/`RuleAllows` and a rule resolver over the snapshot's
  Roles, ClusterRoles, RoleBindings and ClusterRoleBindings that answers like
  `kubectl auth can-i`.

The upstream sources are kubernetes tag **v1.37.0**; the staging modules are
pinned at **v0.37.0**, no `replace`, `CGO_ENABLED=0 go build ./...` passes.

## Pass criterion (tracker)

Golden test against --dry-run=server on kind; port size recorded; per-minor refresh task budgeted

## Method

- `testdata/` is written only by `commands.sh` (goldens and snapshot are never
  edited by hand): 16 `kubectl apply --dry-run=server` scenarios
  (`manifest.json`, one golden per scenario, four server-mutated objects), the
  snapshot (`limitrange.yaml`; `limitrange-hugepages.yaml`, applied for the
  one pod-level hugepages scenario and then restored, because a Pod max on a
  resource denies every pod without a limit for it; `quota-status.yaml` for
  phase A with two pods, `quota-status-3pods.yaml` for phase B, `rbac/*.yaml`
  as `kubectl get -o yaml` writes them), 23 `kubectl auth can-i` verdicts,
  and `commands.sh` to replay it all. Oracle:
  kind context `kind-fathom-oracle`, server v1.37.0, kubectl client v1.36.4,
  namespace `s4`.
- `port/limitranger`: `admission.go` is upstream
  `plugin/pkg/admission/limitranger/admission.go` re-typed (`api.*` ->
  `corev1.*`), with the informer/live-lookup plumbing replaced by a
  `RangeSource` (`snapshot.go`: `SnapshotRanges(dir)` reads
  `<dir>/limitrange.yaml`, `SnapshotRangesFile(path)` any file) and the
  container aggregation of upstream's local `podRequests`/`podLimits` copy
  replaced by `component-helpers/resource.PodRequests/PodLimits` with
  `ExcludeOverhead`, `UseStatusResources=false` and
  `SkipPodLevelResources=true`; upstream's own pod-level override (cpu and
  memory only) stays local, because `component-helpers` would also override
  `hugepages-*`, which the server does not (RESULT, "Drift found and fixed").
  `interfaces.go` is verbatim. Every change is marked `fathom:` in the file.
- `port/quota`: `pods.go`, `persistent_volume_claims.go`, `services.go` are the
  upstream evaluators with the `*api.*` converter arms removed, the gate reads
  turned into the constants of `port/gates` (values from
  `pkg/features/kube_features.go` at v1.37.0), and the four
  `k8s.io/kubernetes`-only helpers inlined (`helpers.go`: `IsNativeResource`,
  `IsExtendedResourceName`, `ScopedResourceSelectorRequirementsAsSelector`,
  `IsDRAExtendedResourceName`; `qos.go`: `GetPodQOS`/`ComputePodQOS`).
  `registry.go` is fathom's 37-line replacement for upstream `registry.go`
  (no ResourceClaim evaluator, no DRA). `check.go` is the admission-time
  check: it calls the staging plugin's exported
  `resourcequota.CheckRequest` (the function the apiserver runs per request)
  with the ported evaluator, so the arithmetic and the
  `exceeded quota: <name>, requested: <r>, used: <u>, limited: <l>` text are
  the server's own code, not a re-implementation. `CheckAll` evaluates every
  quota on its own. `RecomputeUsage` is `quota.CalculateUsage` over the
  ported `Usage` functions, for the `full` slice.
- **`MatchingScopes`**: the method is on the staging `quota.Evaluator`
  interface and `resourcequota.CheckRequest` calls it, but the only staging
  implementations return nothing (`generic.objectCountEvaluator`,
  `generic.MatchesNoScopeFunc`). The per-kind logic (`podMatchesScopeFunc`,
  `pvcMatchesScopeFunc`, `IsTerminating`, `isBestEffort`, PriorityClass
  selector matching, cross-namespace affinity) exists only in
  `k8s.io/kubernetes` and **is ported** here inside `pods.go` and
  `persistent_volume_claims.go`; `ScopedResourceSelectorRequirementsAsSelector`
  is the inlined dependency.
- `port/rbac`: `rbac.go` is upstream `plugin/pkg/auth/authorizer/rbac/rbac.go`
  (`RulesAllow`, `RuleAllows`, the visitor, `Authorize`) without klog and the
  client-go lister adapters; `evaluation_helpers.go` is the five matchers of
  `pkg/apis/rbac/v1/evaluation_helpers.go` (`CompactString` and
  `SortableRuleSlice` dropped);
  `rule.go` is `pkg/registry/rbac/validation/rule.go` without
  `ConfirmNoEscalation`, with upstream's `StaticRoles`/`NewTestRuleResolver`
  as the production resolver; `snapshot.go` reads `<dir>/rbac/*.yaml` and
  answers `Request{Subject, Verb, Group, Resource, Subresource, Name,
  Namespace}` with the user.Info the apiserver would attach to a
  ServiceAccount (`system:serviceaccounts`, `system:serviceaccounts:<ns>`,
  `system:authenticated`).
- `port/chain`: the three ports in the apiserver's order for one object
  (LimitRanger mutate, LimitRanger validate, ResourceQuota last); `NewFiles`
  takes the scenario's LimitRange and quota-status file names.
  `port/snapshot`: YAML/List decoding into typed objects, admission
  attributes for a dry-run create, a `cache.GenericLister` over a slice.
  `port/oracle`: manifest types, the kubectl wrapper stripper, the leaf
  flattener and a kubectl runner for the live tests. `port/gates`: the gate
  constants.
- Comparison rules (`port/chain/chain_test.go`):
  - **Denials**: the only text stripped from kubectl's stderr is kubectl's
    own wrapper `Error from server (<Reason>): error when creating "<file>": `
    (regexp `kubectlWrapper` in `port/oracle/oracle.go`) plus the trailing
    newline. What remains is `StatusError.Error()` verbatim
    (`pods "x" is forbidden: ...`) and must equal the offline error text
    exactly. Every scenario is pinned in both directions.
  - **Mutations**: input, offline result and server result are flattened to
    RFC 6901 leaves after dropping the server-managed paths
    (`creationTimestamp`, `uid`, `generation`, `resourceVersion`,
    `managedFields`, kubectl's `last-applied-configuration` annotation,
    `status`). The offline diff against the input must be a subset of the
    server diff, and on the leaves LimitRanger owns (container and init
    container `resources`, the `kubernetes.io/limit-ranger` annotation) the
    two diffs must be identical. The leaves only the server adds (defaulting,
    ServiceAccount, Priority, DefaultTolerationSeconds admission) are logged.
  - **RBAC**: the offline verdict must equal kubectl's `yes`/`no`; the group
    of a bare resource name is the discovery step kubectl does
    (`oracle.GroupFor`: `deployments` -> `apps`).
- Tests: `go test -race -count=1 ./...` runs `TestGoldens` (offline, always),
  `TestCanIGoldens`, the unit tests per port, `TestRecomputeMatchesControllerStatus`
  (the ported `Usage` functions over the real objects reproduce the quota
  controller's `status.used` of both phases), and two live tests
  (`TestOracleLive`, `TestCanILive`) that replay every scenario and can-i
  against the kind oracle with the **live** LimitRange and quota status and
  skip cleanly when the context is unreachable (`FATHOM_ORACLE_CONTEXT`
  overrides the name). Nothing is created or deleted on the cluster.
- Port size: non-blank lines from the `package` line onward
  (`awk 'f{print} /^package /{f=1; print}' file | grep -cv '^\s*$'`), so the
  license header and the `fathom:` change log at the top are not counted;
  the same rule on the upstream files.

Run: `go test -race -count=1 -v ./...` (about 5 s; the live tests add ~1 s).
