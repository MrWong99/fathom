# zhi — Architecture Proposal (GitOps-PR-native rewrite)

## 1. Name and one-paragraph thesis

**zhi** (keep the name; the product changes). In the words of a DevOps engineer: *zhi is the PR check that already knows your cluster.* You edit `values-prod.yaml` (or a Kustomize overlay, or a Compose `.env`) in a local UI that is just a form over your Git worktree; every keystroke renders the real Helm/Kustomize/Compose output with the same engines Argo CD, Flux and `docker compose` use, and runs the target cluster's admission chain — CRD CEL, VAP/MAP, Kyverno, Gatekeeper, Pod Security, ResourceQuota, LimitRange, RBAC, storage/ingress classes — against a **signed snapshot of that cluster** you imported last week. No cluster credentials at edit time, no cluster credentials at PR time. When you are happy, zhi opens the PR into the folder Argo/Flux/Kargo already watch, with the rendered diff and the findings as a check run and commit trailers recording which snapshot and which contract the change was validated against. Developers ship the chart with a `values.schema.json`, a few CEL rules and an environment-requirements file; consultants fill values in the generated form and get told, before merge, "this PVC will hang: no RWX-capable StorageClass in `eu-prod`" or "restricted-v2 SCC will reject `runAsUser: 1000`". After merge, zhi listens to the controller and tells the PR what actually happened. Git stays the only source of truth; zhi never applies anything (report §1, §8 "what nobody does" 1–4; brief: gap-closest-competitors-deep-dive §"What nobody does").

## 2. Personas and the transition story

**Developers (author side).** They own the chart/kustomization/compose file and the *contract*: which values exist, their types, defaults, enums, UI hints, cross-value rules, and what the environment must provide. They never leave their existing tooling: the contract is `values.schema.json` (Helm 4 validates it natively), a `values.cel.yaml` in the helm-cel pattern, and a Troubleshoot-compatible requirements file — the only clean developer/deployer split the research found is Replicated's Config + Preflight, and we copy the split, not the platform (report §2.2, §6.2; brief: persona-vendor-to-customer §"Contract patterns", implication 1). Developers run `zhi contract lint` in the chart repo's CI and `zhi validate --env dev` against the dev cluster snapshot in their own PRs. If the chart has no schema, zhi synthesizes one from `values.yaml` defaults plus `# @schema` comments and offers to write it back so the developer can enrich it over time (report §9 #10).

**Consultants and supporters (deploy side).** They own the GitOps repo (or the tenant folder in it) and the target environment. Their first act per environment is `zhi snapshot import --context eu-prod`, run with read-only RBAC, producing a signed OCI artifact; their platform team can schedule `zhi snapshot refresh` as a CronJob (report §5.3 "Freshness like a lockfile"). Then `zhi ui` opens a form generated from the developer's schema, with cluster-aware pickers (StorageClass, IngressClass, namespace, existing Secret key, SecretStore) populated from the snapshot — the Rancher `storageclass`/`secret` picker idea, but offline (report §6.1 row "UI hints"; persona brief implication 5). They never see chart internals they did not ask for: the form groups by `x-zhi-group`, hides `x-zhi-advanced` by default, and every finding is phrased against the field they touched, with the policy that produced it cited. "Propose" opens the PR; the bot re-validates on push; merge is a normal review in their SCM.

**The transition to DevOps.** Nothing changes in the artifacts. A developer deploying to their own namespace picks up the deployer surface: same snapshot, same form, same PR. Because the contract is stored *with the chart* and the environment definition is stored *with the GitOps repo*, ownership can move (developers start owning `environments/dev.yaml`, later `staging.yaml`) without a migration — the persona brief documents exactly this pattern in Replicated (admin console vs `helm install` on the same release) and Plural (self-service = tool opens the PR) (brief: persona-vendor-to-customer §5 "Transition", implication 9). What *does* change over time is who reviews the PR, which is CODEOWNERS, not zhi.

The failure classes each persona stops paying for are the report's table (§3.1): developers stop shipping charts that assume privileged dev clusters (#7 PSA, OpenShift SCC/UID); deployers stop discovering quota (#8), missing StorageClass/Secret references (#10), removed APIs (#4) and policy denials (#6) post-merge, and stop waiting 10 min to hours for the first real admission check (§3.2).

## 3. Domain model

Entities, their identity, and where they live. Addressing is **JSON Pointer (RFC 6901)** everywhere — for values and for rendered objects — because the current flat slash-path model cannot express list indices or camelCase Kubernetes keys (audit brief §1.1, lesson 1). Manifests are identified by `apiVersion/kind/namespace/name` plus source file; Compose services by `file + service`.

| Entity | Identity | Lives in | Notes |
|---|---|---|---|
| **Workspace** | Git remote URL + subpath; `.zhi/workspace.yaml` | Git (GitOps repo) | Lists environments, package bindings, SCM provider, snapshot registry, lockfile of plugins/profiles. One workspace = one repo the controllers watch. |
| **Package** | Source ref + digest (`oci://…@sha256`, `git+path@sha`, local chart dir) and `kind: helm \| kustomize \| compose \| exec` | App repo or OCI; referenced from workspace | The deployable unit as Argo/Flux/Compose see it. Contains or references a **Contract**. |
| **Contract** | Digest of (`values.schema.json` + `values.cel.yaml` + `requirements.yaml`) | App repo, next to the chart (`chart/values.schema.json`, `chart/values.cel.yaml`, `chart/zhi/requirements.yaml`) | Developer-authored, signed separately from values (brief: gap-deployer-environments §"Procurability" role separation). Consumed natively; other formats (`# @schema`, Timoni `#Config`, kro, XRD, Compose `${VAR:?}`) ingest into the same internal 2020-12 model (report §6.1). |
| **Environment** | Name, unique per workspace | Git (`.zhi/environments/<name>.yaml`) | Promotion order, targets (1:N), namespaces, GitOps binding (tool, Application/HelmRelease/Kustomization ref, path, branch, hydrated branch), snapshot ref + max age, secret-store bindings, matrix dimensions, distribution profile id (report §9 #15). Derived on `zhi env import` from ApplicationSet generators, Flux `clusters/<env>`, Kargo Stages. |
| **Target** | Cluster identity as recorded by the importer (API server URL hash + `kube-system` namespace UID) or Compose host id | Snapshot provenance | An environment may span several clusters; each has its own snapshot. |
| **Snapshot** | OCI digest; provenance (cluster id, server version, capture time, capturing identity, per-layer hash/resourceVersion, profile id, TTL) | OCI registry (large) or `.zhi/snapshots/` (small/air-gapped); referenced by digest from the environment | The pseudo cluster (report §5.1, §9 #4). Read-only, redactable, signed. Includes a "declared, not observed" overlay. |
| **Values layer** | (environment, layer position, file path, format) | Git | The merge stack per environment: chart defaults < common < variant < env < cluster < runtime (`substituteFrom`, cluster-Secret labels — from snapshot) < machine-managed (Image Updater/Renovate markers, read-only). Every key resolves to (layer, file, yaml.v3 node, line:col) (report §9 #2, #13). |
| **Change** | Branch name + commit SHA(s) | Git worktree → PR | The working set of edits; becomes commits with trailers. |
| **Rendered set** | Digest of canonical rendered manifests per environment | Ephemeral; digest recorded in report and trailers; optional hydrated branch | Each object carries **lineage**: rendered pointer → template file:line → values pointer(s), for back-mapping findings to form fields. |
| **Finding** | Hash of (engine, rule id, manifest identity, `instanceLocation`) — stable for baselines/suppressions | Report | The canonical record: `instanceLocation`, `keywordLocation`, message, severity (Info/Warning/Blocking), fidelity tier + fidelity tag, source engine + policy name/version, file:line:col in Git, values back-pointer, `proposedValue` fix hint, "would fail at" stage (admission/scheduling/runtime) (report §9 #11; audit brief §1.3, lesson 4). |
| **Report** | Digest of canonical JSON | Emitted to PR check + optionally pushed as OCI referrer of the snapshot; DSSE-signed on request | Findings + provenance: snapshot digest, contract digest, rendered digest, dry SHA, tool version, profile version, unknown-input badges (report §9 #26, #30). |
| **Component** | Name unique per package | Contract (`x-zhi-component`) + environment state (`components:` in env file) | Carried from current zhi: dependency graph, cycle detection, cascade disable, mandatory pre-enable — re-addressed to manifest identity / values pointers instead of string prefixes, and mapped onto Helm `condition`, Kustomize `Component`, Compose `profiles` (audit brief §1.5; report §6.1 row "Component toggles"). |
| **Profile** | `openshift-4.20`, `aks-automatic-2026-06`, … + version | OCI (signed), lockfile-pinned | Encodes the opaque half of managed distributions (SCC mutation, Autopilot ratios, AKS safeguards mutators, EKS NodePool matching, Rancher PSACT) as data + CEL (report §9 #18; brief: gap-deployer-environments Part 1). |

**What lives where — the rule.** Git holds everything a human reviews or promotes (workspace, environments, values, contracts, lockfile, snapshot *digests*). The OCI registry holds everything big, signed and immutable (snapshots, profiles, plugin binaries, reports as referrers). Nothing holds secret values: values files carry typed references `ref://<store>/<path>#<key>[@version]`, rendered to ESO `ExternalSecret` by default, SealedSecret (cert in snapshot) or SOPS optional; validation runs with secrets absent (report §9 #14). Local state (UI session, dirty edits, cached renders) is the worktree plus `~/.cache/zhi`; there is no database.

## 4. Architecture

### 4.1 Binaries and packages

One static binary, `zhi`, `CGO_ENABLED=0`, pinned to k8s.io v0.37.0; a `-fips` variant built with `GOFIPS140` for regulated buyers later (brief: gap-go-embedding §5; gap-deployer-environments Part 2). Subcommands are the surfaces: `zhi ui`, `zhi validate`, `zhi render`, `zhi diff`, `zhi propose`, `zhi check` (bot mode), `zhi snapshot {import,refresh,verify,export}`, `zhi env import`, `zhi contract {init,lint,emit}`, `zhi lsp`, `zhi mcp`, `zhi feedback`. No marketplace server, no mirror server in v1 (audit brief §2.7).

```
cmd/zhi/                       Cobra entry; all surfaces
internal/workspace/            .zhi/ files, environments, lockfile
internal/layout/{argo,flux,kargo,compose}/   discover values model from controller CRs / compose layout
internal/values/               layer stack, yaml.v3 node round-trip, effective values, JSON Pointer, lineage
internal/contract/             schema ingest (values.schema.json, #@schema, Timoni, kro, XRD, Compose vars),
                               x-zhi vocabulary, CEL rule compile via k8s.io/apiserver/pkg/cel/environment
internal/render/{helm,kustomize,compose,exec}/  helm.sh/helm/v4, krusty, compose-go v2, ResourceList adapter
internal/snapshot/             format, importer (client-go discovery + dynamic), verify (cosign v3 offline), export adapters
internal/pseudo/               ordered admission-chain emulation (§4.3)
internal/pseudo/engines/{schema,psa,limitrange,quota,vap,map,kyverno,gatekeeper,rbac,webhook}/
internal/profiles/             distribution profiles (data + CEL), loader, versioning
internal/findings/             canonical record, severity mapping, back-mapping
internal/report/               json, sarif, policyreport, markdown, dsse
internal/scm/{github,gitlab}/  branch, commit trailers, PR, check run / commit status, comments
internal/feedback/             Argo REST/notifications, Flux commit-status providers, correlation
internal/ui/                   htmx shell + embedded RJSF island, middleware chain, SSE
internal/lsp/  internal/mcp/
pkg/zhi/resolver/v1/           the one gRPC plugin contract (secret stores, credentialed collectors)
third_party/k8s/{limitranger,rbac,quotacore}/   vendor-forked from k8s.io/kubernetes@v1.37.0 (~2k LOC)
third_party/NOTICES
```

### 4.2 Data flow: edit → PR

1. `zhi ui` opens on a worktree. `internal/layout` reads the controller CRs in the repo (Argo `Application.spec.source(s).helm.{valueFiles,valuesObject,parameters}`, `sourceHydrator.drySource`, ApplicationSet git-files globs; Flux `HelmRelease.spec.{values,valuesFrom,chart.spec.valuesFiles}`, `Kustomization.spec.postBuild.substitute(From)`, patches; Compose `-f` chains and `.env`) and builds the values-layer stack per environment (report §9 #2). Runtime layers (`substituteFrom` ConfigMaps, cluster-Secret labels) come from the snapshot's `gitops/` layer, redacted-but-keyed.
2. The contract is resolved from the package; the form is generated; effective values per environment are shown with their source layer.
3. On every change: mutate → render → validate (Tier 0) → revert if Blocking, else keep — the current web UI's mutate-validate-revert pattern (audit brief §3.2 "Steal this"). Findings map to fields via lineage. Budget: < 2 s per save for a typical app (report §1 "three-tier ladder"); bigger repos validate only environments affected by the changed files.
4. "Propose" → `internal/scm` creates branch `zhi/<env>/<slug>`, writes comment-preserving YAML, commits with trailers (`Zhi-Snapshot: sha256:…`, `Zhi-Contract: sha256:…`, `Zhi-Rendered: sha256:…`, `Zhi-Report: sha256:…`, `Zhi-Tier: T0`), opens the PR with a collapsible per-environment section: rendered diff (dyff, GitHub/GitLab markdown), findings table, unknown-input badges, snapshot age.
5. `zhi check` (GitHub Action / GitLab job / webhook receiver) re-runs on the PR head against *every* environment whose files changed, posts a check run + SARIF, and refuses to clear Blocking findings if a snapshot is older than the environment's `maxAge` (report §5.3 "Freshness").
6. After merge, `internal/feedback` correlates: hydrated SHA ↔ dry SHA (`refs/notes/source-hydrator`) ↔ PR ↔ Argo app status / Flux conditions; posts "synced/healthy at 14:02" or "controller rejected: …" back on the merged PR, and records the "caught pre-push vs. would have failed at" metric (report §9 #24, #31).

### 4.3 The render → validate pipeline (ordered)

Order matters because the apiserver mutates before it validates and because Pod-level checks apply to Pods, not Deployments (report §3.1 rows 7–9, §5.3):

1. **Render** with the real engines: Helm v4 SDK with `Capabilities` (`kubeVersion`, `apiVersions`) and `lookup` fed from the snapshot; `krusty`; compose-go v2 with strict interpolation; exec adapters for Timoni/KCL/Pkl/ytt/`flux build` speaking the KRM `ResourceList`. Warn when a chart looks up a kind absent from the snapshot (report §9 #3).
2. **Expand** workloads to Pod templates (Kyverno-autogen/gator-expand style) so PSA, LimitRange, quota and Pod-scoped policies see what the controllers will create.
3. **Schema**: strict fields, structural schema, defaulting, CRD `x-kubernetes-validations` CEL with server cost constants via `customresource.NewStrategy`/`schema/cel` (kubectl-validate library by pseudo-version); native types via the snapshot's `/openapi/v3` — tagged *approximate* because KEP-5073 rules are not published (brief: gap-k8s-offline-fidelity-facts §1, §4).
4. **Discovery**: removed/deprecated APIs on the target minor; namespaced-vs-cluster scope (replaces pluto/kubent).
5. **NamespaceLifecycle** (namespace exists or is created in this change; OpenShift project-template injection for new namespaces).
6. **LimitRanger** (mutate defaults, then validate) — vendor-forked.
7. **PodSecurity** via `k8s.io/pod-security-admission/policy` with namespace labels; cluster defaults/exemptions from the declared overlay or the distribution profile (EKS/GKE privileged-no-exemptions; OpenShift preset) (fidelity brief §8).
8. **ResourceQuota**: `(used + delta) ≤ hard` per namespace with `status.used` from the snapshot, tagged *stale* → Warning; OpenShift ClusterResourceQuota summed across selected namespaces (profile).
9. **Mutation**: MAP via `k8s.io/apiserver/.../policy/mutating`, Kyverno mutate rules via CLI, AKS/GKE profile mutators; show the mutated diff as "what the cluster will store" (also prevents Argo drift alerts on Autopilot ratio rewrites — deployer brief GKE row).
10. **Validation policies**: VAP via `validating.NewValidator` + `cel.NewCompositedCompiler` as Gatekeeper's `k8scel` driver does (informer-free); Kyverno CLI `apply` with generated `Context`/`Values`/`UserInfo`/`--parameter-resource` files; Gatekeeper via the cluster's *generated* VAP/VAPB first, embedded OPA + `frameworks/constraint` for Rego templates with snapshot inventory; Kubewarden via `kwctl` later (report §9 #7; embedding brief §5).
11. **Webhook match prediction**: which Validating/MutatingWebhookConfigurations match which objects, logic unknown → Warning badge; profile adapters may claim known vendor webhooks (cert-manager, GKE Warden).
12. **RBAC**: `RulesAllow` (vendor-forked) for the controller identity (Argo/Flux SA) against the snapshot's rules; SCC `ConstraintAppliesTo` on OpenShift uses the same snapshot-backed authorizer (deployer brief OpenShift row).
13. **References and catalogs**: Secret/ConfigMap names+keys, ServiceAccounts, StorageClass (+CSIDriver RWX capability), IngressClass/GatewayClass, PriorityClass, RuntimeClass, image registry allow/block lists + IDMS/ITMS, Route/Ingress host collisions.
14. **Immutable fields / SSA conflicts** against the snapshot's last-known live object (report §3.1 row 13).
15. **Environment requirements** (the contract's analyzers) evaluated against the snapshot.
16. **Developer CEL rules** over `values`, `rendered`, `env`.

Severity mapping is fixed in core: Deny/Enforce/schema/CEL/Preflight-fail → Blocking; Warn/Audit/dryrun/webhook-match/stale-quota/native-approximate → Warning; annotations/deprecation-in → Info; PolicyExceptions and `enforcementAction` honoured; `messageExpression` output preserved and the source policy cited (report §9 #9).

### 4.4 Snapshot format

Own format (report §10.1 verdict "Snapshot on-disk anchor"): a directory that packs into one OCI artifact with per-layer tar blobs (`application/vnd.zhi.snapshot.<layer>.v1+tar`), a config blob `manifest.json` (provenance, per-layer digest + resourceVersion, server version, engine versions, feature-gate/API-version state such as MAP v1 vs v1beta1, profile id, TTL), and a cosign v3 bundle as an OCI referrer. Snapshot layers, mirroring report §5.1:

```
manifest.json
discovery/            APIGroupDiscoveryList (preferred versions, scope, deprecations)
openapi/api/v1.json, openapi/apis/<g>/<v>.json     kubectl-validate --local-schemas layout, hash-keyed
crds/                 raw CRDs (x-kubernetes-validations, defaults, pruning, ratcheting kept)
admission/policies/   VAP/VAPB, MAP/MAPB in 1.37 static-manifest (v1) form; params/ objects
admission/webhooks/   Validating/MutatingWebhookConfiguration metadata only
engines/kyverno/      (Cluster)Policy, ValidatingPolicy…, PolicyExceptions, chart version
engines/gatekeeper/   ConstraintTemplates, Constraints, Config/SyncSet, generated VAP/VAPB
engines/kubewarden/   (Cluster)AdmissionPolicy settings
namespaces/           Namespace (labels/annotations), ResourceQuota (spec+status), LimitRange, default NetworkPolicies, ClusterResourceQuota
catalogs/             StorageClass+CSIDriver, IngressClass, GatewayClass, PriorityClass, RuntimeClass, SCC, ComputeClass/NodePool/NodeClass
rbac/                 Roles/ClusterRoles/bindings; subjectrules/<identity>.json
inventory/            names+keys only: Secrets, ConfigMaps, ServiceAccounts, Services, Ingress/Routes, workloads (oldObject)
gitops/               Applications/ApplicationSets, HelmReleases/Kustomizations, substituteFrom (keyed, redacted), (Cluster)SecretStores, Sealed Secrets cert, Kargo Stages
nodes/                optional: allocatable, labels, taints, per-node request aggregates
declared.yaml         operator overlay: PSA defaults/exemptions, static .static.k8s.io bundle dir, webhook stubs, profile id
```

The importer is a kubectl-free, read-only Go client over discovery + a fixed GVR list that degrades per layer ("quota unknown: no list on resourcequotas") and filters by workspace namespaces (report §9 #5). `zhi snapshot export` writes kubeconform, kubectl-validate, Kyverno context, gator inventory and flux-schema layouts so existing CI stays useful (report §9 #22). Compose hosts use the same manifest + `host/` layer (§7).

### 4.5 Fidelity tiers shipped in v1

- **T0-a** (schema/discovery/CRD-CEL) and **T0-b** (full ordered chain above) — default on every save and in `zhi check`. Every finding carries `fidelity: schema-valid | apiserver-valid(crd-exact|native-approx) | policy-valid(skipped: N)` and the report carries badges for unobservable inputs ("PSA exemptions unknown", "authorizer() evaluated as allow", "3 webhooks match, logic unknown", "static policies may exist on 1.37+"). `--strict` turns unknowns into Warnings (report §5.3).
- **T2** live gate — optional, when a kubeconfig or Argo/Flux API token exists: `kubectl apply --dry-run=server --validate=strict` with the controller's fieldManager, Argo `server-side-diff`/`manifestsWithFiles`, `flux diff kustomization`. Live findings are labelled separately, never merged, with the delta explained (side-effect webhooks, mutations, new-resource blindness of SSD) (report §9 #24; fidelity brief §5).
- **T1** (envtest/KWOK hydrated from the snapshot) is *not* in v1; it is a spike (§11) and a phase-3 opt-in `zhi verify --engine apiserver` (report §9 #32).

### 4.6 UI approach

Server-rendered Go templates + htmx for the shell (tree of environments/packages/components, findings page grouped by severity with file:line links, environment × component matrix, rendered-manifest and diff panes, SSE for long operations), reusing the current middleware chain whose CSRF/CSP-nonce/ETag/gzip/SSE-flusher interactions are already solved (audit brief §3.2). The **form itself is a single embedded React island running RJSF v6**: the schema + `x-zhi-*` vocabulary map to widgets (`x-zhi-widget`, `x-zhi-order`, `x-zhi-group`, `x-zhi-secret`, `x-zhi-cluster-ref: storageclass|namespace|secret-key|ingressclass|image`, `x-zhi-advanced`, show/hide by `x-zhi-when` CEL), cluster-ref widgets fetch their options from the snapshot through the local API, and server findings arrive as JSON Schema 2020-12 output units that are mapped by `instanceLocation` into RJSF `extraErrors` — the shape the report identifies as the target (report §4.6 RJSF row, §9 #11). The bundle is built once in CI, committed as an asset and embedded; no Node at runtime, no SPA router. This replaces the current flat map/list row editors the audit calls fragile (audit brief §3.6). Findings against rendered manifests (not values) map back through lineage: rendered pointer → template line → values pointer; when no values pointer exists (e.g. a hard-coded template field) the finding is shown on the package with the template file:line and tagged "developer-side".

### 4.7 CLI / CI / LSP / MCP

- **CLI/CI**: `zhi validate --env prod [--snapshot oci://…] --format json|sarif|policyreport|md --exit-code` (0 clean, 1 Blocking, 2 error, like `flux diff`); `zhi check` for PR events; a GitHub Action and GitLab template wrapping it; pre-commit hook (report §9 #19).
- **LSP**: `zhi lsp` publishes diagnostics from the same pipeline, hover with effective value + source layer, code actions applying `proposedValue`; and zhi writes `.zhi/schema.json` with a `# yaml-language-server: $schema=` modeline so any editor gets completion for free (report §9 #28).
- **MCP**: `zhi mcp` (stdio + streamable HTTP, loopback bind, read-only by default, separate token) exposing `validate`, `explain_finding`, `render_diff`, `effective_values`, `import_snapshot`; ships a SKILL.md. Generated from the same internal service description the UI's REST endpoints use, so it is not a third hand-written projection (report §9 #29; audit brief §3.6 recommendation).

## 5. The developer ↔ deployer contract

Three files, all consumable by tools that are not zhi.

**(a) `values.schema.json`** — JSON Schema 2020-12 (Helm 4 defaults to 2020-12 when `$schema` is absent; `santhosh-tekuri/jsonschema/v6` is the validator in Helm 4, compose-go and kubeconform), with the `x-zhi-*` vocabulary registered as a custom vocabulary so hints are type-checked by zhi and ignored as annotations by everyone else (report §6.1; embedding brief §4.3).

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$vocabulary": { "https://zhi.dev/vocab/ui/v1": false },
  "type": "object",
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1, "maximum": 20, "default": 1,
                      "x-zhi-group": "Scaling", "x-zhi-order": 10 },
    "persistence": { "type": "object", "properties": {
      "enabled": { "type": "boolean", "default": true },
      "accessMode": { "enum": ["ReadWriteOnce", "ReadWriteMany"], "default": "ReadWriteOnce" },
      "storageClass": { "type": "string", "x-zhi-cluster-ref": "storageclass",
                        "x-zhi-help": "Leave empty for the cluster default." } } },
    "db": { "type": "object", "properties": {
      "passwordRef": { "type": "string", "format": "zhi-secret-ref", "x-zhi-secret": true,
                       "x-zhi-cluster-ref": "secret-key" } } },
    "ingress": { "type": "object", "x-zhi-component": "ingress", "properties": {
      "className": { "type": "string", "x-zhi-cluster-ref": "ingressclass" } } }
  }
}
```

**(b) `values.cel.yaml`** — cross-value and value→rendered rules in CEL, helm-cel pattern, VAP variable set plus `values`, `rendered`, `env`; compiled with the k8s CEL environment pinned to the snapshot's minor so the same expressions can later be promoted into a real VAP (report §6.1 "Cross-value rules", §9 #12).

```yaml
apiVersion: zhi.dev/v1
kind: ValuesRules
rules:
  - name: rwx-needs-capable-class
    severity: Blocking
    expression: >-
      values.persistence.accessMode != 'ReadWriteMany' ||
      env.storageClasses.exists(sc, sc.name == values.persistence.storageClass && sc.capabilities.exists(c, c == 'RWX'))
    message: "ReadWriteMany requires an RWX-capable StorageClass in this environment"
  - name: ha-needs-replicas
    severity: Warning
    expression: "!values.ha.enabled || values.replicaCount >= 3"
    messageExpression: "'HA enabled with only ' + string(values.replicaCount) + ' replicas'"
```

**(c) `zhi/requirements.yaml`** — environment requirements: a Troubleshoot-compatible `Preflight` analyzer list (embedded Go library, extended with analyzers the research found missing: RWX-capable StorageClass via CSIDriver, ResourceQuota headroom, PSA namespace level, IngressClass/GatewayClass, CRD *version*, egress reachability), plus `kubeVersion` and `requiredAPIs`; analyzers may reference the deployer's chosen values, as Troubleshoot allows (report §6.1 "Environment requirements"; persona brief implications 2, 4).

```yaml
apiVersion: zhi.dev/v1
kind: Requirements
kubeVersion: ">= 1.30.0-0"
requiredAPIs: ["gateway.networking.k8s.io/v1/HTTPRoute", "external-secrets.io/v1/ExternalSecret"]
analyzers:
  - clusterVersion: { strict: true, outcomes: [{ fail: { when: "< 1.30.0", message: "Requires Kubernetes 1.30+" } }, { pass: { message: ok } }] }
  - customResourceDefinition: { customResourceDefinitionName: certificates.cert-manager.io,
      outcomes: [{ fail: { message: "cert-manager required" } }, { pass: { message: ok } }] }
  - zhiStorageClassCapability: { storageClassName: "{{ .Values.persistence.storageClass }}", accessMode: "{{ .Values.persistence.accessMode }}",
      outcomes: [{ fail: { message: "class cannot provide the requested access mode" } }, { pass: { message: ok } }] }
  - zhiQuotaHeadroom: { namespace: "{{ .Release.Namespace }}", outcomes: [{ warn: { when: "cpu.requests > 80%", message: "namespace CPU quota nearly exhausted" } }] }
```

`zhi contract emit` writes the standard forms — `values.schema.json` into the chart, `Chart.yaml kubeVersion`, and a `troubleshoot.sh/kind: preflight` Secret — so `helm install`, Argo, Flux and `kubectl preflight` users get the contract without zhi at runtime (report §6.2). Severity in the contract is the current zhi triad; it maps 1:1 onto Troubleshoot fail/warn/pass + `strict` and VAP Deny/Warn/Audit (persona brief implication 6).

## 6. GitOps integration

**Discovery.** `internal/layout` never asks the user where values live; it reads what the controllers read. Argo: `Application.spec.source(s).helm.{valueFiles,valuesObject,parameters,fileParameters}` with the documented precedence `parameters > valuesObject > values > valueFiles > chart`, `kustomize.{images,replicas,patches}`, ApplicationSet git-files generators (per-env `config.json/yaml` flattened into template params), cluster generator labels (from the snapshot's cluster Secrets), `sourceHydrator.{drySource,syncSource,hydrateTo}`. Flux: `HelmRelease.spec.{values,valuesFrom[ConfigMap|Secret, valuesKey, targetPath, optional], chart.spec.valuesFiles}` (order: valuesFrom in list order, then inline), `Kustomization.spec.postBuild.substitute(From)` with `${VAR:=default}` strictness (missing var without default = Blocking, matching kustomize-controller 1.9 strict mode), `spec.patches`, `spec.images`. Kargo: `Stage` definitions to show the promotion path and which files each Stage's `yaml-update`/`kustomize-set-image` touches (read-only, machine-managed). Compose: `-f` chains, `compose.override.yaml`, `COMPOSE_FILE`, `.env`, `env_file`, `profiles`, `include` (report §2.1, §4.1; brief: gitops-controllers §Design implications; environments brief implication 4). Layouts supported natively: folder-per-env on trunk, app-of-apps / ApplicationSet-generated, rendered-manifest branches; branch-per-env is read-only/legacy with a warning (it is the documented anti-pattern).

**Producing the PR.** Edits are applied through yaml.v3 nodes so comments, ordering, anchors and Image-Updater/Renovate markers survive; machine-managed keys are read-only in the UI (report §9 #13). One branch per change, commits carry the `Zhi-*` trailers above plus `Argocd-reference-commit-*`-style references when a hydrated branch is involved. The PR body is argocd-diff-preview-shaped: per environment a collapsible rendered diff (dyff), a findings table with severity, fidelity tag, policy source and file:line links, badges for unknown inputs, snapshot id and age, and a one-line "validated against `eu-prod@2026-09-12T10:00Z (sha256:…)`". `zhi check` posts a GitHub Check Run / GitLab commit status and SARIF; both are what branch protection can require. Direct push to a controller-synced branch is supported only behind an explicit per-environment `allowDirectPush: true` for dev environments (report §10.2 Q13 — owner decision; default off).

**Provenance recorded.** Snapshot digest, contract digest, rendered-set digest, report digest, profile version, tool version, dry SHA — in trailers, in the report, and optionally as a DSSE envelope with a SLSA VSA v1 predicate whose subject is the rendered digest and whose `policy` is the snapshot digest + profile version, attached as an OCI referrer of the snapshot (report §9 #26, #30; deployer brief Part 2 item 1). Reports are deterministic: same inputs → same digest, so evidence can be reused.

**Hydrated branches and promotion.** For Argo Source Hydrator estates, zhi validates the `hydrateTo` branch as a PR check (the hydrated YAML is a lintable artifact; the dry SHA comes from `refs/notes/source-hydrator`, not commit messages) and, because Argo "will not move `hydrateTo` → `syncSource`", zhi can be the gate that opens that PR too (report §4.1 Source Hydrator row; environments brief). For gitops-promoter, zhi is a `CommitStatus` provider — the cleanest insertion point on the hydrated path (report §9 #25). For Kargo OSS, zhi's PR is what `git-wait-for-pr`/`git-merge-pr` wait on, and `zhi check` can be called from the built-in `http` step; the container-step integration is Enterprise-only and therefore an Akuity-customer add-on, not a dependency (report §4.1 Kargo row, §10.1 verdict). For Flux Operator ResourceSets and the Argo PR generator, zhi only links the preview URL back into the PR; it never orchestrates preview environments (inner-loop brief implication 14). DRY-repo PRs are the primary write target because they work for Flux, Compose and Kargo OSS alike (report §10.1 "Primary Git flow").

**Post-merge.** `zhi feedback` subscribes to Argo app status (REST/gRPC watch or Argo Notifications webhook target) and Flux conditions (notification-controller commit-status providers), correlates hydrated SHA ↔ dry SHA ↔ PR ↔ author, and comments the outcome on the merged PR: synced/healthy, or the controller's rejection message next to the finding zhi *did or did not* raise pre-push. That last comparison feeds the outcome metric ("Blocking findings caught pre-push per PR" and "would have failed at admission/scheduling/runtime") the deployer persona cares about; it is computed locally with zero egress (report §9 #24, #31). Optionally zhi fires Argo `/api/webhook` or a Flux `Receiver` to skip the 120 s + 60 s jitter / `spec.interval` poll (report §3.2). Rollback stays `git revert`, but zhi's revert PR is annotated with the non-revertible effects it can see (Helm hooks/Jobs, immutable StatefulSet/PVC fields, CRD schema changes) and a warning about `selfHeal` fighting in-cluster rollbacks (gitops-controllers brief).

## 7. Docker Compose

Same model, second target, no afterthought (report §1 "Compose is a first-class second target", §9 #17). The package kind is `compose`; the renderer is compose-go v2 (`cli.NewProjectOptions` + `LoadProject`) — loaded first with interpolation off to run `template.ExtractVariables` (the editable value set with defaults/required flags, i.e. what `docker compose config --variables` prints), then with the UI's mapping to obtain the resolved `types.Project`; schema (2020-12) and the ~28 consistency checks map to Blocking with field pointers. The contract for Compose is the same `values.schema.json` over the variable set (synthesized from `${VAR:-default}` / `${VAR:?}` and Portainer-style `env` metadata when present), the same CEL rules, and requirements analyzers over the host snapshot. Write-back is `.env` and `compose.<env>.yaml` overrides plus typed `x-zhi-*` blocks — `compose.yaml` itself is never rewritten, so plain `docker compose` keeps working and Komodo/Portainer/CI-over-SSH keep syncing (docker-compose brief implications 1, 6, 10).

**Host snapshot** (`host/` layer, imported read-only from the Docker API, Portainer API or Komodo Periphery): Engine/API and Compose versions, OS/arch, cgroup version, rootless/userns-remap, seccomp/AppArmor defaults, default address pools, CPU/memory/GPU, listening host ports (published containers, optionally `ss`), existing networks/volumes/containers/projects (`docker compose ls`), images with digests, registry auth reachability, bind-source existence/permissions, swarm/podman flags, and an optional host policy pack (report §5.1 "Compose analogue"; docker-compose brief implication 3).

**Host-aware checks** (the ones `config -q` cannot see, docker-compose brief §Pain-point evidence): strict interpolation (referenced-but-absent variable → Blocking, defaulted → Warning; empty result in `image`/`ports`/`volumes` → Blocking), published-port collisions against the snapshot and across services/projects, missing bind sources, cross-project `container_name` collisions, `cpus`/`mem_limit` vs capacity, `gpus` without GPU, image tag not cached and registry unreachable (Info) or nonexistent (Blocking when online via registry HEAD), `deploy.*` with `swarm: false` (Warning), Swarm-unsupported attributes with `swarm: true`, podman-compose-unsupported attributes with `podman: true`, version gates (`env_file.required` ≥ 2.24, `pre_start` ≥ 5.3), relative paths in override files resolved against the first `-f` file. The policy pack is CEL over the normalized compose-go JSON (one rule language for both targets), with a default pack mirroring Portainer BE's security dimensions (no privileged, no host pid/net/ipc, binds only under allowed roots, no added capabilities, explicit tags/digests, healthcheck required). A plan view diffs the resolved model against the snapshot's containers/volumes/networks (what would be recreated) in the style of Compose v5.2 reconciliation and Uncloud's plan. Secrets: SOPS (`exec-env`, dotenv output) is the one model identical for Compose and Kubernetes; provider CLIs (`infisical run`, `sops exec-env`) resolve at export time only (report §4.7; environments brief implication 5). Target flavours (`docker-compose`, `swarm`, `podman-compose`/Quadlet, Uncloud) are a snapshot field switching attribute-support matrices; Quadlet export runs on the post-interpolation model (docker-compose brief implication 8).

## 8. Plugin depth

**Answer to the owner: shallow.** A fixed Go core, extension by *data and expressions*, and at most two loadable kinds — one of which does not ship in v1. Every brief converged on this; the thriving tools (Flux, Kyverno, Renovate, Helm 4) extend via data, CEL, digest-pinned OCI binaries or Wasm, and in-path sidecar/gRPC plugins are universally painful (Argo CMP #15006, Kustomize plugins alpha for five years) (report §1 "Shrink the plugin system", §7.1–7.3).

| Concern | Mechanism | Why |
|---|---|---|
| Source loaders (YAML/JSON/TOML/env, Helm values, Kustomize, Compose) | **Fixed core** | Need source positions (yaml.v3 nodes) for file:line findings and comment-preserving writes; no heterogeneity worth a boundary (report §7.2). |
| Layout discovery (Argo, Flux, Kargo, Compose, ApplicationSet) | **Fixed core + declarative file-pattern maps** (Renovate shape) | The CR shapes are stable and few; unknown layouts are configured, not coded. |
| Renderers | **Fixed core** (Helm v4 SDK, `krusty`, compose-go) **+ exec adapter** speaking KRM `ResourceList` for Timoni/KCL/Pkl/ytt/`flux build` | Real engines in-process so "valid in zhi" = "valid for the controller"; typed languages bring CGO/JVM/Node and are excluded from the core (report §4.2, §9 #3). |
| Values contract | **Data** (JSON Schema 2020-12 + `x-zhi-*` vocabulary) | Helm 4 already enforces it; every other format ingests into it (report §6.1). |
| Cross-value / value→rendered rules | **CEL** with a fixed host library (k8s CEL env) | Same language as VAP/CRDs the deployer already meets; promotable to the cluster; no bespoke `when` DSL (Rancher's Ember/Jexl divergence is the cautionary tale) (report §9 #12). |
| Cluster policies (VAP/MAP, Kyverno, Gatekeeper, PSA, quota, LimitRange, RBAC) | **Data** (imported manifests, evaluated untranslated by embedded k8s.io packages, embedded OPA, Kyverno/kwctl CLIs exec'd with zhi-owned exit codes) | Engines need in-process performance and shared type environments; Kyverno is isolated as a subprocess for dependency/CVE hygiene (report §9 #7; embedding brief §0.4). |
| Distribution profiles (OpenShift, AKS, GKE, EKS, Rancher) | **Data + CEL**, versioned, signed OCI, lockfile-pinned | The opaque half of managed admission must be maintained from vendor docs with changelogs an auditor can read (report §9 #18). |
| Snapshot importers (K8s, Docker, Portainer, Komodo, Rancher mgmt cluster, OpenShift/GKE/AKS/EKS APIs) | **Fixed core**; `resolver/v1` only for credentialed vendor collectors (Binary Authorization, cloud policy APIs) | Importers are the product; only credentialed cloud calls need isolation. |
| Secret resolvers (Vault/OpenBao, KMS, Infisical, 1Password) | **`resolver/v1` gRPC** (go-plugin, first-party ones compiled in) | Credentialed, long-lived, heterogeneous — the one place all briefs still want a process boundary; Vault itself chose isolation (report §7.3 #1). |
| Custom checks CEL cannot express | **`check/v1`** — phase 2, Wasm (wazero, zhi-owned ABI) or exec with `ResourceList` in / output units out; signed OCI + lockfile | Kubewarden proves the shape; Helm 4's slow Wasm uptake and the Extism SDK's 18-month gap say do not depend on it in v1 (report §7.3 #2; embedding brief table). |
| SCM targets (GitHub, GitLab; Gitea/Bitbucket later), check runs, comments | **Fixed core**, config-selected | Few, stable APIs; no third party will write one before we do. |
| Report formats (JSON, SARIF, PolicyReport, markdown, DSSE) | **Fixed core + templates** for PR body | Interop formats, not extension points (report §9 #22). |
| UI panels, forms, LSP, MCP | **Fixed core** — output surfaces, not extension points | Backstage/Grafana/VS Code show pluggable UI maximises breakage; current zhi could not even host its own TUI as a plugin (report §7.1; audit brief §2.3). |

**Not pluggable, by decision:** the admission-chain order, severity semantics, the finding record, the snapshot format, the store (Git), transforms in the deploy path (what Flux forbids and Argo suffers), the UI. Distribution of the two loadable kinds and of profiles/snapshots reuses the current OCI client + media types + digest-pinning lockfile and a krew-index-style curated catalog file — no marketplace, no ratings, no mirror server until third-party plugins exist (report §7.3; audit brief §2.7). Versioning like Terraform/LSP: `zhi.plugin.v1`, additive RPCs behind capability flags, tolerant enums.

**Versus the current four gRPC types.** `config` → core loaders + the data contract; `transform` → dropped (a three-way ordering knob nobody exercised, and exactly the kind of in-path mutation controllers reject); `store` → Git (history, rollback, review, authorship for free); `ui` → core. The current boundary costs ~11,000 LOC of gRPC plumbing plus 10,790 generated lines, three lossy serialization layers for values, O(N²) validation, and a `RequiresTTY` leak (audit brief §0, §2.1–2.3, lessons 5–6). The rewrite's one proto (`resolver/v1`, ~5 RPCs) is on the order of 1.5k LOC including client/server, and the check kind, if it ever ships, is a byte contract, not an IDL.

## 9. What to carry from the current zhi, and what to drop

Carry, per the audit brief's concept table (§7.1) and lessons (§7.2):

- **Severity triad** Info/Warning/Blocking — unchanged, now with fidelity tags.
- **Cross-document validation with read access to the whole set** — the concept; reimplemented as one pass over the rendered set, never per-path over gRPC (audit §1.3 O(N²)).
- **`ComponentManager`** — dependency validation, Kahn cycle detection, mandatory pre-enable with transitive deps, cascade disable refusal, `FilterTree` — "best code in the repo", ported almost verbatim and re-addressed to manifest identity and values pointers (audit §1.5).
- **Drift/diff** (`drift.go`, `diff.go`) — promoted to core and generalized from "rendered vs files" to "rendered vs snapshot's last-known live object" and "desired vs controller status" (audit §4.4).
- **`apply.go` subprocess handling** — process groups, `WaitDelay`, pipe-drain-before-Wait, 1 MiB scanner, non-zero exit as result — copied line for line into the exec adapters (Kyverno CLI, kwctl, `flux build`, Timoni) (audit §4.3).
- **Web UI patterns** — mutate-validate-revert inline linting, severity-grouped findings page (now with file:line links), the middleware chain (gzip→ETag→CSRF→CSP nonce→recovery→logging), SSE log pane with `Unwrap()`/`Flush()` chain (audit §3.2).
- **OCI client, media types, multi-platform index, atomic install; digest-pinning lockfile** — the vehicle for snapshots, profiles and the two plugin kinds (audit §2.6).
- **Binary/bundle integrity audit** (`launch/audit.go`: symlink resolution, `..` rejection, SHA-256 vs recorded digest, world-writable warning) — applied to downloaded profiles, policy bundles and plugin binaries (audit §2.4).
- **Interactive OIDC login + local callback server** — reused for cluster/secret-manager auth on import (audit §5.1).
- **CI conventions** — `-race -count=1`, fmt-diff check, codegen freshness check, `testdata/` fixtures; add golden tests over real K8s/Compose fixtures and a policy-conformance table shipped to users (audit §6).
- **Secret references** — the `store.writeonly` idea and the Vault API client (MPL) become `ref://` references (audit §5.1).

Drop: flat slash paths and the `[a-z]` regex; `Val any` and its 330 LOC of coercion; `Metadata["path"]`; non-serializable `Value.Validators`; **Yaegi-interpreted Go in config files** (full-stdlib RCE from a PR — the single most important lesson, audit §1.4); the four gRPC types and generated stubs; JSON-over-protobuf values; `transform.Plugin`; the 27-method `store.Plugin`; the plaintext fallback store; `ui.Controller × 3 frontends`; the TUI (4,536 LOC, forced in-process anyway); the marketplace server (static API keys, "sqlite" that is a JSON file); the unwired Sigstore stack (shell out to cosign v3 / use sigstore-go at the one chokepoint, verified in tests); `text/template`+Sprig as the K8s/Compose bridge and the side-effecting `fileACL`/`fileMode` funcs; the meta-plugin SDK. Defer the air-gap mirror's OCI-layout and bundle code — solid, but only when regulated customers appear (audit §2.6, §7.1).

## 10. MVP, phase 2, phase 3

**MVP — "a consultant validates and proposes a Helm values change for one Kubernetes environment, end to end."**

1. `zhi env import` derives environments from Argo `Application`s and Flux `HelmRelease`s/`Kustomization`s (Helm packages first; Kustomize read-only rendering included because `krusty` is cheap).
2. `zhi snapshot import` with the layers: discovery, OpenAPI v3, CRDs, VAP/VAPB, MAP/MAPB + params, webhook metadata, Kyverno policies, Gatekeeper generated VAP/VAPB, namespaces + quota/LimitRange, catalogs, RBAC + subject rules, allowlisted inventory, GitOps inputs, declared overlay; OCI push/pull with cosign v3 offline verification; `export` to kubeconform/kubectl-validate/Kyverno-context layouts.
3. Tier 0 pipeline (§4.3) minus Rego/OPA and kwctl; Kyverno via CLI; one distribution profile (whichever the owner's consultants deploy into this year — report §10.2 Q7; OpenShift is the evidence-heavy candidate).
4. Contract: `values.schema.json` + `x-zhi` vocabulary + `values.cel.yaml` + requirements with `kubeVersion`, `requiredAPIs` and the Troubleshoot analyzers `clusterVersion`, `customResourceDefinition`, `storageClass` plus zhi's RWX-capability and quota-headroom analyzers; schema synthesis from defaults + `# @schema`.
5. `zhi ui` (form, effective values with source layer, rendered diff, findings), `zhi validate` (JSON/SARIF/markdown, exit codes), `zhi propose` and `zhi check` for GitHub; comment-preserving YAML; commit trailers; ESO `ref://` rendering with store-presence check.
6. Compose: compose-go rendering, strict interpolation, Docker-API host snapshot, port/bind/container-name/capacity checks, `.env`/override write-back, `zhi check` for Compose repos. Included in MVP because it is the same pipeline with a different renderer and the owner's developers author both.

Size: ~28–34k LOC Go non-test (snapshot + pseudo chain ≈ 10k, layout/values/contract ≈ 6k, render adapters ≈ 2k, SCM/report/feedback ≈ 4k, UI ≈ 5k Go + templates + one JS bundle, CLI/workspace ≈ 3k, vendor forks ≈ 2k) plus tests near 1:1 as today. **5–7 months for 1–2 people**, of which the first month is the spikes in §11.

**Phase 2 (≈ 3–4 months, +12k LOC):** GitLab; hydrated-branch checks + gitops-promoter `CommitStatus` provider + Kargo `http` step; post-merge feedback (Argo/Flux status correlation, outcome metrics); OPA + `frameworks/constraint` for Rego Gatekeeper templates with snapshot inventory; second and third distribution profiles; SealedSecret/SOPS rendering; Portainer/Komodo host importers and Swarm/Podman flavour flags; LSP; DSSE/VSA-signed reports and SBOMs; `--strict`/baseline/suppression files; `check/v1` exec contract if a concrete third-party check has appeared.

**Phase 3 (≈ 3–5 months, +8k LOC):** Tier 1 (`zhi verify --engine apiserver` on envtest/KWOK hydrated from the snapshot) if the spike says boot time is acceptable; scheduler-framework fit against snapshot nodes; Kubewarden via kwctl; Wasm `check/v1`; CUE import/export for Timoni/Holos shops; FIPS build variant, air-gap `zhi mirror export/import` reusing the deferred bundle code; MCP server generated from the service description; Rancher PSACT/EKS Pod Identity/GKE Binary Authorization collectors via `resolver/v1`.

## 11. Risks, unknowns, and the lab spikes required before committing

Risks: (1) **native-type fidelity is structurally approximate** and stays so — KEP-5073 will not publish rules; the product must never say "will apply" and must sell the honest fidelity tag as a feature (fidelity brief §1). (2) **Unobservable inputs** — PSA exemptions (no `/configz`), `.static.k8s.io` policies (invisible by design in 1.37), webhook logic, `authorizer()`, stale `status.used`; the declared overlay and profiles cover some, badges cover the rest; the owner must choose the CI default for unknowns (report §10.2 Q2). (3) **Version skew** — one k8s.io minor (v0.37) emulates older clusters downward via `MustBaseEnvSet`; clusters newer than the pin get a skew Warning; per-minor evaluators are explicitly out (embedding brief §2). (4) **Kyverno as subprocess** — exit-code and context-file pitfalls (v1.12 exited 0 on FAIL); zhi owns exit codes and generates context, but Kyverno's CVE cadence and the ClusterPolicy removal in 1.20 (~Nov 2026) mean the CLI must be version-pinned and distributed as a signed OCI artifact with its LICENSE (embedding brief §0.4, §4.1). (5) **Profiles are a maintenance treadmill** — vendor docs change; each profile needs a changelog and an owner. (6) **Adoption risk of the JS island** — mitigated by a single committed bundle and no runtime Node. (7) **cel-go path rename** — never import `cel.dev/cel-go` directly while k8s.io pins `github.com/google/cel-go` (embedding brief §0.2). (8) **License adjacency** — never import flux-operator/schema-catalog (AGPL), helm-docs/Komodo (GPL), Vault/VSO server code (BUSL); exec/read/HTTP only; `govulncheck` in CI (report §9 #21). (9) **Practitioner validation is thin** — Reddit unreachable, budgets exhausted; run the owner-company survey on failure-class frequency before ordering the check backlog (report §10.2 Q17).

Spikes, each a week or less, before architecture freeze (report §1 last bullet, §10.2 Q16):

- **S1 kubectl-validate CEL**: a CRD with a failing `x-kubernetes-validations` rule through `pkg/validator` by pseudo-version; confirm ratcheting with `oldObject`.
- **S2 Kyverno CLI MAP+params**: `kyverno apply --parameter-resource --context-file` with a MAP v1 and a paramRef VAP; confirm exit codes and PolicyReport output on v1.19.1.
- **S3 VAP offline informer-free**: `validating.NewValidator` + `cel.NewCompositedCompiler` with snapshot `namespaceObject`, params and an RBAC-backed authorizer; measure per-object latency.
- **S4 Vendor-fork viability**: copy `limitranger`, `rbac.RulesAllow`, `quota/v1/evaluator/core` from k8s.io/kubernetes@v1.37.0; check the in-tree `pkg/apis/core` type conversions the embedding brief flags as UNVERIFIED.
- **S5 Helm 4.3 2020-12 end-to-end**: a `values.schema.json` with `$vocabulary` and `x-zhi-*` through `helm template` and the Helm 4 SDK; confirm annotation-ignore behaviour and `$ref` loaders.
- **S6 Gatekeeper `k8scel` userInfo**: whether the driver exposes `request.userInfo` offline; decide generated-VAP-first vs driver embedding.
- **S7 Tier-0 budget**: full chain on a 40-object app against a 300-CRD snapshot, target < 2 s warm; decides whether per-keystroke validation is honest.
- **S8 KWOK/envtest boot** (phase-3 gate): kwokctl binary runtime and envtest with a restored snapshot and `--admission-control-config-file`; measure boot, confirm VAP + quota + fake-node interplay.
- **S9 Lineage**: prove rendered-pointer → template line → values pointer for Helm (template positions from the SDK) and Kustomize (origin annotations); the UI's inline errors depend on it.
- **S10 Snapshot size/time**: import of a real production cluster with the fixed GVR list under read-only RBAC; size with and without inventory; hash-keyed OpenAPI refresh.

## 12. Differentiation

- **ConfigHub** validates on edit (`vet-schemas`, `vet-celexpr`, Triggers) but replaces Git as the source of truth and, since August 2026, delivers OCI-pull only with no Git write and no admission/quota import. zhi is Git-native and stateless toward the cluster; a ConfigHub estate could even consume zhi's reports, but the two disagree on where truth lives (report §4.1, §8; competitors brief §ConfigHub).
- **Devtron** has the best schema-per-scope GUI↔YAML form and approval flow, but no policy engine, no snapshot, and it owns the manifest repo — no PR into the customer's repo; locking and approvals are Enterprise-tagged. zhi copies the per-scope schema idea and puts the form in front of *your* repo with the cluster's real policies (report §4.6; competitors brief §Devtron).
- **Flux Schema** is the CRD/CEL layer of a pseudo cluster, fully offline and Apache-2.0, with an AGPL catalog; it has no values model, VAP/MAP, Kyverno, Gatekeeper, PSA, quota, UI or Git write. zhi's snapshot export emits its layout, and zhi can shell out to it; it is a complement, and it validates the "policies as files" direction zhi builds on (report §4.1, §8; competitors brief §Flux Schema).
- **Replicated KOTS + Preflight** is the reference for the developer/deployer split; zhi copies Config+Preflight but evaluates against an imported snapshot instead of at install time in the customer cluster, needs no in-cluster admin console (KOTS's most criticised part), no SaaS control plane, and emits a Troubleshoot-compatible Preflight so Replicated shops lose nothing (report §2.2, §6.2; persona brief).
- **Cyclops** is the closest OSS "schema → form → Git" UI; it is cluster-attached (controller + CRD even in Git mode), JSON-Schema-only, commits directly (PR flow undocumented), and is slowing. zhi is cluster-detached, policy-aware and PR-first (report §4.6; competitors brief §Cyclops, post-mortems).
- **Kargo** promotes and already opens PRs but has no validation step; custom container steps are Enterprise-only. zhi is what runs *before* Kargo's PR and what its `http` step can call — host/complement, not competitor (report §4.1, §10.1).
- **Nobody** ships the six things in report §8: a portable signed policy snapshot, all engines in one pre-push report with per-finding fidelity, one persona authoring schemas + CEL + requirements another fills per environment, PRs onto the branch the controllers already watch, Compose as a first-class target, and validator extensibility that is OSS by default. The post-mortems say why the previous attempts died — SaaS-locked value (Datree), validation outside the push path (Monokle), cluster-attached UI without write-back (Kubevious, Kubeapps), enterprise UI on a free controller (Weave GitOps) — and this design avoids each by construction (report §4.6; competitors brief Task C).

## 13. "If I had half the time"

Cut in this order, each cut preserving the thesis "PR check that knows your cluster":

1. **Drop the local UI from the MVP; ship `zhi check` + `zhi validate` + `zhi propose` first.** The PR bot delivers most of the value (rendered diff + cluster-aware findings before merge) with none of the form/lineage work; consultants edit YAML in their editor with the emitted `.zhi/schema.json` modeline for completion. The form island and lineage (S9) come back as phase 2.
2. **Compose to phase 2.** Same pipeline, but a second renderer, second snapshot layer and second check set are weeks.
3. **Kyverno and Gatekeeper only via the cluster's generated VAP/VAPB and the Kyverno CLI; no OPA/Rego, no kwctl.** VAP/MAP + CRD CEL + PSA + quota + LimitRange + RBAC + references already cover the evidenced failure classes 3–10, 12 (report §3.1).
4. **One SCM (GitHub), one distribution profile, one GitOps layout family first** (whichever of Argo/Flux the owner's estate runs — report §10.2 Q1), DRY PRs only; hydrated-branch, gitops-promoter and Kargo hooks later.
5. **No signing ceremony beyond cosign verification of snapshots**: DSSE/VSA, SBOMs, FIPS and air-gap wait for a regulated buyer (report §10.2 Q14).
6. **Requirements file reduced to `kubeVersion` + `requiredAPIs` + the three Troubleshoot analyzers**; the extended analyzers become CEL rules over `env` until demand appears.
7. **Post-merge feedback reduced to a link**: record trailers now, correlate later.

What I would not cut even at half time: the snapshot format and importer (the moat), the ordered Tier-0 chain with fidelity tags (the honesty), the canonical finding record with pointers and file:line (the lesson the current codebase paid for), comment-preserving YAML writes, and CEL-not-code as the only rule language.