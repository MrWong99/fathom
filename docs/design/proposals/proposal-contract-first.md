I have everything I need from the research; the remaining briefs are covered by the report's synthesis. Writing the proposal now.

# zhi v2 — Contract-First Architecture Proposal

*Principal architect draft, 2026-09-13. Citations: `report §n` = landscape synthesis; `audit §n` = zhi audit brief; `persona` = persona-vendor-to-customer brief; `gap-embed`, `gap-fidelity`, `gap-deployer`, `gap-competitors` = gap briefs.*

---

## 1. Name and one-paragraph thesis

**zhi** (name retained; the product is new). In the words a DevOps engineer would use: *zhi is the pre-push check the GitOps controllers do not have.* Your developers ship a chart or a Compose project plus a **contract** — a `values.schema.json`, a handful of CEL rules, and a preflight-style list of what the target must provide. You import a **snapshot** of the cluster you are deploying into (CRDs, VAP/MAP, Kyverno, Gatekeeper, PSA labels, quotas, LimitRanges, RBAC, storage/ingress classes — read-only, once, signed). zhi generates the values form from the contract, renders with the real Helm/Kustomize/Compose engines, runs the apiserver's admission chain offline in the server's order against that snapshot, tells you *which field* will fail *at which stage* with *what fidelity*, and then opens the PR into the folder Argo CD or Flux already watch — with the snapshot digest in the commit trailers and a machine-readable report on the check. It never applies to a cluster. Everything zhi emits (`values.schema.json`, `Chart.yaml kubeVersion`, a Troubleshoot `Preflight`) keeps working when zhi is not installed (report §1, §6.2, §8 "what nobody does").

---

## 2. Personas and the transition story

**Developers author the contract, in the app repo, next to the chart.** `zhi contract init` synthesizes a draft `values.schema.json` from `values.yaml` defaults plus existing `# @schema`/`## @param` annotations (report §6.1 priority list), which the developer enriches with `x-zhi-*` hints, CEL cross-value rules in `values.cel.yaml` (helm-cel pattern, report §4.2), and a `requirements.yaml` of environment needs using Troubleshoot's analyzer vocabulary plus zhi's extensions (persona §2, report §6.1). The contract is packaged *inside* the chart (Helm packages all non-ignored files) or as a digest-pinned OCI artifact next to it, and is signed with the chart. `zhi contract test` runs a conformance table (`values → expected findings`) in the developer's CI (audit §6). Developers never need cluster access; they declare *what is configurable, how it is checked, and what the environment must offer.*

**Consultants and supporters deploy from the GitOps repo.** `zhi snapshot import --context customer-prod` captures the target's constraint posture with their existing read-only credentials (report §5.1; §9 #5). `zhi open --env customer-prod` discovers where the values live from the Argo/Flux resources in the repo (report §9 #2), generates the form from the contract, populates cluster-aware pickers (StorageClass, IngressClass, namespaces, existing Secret keys) from the snapshot — works air-gapped (persona implication 5) — and validates every keystroke through the full Tier-0 pipeline. Findings show fidelity badges ("CRD CEL exact", "quota approximate: status.used is stale", "3 webhooks match, logic unknown") (report §5.3). `zhi propose` writes the values back with comments preserved and opens the PR. The deployer never reads a Helm template; the developer never reads a cluster policy. The contract is the border crossing.

**Transition to DevOps.** The same two artifacts serve a team where developers deploy themselves: the developer opens the deployer surface against their own environment's snapshot; nothing changes in the chart, the contract, or the repo layout (report §6.2; persona implication 9). Role separation is enforced by Git (who can write the app repo vs the GitOps repo) and by the contract's `x-zhi-persona` hint, which only affects form grouping, never enforcement. Platform teams get a third role for free: they own snapshot refresh (a scheduled job pushing a signed OCI bundle, report §5.3 "freshness like a lockfile") and distribution profiles, without owning anybody's values.

---

## 3. Domain model

Addressing rule: **JSON Pointer (RFC 6901) everywhere**, never the current slash-path (audit §1.1 shows it cannot express list indices or camelCase keys). The canonical finding record is the JSON Schema 2020-12 output unit (`instanceLocation`, `keywordLocation`) into which apiserver `field.ErrorList`, CEL `fieldPath`, PolicyReport and Rego paths are normalized (report §4.8, §9 #11).

| Entity | Identity | Lives in | Notes |
|---|---|---|---|
| **Workspace** | Git remote + path of `.zhi/` | GitOps repo | `.zhi/workspace.yaml`: environments, targets, packages. Most fields are *discovered* from Argo/Flux CRs and only overridden here. |
| **Package** | `type` (helm/kustomize/compose) + source (`oci://…@sha256`, git path@commit, local path) | App repo (source) or OCI | The deployable unit. Renderer fixed per type (report §9 #3). |
| **Contract** | Digest of `{values.schema.json, values.cel.yaml, requirements.yaml, contract.yaml}` | With the package | Developer-authored; signed with the chart (report §9 #16). Components live here as `x-zhi-component` groups. |
| **Environment** | `name` unique per workspace | `.zhi/workspace.yaml` | Promotion order, 1:N targets, namespaces, GitOps tool + path, snapshot lock entry, secret-store bindings, profile id, matrix dimensions (report §9 #15). |
| **Target** | Kubernetes: `kube-system` namespace UID; Docker host: engine ID | Workspace (id only) | Kube-context is a local hint, never committed. |
| **Snapshot** | `sha256` of the OCI manifest | OCI registry (or `.zhi/snapshots/` directory in air-gap) | Layers per §5.1 of the report; provenance manifest; `declared.yaml` overlay for unobservables (PSA exemptions, static `.static.k8s.io` policies, webhook stubs) (gap-fidelity §2, §8). Pinned per environment in `.zhi/snapshots.lock` with `capturedAt` and TTL. |
| **Values layer** | `(file, pointerPrefix, rank, owner)` | GitOps repo | One entry per file in the merge stack: `common → variant → env`, Flux `substituteFrom` ConfigMaps (from snapshot), Argo `parameters/valuesObject`. `owner ∈ {human, machine:renovate, machine:image-updater, controller}`; machine-owned pointers are read-only in the UI (report §9 #13). |
| **Effective values** | Derived | Memory | Fold of layers per environment with Helm last-wins/`null`-deletes semantics (report §2.1); each leaf carries its source layer for the "why is this value X" explorer (report §3.1 class 16). |
| **Resource** | `group/version/Kind/namespace/name` (K8s) or `project/service` (Compose) | Memory (rendered) | Output of render; carries a source map to template file:line and, where recoverable, the values pointer that produced each leaf. |
| **Finding** | Stable hash of `(ruleId, resourceId, instanceLocation)` | Report | `severity ∈ {Info, Warning, Blocking}`, `tier ∈ {t0a, t0b, t1, t2}`, `engine`, `fidelity` flags, `wouldFailAt ∈ {schema, admission, scheduling, runtime}`, `valuesPointer`, `file:line:col`, `proposedValue` (audit lesson 4; report §9 #9, #31). |
| **Report** | Digest of canonical JSON | PR check + OCI referrer; never committed | Findings + provenance `(snapshot, contract, package, values, profile, zhi version)`; deterministic (report §9 #30, gap-deployer checklist). |
| **Component** | Name within a contract | Contract | Dependency graph with cycle detection and cascade semantics carried from `internal/core/component.go`, re-addressed to schema pointers and mapped to native switches (`enabled`, Kustomize Component, Compose profile) (audit §1.5; report §6.1). |
| **Proposal** | Branch + commit SHA + PR URL | Git host | Commit trailers carry provenance (§6 below). |
| **Secret reference** | `ref://<store>/<path>#<key>[@version]` | Values files | Never a value. Rendered to `ExternalSecret` v1 by default; `SealedSecret`/SOPS optional; Blocking if the store is absent from the snapshot's `(Cluster)SecretStore` inventory (report §9 #14). |

**What lives in Git vs elsewhere.** In Git: contract (app repo), workspace file, values files, snapshot *lock* (digests only), suppressions/baselines. In OCI: snapshots, distribution profiles, policy/schema packs, CLI tool bundles (Kyverno CLI, kwctl, cosign), reports as referrers. Nowhere persistent: kubeconfigs, store tokens, UI session state, decrypted secrets. Rationale: giterminism — a report must be reproducible from committed inputs plus a digest-pinned snapshot (report §9 #30).

---

## 4. Architecture

### 4.1 Binaries and packages

One binary, `cmd/zhi`, `CGO_ENABLED=0`, no marketplace or mirror server in v1 (audit §2.7). Go packages:

| Package | Responsibility | Key embedded deps (gap-embed §1, §5) |
|---|---|---|
| `pkg/contract` | Load/emit schema (2020-12 + `x-zhi` vocabulary), compile CEL rules, parse requirements, synthesize schema from defaults/annotations | `santhosh-tekuri/jsonschema/v6`; CEL only via `k8s.io/apiserver/pkg/cel/environment` |
| `pkg/values` | Comment/order-preserving YAML nodes, JSON Pointer ops, layer fold, write-back | `gopkg.in/yaml.v3` Node API |
| `pkg/discover` | Argo/Flux/Kargo/Compose layout → values layers; hydrated-branch detection | `k8s.io/apimachinery` unstructured decoding of CRs |
| `pkg/render` | Helm, Kustomize, Compose renderers producing a `ResourceSet` + source map | `helm.sh/helm/v4` (HIP-0004), `sigs.k8s.io/kustomize/api/krusty`, `compose-go/v2` |
| `pkg/snapshot` | Format, K8s importer (discovery + dynamic client, fixed GVR list, degrade per layer), Docker importer, declared overlay, OCI push/pull, lockfile, TTL | `client-go`, `oras-go/v2` (carried from `pkg/sharing/client`) |
| `pkg/admit` | Ordered admission-chain emulation (Tier 0-b) | `k8s.io/apiserver` policy/validating+mutating, `apiextensions-apiserver` schema/cel, `pod-security-admission/policy`, kubectl-validate `pkg/validator` (pseudo-version), vendor-forked LimitRanger/RBAC/quota evaluators (~2k LOC), Gatekeeper `k8scel` driver + OPA `v1/rego` |
| `pkg/engines` | Subprocess adapters: Kyverno CLI (generated `Context`/`Values`/`UserInfo`/`--parameter-resource` files), kwctl, flux-schema, `kubectl --dry-run=server` | `internal/exec` (carried from `apply.go`) |
| `pkg/require` | Environment requirements: Troubleshoot analyzers on the snapshot-as-bundle + zhi extensions | `replicatedhq/troubleshoot` analyzers only |
| `pkg/profile` | Versioned distribution profiles (OpenShift SCC via `sccmatching`, GKE Autopilot, AKS Safeguards, EKS Auto Mode, Rancher PSACT) as data + a few Go checks | `openshift/apiserver-library-go/.../sccmatching` |
| `pkg/finding` | Canonical record, severity/tier mapping, back-mapping, SARIF/JSON/PolicyReport/KRM `results[]` output | — |
| `pkg/report` | Assemble, canonicalize, digest, DSSE (phase 3) | cosign v3 via exec |
| `pkg/gitops` | Branch, commit trailers, PR/MR (GitHub, GitLab), check-run, post-merge status subscription | go-git + host REST |
| `pkg/session` | The one in-process controller the UI, CLI, LSP, MCP share | — |
| `pkg/ui` | Server-rendered htmx app; schema→form renderer; findings, rendered diff (`dyff`), matrix | carried middleware chain (audit §3.2) |
| `pkg/lsp`, `pkg/mcp` | Thin projections of `pkg/session` | `go-sdk/mcp` |
| `pkg/plugin/resolver` | `resolver/v1` interface, first-party implementations compiled in; external go-plugin loading phase 2 | `hashicorp/go-plugin` |

### 4.2 Data flow from edit to PR

1. `zhi open --env prod`: load workspace → discover layers → resolve package + contract (verify signature) → resolve snapshot from lockfile (refuse Blocking-clear on stale TTL) → start local UI on loopback.
2. Every field edit posts to the session, which applies the change to an in-memory overlay on the YAML node tree, runs the pipeline (debounced, cancellable, <2 s target, report §1), and returns fragments. The change is reverted if it introduces a Blocking finding on save — the mutate-validate-revert pattern carried from the current web UI (audit §3.2).
3. Save writes only touched leaves back through the yaml.v3 node tree, preserving comments, ordering, anchors, and machine-managed markers.
4. `zhi propose --env prod` re-runs the pipeline from committed inputs only, creates branch `zhi/<env>/<slug>`, commits with trailers, pushes, opens the PR with the findings table, and attaches SARIF to a check-run.

### 4.3 The render → validate pipeline and its ordering

The order mirrors the apiserver so that mutation precedes validation and typed objects exist before policy engines see them (report §5.3; §9 #6):

```
0  fold values layers per environment           → effective values + source layer
1  values contract: JSON Schema (Helm 4 semantics: merged .Values vs all subchart
   schemas) → CEL rules over {values, env}         [Blocking short-circuits nothing; render continues]
2  render: Helm (Capabilities from snapshot discovery; lookup backed by snapshot
   inventory) | Kustomize (krusty) | Compose (strict interpolation)
3  admission emulation, per resource, in server order:
   expand workloads → Pods
   strict fields + structural schema + defaulting + CRD CEL (kubectl-validate strategy)
   NamespaceLifecycle (namespace exists in snapshot or in the rendered set)
   LimitRanger (mutate, then validate)
   PodSecurity (namespace labels; cluster defaults from declared overlay/profile)
   ResourceQuota  (used + delta ≤ hard; Warning when status.used is stale)
   MAP → Kyverno mutations → "what the cluster will store" diff
   VAP → Kyverno → Gatekeeper (generated VAP first, Rego via OPA) → Kubewarden (kwctl)
   webhook match prediction (rules/selectors/matchConditions; logic unknown)
   RBAC RulesAllow for the controller identity (SelfSubjectRulesReview in snapshot)
4  referential + requirements: Secret/ConfigMap keys, classes + CSIDriver capability,
   image allowlists/IDMS, Troubleshoot analyzers with chosen values substituted
5  distribution profile checks
6  normalize → back-map → severity → tier → report
```

Stage 1 runs before render so the deployer gets contract feedback even when the chart cannot render; stage 4 runs after admission because requirements reference both chosen values and rendered output (persona §2 "value-dependent requirements").

### 4.4 Snapshot format

A directory that is also an OCI artifact (one layer per section, config blob = provenance), signed as a cosign v3 bundle referrer, importable from a tarball for air-gap (report §9 #4; gap-deployer checklist):

```
manifest.json        provenance: clusterId, serverVersion, capturedAt, identity,
                     per-layer digest + resourceVersion, engine versions, profileId, ttl
discovery/           APIGroupDiscoveryList (feeds Helm Capabilities, removed-API check)
openapi/v3/          kubectl-validate layout: api/<v>.json, apis/<g>/<v>.json (hash-keyed)
crds/                raw CRDs (x-kubernetes-validations, defaults, ratcheting intact)
admission/           VAP/VAPB, MAP/MAPB (v1; v1beta1 fallback on 1.34/1.35), params,
                     Validating/MutatingWebhookConfiguration metadata
engines/             kyverno/, gatekeeper/ (templates, constraints, generated VAPs, Config/SyncSet),
                     kubewarden/
namespaces/          labels/annotations, ResourceQuota (spec+status), LimitRange, NetworkPolicy
catalogs/            StorageClass+CSIDriver, IngressClass, GatewayClass, PriorityClass,
                     RuntimeClass, SCC, ComputeClass, NodePool/NodeClass
rbac/                Roles/Bindings + SelfSubjectRulesReview per deploying identity
inventory/           Secret/ConfigMap names+keys (never values), ServiceAccounts,
                     Service/Ingress/Route hosts, workloads (oldObject, immutable fields)
gitops/              Argo apps/appsets, Flux HR/Ks, (Cluster)SecretStore, Sealed Secrets cert
nodes/               optional: allocatable, labels, taints
declared.yaml        operator-supplied: PSA defaults/exemptions, static policies dir,
                     webhook stubs, Rancher PSACT, profile id
```

The report's contradiction table settles the anchor: own format, kubectl-validate layout for schemas, raw CRDs, 1.37 static-manifest form for VAP/MAP; export adapters to kubeconform/kwokctl/flux-schema layouts (report §10.1 "snapshot on-disk anchor"; §9 #22).

### 4.5 Fidelity tiers shipped in v1

- **T0-a** (schema) and **T0-b** (engines in-process/CLI) ship in v1 as the default on every save (report §5.2, §5.3).
- **T2** (live `kubectl --dry-run=server` / Argo `server-side-diff` / `flux diff kustomization`) ships in v1 as an *optional* gate when a kubeconfig exists — it is cheap and gives deployers the ceiling to calibrate T0 against; live findings are shown separately with the explained delta (report §5.3; §9 #24).
- **T1** (envtest/KWOK) is deferred until the boot-time spike (§11) — not promised in v1 (report §10.2 Q4).

Every finding carries its tier; the UI never says "will apply" (report §9 #8). `--strict` promotes unobservables (PSA exemptions, `authorizer()` assumed allow, webhook matches) from badge to Warning; CI default is badge + Warning, per report §10.2 Q2 pending the owner's decision.

### 4.6 UI approach

Stack: Go `html/template` + htmx, embedded static assets, no build step — carried from the current web UI together with its middleware chain (CSRF, CSP nonce, ETag/gzip ordering, SSE flusher unwrap) (audit §3.2, §7.1). Forms are **generated server-side from the JSON Schema**: each property becomes a field whose DOM id is its JSON Pointer; `x-zhi-widget`, `x-zhi-order`, `x-zhi-group`, `x-zhi-cluster-ref` select widgets and pickers; `if/then`, `dependentRequired` and `x-zhi-component` drive show/hide. Nested objects render recursively; arrays of objects render as repeatable fieldsets; anything the renderer cannot express (deep `oneOf`, free-form `additionalProperties`) falls back to an embedded YAML editor bound to that subtree with the same pointer id — replacing the current fragile map/list row editors (audit §3.6). Findings map to fields by pointer: a finding on rendered `apps/v1/Deployment/x#/spec/template/spec/containers/0/resources/limits/memory` is back-mapped to `#/resources/limits/memory` when the source map resolves, otherwise shown on the rendered pane with template file:line. The session API returns findings in the RJSF `extraErrors`/ErrorSchema shape so a React front end could be swapped in later without touching the pipeline (report §4.6; §10.2 Q12 answered: Go-native first, RJSF as an exit).

Panes: form ↔ YAML toggle (Devtron's best idea, gap-competitors), rendered manifests with the mutated-object diff ("what the cluster will store"), findings grouped by severity with file:line links (audit §3.6), effective-value explorer per environment, environment matrix (phase 2).

### 4.7 CLI, CI, LSP, MCP surfaces

- CLI: `zhi contract {init,lint,emit,test}`, `zhi snapshot {import,refresh,verify,export,diff}`, `zhi env {list,add}`, `zhi open`, `zhi validate --env prod --exit-code --format json|sarif|policyreport`, `zhi explain <finding-id>`, `zhi propose`, `zhi status --pr` (post-merge correlation).
- CI: same binary; exit code owned by zhi regardless of engine exit codes (the Kyverno CLI exit-0-on-FAIL lesson, report §3.3); GitHub Action and GitLab template wrappers.
- LSP: `zhi lsp` serves diagnostics from the same pipeline for values files, hover from schema descriptions, code actions applying `proposedValue`; `zhi contract emit` also writes `.zhi/schema.json` and injects the `# yaml-language-server: $schema=` modeline for editors without zhi (report §9 #28).
- MCP: `zhi mcp` (stdio + streamable HTTP, loopback, read-only default) exposing `validate/explain/diff/import_snapshot`; an outbound surface generated from `pkg/session`, not a boundary (report §9 #29; audit §3.6).

---

## 5. The developer ↔ deployer contract

Two artifacts, kept separate as KOTS keeps `Config` and `Preflight` separate, never merged into templates (report §6.2; persona implication 1).

**Consumed:** `values.schema.json` (drafts 4–2020-12; 2020-12 when `$schema` is absent, matching Helm 4.3 and `jsonschema/v6`, gap-embed §4.3), `# @schema` / `## @param` / helm-docs comments, ytt `@schema`, Timoni `#Config` (via CUE import, phase 3), kro SimpleSchema, XRD/CRD `openAPIV3Schema`, Compose `config --variables`, Rancher `questions.yaml` and OLM `x-descriptors` hints on import (report §6.1). **Emitted:** `values.schema.json` with `x-zhi-*` left in place (unknown keywords are annotations to every other validator), `Chart.yaml kubeVersion`, `templates/zhi-preflight.yaml` containing the Troubleshoot-native subset of requirements as a `troubleshoot.sh/kind: preflight` Secret, and a kubeconform/kubectl-validate schema directory derived from the snapshot (report §9 #22).

**Values contract** — `values.schema.json` excerpt with the zhi vocabulary registered as a custom vocabulary in `jsonschema/v6` so hints are type-checked:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "properties": {
    "persistence": {
      "type": "object", "x-zhi-group": "Storage", "x-zhi-order": 20,
      "properties": {
        "storageClass": { "type": "string", "x-zhi-cluster-ref": "storageclass",
                          "x-zhi-persona": "deployer" },
        "accessMode":   { "type": "string", "enum": ["ReadWriteOnce", "ReadWriteMany"] }
      }
    },
    "db": { "properties": {
      "passwordRef": { "type": "string", "format": "zhi-secret-ref",
                       "x-zhi-secret": { "render": "externalsecret" } } } },
    "image": { "properties": {
      "tag": { "type": "string", "x-zhi-machine-managed": "renovate" } } }
  }
}
```

**Cross-value rules** — `values.cel.yaml`, helm-cel compatible, VAP variable conventions, evaluated in the k8s CEL environment pinned to the snapshot's minor (report §6.1 "cross-value rules"; §9 #12):

```yaml
rules:
- name: rwx-needs-capable-class
  severity: blocking
  rule: >
    values.persistence.accessMode != 'ReadWriteMany' ||
    env.storageClasses.exists(sc, sc.name == values.persistence.storageClass
      && 'ReadWriteMany' in sc.accessModes)
  messageExpression: "'StorageClass ' + values.persistence.storageClass + ' cannot do RWX on this cluster'"
  fieldPath: /persistence/storageClass
- name: replicas-fit-quota
  severity: warning
  rule: quantity(values.resources.requests.cpu).asApproximateFloat() * values.replicaCount
        <= env.namespace.quota.cpuRequestsAvailable
```

Rules are stored so they can later be promoted to a cluster `ValidatingAdmissionPolicy` unchanged (report §9 #12).

**Environment requirements** — `requirements.yaml`: Troubleshoot analyzers verbatim plus a `zhi` block for what Troubleshoot lacks (RWX capability via CSIDriver, quota headroom, PSA level, CRD *version*, IngressClass) (persona implication 2; report §6.1 "environment requirements"):

```yaml
kubeVersion: ">= 1.30.0-0"
requiredAPIs: [ "cert-manager.io/v1/Certificate", "gateway.networking.k8s.io/v1/HTTPRoute" ]
analyzers:                       # troubleshoot.sh/v1beta2, emitted as-is into the Preflight Secret
- storageClass: { storageClassName: "{{ .Values.persistence.storageClass }}",
                  outcomes: [ { fail: { message: "StorageClass missing" } }, { pass: { message: ok } } ] }
- distribution: { outcomes: [ { fail: { when: "== docker-desktop" } } ] }
zhi:                             # evaluated only by zhi; omitted from the emitted Secret
- podSecurityLevel: { maxEnforce: baseline, severity: blocking }
- ingressClass: { name: "{{ .Values.ingress.className }}" }
- egress: { hosts: [ "ghcr.io:443" ], severity: warning }
```

Severity mapping is fixed: schema/CEL/Deny/Enforce/preflight-fail → Blocking; VAP Warn, Kyverno Audit, Gatekeeper warn/dryrun, stale quota, native best-effort → Warning; deprecations → Info (report §9 #9).

---

## 6. GitOps integration

**Discovery.** `pkg/discover` reads the repo, not the cluster: Argo `Application.spec.source(s).helm.{valueFiles,valuesObject,parameters}`, ApplicationSet git-files generators, `sourceHydrator.drySource`; Flux `HelmRelease.spec.values/valuesFrom`, `Kustomization.postBuild.substitute(From)` and patches; Kargo `Stage`s for promotion order; Compose `-f` lists, `.env`, `compose.<env>.yaml` (report §2.1; §9 #2). Each discovered key becomes a values layer with rank and owner. Inputs that live only in the cluster (`substituteFrom` ConfigMaps, Argo cluster-Secret labels) come from the snapshot's `gitops/` layer, keyed but redacted, so the fold reproduces the controller's render (report §2.1 "Git alone cannot reproduce"). Branch-per-environment is not modelled; folder-per-environment on trunk is (report §2.1).

**Write target.** DRY folder first (works for Flux, Compose, Kargo OSS); hydrated `hydrateTo` branches are validated as a PR check read path, never written (report §10.1 "primary Git flow"; owner Q1 pending). zhi never pushes to a branch a controller syncs without review unless the owner enables it per environment (report §10.2 Q13 — default off).

**PR production.** Branch `zhi/<env>/<slug>`; one commit per proposal touching only human-owned layers; trailers:

```
Zhi-Snapshot: sha256:…   Zhi-Contract: sha256:…   Zhi-Package: oci://…@sha256:…
Zhi-Profile: openshift-4.20   Zhi-Tier: t0b   Zhi-Report: sha256:…
```

PR body: findings table with severity, tier and fidelity badges, the mutated-object diff summary, the effective-value delta per environment. Check-run: SARIF (GitHub) or PolicyReport/JSON (GitLab), exit code owned by zhi. This is the `hydrator.metadata`-style provenance the report asks for (report §9 #1) and what Kargo's `git-wait-for-pr`/`git-merge-pr` and gitops-promoter's CommitStatus gates consume (report §4.1; §9 #25).

**Post-merge.** `zhi status --pr <n>` subscribes to Argo application status/Notifications or Flux commit-status providers, optionally fires the controller webhook/Receiver to skip the 120 s + 60 s jitter poll (report §3.2; §9 #24), and correlates a sync failure back to the report: if the failing stage matches an unobservable badge ("3 webhooks matched"), it records a "would-have-caught" miss; if it matches a Blocking finding that was suppressed, it records an override. That count per PR is the adoption metric (report §9 #31). Drift between hydrated SHA, dry SHA and live state is displayed as a read-only third state, never reconciled by zhi (report §4.7).

---

## 7. Docker Compose

The model is unchanged; only the renderer and the snapshot differ (report §1; §4.5; §9 #17).

- **Package** = Compose project; **values** = the interpolation variable set (`docker compose config --variables`) plus `compose.<env>.yaml` override leaves, both addressed by JSON Pointer; the contract's schema describes variables (`type`, `enum`, `x-zhi-secret`) and the override subtree.
- **Render** = `compose-go/v2` load with strict interpolation (unset variable → Blocking, not `""`), `include`/`extends`/profiles resolved, `x-*` preserved; profiles map to components.
- **Host snapshot** contains: Engine/API version, OS/arch, cgroup version, rootless/userns, seccomp/AppArmor defaults, address pools, CPU/memory/GPU, listening host ports, existing networks/volumes/containers/projects, images with digests, registry reachability, bind-source existence, Swarm/Podman flags. Importers: Docker API over socket/SSH, Portainer API, Komodo Periphery over HTTP — never Komodo's GPL code (report §5.1 Compose analogue; gap-embed §3).
- **Checks**: port collisions against listening ports and other projects, missing bind sources, `deploy.*` without Swarm, resource limits vs host capacity, `container_name` collisions, cross-project network references, image allowlist/reachability, plus CEL rules over the normalized compose-go JSON (`rendered` = project model) and optional Rego via embedded OPA for teams with conftest policies (report §3.1 class 17; §7.2).
- **Write-back**: `.env` and `compose.<env>.yaml` so plain `docker compose up` still works; PR into the repo Portainer Git stacks or Komodo ResourceSync consume (report §4.5).
- **Secrets**: SOPS is the one model identical for Compose and Kubernetes (`sops exec-env`), so `ref://` resolution for Compose targets SOPS/age and provider CLIs (report §4.7; owner Q8).

Podman/Quadlet is a flavour flag on the Kubernetes path (unsupported kinds = Blocking), not a third target (gap-deployer Part 3).

---

## 8. Plugin depth

The owner's question — how deep must the plugin system be? — has a short answer: **shallow, and mostly data.** Every one of the 17 briefs converged on this (report §1; §7.3). The tools that thrive extend through files, CEL, digest-pinned CLI binaries or (slowly) Wasm; every gRPC/sidecar plugin in a deploy path is a documented pain point (Argo CMP #15006, Kustomize plugins alpha for five years) (report §7.1).

| Concern | Mechanism | Why |
|---|---|---|
| Loaders (YAML/JSON/TOML/env, Helm values, Kustomize, Compose, Argo/Flux discovery) | **Fixed Go core**, file-pattern config | Needs source positions and comment preservation; Renovate regrets losing them (report §7.2) |
| Renderers | **Fixed**: Helm v4, krusty, compose-go in-process; **exec adapters** (KRM `ResourceList`) for Timoni/KCL/Pkl/ytt/`flux build` | CGO/JVM/Node dependencies excluded from core; `ResourceList` is the one graduated KRM contract (report §4.2) |
| Values rules, cross-value rules | **CEL** with fixed host library (`values`, `rendered`, `env`) | Same language the cluster enforces; Kyverno moved to CEL with no user libraries (report §7.1, §9 #12) |
| Cluster policies | **Data**: imported VAP/MAP/Kyverno/Gatekeeper manifests evaluated untranslated | Import, do not re-implement; policies stay promotable to the cluster |
| Third-party engines | **CLI exec**, version-pinned OCI bundles, exit codes owned by zhi | Kyverno's module has a fork `replace` and 10 GHSAs in 2026 (gap-embed §0.4) |
| Distribution profiles | **Data** (versioned OCI artifacts) + small Go checks | Opaque vendor admission must be encoded from docs (gap-deployer cross-cutting) |
| Environment requirements | **Data**: Troubleshoot analyzers + zhi extensions | Vocabulary already exists and survives without zhi (persona) |
| Secret stores, vendor cloud collectors (Binary Authorization, Rancher PSACT, Azure Policy assignments) | **`resolver/v1`**: go-plugin gRPC, first-party implementations compiled in; external loading phase 2 | The one place every brief keeps a process boundary: credentialed, long-lived, heterogeneous; Vault itself chose isolation (report §7.3) |
| Pure validators CEL cannot express | **`check/v1`**: exec with `ResourceList` in / output units out; Wasm via wazero only if demand appears (phase 2–3) | Kubewarden proves the shape; Helm 4's Wasm uptake and Extism's 18-month tag gap say do not depend on it yet (report §7.1; gap-embed §1) |
| Targets/publishers (Git hosts, OCI, PR comments, CommitStatus) | **Fixed core**, templates for PR body | No evidence of demand for pluggable targets |
| UI panels, forms, LSP, MCP | **Not pluggable**; schema vocabulary drives widgets | Backstage/Grafana/VS Code show in-process UI plugins maximize breakage (report §7.1); audit lesson 6 |
| Distribution | Signed OCI + digest lockfile + curated catalog file | Retain from current zhi; no marketplace until third-party plugins exist (report §9 #20) |

**Not pluggable, by decision:** the finding record, the admission order, the renderer engines for the three package types, the CEL host library, storage (Git), the UI, and any hook that mutates values or manifests between edit and commit (the current `transform` type — exactly what Flux forbids and Argo suffers from, report §7.3).

**Versus the current four gRPC types:** `config` becomes fixed loaders plus discovery (its four-method shape survives as an in-process interface, audit §2.7); `transform` is dropped; `store` is dropped because Git supersedes all 27 methods (audit §5.1); `ui` is dropped because surfaces are projections of one session, not extension points (audit §3.6). Net effect: from four gRPC types and ~11,000 LOC of transport (audit §0) to zero required boundaries in v1 and at most two optional ones later, versioned as `zhi.plugin.v1` with capability flags (report §7.3).

---

## 9. What to carry from the current zhi and what to drop

Following the audit brief's §7.1 table.

**Carry, near-verbatim:** the Info/Warning/Blocking triad (audit §1.6); `ComponentManager` dependency graph, cycle detection, mandatory pre-enabling and cascade disable, re-addressed to schema pointers (audit §1.5); `apply.go` subprocess handling — process groups, `WaitDelay`, pipe-drain ordering, 1 MiB scanner — reused for every exec engine, cosign, git and `kubectl --dry-run=server` (audit §4.3); pre-check gating as the T2 hook (audit §4.4); the mutate-validate-revert inline UX and the severity-grouped findings page, now with file:line links (audit §3.2, §3.6); the server-rendered htmx stack and its middleware chain, including the solved ETag/CSP/gzip/SSE interactions (audit §3.2); the SSE log pane for T2 and post-merge status; `launch/audit.go` binary integrity hygiene, applied to downloaded CLI tool bundles and policy packs (audit §2.4); the OCI client with custom media types and the digest-pinning lockfile, re-targeted to snapshots, profiles and tools (audit §2.6); `-race -count=1`, fmt-diff CI, codegen-freshness checks, table-driven tests, `startTestServer(t)` (audit §6).

**Carry conceptually:** `TreeReader` becomes the read-only snapshot/effective-values reader that CEL and analyzers see (audit §1.6); drift/diff becomes desired-vs-hydrated-vs-live display, with `dyff` replacing the hand-rolled LCS diff for Kubernetes-aware output (audit §4.4; report §4.8); the `labels` registry becomes the `x-zhi` schema vocabulary (audit §2.7); the Vault client and `store.writeonly` idea become `ref://` secret references (audit §5.1); the OIDC callback server returns for secret-store and cluster login (audit §5).

**Drop:** flat slash paths and the `[a-z]` segment regex (audit §1.1); `Val any` and 330 LOC of coercion (audit §1.2); `Metadata["path"]` and non-serializable `Value.Validators` (audit §1.3); Yaegi-interpreted Go as a policy language — "remote code execution by design" in a tool that will evaluate PRs from forks (audit §1.4); the four gRPC plugin types, 10,790 generated LOC and JSON-over-protobuf (audit §2.1–2.2); `transform` and its `ValidatePolicy` tri-state; the 27-method store, the plaintext fallback store; `ui.Controller` × three frontends plus a hand-written MCP; the TUI and `RequiresTTY` (audit §2.3, §3.3); the marketplace server, ratings, advisories and publisher tiers (audit §2.6); the unwired Sigstore stack — shell out to cosign v3 and wire the chokepoint first (audit lesson 7); `text/template` + Sprig as the bridge to Kubernetes and Compose, with `fileACL`/`fileMode` (audit §4.1–4.2); the meta-plugin SDK (audit §2.4). **Defer:** the air-gap mirror's OCI layout and bundle format — good code, needed only when a regulated customer appears (audit §2.6; gap-deployer Part 2).

---

## 10. MVP, phase 2, phase 3

**MVP — "a consultant can do it end to end on a Helm-based Argo/Flux repo."**
Developer side: `zhi contract init|lint|emit|test` (schema synthesis from defaults and `# @schema`, CEL rules, requirements; emits `values.schema.json`, `kubeVersion`, Preflight Secret). Deployer side: `zhi snapshot import` (layers 1–11 of report §5.1, MAP v1/v1beta1 fallback, declared overlay, OCI push/pull, lockfile); Argo/Flux discovery for Helm values layers; Helm v4 in-process render with snapshot Capabilities and `lookup`; Kustomize render for validation only (no write-back); T0-a + T0-b core — JSON Schema, CEL rules, kubectl-validate structural + defaulting + CRD CEL, LimitRanger, PSA, quota arithmetic, native VAP/MAP, Kyverno via CLI with generated side files, webhook match prediction, RBAC; referential checks (classes, CSIDriver, Secret/ConfigMap keys, images vs allowlists); Troubleshoot subset + `kubeVersion`/`requiredAPIs`; findings with back-mapping where the source map resolves; local web UI with generated form, YAML fallback, rendered/mutated diff, findings; comment-preserving write-back; `zhi propose` to GitHub and GitLab with trailers and SARIF check; `zhi validate --exit-code` for CI; optional T2 via `kubectl --dry-run=server`. Secrets as `ref://` rendered to `ExternalSecret`. One environment at a time.
Size: ~28k non-test Go LOC (the current tree is 42k with 21% distribution infrastructure and 11k gRPC plumbing that disappear, audit §0), tests near 1:1. **5–6 months for two people, 8–9 for one.**

**Phase 2 (+3–4 months, ~+15k LOC):** Compose as a first-class target with host snapshot importers (Docker API, Portainer, Komodo Periphery); Gatekeeper Rego via OPA + `k8scel`, Kubewarden via kwctl; the first two distribution profiles the consultants actually hit (owner Q7 — the research suggests OpenShift first, gap-deployer Part 1); environment matrix evaluation; Kustomize patch write-back; `resolver/v1` external loading with signed OCI + lockfile; LSP; post-merge status correlation and the "would-have-caught" metric; decidability report per policy (report §9 #23); `zhi snapshot diff`.

**Phase 3 (+3–4 months, ~+15k LOC):** T1 envtest/KWOK backend if the spike passes; `check/v1` exec/Wasm; the regulated pack — DSSE + SLSA VSA reports, CycloneDX/SPDX SBOMs, `GOFIPS140` variant, offline cosign trusted roots, air-gap export/import reusing the mirror format (report §9 #26); MCP; Kargo `http` step and gitops-promoter CommitStatus provider; CUE import/export; Podman-kube profile; remaining profiles.

Total: roughly 55–60k non-test LOC over 12–14 months for a two-person team, ending slightly above today's size with none of today's transport overhead.

---

## 11. Risks, unknowns, and the lab spikes required before committing

**Named spikes (two to five days each, before architecture freeze):**

1. **S-CRDCEL** — kubectl-validate `pkg/validator` at the 2026-01 pseudo-version on a CRD with a *failing* `x-kubernetes-validations` rule; confirm budget errors and ratcheting with `oldObject` (report §10.2 #16; gap-embed §0.3).
2. **S-VAPMAP** — `validating.NewValidator` + `cel.NewCompositedCompiler` with snapshot `params`, `namespaceObject`, RBAC-backed `authorizer`; apply a MAP mutation offline; measure per-object latency (gap-embed §5).
3. **S-KYVERNO** — `kyverno apply --parameter-resource --context-file --userinfo --policy-report` fed entirely from generated snapshot files, including a MAP with paramRef; assert zhi-owned exit codes (gap-fidelity §9).
4. **S-FORK** — vendor-fork LimitRanger, RBAC `RulesAllow`, quota `evaluator/core` from `k8s.io/kubernetes@v1.37.0`; confirm ~2k LOC and that `pkg/apis/core` type conversion is bounded (gap-embed §1, flagged UNVERIFIED).
5. **S-HELM4** — Helm 4.3 SDK in-process render with snapshot-driven Capabilities and a snapshot-backed `lookup`; 2020-12 `values.schema.json` end-to-end including subchart schema merging (report §10.2 #16).
6. **S-YAML** — yaml.v3 node round-trip over the company's real values files: comments, anchors, Renovate/Image Updater markers, `null` deletions.
7. **S-SOURCEMAP** — *my design, not in the research:* recover values-pointer → rendered-pointer mappings by rendering with sentinel substitutions per candidate leaf and diffing; measure cost and coverage. Fallback if it fails: findings anchor on the rendered document with template file:line, and on values only for direct literal matches.
8. **S-IMPORT** — snapshot import against two real customer-shaped clusters: required RBAC, wall time, size, redaction correctness, MAP API version fallback.
9. **S-FORM** — Go-native schema→form renderer against Bitnami's redis schema and a contract with `oneOf`/arrays; decide the YAML-fallback boundary (report §10.2 Q12).
10. **S-T1** — envtest with `--admission-control-config-file` and KWOK with VAP + quota; boot-time benchmarks (report §10.2 #16; gap-fidelity §7) — gates phase 3 only.
11. **S-GATOR** — whether Gatekeeper's `k8scel` driver exposes `request.userInfo` offline (gap-fidelity §6).

**Risks and how the design absorbs them:**

- *Offline ≠ online* (Kyverno #5476 "passes with kyverno apply, fails in cluster", persona pain points). Absorbed by per-finding tier and fidelity badges, the T2 optional gate, and post-merge correlation; the product never claims "will apply" (report §9 #8).
- *Native-type validation stays approximate* (KEP-5073 rules not published to OpenAPI; gap-fidelity §1). Absorbed by labelling; only CRDs claim near-exact.
- *Unobservables*: PSA exemptions, static `.static.k8s.io` policies, opaque webhooks, GKE Warden (gap-fidelity §2, §8; gap-deployer). Absorbed by the declared overlay, profiles and `--strict`.
- *Staleness*: `status.used`, drifting policies. Absorbed by TTL in the lockfile, refusal to clear Blocking on stale snapshots, cheap hash-addressed refresh (report §5.3).
- *Version skew*: one k8s.io minor per binary. Accepted: pin latest, emulate downward via `MustBaseEnvSet`, warn when the snapshot minor exceeds the pin (gap-embed §2).
- *Supply chain*: Kyverno CVE cadence isolated by subprocess; `govulncheck` in CI; AGPL catalog fetched only at user option; no Kubernetes/Helm/Flux trademarks in the name (gap-embed §3; report §9 #21).
- *Secrets in snapshots*: inventory stores names and keys only; redaction is a tested layer, not a flag.
- *Adoption*: no chart migration, contract synthesized from what exists; the tool sits in the push path rather than being a separate editor (Monokle's failure, gap-competitors Task C).
- *Scope creep into a platform*: Git remains the source of truth (ConfigHub's opposite bet); no in-cluster component (Cyclops, Kubeapps, Kubevious).
- *Bus factor*: a two-person team owning renderers, engines, UI and Git integration. Mitigated by the fixed-core decision — most extension is data.

---

## 12. Differentiation

- **ConfigHub** — validates on edit (`vet-schemas`, `vet-celexpr`) but replaces Git as the source of truth and delivers OCI-pull only since August 2026; no admission, quota or snapshot import (report §4.1; gap-competitors). zhi is Git-native and snapshot-driven; ConfigHub can at most be a publisher target.
- **Devtron** — the strongest OSS incumbent for schema-per-scope GUI ↔ YAML with locked keys and approvals, but no policy engine, no snapshot, PR only into a Devtron-owned repo, and the useful parts carry Enterprise tags (report §4.6; gap-competitors). zhi copies the form/YAML toggle and the developer-defines/deployer-fills split, writes into the customer's own repo, and keeps validators OSS.
- **Flux Schema + Ecosystem Schema Catalog** — real CRD structural + CEL parity with apiserver code, offline, but no values model, no VAP/MAP/Kyverno/Gatekeeper/PSA/quota, no UI, no Git write, and an AGPL catalog (report §4.1; gap-competitors "four questions"). zhi is a superset on the validation half and treats flux-schema as a compatible exporter target; it generates its own catalog from the customer's CRDs, which is the import feature anyway.
- **Replicated KOTS + Preflight** — the only clean contract split, and zhi copies it; but it is a proprietary SaaS control plane at $2–3k/month, checks run at install time inside the customer cluster, and the Config DSL is flat and string-typed (persona §Summary). zhi evaluates the same Preflight vocabulary offline against an imported snapshot, uses JSON Schema + CEL instead of a form DSL, and emits a KOTS-compatible Preflight so Replicated users still benefit.
- **Cyclops** — Helm schema forms in an in-cluster UI with direct commits; schema-only validation, controller-coupled, slowing (report §8). zhi is stateless, cluster-detached, PR-based and policy-aware.
- **Kargo** — the promotion layer that already edits values files and opens PRs, with no validation step and Enterprise-only custom steps (report §4.1; gap-competitors "settled"). zhi complements it: the PR zhi opens is what `git-wait-for-pr` waits on, and phase 3 offers an `http` step target.
- **The whitespace itself** — after 10+ targeted searches, no product imports VAP/MAP + Kyverno + Gatekeeper + PSA + quotas + CRDs + classes + RBAC into a portable signed snapshot, renders with the real engines, runs every engine in one pre-push report with per-finding fidelity, splits authoring and filling across personas, writes a PR into the watched folder, and does the same for Compose (report §8 "what nobody does"). Prior attempts died for identifiable reasons zhi avoids: SaaS-locked value (Datree), validation outside the push path (Monokle), cluster-attached UI without write-back (Kubevious, Kubeapps), enterprise UI on a free controller (Weave GitOps), fighting Helm (Glasskube) (gap-competitors Task C).

---

## 13. "If I had half the time"

Cut in this order, each cut preserving the thesis "contract in, validated PR out":

1. **Compose** — phase 2 anyway; the Kubernetes path alone proves the model.
2. **The generated form** — ship the local UI as YAML editor + rendered diff + findings panel with pointer-anchored errors; the LSP-style diagnostics carry the deployer until S-FORM lands. This is the largest single UI cost and the one most easily replaced later (RJSF exit already designed in).
3. **Gatekeeper Rego and Kubewarden** — consume Gatekeeper's *generated* VAPs only; Rego and kwctl wait.
4. **Distribution profiles** — ship only what the snapshot observes (PSA labels, quotas, SCC objects as data); no opaque-webhook emulation.
5. **PR creation** — emit the branch, the commit with trailers and the SARIF file; let the human open the PR. Keep `--exit-code`.
6. **Kustomize** entirely; **T2** live gate; **LSP/MCP**; **OCI snapshot distribution** (commit a redacted snapshot directory into `.zhi/snapshots/` and accept the repo bloat until the registry path is built).

What survives the cut is the irreducible product: contract (schema + CEL + requirements), snapshot import, Helm render, the ordered Tier-0 chain (schema, CRD CEL, LimitRange, PSA, quota, VAP/MAP, Kyverno CLI, referential checks), findings with severity and fidelity, comment-preserving write-back, and a CI exit code. That alone removes the 10-minutes-to-hours wait the owner described (report §3.2), and everything cut is additive on top of it.