# zhi 2 — Architecture Proposal (Radical Minimalism)

*Prepared 2026-09-13 against the landscape research synthesis (report §1–§11), the zhi audit brief, and the persona brief. Section numbers cited as "report §n" refer to the synthesis; brief names refer to the slices under `research/briefs/`.*

---

## 1. Name and thesis

**Name: `zhi`** — keep it. The binary, the GHCR namespace and the owner's muscle memory already exist; the rewrite changes what the binary does, not what it is called. Trademark rules forbid "Helm", "Flux" or "Kubernetes" in the name anyway (gap-go-embedding-and-license-audit §3), and "zhi 2" in changelogs is enough to mark the break.

**Thesis, in a DevOps engineer's words.** zhi is one static binary that turns a chart's `values.schema.json` into a form, renders your values with the real Helm/Kustomize/Compose engine, runs the *rendered* manifests through an offline copy of the target cluster's admission chain — OpenAPI and CRD CEL, ValidatingAdmissionPolicy/MutatingAdmissionPolicy, Pod Security, ResourceQuota and LimitRange, RBAC, Kyverno — and opens a pull request into the folder Argo CD or Flux already watches. It needs no cluster credentials at edit time, answers in under two seconds per keystroke, and tells you, per finding, how sure it is. It never applies anything; Git stays the only source of truth. It replaces the ten-minutes-to-hours loop of "edit YAML, push, wait for CI and the controller, read the sync error, revert" (report §3.2: Argo polls every 120 s + 60 s jitter, CI adds 30–60 min, Komodor's median time-to-detect is ~40 min) with a loop that ends before `git push`. Everything that is not that loop is cut from v1.

---

## 2. Personas and the transition story

**Developers author the contract.** Next to the chart (or the Compose file) they commit three files, two of which are existing ecosystem formats: `values.schema.json` (JSON Schema 2020-12, the format Helm 4 already enforces at install with the same library zhi uses, report §4.8), `values.cel.yaml` (cross-value rules in CEL, the helm-cel pattern, report §6.1), and a `Preflight` spec in the Troubleshoot.sh format for environment requirements (report §6.2; persona-vendor-to-customer, "Contract patterns"). Developers know the software: they know `ingress.enabled` needs `service.type != NodePort`, that the chart needs cert-manager's CRD, that `persistence.storageClass` must be RWX-capable. They encode that once. They never need to know what a customer's cluster looks like. `zhi contract check` runs in the app repo's CI and fails if the schema no longer matches `values.yaml` or a CEL rule does not compile.

**Consultants and supporters deploy.** They own the GitOps repo and the cluster. Their loop is: `zhi snapshot import --context customer-prod --out .zhi/snapshots/prod` once (read-only RBAC, minutes), then `zhi ui` in the GitOps repo. The UI shows the developer's form, with cluster-aware pickers filled from the snapshot (StorageClasses, IngressClasses, namespaces, existing Secret names — Rancher's `storageclass`/`secret` field types re-implemented offline, persona brief implication 5). Every edit re-renders and re-runs the admission chain against the snapshot; findings appear inline on the field that caused them, badged with fidelity ("CRD rule: exact", "quota: usage stale by 3 days", "3 webhooks match, logic unknown"). "Open PR" writes `values-prod.yaml` with comments preserved, commits with provenance trailers and opens the PR. They never touched a chart, never read a template, and never waited for the pipeline to tell them the namespace is `restricted`.

**The transition to "developers deploy themselves" costs nothing.** The same three files serve a developer who now also owns a dev environment: they import their own snapshot and pick up the deployer surface. There is no second artifact, no "vendor portal" versus "customer portal" (Replicated's split, persona brief §"Transition"); the split is a *role over the same repo*, enforced by CODEOWNERS on the contract files if the company wants it. This is the report's recommendation verbatim (§6.2): "a developer deploying to their own environment simply picks up the deployer surface."

---

## 3. Domain model

Eight entities. Identity and addressing are fixed up front because the audit brief's most expensive lesson is the flat, lowercase-only path model that could not express `imagePullPolicy` or `containers[0]` (audit §1.1, lesson 1).

| Entity | Identity | Lives in |
|---|---|---|
| **Workspace** | Git remote URL + path of `zhi.yaml` | Git (GitOps repo) |
| **Package** (the developer contract) | `name@version` from `Chart.yaml` / Compose project name, plus git tree hash of the package dir | Git (app repo or vendored chart) |
| **Environment** | name, unique in workspace | Git (`zhi.yaml`) |
| **Snapshot** (the pseudo cluster) | sha256 of canonical tarball; human ref `<cluster-id>@<captured-at>` | directory in repo (`.zhi/snapshots/<env>/`) or OCI artifact; `zhi.yaml` pins the digest like a lockfile |
| **Values layer** | file path + JSON Pointer, ordered per environment | Git |
| **Finding** | hash(engine, rule, resource, pointer, message) | never committed; in reports |
| **Report** | sha256 of canonical JSON body | PR check output / CI artifact |
| **Component** | schema-path group named in `x-zhi-component` | Git (schema) |

**Addressing is JSON Pointer (RFC 6901) everywhere.** Values: `/persistence/storageClass`. Rendered resources: `apps/v1/Deployment/<ns>/<name>` + `/spec/template/spec/containers/0/resources/limits/memory`. This is the native form of the 2020-12 output unit (`instanceLocation`), of CEL `fieldPath`, and a mechanical conversion from apiserver `field.ErrorList` (report §4.8, design implication 11). Every node in the document model carries a source position (yaml.v3 node line/column) so a finding can cite `values-prod.yaml:42:7` (audit lesson 4).

**Values layer and effective value.** An environment binds an ordered list of layers — `values.yaml` (chart default) → `values-common.yaml` → `values-prod.yaml` → Argo `parameters` / Flux `valuesFrom` — with Helm's coalesce semantics (`null` deletes). The UI edits the *top-most editable layer* and shows, per field, which layer the effective value came from (report §3.1 class 16, environments-secrets-drift implication 4).

**Finding** is the canonical record and the only thing every engine must produce: `{severity ∈ Info|Warning|Blocking, tier ∈ schema|apiserver-crd|apiserver-native|policy|quota|live, engine, ruleID, message, resource?, pointer, valuesPointer?, source{file,line,col}?, fixHint?}`. Severity mapping follows report design implication 9 (Deny/Enforce/schema/CEL → Blocking; Warn/Audit/stale quota/webhook match → Warning; deprecation-in → Info). This is the type the audit says the current `ValidationResult` should have been (audit §1.3).

**Report** is `{provenance{zhiVersion, package, environment, snapshotDigest, snapshotCapturedAt, baseCommit}, findings[], rendered{digest}, skipped[]}` — deterministic given identical inputs (giterminism, report implication 30), sorted, timestamps outside the hashed body.

**Component** is only a schema-path group plus `dependsOn`, carried as `x-zhi-component` metadata. Dependency violations are generated as CEL rules at load time; the cascade/cycle logic from `internal/core/component.go` is carried (~150 LOC), re-addressed to schema paths (audit §1.5). No `_components/<name>` pseudo-paths.

**What lives in Git versus elsewhere.** Git holds the contract, the values layers, `zhi.yaml`, and either the snapshot directory or its digest. Reports are never committed: they go to the PR (comment + check) and to CI artifacts, so review history carries the evidence without bloating the repo. Nothing lives in a database; there is no server-side state (the ConfigHub and Devtron failure mode of owning the source of truth, report §8).

```yaml
# zhi.yaml (workspace, deployer-owned)
packages:
  - name: myapp
    path: charts/myapp            # or oci://ghcr.io/acme/myapp:1.4.2
environments:
  - name: prod
    gitops: {tool: argocd, application: apps/prod/myapp.yaml}   # values discovered from the CR
    snapshot: {path: .zhi/snapshots/prod, digest: sha256:9f3e…, maxAgeDays: 14}
    namespaces: [myapp]
```

---

## 4. Architecture

### 4.1 One binary, twelve packages

`cmd/zhi` builds to one static binary, `CGO_ENABLED=0`, because every library in the embed set is pure Go with no `replace` directives (gap-go-embedding-and-license-audit §1, §5): `k8s.io/{apiserver,apiextensions-apiserver,pod-security-admission,client-go} v0.37.0`, `helm.sh/helm/v4 v4.3.0`, `sigs.k8s.io/kustomize/api v0.21.1`, `compose-go/v2 v2.15.0`, `santhosh-tekuri/jsonschema/v6 v6.0.3`, `kubectl-validate` by pseudo-version, `homeport/dyff`. CEL is imported *only* through `k8s.io/apiserver/pkg/cel/environment` — never `cel.dev/cel-go` directly — to avoid two CEL runtimes while k8s.io still pins the google path (report §1, embedding brief §0.2). Three small packages are vendor-forked from `k8s.io/kubernetes@v1.37.0` (LimitRanger validation funcs, RBAC `RulesAllow`, pod/PVC quota evaluators, ~2 k LOC) because they live only in-tree behind 33 replaces.

| Package | Responsibility |
|---|---|
| `internal/doc` | YAML document model with source positions; JSON Pointer get/set; comment- and order-preserving write-back (report implication 13) |
| `internal/schema` | 2020-12 compile/validate; `x-zhi` custom vocabulary (type-checked hints); inference from `values.yaml` + `# @schema` comments when a chart ships no schema |
| `internal/cel` | one `EnvSet` pinned to the snapshot's minor via `MustBaseEnvSet`; variables `values`, `rendered`, `env`, `snapshot` |
| `internal/render` | Helm v4 SDK (Capabilities from snapshot discovery, `lookup` from snapshot inventory), krusty, compose-go |
| `internal/snapshot` | format, loader, importer (client-go discovery + dynamic client, fixed GVR list, graceful degradation) |
| `internal/admit` | Tier-0 chain (§4.3): discovery, kubectl-validate strategy, LimitRanger, PSA, quota, MAP→VAP, RBAC, webhook match |
| `internal/exec` | subprocess runner (audit's `apply.go` handling, verbatim) and the Kyverno CLI adapter |
| `internal/finding` | canonical record, severity mapping, rendered→values back-map, report assembly, SARIF/JSON writers |
| `internal/gitops` | Argo/Flux/Compose layout discovery; layer resolution; effective values |
| `internal/git` | worktree ops via the `git` binary; trailers; GitHub/GitLab PR REST |
| `internal/web` | server-rendered htmx UI + the JSON endpoints its own fragments call |
| `pkg/report` | the public, versioned report types CI consumers parse |

No `pkg/zhiplugin`, no proto, no generated code. The audit measured the current process boundary at ~11 000 LOC of stubs and client/server pairs (audit §0, lesson 6); none of it is paid for here.

### 4.2 Data flow from edit to PR

1. **Load.** `zhi ui` reads `zhi.yaml`, the package contract, the environment's values layers (discovered from the Argo `Application` / Flux `HelmRelease` file or bound explicitly), and the snapshot into memory. Schema and CEL rules compile once.
2. **Edit.** The browser posts one field change (htmx). The server sets the pointer in the top layer (`internal/doc`), keeping everything else byte-identical.
3. **Validate values** (milliseconds): schema over the merged effective values, replicating Helm 4's merged-`.Values`-vs-all-subchart-schemas semantics (report implication 10); then CEL rules with `values` bound.
4. **Render** (hundreds of ms): Helm/Kustomize/Compose with snapshot-fed Capabilities and API versions.
5. **Admit** (tens to hundreds of ms): the ordered chain in §4.3 over the rendered set; CEL rules that reference `rendered` or `snapshot` run here; Preflight analyzers run against the snapshot.
6. **Back-map and respond.** Findings with a `valuesPointer` are swapped in under the field (the mutate-validate-revert pattern the audit singles out, audit §3.2); the rest go to the rendered pane keyed by resource. A Blocking finding on save reverts the field, as today.
7. **Open PR.** Write the layer file, `git commit` with trailers, push `zhi/<env>/<package>-<short>`, create the PR with the report and a dyff-rendered manifest diff (§6).

CI runs the same function without a browser: `zhi validate --env prod --format sarif --exit-code`. The UI and CI cannot disagree because there is one `Validate(workspace, env) → Report` entry point.

### 4.3 The render→validate pipeline, in order

Order matters because the apiserver mutates before it validates and PSA/quota apply to Pods, not Deployments (report §3.1 classes 7–9; cluster-state-import implication 3). Tier 0 emulates the chain in this sequence, each stage tagging its findings:

1. **Discovery** — GVK served on the target minor? Removed/deprecated? (replaces pluto/kubent; Info or Blocking by removed-in).
2. **Workload → Pod expansion** — Deployment/StatefulSet/DaemonSet/Job/CronJob templates become synthetic Pods (gator-expand / Kyverno-autogen style).
3. **Structural schema + defaulting + CRD CEL** — via kubectl-validate's `customresource.NewStrategy` path, which runs `cel.NewValidator` with server cost budgets (embedding brief §0.3). Tag: `apiserver-crd` (near-exact) or `apiserver-native` (best-effort — KEP-5073 explicitly does not publish native rules to OpenAPI, gap-k8s-offline-fidelity-facts §1).
4. **NamespaceLifecycle** — target namespace exists in snapshot (or is created by this PR).
5. **LimitRanger** — mutate defaults, then validate min/max/ratio; show the defaulting as an Info diff.
6. **PodSecurity** — `policy.EvaluatePod` with the namespace's labels; cluster defaults/exemptions come from the snapshot's *declared* overlay because no API exposes them (fidelity brief §8).
7. **ResourceQuota** — `(status.used + delta) ≤ hard` per namespace; Warning when `status.used` is older than the snapshot's freshness threshold (report implication 27).
8. **MutatingAdmissionPolicy** then **ValidatingAdmissionPolicy** — `k8s.io/apiserver` validating/mutating packages with params, `namespaceObject` and an RBAC-backed `authorizer` from the snapshot, informer-free exactly as Gatekeeper's `k8scel` driver does (embedding brief §5). Gatekeeper clusters on ≥ v3.20 are covered through the VAP/VAPB Gatekeeper generates by default (fidelity brief §6); Rego-only templates are reported as "not evaluated" in v1.
9. **Kyverno** — `kyverno apply` subprocess (v1.19.1) with a `Context` file, `--parameter-resource`, `--userinfo` and `--policy-report` generated from the snapshot; exit codes owned by zhi (the v1.12 exit-0-on-FAIL trap, report §3.3). Shelling out is the verdict of the embedding audit: PSA fork replace, 400+ requires, ten 2026 GHSAs.
10. **Webhook match prediction** — which Validating/MutatingWebhookConfigurations match; logic unknown → Warning "N webhooks match, cannot evaluate"; adapters *claim* known services (kyverno-svc, gatekeeper-webhook-service) so they are not double-counted.
11. **RBAC** — can the controller identity (Argo/Flux ServiceAccount from the snapshot's `SelfSubjectRulesReview`) create/update each kind in each namespace?
12. **References** — `secretKeyRef`/`configMapKeyRef`/`serviceAccountName`/StorageClass/IngressClass/PriorityClass exist in the snapshot inventory (names and keys, never values).

`--strict` turns every "unknown" into a Warning; the default badges them. Tier 0 is never presented as "will apply" (report implication 8).

### 4.4 Snapshot format

A directory (tar-able, OCI-pushable) whose layout is deliberately the union of formats other tools already read, so export adapters are trivial (report §10.1, "snapshot on-disk anchor"):

```
manifest.json          provenance: clusterID (kube-system UID), context, serverVersion,
                       capturedAt, capturedBy, zhiVersion, per-layer sha256 + resourceVersion,
                       degraded[] ("resourcequotas: forbidden"), profile?
discovery.json         APIGroupDiscoveryList
openapi/v3/api/v1.json, openapi/v3/apis/<g>/<v>.json     kubectl-validate --local-schemas layout
crds/*.yaml            raw CRDs (x-kubernetes-validations intact)
admission/{vap,vapb,map,mapb,params,webhooks}/*.yaml     1.37 static-manifest form
engines/kyverno/*.yaml, engines/gatekeeper/*.yaml
namespaces/<ns>/{namespace,resourcequota,limitrange}.yaml
catalog/{storageclasses,csidrivers,ingressclasses,gatewayclasses,priorityclasses,runtimeclasses}.yaml
rbac/*.yaml, rbac/subjectrules/<identity>.json
inventory/<ns>/{secrets,configmaps,serviceaccounts,workloads}.json   names/keys only, allowlisted
gitops/*.yaml          Argo Applications, Flux HelmRelease/Kustomization, (Cluster)SecretStores
declared.yaml          operator-supplied: PSA defaults/exemptions, static .static.k8s.io policies,
                       webhook stubs, profile id  — "declared, not observed"
```

Layers hash independently so `zhi snapshot refresh` re-fetches only what changed (`/openapi/v3` is hash-addressed, cluster-state-import §L1). As an OCI artifact it is one config blob (`manifest.json`) and one tar layer, media type `application/vnd.zhi.snapshot.v1+tar`, signed by exec'ing `cosign` rather than embedding Sigstore — the audit found the current 1 236-line signing stack has zero callers (audit §2.6, lesson 7). The importer is read-only client-go over a fixed GVR list; it degrades per layer and records why (report implication 5). Freshness is enforced like a lockfile: Blocking findings cannot be cleared against a snapshot older than `maxAgeDays` (report §5.3).

### 4.5 Fidelity tiers shipped in v1

- **Tier 0 (default, every edit):** everything in §4.3, in-process plus the Kyverno subprocess. Target < 2 s on a Bitnami-sized chart (unbenchmarked — spike S9).
- **Tier 2 (opt-in, CLI only):** `zhi validate --live --context prod` runs SSA `--dry-run=server --validate=strict` through client-go with the controller's field manager and reports live findings *separately* with the delta explained (side-effect webhooks, mutations) (report §5.2). It costs ~300 LOC and doubles as the calibration oracle for Tier 0 in tests.
- **Tier 1 (envtest/KWOK)** is not in v1: it breaks the single-binary rule (200–400 MB per minor), its boot time is unmeasured, and `--admission-control-config-file` under envtest is unverified (report §10.2 Q4; fidelity brief §7).

### 4.6 UI approach

Server-rendered Go `html/template` + htmx, no build step, no SPA — the exact stack the audit says to keep, with its solved middleware chain (CSRF, CSP nonce vs ETag, SSE flusher unwrap) (audit §3.2). The research recommends RJSF/JSON Forms (report §4.6), but a JS form stack means a Node toolchain, a second validator (ajv) whose 2020-12 support is "limited testing", and a second place for widget logic. Radical minimalism says: the form is generated in Go by walking the compiled schema — object → fieldset, `enum` → select, boolean → toggle, number → input with min/max, string `format` → typed input, `x-zhi-widget` overrides, `x-zhi-ref: storageclass|ingressclass|namespace|secret-key` → select filled from the snapshot, `x-zhi-secret` → masked reference input. Browser-side validation is nil; the server is the validator, and the 500 ms debounced inline post returns the field's fragment with its findings. This is unproven at Bitnami scale (schema-validation-tech "Uncertainties": no Go-native renderer exists) and is spike S6.

Findings map to fields via `valuesPointer`. Values→rendered lineage is the hard part (report implication 11 asks for it; no engine provides it). v1 uses a deliberately simple heuristic: a rendered leaf that string-equals a unique effective value is attributed to that value's pointer; otherwise the finding sits on the resource in the rendered pane with its manifest pointer and the chart template file if Helm's error carries it. Accuracy is spike S7; if it is poor, `x-zhi-affects` hints in the schema are the fallback.

The UI runs on loopback, single user, no auth in v1. A shared multi-user server is a phase-3 question, not a v1 one.

### 4.7 Surfaces

- **CLI:** `zhi snapshot import|show|diff|refresh`, `zhi validate`, `zhi render`, `zhi ui`, `zhi pr`, `zhi contract check`. Exit codes: 0 clean, 1 Blocking, 2 usage/error; `--format json|sarif|junit`.
- **CI / pre-commit:** the same `validate`; a GitHub Action / GitLab job template that posts SARIF and a PR comment.
- **LSP and MCP:** not in v1. Both are output surfaces over the same `Validate` function (report implications 28–29; plugin-architectures "Direction (d),(e)"). Findings already carry file:line:col, so `zhi lsp` (diagnostics + hover + code action from `fixHint`) is a phase-3 wrapper, and `zhi mcp` (stdio, `validate/explain/render`) follows once the report types stabilise — the audit's lesson 9 is that the fourth re-marshalling of an unstable surface is the expensive one. The free editor integration ships in v1: `.zhi/schema.json` plus the `# yaml-language-server: $schema=` modeline injected into managed values files.

---

## 5. The developer↔deployer contract

Three consumed files, all existing formats or one-line extensions of them; one emitted set.

**(1) `values.schema.json`** — JSON Schema 2020-12 (Helm 4's default when `$schema` is absent, embedding brief §4.3). zhi's `x-zhi-*` keys are registered as a custom vocabulary so they are type-checked by zhi and ignored as annotations by Helm and every other validator (report §6.1, row "UI hints"):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1, "maximum": 20,
                      "x-zhi-component": "web", "x-zhi-order": 10 },
    "persistence": { "type": "object", "properties": {
        "storageClass": { "type": "string", "x-zhi-ref": "storageclass",
                          "x-zhi-help": "Must support ReadWriteMany" } } },
    "db": { "type": "object", "properties": {
        "passwordRef": { "type": "string", "x-zhi-secret": true,
                         "pattern": "^ref://[a-z0-9-]+/.+#.+$" } } }
  }
}
```

**(2) `values.cel.yaml`** — cross-value and rendered-output rules, evaluated in the same CEL environment the cluster uses; severity is zhi's triad:

```yaml
rules:
  - name: ingress-needs-clusterip
    expr: "!values.ingress.enabled || values.service.type == 'ClusterIP'"
    message: "Ingress requires service.type=ClusterIP"
    severity: blocking
  - name: rwx-class
    expr: >-
      snapshot.storageClasses.exists(sc, sc.name == values.persistence.storageClass
        && 'ReadWriteMany' in sc.csi.accessModes)
    message: "Chosen StorageClass cannot provide RWX on this cluster"
    severity: blocking
  - name: memory-limit-sane
    expr: "rendered.pods().all(p, p.containers.all(c, quantity(c.resources.limits.memory) <= quantity('8Gi')))"
    severity: warning
```

`values` is the merged effective values; `rendered` the manifest set with helpers (`pods()` after expansion); `snapshot` a typed read-only view of the imported cluster; `env` the environment metadata. Rules are stored as plain data so they can later be promoted to a VAP on the cluster (report implication 12).

**(3) Environment requirements** — a Troubleshoot.sh `Preflight` spec, consumed as-is for the analyzers zhi supports offline in v1 (`clusterVersion`, `customResourceDefinition`, `storageClass`, `distribution`), evaluated against the *snapshot* instead of at install time (persona brief implication 2–4). Requirements may reference the deployer's chosen values, which is the whole point:

```yaml
apiVersion: troubleshoot.sh/v1beta2
kind: Preflight
spec:
  analyzers:
    - clusterVersion:
        strict: true
        outcomes:
          - fail: {when: "< 1.30.0", message: "Requires Kubernetes 1.30+ (VAP)"}
          - pass: {message: "ok"}
    - customResourceDefinition:
        customResourceDefinitionName: certificates.cert-manager.io
        outcomes: [{fail: {message: "cert-manager required"}}, {pass: {message: "ok"}}]
    - storageClass:
        storageClassName: "{{ .Values.persistence.storageClass }}"
        outcomes: [{fail: {message: "StorageClass missing"}}, {pass: {message: "ok"}}]
```

Anything the Troubleshoot vocabulary lacks (RWX capability via CSIDriver, quota headroom, PSA level, IngressClass) is a CEL rule over `snapshot` in file (2) — no new analyzer DSL (report §6.1, row "Environment requirements").

**Emitted:** `values.schema.json` written back to charts that had none (inferred from defaults and `# @schema` comments), a `Preflight` Secret template so `kubectl preflight` users get the checks without zhi, and `.zhi/schema.json` + modeline for editors. The contract therefore survives without zhi at runtime (report §6.2).

---

## 6. GitOps integration

**Discovery.** zhi reads the controller CRs *from the repo*, not the cluster, and maps each value to file + pointer + layer (report implication 2): Argo `Application.spec.source(s).helm.{valueFiles,valuesObject,parameters}` and `sourceHydrator.drySource`; ApplicationSet git-files `config.json|yaml`; Flux `HelmRelease.spec.{values,valuesFrom,chart.spec.valuesFiles}` and `Kustomization.spec.postBuild.substitute(From)` (strict missing-variable failure since kustomize-controller 1.9.0 is replicated); Kustomize overlays via krusty; Compose `.env` + `compose.<env>.yaml`. In-cluster inputs (`substituteFrom` ConfigMaps, Argo cluster-Secret labels) come from the snapshot's `gitops/` layer, redacted to keys. When discovery fails, `zhi.yaml` binds layers explicitly. Folder-per-environment on trunk is the supported topology; branch-per-environment is read-only legacy (gitops-controllers implications).

**DRY first, hydrated second.** The PR targets the DRY source folder — it works for Flux, Compose and Kargo OSS alike (report §10.1 verdict). Validating a Source Hydrator `hydrateTo` branch as a PR check (`zhi validate --rendered-dir`) is phase 2, pending the owner's answer to open question 1.

**PR production.** Branch `zhi/<env>/<package>-<short-hash>`; one commit touching only the top layer file, comment- and order-preserving (Image Updater needed until 1.3.0 to get this right — report §4.7); machine-managed markers (`$imagepolicy`, Renovate) are read-only in the UI. Commit trailers carry provenance: `Zhi-Package: myapp@1.4.2`, `Zhi-Snapshot: sha256:… (prod, captured 2026-09-12T10:00Z)`, `Zhi-Report: sha256:…`, `Zhi-Version: 2.0.0` — the `hydrator.metadata` idea expressed as trailers (report implication 1). The PR body has a severity summary, the per-environment matrix if several environments share the package, the dyff-rendered manifest diff in GitHub/GitLab markdown (collapsible per resource, argocd-diff-preview style), and the snapshot age. Providers: GitHub and GitLab REST over `net/http`, tokens from the environment or `gh auth token`. Git operations shell out to `git` using the subprocess handling the audit says to copy line for line (audit §4.3).

**Post-merge.** v1 does nothing automatic. The documented setup is `zhi validate` as a required check on every PR touching the environment folder, so Renovate/Kargo/human PRs are validated uniformly (report §4.7). Phase 2 adds Argo app-status / Flux commit-status subscription ("expected vs actual") and a gitops-promoter `CommitStatus` provider (report implications 24–25). Kargo OSS composes only built-in steps and custom container steps are Enterprise-only (gap-closest-competitors "Kargo — settled"), so the integration is "zhi opens the PR `git-wait-for-pr` waits on" — nothing to build.

---

## 7. Docker Compose

Same model, second renderer, host snapshot instead of cluster snapshot (report §4.5, docker-compose-landscape implications). compose-go v2 is *the* model: load with interpolation off, `template.ExtractVariables` yields the value set (name, default, required) that becomes the form when no `values.schema.json` exists; a developer may ship one with `x-zhi-compose-var` bindings for richer typing. Render = `LoadProject` with the UI's mapping → normalized `types.Project` JSON. Validate = compose-spec 2020-12 schema + the ~28 consistency checks + zhi's **strict interpolation** (variable referenced but unset → Blocking, defaulted → Warning; empty `image`, `ports`, bind sources flagged) — the class that `config -q` cannot see. CEL rules run over `rendered` = the normalized project, so the default policy pack (no privileged, no host PID/net/IPC, binds under allowed roots, no `latest`, healthcheck required — Portainer BE's security dimensions as data) is the same language as the Kubernetes side. No Rego.

**Host snapshot** (`zhi snapshot import --docker <context>`, one read-only Engine API pass): Engine/API and Compose versions, OS/arch, cgroup version, rootless/userns, seccomp/AppArmor defaults, address pools, NCPU/MemTotal/GPU, published host ports, existing networks/volumes/containers/projects, images with digests, registry reachability, swarm flag, and — only when the importer runs on the host itself — bind-source existence under declared roots. Host-aware checks: port collision against snapshot and sibling services, missing bind source, cross-project `container_name` collision, `cpus`/`mem_limit` vs capacity, `deploy.*` with `swarm: false` → Warning, unknown `x-` keys → Warning. Podman/Swarm are flavour flags on the snapshot that switch attribute-support matrices; nothing is built for them.

**Write-back** never rewrites `compose.yaml`: values land in `.env` and `compose.<env>.yaml`, so plain `docker compose up`, Portainer Git stacks, Komodo ResourceSync and CI-over-SSH all keep working. The PR flow is identical.

---

## 8. Plugin depth — how deep must it be?

**Answer: no loadable plugins in v1.** Data + CEL + exec adapters cover every failure class the research evidenced, and every brief independently concluded "shallow" (report §7.3, plugin-architectures implication 2). The current four gRPC types were the Terraform/Vault shape — right for a platform, over-built for a validator whose users edit YAML.

| Concern | v1 mechanism | Why (evidence) |
|---|---|---|
| Source loaders (YAML/JSON/env, Helm values, Kustomize, Compose, Argo/Flux CR discovery) | Fixed Go | Need source positions and write-back; Renovate regrets losing positions to a config-only loader model (plugin-architectures §"Pain-point") |
| Renderers | Fixed Go: Helm v4 SDK, krusty, compose-go | "Valid in zhi" must equal "valid for the controller" (report §1); Timoni/KCL/Pkl/ytt are exec adapters *later* speaking KRM `ResourceList`, not v1 |
| Value types, defaults, enums, UI hints | Data: JSON Schema 2020-12 + `x-zhi` vocabulary | Every ecosystem format ingests into it (report §6.1); Helm 4 enforces it for free |
| Cross-value and environment rules | CEL (VAP variable set + k8s library + `values`/`rendered`/`snapshot`) | The language the cluster already speaks; Kyverno, Kubewarden, Gatekeeper all re-platformed on CEL with *no* user libraries (report §4.3, §7.1) |
| Environment requirements | Data: Troubleshoot `Preflight` subset + CEL | The only clean vendor→customer split found (report §6.2); the "custom analyzers" Replicated upsells are CEL here |
| Cluster policies | Data imported untranslated (VAP/MAP/CRD CEL/PSA/quota) evaluated by fixed engines; Kyverno via exec adapter with generated side files | Engines need in-process performance and a shared type environment (report §7.2); Kyverno's module is not embeddable (embedding brief §0.4) |
| Distribution profiles (OpenShift, GKE Autopilot, AKS, EKS Auto, Rancher) | Data: CEL rule pack + `declared.yaml` overlay, versioned | The opaque half of managed platforms must be encoded from docs anyway (gap-deployer-environments "Cross-cutting finding"); a rule pack is a file, not a binary |
| Snapshot importers | Fixed Go: Kubernetes (dynamic client), Docker Engine API | Read-only, credentialed, heterogeneous — but only two in v1; Rancher/OpenShift/cloud APIs are phase-2 Go code, not plugins |
| Secrets | Data: typed `ref://store/path#key` validated against snapshot inventory; no resolution | Values files must never hold secrets (report implication 14); resolving them is the one credentialed integration and is deferred |
| Targets | Fixed Go: Git branch + GitHub/GitLab PR | Git is the only write target (report implication 1) |
| UI, LSP, MCP | Fixed surfaces over one `Validate` function | Nothing in 2026 rewards a pluggable UI (Grafana/Backstage sandbox debt, plugin-architectures §"Direction") |

**Not pluggable, ever:** the UI, the store (Git), transforms in the render path (Flux forbids them, Argo CMPs are issue #15006, Kustomize plugins never left alpha — report §7.1), the report format, the snapshot format.

**Why data + CEL + exec suffice.** Walk report §3.1: classes 1–13 and 15–17 each map to a snapshot layer plus a fixed engine or a CEL rule; class 14 (opaque webhooks) is unpluggable by nature — a plugin could not evaluate it either. The persona brief's evidence for "extensibility that mattered" was custom analyzers, `runPod`-style checks and field renderers (persona implication 10): analyzers are CEL over `snapshot`; field renderers are `x-zhi-widget`; `runPod` is a live-cluster check that belongs in Tier 2, not a plugin.

**The two doors, left open at zero cost.**
- `check/v1`: the Kyverno adapter *is* the first check. It is written against an internal `Check` interface (`rendered set + snapshot dir → findings JSON`) and runs as a subprocess with a digest-pinned binary. Externalising it means reading a `checks:` list in `zhi.yaml` with `{name, binary, sha256}` and the same JSON contract — no new protocol, no marketplace. Wasm via wazero can back the same interface if demand appears (report §7.3 item 2; Helm 4's slow Wasm uptake says do not depend on it now).
- `resolver/v1`: `SecretRef` is a type and `Resolver` an interface with one implementation (snapshot inventory). If Vault/KMS resolution is ever needed, go-plugin gRPC behind that interface is the ecosystem-validated shape (Vault chose isolation, not Wasm — report §7.1). Nothing in v1 imports go-plugin.

**Compared with the current four types:** `config` → data (schema) + fixed loaders; `transform` → dropped (a three-way ordering knob nobody exercised, audit §2.7); `store` (27 methods) → Git; `ui` (25 methods × 8 implementations) → one server-rendered UI. The marketplace, ratings, advisories and mirror server are dropped; the OCI client and digest lockfile survive as the snapshot/profile transport (audit §2.7 "keep").

---

## 9. What to carry from the current zhi, and what to drop

**Carry (audit §7.1 "Keep"):** the Info/Warning/Blocking triad; cross-value validation as a concept, re-done as one whole-document pass (audit §1.3 O(N²) trap); `TreeReader`'s read-only seam as the `snapshot`/`values` views handed to CEL; the component dependency/cycle/cascade logic, re-addressed (audit §1.5); `apply.go`'s subprocess handling verbatim for `git`, `kyverno` and `cosign` (audit §4.3); the web UI stack and middleware chain, the mutate-validate-revert inline pattern, the severity-grouped findings page with source links, the SSE log pane for `zhi validate --live` output (audit §3.2); the OCI client with custom media types and the digest lockfile (audit §2.6) as snapshot transport; `launch/audit.go`'s binary-integrity hygiene for the Kyverno CLI and profile packs; `-race -count=1`, fmt-diff CI, golden fixtures, and a new policy conformance suite (`rule → manifest → expected findings`) shippable to users (audit §6).

**Drop (audit §7.1 "Drop"):** the flat slash path model and its `[a-z]` regex; `Val any` and the 330 LOC of coercion (a schema-derived type makes it vanish, audit lesson 2); `Metadata["path"]`; `Value.Validators`; Yaegi-interpreted Go in config files — full-stdlib RCE from a pull request (audit §1.4, lesson 3); all four gRPC plugin types, protos and generated stubs; the JSON-over-protobuf envelope; the 27-method store and its plaintext fallback; the 25-method `ui.Controller` and its three frontends; the TUI (4 536 LOC, forced in-process by `RequiresTTY`); the marketplace server (a JSON file named `sqlite.go`, audit lesson 8); the unreachable Sigstore stack (exec cosign instead); `text/template` + Sprig as the Kubernetes bridge (string interpolation with no schema awareness *is* the problem, audit §4.2); `fileACL`/`fileMode` side-effecting template functions; the meta-plugin SDK. **Defer:** the air-gap mirror's OCI layout and export/import code — solid, and the regulated-buyer path (report implication 26) will want it, but only when such a customer exists.

---

## 10. MVP and phases

**MVP (v2.0): what a consultant can do end to end.**
1. `zhi snapshot import --context customer-prod` → signed directory committed to `.zhi/snapshots/prod/`.
2. `zhi ui` in the GitOps repo → environment `prod` discovered from the Argo `Application` file.
3. Fill the developer's form (schema, `x-zhi` widgets, StorageClass/namespace pickers from the snapshot); every edit shows findings from discovery, OpenAPI/CRD CEL, LimitRanger, PSA, quota, VAP/MAP, Kyverno, RBAC, reference checks, Preflight analyzers and CEL rules, each badged.
4. See the rendered manifests and the dyff diff against the previous render.
5. "Open PR" → GitHub or GitLab PR with report, diff and trailers.
6. CI runs `zhi validate --env prod --format sarif --exit-code` as a required check.
Scope: Helm and Kustomize renderers; one environment at a time; secrets as references validated against inventory; no profiles beyond the declared overlay; no Tier 1; no LSP/MCP.

**Phase 2 (v2.1–2.2):** Docker Compose target + host snapshot; per-environment matrix in one PR; hydrated-branch validation; Argo/Flux status subscription and gitops-promoter CommitStatus; distribution profiles as data (owner picks the first from Q7 — OpenShift's `restricted-v2`/UID-range warning is the documented #1 vendor-chart failure, gap-deployer-environments §OpenShift); `ExternalSecret` generation from refs; Gatekeeper Rego via embedded OPA if a customer runs pre-3.20 Gatekeeper; snapshot refresh as a scheduled CI job pushing OCI; `check/v1` externalised if a second check appears.

**Phase 3 (v2.3+):** LSP and MCP wrappers; Tier 1 envtest/KWOK backend after benchmarking; managed-distro importers (Rancher PSACT, OpenShift project templates, AKS/GKE Gatekeeper objects); DSSE/SLSA VSA signed reports, SBOMs and a `GOFIPS140` variant when a regulated buyer asks; multi-user shared UI.

**Size.** MVP ≈ 24–26 k LOC non-test Go (doc/schema/CEL 3 k, render 2 k, snapshot 3 k, admit 3 k + 2 k vendored, findings/report 1.5 k, gitops+git/PR 2.5 k, web 4 k, CLI 1.5 k, Compose scaffolding 1 k) plus templates/CSS and a comparable test corpus — roughly 60 % of today's 42 k non-test LOC while doing the actual job. Timeline for a 1–2 person team: MVP 5–6 months (the first six weeks are spikes S1–S5, §11), phase 2 +3–4 months, phase 3 +3 months.

---

## 11. Risks, unknowns, and the lab spikes required

**Named spikes (before architecture freeze; report §10.2 Q16 plus this design's own bets):**
- **S1 kubectl-validate CEL** — a failing `x-kubernetes-validations` rule produces a finding through `pkg/validator` (asserted from source, never run).
- **S2 Kyverno CLI side files** — `--context-file` + `--parameter-resource` with a MAP and a paramRef VAP generated from a real snapshot; exit-code and report-format capture.
- **S3 Helm 4.3 + 2020-12 schema** end to end, including merged-`.Values`-vs-subchart semantics and snapshot-fed `--kube-version`/`--api-versions`; whether `lookup` can be backed by a fake client from the SDK.
- **S4 VAP/MAP outside the apiserver** — `validating`/`mutating` packages with the `k8scel` construction, RBAC-backed authorizer stub, `namespaceObject`, cost budgets; is `request.userInfo` reproducible.
- **S5 vendor-fork viability** — LimitRanger, `RulesAllow`, `evaluator/core` compile against staging types without dragging `pkg/apis/core` (flagged UNVERIFIED in the embedding brief §5).
- **S6 Go-native schema→form** on Bitnami redis and two internal charts: render time, htmx fragment size, usability without client-side JS.
- **S7 back-map accuracy** — the string-equality heuristic on five real charts; decide whether `x-zhi-affects` is required.
- **S8 snapshot import** on a real customer cluster: size (OpenAPI v3 with 200+ CRDs), duration, RBAC needed, redaction correctness.
- **S9 Tier-0 latency** per edit on a large chart; the < 2 s promise is unmeasured.
- **S10 Gatekeeper-generated VAP coverage** on an AKS Azure Policy cluster (are the `k8sazure*` templates CEL or Rego?).

**Risks with mitigations.** *Native-type fidelity stays approximate* (KEP-5073 non-goal) — mitigated by tier badges and Tier 2 live check. *Unobservable inputs* (PSA exemptions, `.static.k8s.io` policies, opaque webhooks) — `declared.yaml` plus `--strict`; the owner must answer Q2 (permissive-with-badge vs Warning as CI default). *Version skew* — pin k8s.io v0.37.0, emulate older minors downward via `MustBaseEnvSet`, warn when the snapshot minor exceeds the pin (embedding brief §2). *cel-go module path churn* — import only through k8s.io. *Kyverno cadence* (ClusterPolicy removal in 1.20) — subprocess isolation makes it a data problem. *Adoption* — Monokle and Datree died outside the push path or inside a SaaS (gap-closest-competitors "post-mortems"); the CI check is the product, the UI is the on-ramp. *No failure-class frequency data* — instrument "Blocking findings caught pre-push, would-have-failed-at stage" from day one (report implication 31) and run the owner's internal survey (Q17). *Owner decisions still open:* DRY vs hydrated primacy (Q1), first distribution profile (Q7), secrets ownership (Q8), whether the UI must be multi-user before phase 3.

---

## 12. Differentiation

Report §8 lists what nobody does; this design does exactly those six things and nothing else.

- **ConfigHub** validates on edit (`vet-schemas`, `vet-celexpr`) but replaced Git as the source of truth and delivers OCI-pull only since August 2026; no admission or quota import. zhi is Git-native and stateless; it could even consume ConfigHub's OCI output as a rendered input. *Beats* on source-of-truth model.
- **Devtron v2.2.0** has the best schema-GUI↔YAML editing but is a whole platform that owns the manifest repo, has no policy engine, and tags locked keys and approvals "Enterprise". zhi copies the per-scope schema idea, opens PRs into *your* repo, and runs the cluster's own policies. *Beats* on BYO repo and policy fidelity; *does not compete* on CI/CD.
- **Flux Schema** is the CRD/CEL layer of a pseudo cluster — same apiserver code, offline, pinned to a minor — with no values model, no VAP/MAP/Kyverno/PSA/quota, no UI, no Git write. zhi *complements*: consume `flux schema extract crd` output as a snapshot layer, emit a kubeconform/flux-schema-compatible schema dir; never vendor the AGPL catalog.
- **Replicated KOTS + Preflight** is the reference for the developer/deployer split, but proprietary ($2–3 k/month base), install-time in the customer cluster, with a flat form DSL. zhi copies the split, evaluates the same `Preflight` format offline against a snapshot, and emits the Preflight Secret so Replicated users lose nothing. *Complements* and undercuts.
- **Cyclops** is the closest OSS "schema→form→Git" but needs an in-cluster controller and CRD, validates schema only, commits directly (PR flow undocumented), and has slowed (v0.21.1, 2026-06-26). zhi is cluster-detached and policy-aware. *Beats*.
- **Kargo** promotes and opens PRs but has no validation step; custom steps are Enterprise-only. zhi produces the PR Kargo's `git-wait-for-pr` waits on and can run as the required check before `git-merge-pr`. *Complements* in OSS without building anything.

The durable moat, per the post-mortems, is validation *in the push path* against *the target's actual constraints*, delivered as a *stateless* tool that *writes back to Git* — the four properties Datree, Monokle, Kubevious/Kubeapps and Weave GitOps each lacked one of (report §4.6).

---

## 13. "If I had half the time"

Cut in this order, each cut leaving the loop intact:

1. **GitLab** — GitHub only; the PR client is the last thing written anyway.
2. **Kustomize renderer** — Helm only for MVP; krusty is small to add later.
3. **Kyverno adapter** — ship VAP/MAP + CRD CEL + PSA + quota + LimitRange; Kyverno clusters get a "not evaluated" badge until phase 2.
4. **RBAC and webhook match prediction** — both Warnings-only layers.
5. **Snapshot inventory and `x-zhi-ref` pickers** — plain inputs; reference checks become CEL rules the developer writes.
6. **Schema inference from `# @schema`** — require a `values.schema.json`; the developer persona can write one.
7. **dyff PR diff** — plain unified diff of rendered YAML.
8. **Preflight analyzer support** — everything as CEL over `snapshot`.
9. **`zhi validate --live` (Tier 2)** — the calibration oracle moves to the test suite only.
10. **Compose scaffolding** — already phase 2; remove the package stub.

What survives is still the thesis: schema form → Helm render → discovery/OpenAPI/CRD CEL/PSA/quota/VAP against a snapshot → GitHub PR → CI check. Roughly 15 k LOC, three months for two people. What must never be cut: the snapshot format with provenance and freshness, the canonical finding record with pointer and tier, comment-preserving write-back, and the single `Validate` function shared by UI and CI — every later feature hangs off those four.