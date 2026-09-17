# zhi — Architecture Proposal for the Rewrite (Engine-First)

## 1. Name and one-paragraph thesis

The name **zhi** stays; the codebase does not. In the words of the engineer who will run it: zhi is `helm template | kubeconform | kyverno apply` grown up into one static binary that already knows your cluster. You import a read-only snapshot of a target cluster's admission posture once — CRDs with their CEL rules, ValidatingAdmissionPolicies and MutatingAdmissionPolicies, Kyverno and Gatekeeper policies, Pod Security labels, ResourceQuotas and LimitRanges, storage/ingress classes, the RBAC of the GitOps controller — and from then on every values edit, whether typed in an editor, filled into a form, or pushed to CI, is rendered with the real Helm, Kustomize or Compose engine and run through the same admission chain the API server would run, offline, in the order the API server runs it, in about a second. Every finding names the file and line, the rule, the fidelity tier it came from, and the stage where the cluster would have failed (admission, scheduling, runtime). The form, the LSP server and the MCP server are thin skins over that engine; the pre-commit hook and the CI job execute identical code and emit a byte-identical report, so green on your laptop is green in the pipeline. zhi writes only to Git — a PR into the folder Argo CD, Flux or Kargo already watch — and never to a cluster. This is report §1 (bullets 1–3) and §8 ("what nobody does", items 1–5) restated as a product.

## 2. Personas and the transition story

**Developers author the contract.** They own the chart or Compose file and add two artifacts beside it, both plain files in the app repo: a *values contract* (`values.schema.json`, JSON Schema 2020-12, with `x-zhi-*` UI hints and a `values.cel.yaml` of cross-value rules) and an *environment contract* (`zhi-requirements.yaml`: minimum Kubernetes version, required APIs, Troubleshoot-compatible analyzers). This is the KOTS Config + Preflight split that the persona brief identifies as the only clean developer/deployer separation in the field, with its weaknesses removed: JSON Schema instead of a flat form DSL, CEL instead of `when` templates, evaluation against an imported snapshot instead of at install time inside the customer cluster (persona brief, summary and "Contract patterns"; report §6.2). Developers get value from day one without any deployer: `zhi contract check` lints the schema against the chart's `values.yaml`, Helm 4 enforces the same schema in every pipeline (report §4.2), and `zhi validate --env dev` runs the whole chain against their own dev-cluster snapshot.

**Consultants and supporters deploy.** They hold the kubeconfig, so they own the snapshot: `zhi snapshot import --context customer-prod --namespaces shop` produces a signed, redacted bundle (report §5.1) that they commit as a digest reference into the GitOps repo's environment file. They open `zhi ui`, get a form generated from the developer's schema, and fill environment-specific values: the StorageClass dropdown lists the classes in *that* customer's snapshot, the IngressClass picker likewise, the secret field accepts only a reference to a `(Cluster)SecretStore` that exists there (report §6.1 "cluster-aware pickers"; persona brief implication 5). Every keystroke re-runs the engine; blocking findings cite the imported policy that would reject the manifest. They press "open PR"; the PR body carries the rendered diff, the findings summary and the provenance trailers; CI re-runs `zhi validate --exit-code` and reproduces the same report digest. They never learn the chart's internals and they never wait for the pipeline to discover a PSA denial (report §3.1 rows 6–10, §3.2).

**The invisible border is a CODEOWNERS line.** Developers own `charts/**` and `values.schema.json`; deployers own `.zhi/environments/**`, `.zhi/snapshots/**` and `envs/<env>/values.yaml`. When the company moves to a DevOps culture, the border moves to a single team: the developer who now deploys her own service adds an environment file and imports her own snapshot, then picks up the deployer surface unchanged — same schema, same rules, same UI, same CI job. No artifact changes hands and no artifact changes shape (report §6.2 last sentence; persona brief implication 9). For regulated customers the two halves are signed separately, so an auditor can see who declared the rules and who chose the values (gap-deployer brief, procurability checklist).

## 3. Domain model

Addressing is settled first, because the current flat slash-path model is the audit's "fatal for the rewrite" finding (audit brief §1.1): it cannot express list indices, camelCase keys, or annotation keys containing `/`. The rewrite uses **RFC 6901 JSON Pointers everywhere** — for values (`/persistence/storageClass`), for rendered documents (`/spec/template/spec/containers/0/resources/limits/memory`), and for findings (`instanceLocation`). `~1` escaping handles `app.kubernetes.io/name`. Helm `--set` dotted notation is a derived *display* form only. This is also the report's canonical error record, the 2020-12 output unit (report §4.8, §9.11), so no translation layer sits between validators and the UI.

| Entity | Identity | Lives in | Notes |
|---|---|---|---|
| **Workspace** | Git remote URL + path of `zhi.yaml` | Git (GitOps repo) | Declares packages, environments, lockfile; one per repo |
| **Package** (the developer contract) | OCI digest or `git+path@sha` | Git (app repo) or OCI | Chart/Kustomize base/Compose project + `values.schema.json` + `values.cel.yaml` + `zhi-requirements.yaml` + component definitions |
| **Environment** | `name` within workspace | Git: `.zhi/environments/<name>.yaml` | Promotion order, targets (1:N), namespaces, GitOps binding (tool, CR, file, merge layer), snapshot digest, secret-store bindings, matrix dimensions (report §9.15; environments brief implication 11) |
| **Target** | cluster identity hash or host id; kind `kubernetes` or `compose-host` | Git (reference) | Distribution profile id (`openshift-4.20`), kube-context name is local-only |
| **Snapshot** | `sha256:` of its manifest | OCI artifact, cached under `~/.zhi/snapshots`; optionally vendored under `.zhi/snapshots/` for air-gap | Layers in §5.1 of the report; capture time, capturing identity, per-layer hashes, TTL, `declared.yaml` overlay for unobservable facts |
| **Values layer** | `{file, layer, pointer}` | Git | One file contributing to effective values; layers `base < variant < env < cluster < runtime(substituteFrom) < machine-managed` (environments brief implication 4) |
| **Effective values** | derived | memory | Merged per environment, each leaf carrying lineage `{layer, file, pointer, line, col}`; `null` deletes; strict `${VAR:=default}` |
| **Rendered set** | derived | memory (optionally hydrated branch) | Documents with identity `file + apiVersion/kind/namespace/name` (K8s) or `file + service` (Compose), each node with source position (audit brief §7.3) |
| **Finding** | `{engine, ruleID, instanceLocation, documentID}` | Report | Severity Info/Warning/Blocking; tier T0-a/T0-b/T2; `keywordLocation`; message; source `file:line:col`; back-mapped values pointer(s); `wouldFailAt` admission/scheduling/runtime; `proposedValue`; fidelity caveats |
| **Report** | `sha256:` of canonical JSON | PR check output; optional OCI attestation | Findings + provenance (snapshot digest, dry SHA, package digest, profile version, engine versions, unknowns); deterministic (report §9.30) |
| **Component** | `name` | Git (package) | Named group mapped onto native switches — Helm `enabled`/`condition`, Kustomize `Component`, Compose `profiles`; dependency graph, cycle detection and cascade rules carried from `internal/core/component.go` (audit brief §1.5), re-addressed to document identity |
| **Secret reference** | `ref://<store>/<path>#<key>[@version]` | Git (as value) | Never a value; rendered to ESO `ExternalSecret` by default (report §9.14) |
| **Lockfile** | `zhi.lock.yaml` | Git | Digests of snapshots, policy packs, profiles, exec-adapter binaries (Kyverno CLI, gator), plugins — the current `zhi-plugins.lock` shape generalised (audit brief §2.6) |

What is deliberately **not** in Git: secret values, reports (attached to PRs and optionally pushed as attestations), snapshot caches, and rendered manifests unless the estate uses hydrated branches (report §10.1 "Primary Git flow": DRY-centric primary, hydrated as a supported check path).

## 4. Architecture

### 4.1 Packages and binaries

One module, one binary (`cmd/zhi`), `CGO_ENABLED=0`, built per the embedding recipe in the gap-go-embedding brief §5: `k8s.io/{apiserver,apiextensions-apiserver,pod-security-admission,client-go,…} v0.37.0`, `helm.sh/helm/v4 v4.3.0`, `sigs.k8s.io/kustomize/api v0.21.1`, `compose-go/v2 v2.15.0`, `santhosh-tekuri/jsonschema/v6 v6.0.3`, `open-policy-agent/opa v1.20.2`, `gatekeeper/v3 v3.23.1` (for `pkg/drivers/k8scel`), kubectl-validate by pseudo-version, `homeport/dyff`, `replicatedhq/troubleshoot` (analyzers), `oras-go/v2`. No `replace` directives; CEL only through `k8s.io/apiserver/pkg/cel/environment` (gap-go-embedding §0.2).

```
pkg/
  model/      values tree with yaml.v3 nodes, JSON pointer ops, document identity, lineage
  schema/     2020-12 compile/validate, x-zhi vocabulary, inference from values.yaml + "# @schema"
  rules/      CEL over values/rendered/env/snapshot via apiserver environment; severity attrs
  render/     helm (v4 SDK, snapshot-driven Capabilities, snapshot-backed lookup)
              kustomize (krusty), compose (compose-go), exec (KRM ResourceList adapter)
  snapshot/   format, importer (client-go discovery + dynamic), exporters (kubeconform dir,
              kubectl-validate --local-schemas, Kyverno Context/Values, gator inventory)
  admit/      ordered chain: expand, structural+defaulting+CRD-CEL, namespace, limitranger
              (fork), psa, quota (fork), map, vap, webhook-match, rbac (fork), refs
  engines/    exec adapters: kyverno, gator, kwctl, flux-schema, conftest; lockfile-pinned
  profile/    distribution profiles as data (openshift, gke-autopilot, aks, eks-auto, rancher)
  compose/    host snapshot, host-aware checks, policy pack
  finding/    canonical record, severity mapping, JSON/SARIF/PolicyReport/LSP/KRM encoders
  gitops/     Argo/Flux/Kargo CR discovery, layer mapping, comment-preserving writer, PR/Check
  secrets/    ref parsing, ESO/SealedSecret/SOPS rendering, resolver interface
  pipeline/   Validate(ctx, Workspace, Env, Options) (Report, error) — the only entry point
internal/
  cli/  ui/  lsp/  mcp/            thin surfaces over pkg/pipeline
```

`pkg/pipeline.Validate` is a pure function of committed inputs plus a versioned snapshot — "giterminism as a product rule" (report §9.30). That single property is what makes pre-commit, CI, UI, LSP and MCP agree.

### 4.2 Data flow from edit to PR

1. **Discover.** `gitops/` reads Argo `Application`/`ApplicationSet` (`source(s).helm.{valueFiles,valuesObject,parameters}`, git-files generators, `sourceHydrator.drySource`), Flux `HelmRelease.spec.{values,valuesFrom}` and `Kustomization.spec.postBuild.substitute(From)`, Kargo Stages, and maps every key to `{file, pointer, layer}` (report §9.2; gitops brief design implications).
2. **Edit.** UI form, editor (LSP), or CLI `--set` mutates one writable layer via `model/` with comments and ordering preserved; machine-managed fields (image-automation markers, Renovate) are read-only (report §9.13).
3. **Validate.** The pipeline below runs; findings return with pointers; the UI maps them to fields, the LSP to ranges.
4. **Review.** Rendered manifests and a dyff semantic diff against the previous revision (or the hydrated branch) are shown beside the findings (inner-loop brief implication 3).
5. **Publish.** `zhi pr` creates a branch, commits with trailers, opens the PR with the report; CI re-validates and posts a Check Run/SARIF (§6 below).

### 4.3 The render → validate pipeline and its ordering

The order is the report's recommended admission emulation (§5.3) with the values-level checks in front because they are cheap and map directly to form fields:

1. Merge values layers; strict interpolation; schema validation of the *final merged* values against all subchart schemas, replicating Helm 4's merged-`.Values` semantics (report §9.10).
2. CEL value rules (`values.cel.yaml`) with `values`, `oldValues` (previous Git revision), `env`, `snapshot`.
3. Render: Helm v4 SDK with `--kube-version`/`--api-versions` derived from the snapshot's discovery layer and `lookup` served from the snapshot inventory; krusty; compose-go. Subprocess adapters for Timoni/KCL/Pkl/ytt/`flux build` speak KRM `ResourceList` (report §9.3).
4. Discovery check: removed/deprecated APIs on the target minor (replaces pluto/kubent; report §3.1 row 4).
5. Expand workloads to Pods (gator-expand/Kyverno-autogen style) so PSA and quota see what the controller creates (report §3.1 row 7).
6. Structural schema + defaulting + CRD `x-kubernetes-validations` via kubectl-validate's `customresource.NewStrategy` + `rest.BeforeCreate`, with the server's cost constants (gap-k8s-offline §4; gap-go-embedding §0.3). CRDs are near-exact; native types are labelled approximate because KEP-5073 rules are not published to OpenAPI (gap-k8s-offline §1).
7. NamespaceLifecycle: namespace exists in snapshot, or the PR creates it — on OpenShift the project-request template's quota/LimitRange/NetworkPolicy are injected first (gap-deployer brief, OpenShift table).
8. LimitRanger: mutate, then validate (vendor-forked `PodMutateLimitFunc`/`PodValidateLimitFunc`).
9. PSA: `policy.EvaluatePod` against the namespace's enforce level; cluster defaults/exemptions come from the profile or `declared.yaml`, never guessed (gap-k8s-offline §8).
10. Quota: forked evaluators, `(used + delta) ≤ hard` per namespace across all documents in the change; Warning when `status.used` is older than the snapshot TTL (report §9.27).
11. Mutations: MAP via the apiserver `mutating` package, Kyverno mutate via CLI; the mutated diff is shown as "what the cluster will store" (gap-deployer brief: GKE Autopilot/AKS mutators, encoded as profiles).
12. Validation: VAP through `validating.NewValidator` + `cel.NewCompositedCompiler` exactly as Gatekeeper's `k8scel` driver does, with params and `namespaceObject` from the snapshot and an RBAC-backed `authorizer`; Kyverno CLI with generated `Context`/`--parameter-resource`/`--userinfo`; Gatekeeper via its generated VAP/VAPB first, Rego templates via OPA + `frameworks/constraint` with snapshot inventory (report §4.3; gap-go-embedding §5).
13. Webhook match prediction: which `*WebhookConfiguration` rules the request would hit, reported as Warning "logic unknown" (report §3.1 row 14).
14. RBAC: `RulesAllow` for the controller's identity from the snapshot (report §3.1 row 12).
15. Referential checks: Secret/ConfigMap names and keys, classes, PriorityClass, ServiceAccount, images against allow/mirror lists (report §3.1 rows 10–11).
16. Environment requirements: Troubleshoot analyzers run on the snapshot as a bundle (`analyzer.DownloadAndAnalyze` needs no cluster, gap-go-embedding table), plus zhi's extended analyzers.
17. Severity mapping (report §9.9) and back-mapping to values pointers; report assembly with a deterministic digest.

Every finding carries its tier and the list of unobservable inputs it depended on ("PSA exemptions unknown", "authorizer() evaluated as allow", "3 webhooks match"). Default policy for unknowns: permissive plus badge locally; `--strict` in CI turns them into Warnings; they are never Blocking because they are not observable (report §10.2 question 2 — owner to confirm).

### 4.4 Snapshot format

A directory (tarball for transport, OCI artifact for distribution), own layout as the report's verdict on the on-disk anchor (§10.1):

```
manifest.json          provenance: cluster id, server version, capture time, identity,
                       per-layer sha256, engine versions (Kyverno, Gatekeeper), TTL
discovery.json         APIGroupDiscoveryList
openapi/api/v1.json    kubectl-validate --local-schemas layout, hash-addressed
openapi/apis/<g>/<v>.json
crds/*.yaml            raw CRDs (keep x-kubernetes-validations, defaults, pruning)
admission/*.yaml       VAP/VAPB, MAP/MAPB, webhook configs — 1.37 static-manifest form
policies/kyverno/  policies/gatekeeper/  policies/kubewarden/
namespaces/  quota/  limitranges/  classes/  rbac/  inventory/  gitops/  nodes/ (opt)
declared.yaml          operator-supplied: PSA defaults/exemptions, static .static.k8s.io
                       policies, webhook stubs, profile id
profile.yaml           distribution profile pin
```

Media types follow the existing `vnd.zhi.*` convention (`application/vnd.zhi.snapshot.<layer>.v1+json`); the OCI push/pull code and the multi-platform index handling are lifted from `pkg/sharing/client` (audit brief §2.6). Signing is a cosign v3 bundle as a referrer, produced and verified by shelling out to cosign, not by the unreachable in-tree Sigstore stack (audit brief §2.6, lesson 7). Exporters write kubeconform, kubectl-validate, Kyverno and gator side files so platform teams can keep their existing CI steps while adopting the snapshot (report §9.22).

### 4.5 Fidelity tiers shipped in v1

- **T0-a** (schema, discovery, structural + CRD CEL): ms, always on.
- **T0-b** (full chain above with engines): target under 2 s per keystroke for a typical chart; measured, not promised, until the spike in §11 lands.
- **T2** (`kubectl --dry-run=server --validate=strict`, Argo `server-side-diff`/sync `{dryRun}`, `flux diff kustomization`): optional when a kubeconfig is present; findings labelled "live" and shown as a delta, never merged (report §5.3).
- **T1** (envtest/KWOK hydrated from the snapshot) is *not* in v1; it waits for boot-time benchmarks and the static-admission spike (report §9.32, §10.2 question 4).

### 4.6 UI approach

The UI is a local web app served by `zhi ui` over loopback from the same binary, embedded via `embed.FS`. The shell — findings page grouped by severity with `file:line` links, rendered-manifest pane, dyff diff, SSE log pane for snapshot import and exec adapters — is Go `html/template` + htmx, reusing the middleware chain and the ETag/CSP/`Flush()` lessons the audit calls "already solved correctly" (audit brief §3.2, §7.1). The values form is one RJSF island (Apache-2.0, v6.x, Ajv2020 configured explicitly): the schema is the developer's `values.schema.json`, the `ui:*` schema is generated by the engine from `x-zhi-*` hints, and validation in the browser is advisory only — the authoritative findings arrive from `/api/v1/validate` as 2020-12 output units and are injected through RJSF's `extraErrors` keyed by `instanceLocation` (report §4.6 RJSF row; schema-validation brief implication 7). Conditional visibility (`x-zhi-show-if`, component toggles) is evaluated by the engine in CEL and returned as a visibility map, so the browser never runs a second expression language — the Rancher Ember-vs-Jexl divergence (persona brief, pain points) cannot recur. The audit's "mutate-validate-revert" trick becomes unnecessary: `Validate` is pure, so inline validation is a call with candidate values. Cluster-aware pickers are populated from the snapshot's `classes/` and `inventory/` layers. This costs one JS build step, isolated in `internal/ui/web/` with a committed `dist/` and a CI check modelled on `make proto-check` (audit brief §6).

### 4.7 CLI, CI, LSP, MCP surfaces

- **CLI:** `zhi snapshot import|refresh|inspect|export`, `zhi env add|list|matrix`, `zhi validate --env <e> [--strict] [--exit-code] [--format json|sarif|policyreport|github]`, `zhi render`, `zhi diff`, `zhi contract init|check`, `zhi pr`, `zhi ui`, `zhi lsp`, `zhi mcp`. Exit codes: 0 clean, 1 Blocking, configurable `--warn-exit-code` for Warnings (Kyverno CLI precedent), ≥3 tool error (`flux diff` precedent). `zhi` owns the exit code of every exec adapter, closing the Kyverno-exits-0 hole (report §3.3).
- **CI:** a GitHub Action and GitLab template wrapping `zhi validate`; SARIF for code scanning; PR comment in argocd-diff-preview style (inner-loop brief implication 13); pre-commit hook config. Snapshot refresh as a scheduled job pushing a signed OCI bundle (report §5.3 "freshness like a lockfile").
- **LSP** (phase 2): diagnostics from findings with ranges from yaml.v3 positions, hover from schema `description`, code actions applying `proposedValue`; plus `.zhi/schema.json` and the `# yaml-language-server: $schema=` modeline for editors without zhi (report §9.28).
- **MCP** (phase 2): stdio and Streamable HTTP, loopback bind, read-only by default; tools `validate`, `explain_finding`, `render`, `diff`, `import_snapshot`; generated from the same command descriptors as the CLI so the audit's "third re-marshalling" does not return (audit brief §3.5–3.6; report §9.29).

## 5. The developer ↔ deployer contract

zhi *consumes* every schema format the ecosystem produces — `values.schema.json` (draft 4 through 2020-12), `# @schema` comments (losisin/dadav), helm-docs `# --`, Bitnami `## @param`, ytt schema export, Timoni `#Config`, kro SimpleSchema, XRD/CRD `openAPIV3Schema`, Compose `${VAR}` sites — into one internal 2020-12 model, and infers types from `values.yaml` defaults when nothing exists (report §6.1). It *emits* three files.

**`values.schema.json`** — JSON Schema 2020-12 plus the `x-zhi-*` vocabulary, registered as a custom vocabulary in `santhosh-tekuri/jsonschema/v6` so hints are type-checked by zhi and ignored as annotations by Helm 4, compose-go and kubeconform (report §6.1 "UI hints"):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1, "maximum": 20, "default": 1 },
    "persistence": {
      "type": "object", "x-zhi-group": "Storage", "x-zhi-order": 20,
      "properties": {
        "storageClass": { "type": "string", "x-zhi-cluster-ref": "storageclass",
                          "x-zhi-help": "Needs ReadWriteMany when replicaCount > 1" },
        "size": { "type": "string", "pattern": "^[0-9]+(Gi|Mi)$", "default": "10Gi" }
      }
    },
    "db": { "type": "object", "properties": {
      "passwordRef": { "type": "string", "format": "zhi-secret-ref", "x-zhi-secret": true },
      "external":    { "type": "boolean", "x-zhi-component": "external-db" }
    } }
  }
}
```

Vocabulary: `x-zhi-widget`, `x-zhi-group`, `x-zhi-order`, `x-zhi-help`, `x-zhi-secret`, `x-zhi-component`, `x-zhi-cluster-ref` (`storageclass|ingressclass|gatewayclass|namespace|secret-key|configmap-key|priorityclass|runtimeclass|image`), `x-zhi-show-if` (CEL), `x-zhi-immutable`, `x-zhi-machine-managed`, `x-zhi-severity`. Rancher `storageclass`/`secret` types, OLM `x-descriptors` and Kubeapps `form`/`hidden` are mapped onto these on import (persona brief, implications 5–6).

**`values.cel.yaml`** — cross-value and environment rules, the helm-cel pattern with the VAP variable set (report §6.1 "Cross-value rules", §9.12). Rules are stored so they can later be promoted to a cluster VAP unchanged:

```yaml
apiVersion: zhi.dev/v1
kind: ValueRules
rules:
- name: rwx-needs-capable-class
  expression: >
    values.replicaCount <= 1 ||
    snapshot.storageClasses.exists(sc, sc.name == values.persistence.storageClass
                                       && sc.rwxCapable == true)
  message: "replicaCount > 1 requires a ReadWriteMany-capable StorageClass on this cluster"
  severity: Blocking
  fieldPath: /persistence/storageClass
  wouldFailAt: runtime           # PVC Pending, not admission
- name: external-db-needs-host
  expression: "!values.db.external || has(values.db.host)"
  severity: Blocking
  fieldPath: /db/host
```

`sc.rwxCapable` is derived from CSIDriver capability reporting where the driver publishes it and is `unknown` (Warning) otherwise — the missing analyzer the persona brief calls out (implication 2, uncertainties).

**`zhi-requirements.yaml`** — the environment contract. Troubleshoot analyzer syntax is embedded verbatim so the same file compiles to a `troubleshoot.sh/v1beta2 Preflight` Secret for `kubectl preflight` users, and requirements may reference the deployer's chosen values exactly as Troubleshoot allows (persona brief §2, implication 4, 11):

```yaml
apiVersion: zhi.dev/v1
kind: Requirements
kubeVersion: ">= 1.29.0-0"                  # also written to Chart.yaml
requiredAPIs: [cert-manager.io/v1/Certificate, gateway.networking.k8s.io/v1/GatewayClass]
analyzers:                                   # troubleshoot.sh/v1beta2 analyzers, verbatim
- storageClass:
    storageClassName: "{{ .Values.persistence.storageClass }}"
    outcomes: [{fail: {message: "StorageClass not found"}}, {pass: {message: ok}}]
- nodeResources:
    outcomes: [{warn: {when: "sum(cpuCapacity) < 8", message: "Sized for 8 cores"}},
               {pass: {message: ok}}]
zhi:                                         # analyzers Troubleshoot lacks
- psaLevel:      {namespace: "{{ .Env.namespace }}", maxEnforce: baseline}
- quotaHeadroom: {namespace: "{{ .Env.namespace }}", severity: Warning}
- crdVersion:    {name: certificates.cert-manager.io, servedVersion: v1}
```

Severity mapping across everything is fixed: schema/CEL/VAP Deny/Kyverno Enforce/Preflight `fail` (+`strict`) → Blocking; VAP Warn, Kyverno Audit, Gatekeeper warn/dryrun, PSA warn/audit, webhook predictions, stale quota, native best-effort checks → Warning; deprecations and annotations → Info (report §9.9).

## 6. GitOps integration

**Discovery.** Values are found where the controllers say they are: Argo `Application.spec.source(s).helm.{valueFiles,valuesObject,parameters,fileParameters}` with its documented precedence, `kustomize.{images,replicas,patches}`, ApplicationSet git-files globs and cluster-Secret labels, `sourceHydrator.drySource`; Flux `HelmRelease.spec.{values,valuesFrom,chart.spec.valuesFiles}` (list order → inline), `Kustomization.spec.postBuild.substitute(From)` with `${VAR:=default}` strictness; Kargo Stages for the promotion order; Compose `.env`/`compose.<env>.yaml` (report §2.1, §9.2). Inputs that live in-cluster (`substituteFrom` ConfigMaps, cluster-Secret labels) are captured keyed-and-redacted in the snapshot's `gitops/` layer so Git plus snapshot reproduces the controller's render (environments brief, pain points). The effective-value explorer shows every leaf with its source layer per environment, the "run Helm in your head" failure class (report §3.1 row 16).

**The PR.** `zhi pr --env prod` writes only the environment's writable layer with comments and key order preserved, on branch `zhi/<env>/<slug>`, and commits with trailers:

```
Zhi-Package: sha256:…        Zhi-Snapshot: customer-a-prod@sha256:…  (captured 2026-09-12T10:00Z)
Zhi-Profile: openshift-4.20  Zhi-Report: sha256:…   Zhi-Dry-Sha: <dry commit>
```

— the `hydrator.metadata`/`Argocd-reference-commit-*` shape Argo already uses (gitops brief, design implications). The PR body contains the dyff diff in GitHub/GitLab markdown, collapsible per document, the findings table with fidelity badges, the list of unknowns, and the snapshot age. CI runs `zhi validate --env prod --strict --exit-code`; because the inputs are committed and the snapshot is digest-pinned, the report digest matches the one in the trailer, and the Check Run is the evidence. Hydrated-branch estates get the same check on `hydrateTo` branches, reading `refs/notes/source-hydrator` for the dry SHA (report §4.1 Source Hydrator row). Kargo consumes the PR through `git-wait-for-pr`/`git-merge-pr` in OSS; a Kargo `http` step target and a gitops-promoter `CommitStatus` provider are phase-2 hooks (report §9.25; gap-competitors "Kargo — settled").

**Post-merge.** zhi never applies. `zhi watch --env prod` (phase 2) subscribes to Argo application status/Notifications or Flux commit-status and drift events, correlates hydrated SHA ↔ dry SHA ↔ author, and optionally fires the Argo `/api/webhook` or Flux `Receiver` to skip the 120 s + 60 s jitter poll (report §3.2, §9.24). Drift is a read-only third state next to "valid" and "stale snapshot"; rollback is `git revert` with warnings about non-revertible effects (Helm hooks, immutable fields, PVCs) surfaced from the same diff (gitops brief). Direct push to a synced branch for dev environments is an owner decision (report §10.2 question 13); the default is PR-only.

## 7. Docker Compose

The same entities apply with compose-go v2 as the model: the *package* is the Compose project plus contract; *values* are the variables `template.ExtractVariables` enumerates (with defaults and `:?` required flags) plus typed `x-zhi-*` blocks bound through `cli.WithExtension`; the *environment* is `.env.<env>` + `compose.<env>.yaml`; the *target* is a host or fleet; *render* is `LoadProject` with the environment's `Mapping`; `schema.Validate` and the ~28 consistency checks become Blocking findings with field paths (compose brief, implications 1–2). zhi adds the strict interpolation compose-go lacks — a referenced-but-unset variable is Blocking, a defaulted one Warning, empty results for `image`/`ports`/`volumes` are flagged — because "unset becomes `""`" is the most common Compose failure (report §3.1 row 17).

The **host snapshot** (`zhi snapshot import --docker-host ssh://…`, or via the Portainer/Komodo APIs) contains: Engine/API and Compose versions, OS/arch, cgroup version, rootless/userns and seccomp/AppArmor defaults, default address pools, CPU/memory/GPU, listening host ports (published ports of running containers, optionally `ss`), existing networks/volumes/containers/projects, images with digests, registry reachability, bind-source existence and permissions, and flavour flags (`swarm`, `podman`) (report §5.1 "Compose analogue"; compose brief implication 3). Host-aware checks: published-port collision against the snapshot and across services/projects, missing bind sources, cross-project `container_name` collisions, `cpus`/`mem_limit` versus capacity, `gpus` without GPU, `deploy.*` with `swarm: false` (Warning), Swarm- or podman-unsupported attributes when those flags are set, version gates (`env_file.required` ≥ 2.24, `pre_start` ≥ 5.3), image tag existence via registry HEAD when online (compose brief implication 4). The policy pack is CEL over the normalised compose-go JSON — one rule language for both targets — with a default pack mirroring Portainer's security dimensions (no privileged, no host namespaces, binds under allowed roots, no added capabilities, explicit tags/digests, healthcheck required); Rego packs run through the conftest exec adapter for teams that have them (compose brief implication 5). Write-back never rewrites `compose.yaml`: values land in `.env.<env>`, overrides and `x-zhi-*` blocks so plain `docker compose` keeps working, and a `config --hash`-equivalent is recorded for drift (compose brief implication 6). Post-merge is whatever the estate runs: Portainer polling, Komodo `ResourceSync`, or CI over SSH (report §4.5).

## 8. Plugin depth

The owner's question — how deep must the plugin system be — has a short answer backed by every brief: **shallow**. Thriving tools in 2026 extend through data, CEL, and digest-pinned CLI binaries; sidecar/gRPC plugins inside the deploy path are universally painful (Argo CMP #15006, Kustomize alpha for five years, Flux forbidding user code) (report §7.1, §1 bullet "Shrink the plugin system"). The audit adds the internal evidence: the current four gRPC types cost ~11,000 LOC of plumbing, could not host the one plugin users would swap (the TUI), and had zero third-party implementors (audit brief §2, lesson 6).

| Concern | Mechanism | Why |
|---|---|---|
| Value loaders (YAML/JSON/TOML/.env, Helm values, Kustomize, Compose, Argo/Flux/Kargo CR discovery) | **Fixed Go core**, configured by file patterns | Needs source positions and comment-preserving write-back; Renovate regrets losing positions (report §7.1) |
| Renderers | **Fixed**: Helm v4 SDK, krusty, compose-go; **exec adapter** speaking KRM `ResourceList` for Timoni/KCL/Pkl/ytt/`flux build` | HIP-0004 stability, no CGO; typed languages need CGO/JVM/Node (report §4.2, §7.2) |
| Value schema and UI hints | **Data**: JSON Schema 2020-12 + `x-zhi-*` vocabulary | Helm 4 enforces it for free; every format ingests into it (report §6.1) |
| Cross-value and environment rules | **CEL** via `k8s.io/apiserver/pkg/cel/environment` | Same runtime the cluster uses; no second CEL module; no bespoke `when` DSL (gap-go-embedding §0.2; persona brief implication 6) |
| Imported cluster policies | **Data**: VAP/MAP/Kyverno/Gatekeeper/PSA objects evaluated untranslated | Kubernetes 1.37 made "policies as files" official (report §4.3) |
| Built-in admission emulation | **Fixed core** (embedded k8s.io + three vendor-forked packages) | Needs shared type environment and in-process speed (report §7.2 "Validators", §9.7) |
| Third-party engines (Kyverno CLI, gator, kwctl, flux-schema, conftest) | **Exec adapters**: version-pinned binaries pulled by digest from OCI, recorded in the lockfile; zhi owns exit codes | Kyverno is not embeddable (PSA fork replace, 400+ requires, 2026 CVE cadence); Flux 2.9's plugin model proves the distribution shape (gap-go-embedding §0.4; report §7.1) |
| Cluster and host importers | **Fixed Go core** (client-go, Docker API, Portainer/Komodo/Rancher/OpenShift API adapters) | Read-only, must degrade gracefully per layer (report §9.5) |
| Distribution profiles | **Data**: versioned YAML + CEL packs with changelogs | The opaque half of managed platforms can only be encoded from vendor docs (gap-deployer brief, cross-cutting finding) |
| Secret stores and credentialed cloud collectors | **`resolver/v1`** — go-plugin gRPC, first-party compiled in, phase 2 | The one place every brief still wants a process boundary; Vault chose isolation over Wasm (report §7.3) |
| Checks CEL cannot express | **`check/v1`** — wazero with a zhi-owned ABI, or exec with the `ResourceList`/output-unit contract, phase 3, only on demand | Kubewarden proves the shape; Helm 4's slow Wasm uptake and the Extism SDK's 18-month silence say do not depend on it yet (report §7.3) |
| Publishers (Git PR, OCI push, PR comment, Check Run, CommitStatus) | **Fixed core** with templates for bodies | Provenance must be uniform across estates (report §9.1) |
| UI panels, LSP, MCP | **Fixed surfaces**, never extension points | Grafana/Backstage sandbox and versioning debt; audit lesson 9 (report §7.1; audit brief §7.2) |

**Not pluggable:** the finding record, the snapshot format, the pipeline order, policy-engine semantics, the Git writer, store backends (Git is the store), transforms in the GitOps path (Flux forbids them, Argo suffers them), and UI widgets beyond the vocabulary.

**Versus the current four types:** `config.Plugin` becomes loaders-as-data plus the contract files; `transform.Plugin` and its three-way `ValidatePolicy` knob are dropped; `store.Plugin` (27 methods) is replaced by Git plus secret *references* and, in phase 2, `resolver/v1`; `ui.Controller` (25 methods × 8 implementations) collapses into `pkg/pipeline` consumed by three surfaces (audit brief §2.7, §7.1). What survives from the plugin subsystem is the distribution hygiene — signed OCI artifacts, digest lockfile, binary integrity audit — applied to exec adapters, profiles and snapshots. There is no marketplace, ratings or search until third-party plugins exist; a curated krew-index-style catalog file is enough (report §7.3; audit brief §2.6).

## 9. What to carry from the current zhi and what to drop

**Carry** (audit brief §7.1, cited verdicts):
- The **severity triad** Info/Warning/Blocking — "right granularity; maps to CI gate vs advisory".
- **`ComponentManager`** dependency graph, Kahn cycle detection, mandatory-with-dependencies pre-enabling, `DisableCascade` — "best code in the repo"; re-addressed from string prefixes to document identity and native switches.
- **`apply.go` subprocess handling** — process-group teardown, `WaitDelay`, pipe-drain ordering, 1 MiB scanner — "copy essentially line for line", now the runner for every exec adapter and pre-check.
- **Drift/diff concept** (`drift.go`, `diff.go`) — promoted from "rendered vs disk" to "desired vs previous revision / hydrated branch / live", with dyff for semantic output.
- **Web middleware chain** and the ETag/CSP-nonce, gzip-before-ETag, `Unwrap()`/`Flush()` SSE lessons; severity-grouped findings page; SSE log pane.
- **Schema-driven widget selection** — reborn as the `x-zhi-*` vocabulary replacing the 1,573-LOC `labels` registry.
- **OCI client, custom media types, atomic install, lockfile** — the vehicle for snapshots, profiles, policy packs and adapter binaries.
- **`launch/audit.go` integrity hygiene** (symlink resolution, digest compare, world-writable warning) for downloaded binaries and bundles.
- **Test and CI conventions**: `-race -count=1`, fmt-diff gate, codegen-staleness check (reused for the UI `dist/`), `testdata/` fixtures, `startTestServer` port-0 pattern; add golden-file tests over real manifests and a policy conformance table (audit brief §6).
- **Secret-reference seed**: the Vault HTTP client (MPL) and the `store.writeonly` idea become `ref://` handling.
- **Interactive OIDC login + callback server** if snapshot import needs it.

**Drop** (audit brief §7.1): the flat slash-path tree and its `[a-z]` regex; `Val any` and 330 LOC of coercion; `ValidationResult` without a path; the dead `Value.Validators` mechanism; **Yaegi-interpreted Go validators** ("full-stdlib RCE from a config file — drop, urgently"); the four gRPC plugin types and 10,790 generated LOC; JSON-over-protobuf values; `transform.Plugin`; `store.Plugin` and the plaintext fallback store; the 25-method `ui.Controller` × 3 frontends; the TUI (4,536 LOC) and `RequiresTTY`; the marketplace server, ratings, advisories; the unreachable Sigstore stack (shell out to cosign); `text/template` + Sprig as the Kubernetes/Compose bridge and the side-effecting `fileACL`/`fileMode`; the meta-plugin SDK. **Defer** the air-gap mirror: the OCI-layout and bundle code is solid and returns in phase 3 if regulated customers appear (audit brief §2.6).

## 10. MVP and phases

**Phase 1 — the consultant's loop, Kubernetes-first (≈ 8 months for two people, 12+ for one).** A consultant can:

1. `zhi snapshot import --context customer-prod --namespaces shop` → signed bundle with layers 1–11 of report §5.1 (nodes optional), degrading per layer; push to OCI or vendor into Git.
2. `zhi env add customer-prod --snapshot … --profile openshift-4.20`; Argo/Flux discovery maps the values layers.
3. `zhi ui`: schema-generated form with cluster-aware pickers, inline findings, rendered manifests, dyff diff, effective-value view, component toggles.
4. Engine T0-a + T0-b: schema, CEL rules, Helm v4 and krusty render, discovery/deprecation, workload expansion, structural + CRD CEL, LimitRanger, PSA, quota, MAP/VAP in-process, Kyverno via CLI adapter, Gatekeeper via its generated VAP/VAPB (Rego through gator adapter when on PATH), webhook match prediction, RBAC, referential checks, requirements (`kubeVersion`, `requiredAPIs`, embedded Troubleshoot analyzers), one profile (OpenShift — the SCC/UID failure is the #1 vendor-chart failure, gap-deployer brief; owner to confirm which platform consultants meet first).
5. Compose T0: compose-go render, strict interpolation, schema + consistency, `.env`/override write-back — without the host snapshot.
6. `zhi pr` to GitHub and GitLab with trailers and report; `zhi validate --exit-code --format sarif|json|github` in CI; pre-commit hook; T2 live check when a kubeconfig is present.
7. Secrets as `ref://` values rendered to ESO `ExternalSecret`; Blocking if the store is missing from the snapshot.

Rough size, non-test Go LOC: `model/schema/rules/finding` 5k; `render` 3k; `snapshot` 5k; `admit` incl. ~2k forked 7k; `engines` 2k; `gitops` incl. YAML round-trip and PR 5k; `secrets` 1k; `pipeline` + `cli` 4k; `ui` 3k Go + ~3k TypeScript. **≈ 35k Go + 3k TS**, tests near 1:1 as today — smaller than the current 42k because four plugin transports, a store layer, a TUI and a marketplace disappear (audit brief §0).

**Phase 2 (≈ 4–5 months):** Compose host snapshot and host-aware checks; LSP and MCP; `zhi watch` post-merge feedback; Kargo `http` step and gitops-promoter `CommitStatus` provider; `resolver/v1` with Vault/SOPS/age and Sealed Secrets cert-in-snapshot; OPA + `frameworks/constraint` embedded for Rego templates; kwctl adapter; profiles for GKE Autopilot, AKS, EKS Auto Mode, Rancher PSACT; environment matrix view; decidability report per policy (report §9.23). ≈ +14k LOC.

**Phase 3 (≈ 5–6 months):** T1 backend (envtest/KWOK) after the benchmark spike; scheduler-framework fit against snapshot nodes; `check/v1` Wasm if demand appears; DSSE + SLSA VSA reports, CycloneDX/SPDX SBOMs, `GOFIPS140` build, offline cosign roots, air-gap mirror export/import; CUE import/export; Podman-kube/Quadlet profile. ≈ +12k LOC.

## 11. Risks, unknowns, and the lab spikes required before committing

Named spikes, each one to two weeks, to run before architecture freeze (report §10.2 item 16 plus this proposal's own unknowns):

- **S1 CRD-CEL parity:** embed kubectl-validate by pseudo-version and prove a failing `x-kubernetes-validations` rule with the server's cost constants; confirm ratcheting with `oldObject`.
- **S2 Native VAP/MAP in-process:** `validating.NewValidator` + `cel.NewCompositedCompiler` per the `k8scel` pattern with snapshot params, `namespaceObject`, and an RBAC-backed authorizer; then the `mutating` package producing a JSON patch.
- **S3 Kyverno CLI side files:** generate `Context`/`--parameter-resource`/`--userinfo` from a snapshot and reproduce a known in-cluster denial, including a MAP paramRef.
- **S4 Vendor-fork viability:** copy LimitRanger, RBAC `RulesAllow` and quota evaluators from `k8s.io/kubernetes@v1.37.0`; the unverified part is whether `pkg/quota/v1/evaluator/core` drags in-tree `pkg/apis/core` types requiring conversion (gap-go-embedding §5).
- **S5 Helm v4 SDK in-process render** with faked Capabilities and snapshot-backed `lookup`; Helm 4.3 accepting a 2020-12 `values.schema.json` end to end (report §10.2 item 16).
- **S6 Value lineage:** back-mapping a rendered field to the values key that produced it has no ready mechanism in the research; try differential rendering (perturb leaves, diff outputs) and fall back to "finding on rendered document with related keys by heuristic" if it is too slow.
- **S7 Comment-preserving round-trip** of real-world Helm values files (anchors, image-automation markers) with yaml.v3 nodes.
- **S8 T0-b latency** on a realistic chart with 50 Kyverno policies: the "<2 s" figure is an estimate (report §5.2).
- **S9 Gatekeeper `k8scel` offline `request.userInfo`** (gap-k8s-offline §6, unverified).
- **S10 KWOK/envtest boot time and `--admission-control-config-file`** — gates phase 3 only.
- **S11 Practitioner survey** at the owner's company on failure-class frequency, so the check order matches reality (report §10.2 item 17).

Risks: native-type validation stays approximate for years (gap-k8s-offline §1) — mitigated by tier labels and T2; k8s.io minor skew when a customer runs a newer minor than the pinned v0.37 (skew Warning, one binary; gap-go-embedding §2); Kyverno's CVE cadence and `ClusterPolicy` removal in 1.20 (subprocess isolation, lockfile pinning); the cel-go module rename (import only through k8s.io); Argo/Flux internals moving on minors (git notes in 3.3, gRPC types in 3.5 — read CRs and notes, not commits); AGPL adjacency (flux-operator, schema-catalog: fetch, never import; report §9.21); Kargo custom steps being Enterprise-only (PR-based integration in OSS); opaque platform admission (profiles maintained from vendor docs, versioned); the "unknowns policy" and "hydrated vs DRY" owner decisions; adoption friction if zhi ever asks teams to migrate charts (it never does — it consumes their formats); one JS build step in an otherwise Go-only repo; and the bus-factor of a 1–2 person team against a ~60k LOC roadmap, which is why phase 1 is scoped to the consultant loop and everything else is cut-able (§13).

## 12. Differentiation

Report §8 finds no product that combines snapshot import, all-engine offline evaluation, a two-persona contract, PR output, and Compose parity. Against each named neighbour:

- **ConfigHub** validates on edit (`vet-schemas`, `vet-celexpr`, Triggers) but replaces Git as the source of truth — OCI-pull-only delivery since August 2026, no Git write, no admission/quota import (report §4.1, §8; gap-competitors "ConfigHub — deep dive"). zhi is Git-native and stateless; ConfigHub could at most be an OCI publishing target.
- **Devtron** has the strongest living schema-per-scope GUI, but locked keys and approvals are Enterprise-tagged, there is no policy engine or snapshot, Dry Run only renders, and it owns the manifest repo rather than opening a PR into yours (report §4.6, §8). zhi copies schema-per-scope and adds the pseudo cluster and the PR.
- **Flux Schema** is the CRD/CEL layer of a pseudo cluster, fully offline and reusing the same apiserver code — and nothing else: no VAP/MAP, Kyverno, Gatekeeper, PSA, quotas, values model, UI or Git write; its catalog is AGPL (report §4.1, §8). zhi treats it as an exec adapter and generates its own catalog from the customer's CRDs.
- **Replicated KOTS + Troubleshoot** has the right contract split and the richest requirement vocabulary; it is proprietary ($2–3k/month base), needs an in-cluster admin console, and runs preflights at install time in the customer cluster (persona brief summary; report §4.6). zhi copies the split, embeds the analyzers, evaluates them offline against an imported snapshot, and emits a Preflight Secret so Replicated users lose nothing.
- **Cyclops** generates forms from `values.schema.json` inside an in-cluster controller; JSON Schema only, direct commit rather than PR, no policy awareness, slowing (report §4.6, §8; gap-competitors post-mortems). zhi is cluster-detached and adds every layer Cyclops lacks.
- **Kargo** already edits values files and opens PRs, has no validation step, and gates custom container steps behind Enterprise (report §4.1; gap-competitors "Kargo — settled"). zhi complements it: the PR zhi opens is the PR Kargo waits on, and a `http` step target serves Enterprise users.

The positioning the research supports: above Helm/Kustomize/Compose (never fighting them, as Glasskube did), beside Argo/Flux/Kargo (never becoming their UI, as Weave GitOps did), and stateless with respect to the cluster (never cluster-attached, as Kubevious, Kubeapps and Cyclops were) (report §8 closing paragraph; gap-competitors Task C). For platform teams already running kubeconform and `kyverno apply` in CI, the adoption path is a drop-in: same rendered-manifest input, same exit-code contract, plus the snapshot they could not generate before and the exporters that feed their existing steps (report §9.22).

## 13. "If I had half the time": what I would cut first

In order, cheapest to lose first:

1. **T2 live checks** — the snapshot path is the product; live dry-run is a convenience.
2. **GitLab PR writer** — GitHub first; the Git commit and trailers are identical.
3. **Gatekeeper Rego and kwctl** — consume Gatekeeper's generated VAP/VAPB only; Rego and Kubewarden move to phase 2.
4. **All profiles but one** — ship OpenShift (or whichever the survey names) and a documented `declared.yaml` escape hatch.
5. **Compose entirely from phase 1** — compose-go is cheap to add later; the Kubernetes loop proves the thesis.
6. **Embedded Troubleshoot analyzers** — keep `kubeVersion`, `requiredAPIs`, storage/ingress class presence and PSA level as native checks; defer the analyzer vocabulary.
7. **Component toggles in the UI** — keep component definitions in the contract and the graph logic in the library; expose them via CLI first.
8. **Effective-value explorer and environment matrix** — show the single writable layer and one environment.
9. **Snapshot OCI push and signing** — directory plus tarball with digest in the lockfile; cosign later.
10. **The web form itself, last** — the CLI, the pre-commit hook, the CI report and the `.zhi/schema.json` modeline for editors deliver most of the validation value to developers; the form is what consultants need and is the last thing I would remove, not the first.

What I would not cut under any schedule: JSON-pointer addressing with source positions, the ordered T0 chain with tier labels, the deterministic report, Git-only writes, and the exec-adapter exit-code ownership — those are the properties that make pre-commit and CI agree, and that agreement is the contract.