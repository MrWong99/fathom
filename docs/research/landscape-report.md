# zhi Rewrite — Landscape Research Synthesis (September 2026)

Prepared 2026-09-13 from 17 research briefs (12 primary slices, one persona slice, four gap-closing slices) plus the critic's contradiction list. Slice keys are cited inline as `(slice: key)`. Tool status claims are reproduced as verified by the slices; where slices disagree, both positions are shown and the stronger evidence is named. Section 10 collects everything the human owner must still decide.

---

## 1. Executive summary

- **The whitespace is real and narrow.** All 17 briefs converge: no tool renders Helm/Kustomize/Compose values to final manifests, evaluates that output against an *offline snapshot* of the target cluster's admission posture (CRD schemas + CEL, VAP/MAP, Kyverno, Gatekeeper, PSA, ResourceQuota/LimitRange, RBAC, classes), and opens a PR into the folder Argo CD/Flux/Kargo already watch. The closest attempts either died (Datree, Monokle, Glasskube, Weave GitOps, Kubevious), stalled (Cyclops, kubectl-validate), or are whole platforms with their own source of truth (Devtron, ConfigHub) (slice: gap-closest-competitors-deep-dive; slice: platform-engineering-config-uis).
- **Git is the only write target.** Never apply to a cluster; "apply" becomes a commit/PR into the DRY env folder (or a hydrated branch), carrying provenance (snapshot digest, dry SHA, author) as commit trailers and a signed validation report (slice: gitops-controllers; slice: environments-secrets-drift; slice: gap-deployer-environments-openshift-managed-regulated).
- **Validate the rendered result, not the values.** Every evidenced failure class (policy denial, quota, PSA, missing refs, immutable fields, removed APIs) is visible only in rendered manifests. Render with the real engines in-process (Helm v4 SDK, kustomize `krusty`, compose-go v2) so "valid in zhi" equals "valid for the controller" (slice: k8s-config-languages; slice: docker-compose-landscape; slice: gap-go-embedding-and-license-audit).
- **Feedback today is 10 minutes to hours.** Argo CD polls Git every 120 s + 60 s jitter, the ApplicationSet PR generator every 30 min, Flux per `spec.interval`; CI queues add 30–60 min; the first *real* admission check is post-merge, and Argo's Server-Side Diff skips new resources and mutating webhooks by default. Komodor's 2025 data puts median time-to-detect at ~40 min and time-to-resolve at >50 min (slice: devops-pain-points; slice: gap-k8s-offline-fidelity-facts).
- **Kubernetes has made the constraints portable.** VAP is GA since 1.30, MAP GA in 1.36 (`admissionregistration.k8s.io/v1`), CRD CEL rules stable since 1.29 with ratcheting GA in 1.33, PSA stable since 1.25, and 1.37 ships manifest-based (static) admission in beta. A growing share of admission is "data + CEL" executable offline with upstream Go packages (slice: k8s-2026-features-and-trends; slice: policy-engines-offline-validation).
- **But native-type validation stays approximate.** KEP-5073 (declarative validation) is GA as a *gate* in 1.36, yet publishing its rules to OpenAPI is an explicit non-goal; offline validation of Deployments/Services etc. remains "best-effort OpenAPI", while CRDs reach near-exact fidelity. Report fidelity per finding, never "will apply" (slice: gap-k8s-offline-fidelity-facts; slice: schema-validation-tech).
- **Embed the k8s.io stack; shell out to Kyverno.** `k8s.io/apiserver`, `apiextensions-apiserver`, `pod-security-admission` v0.37.0 have no `replace` directives and build with `CGO_ENABLED=0`; LimitRanger, RBAC rules and quota evaluators must be vendor-forked from `k8s.io/kubernetes` (~2 k LOC). Kyverno carries a PSA fork replace, 400+ requires and 10 GHSAs in 2026 — use the CLI (`kyverno apply --context-file --parameter-resource`). OPA/Gatekeeper's `k8scel` driver and `frameworks/constraint` embed cleanly (slice: gap-go-embedding-and-license-audit).
- **CEL is the rule language; JSON Schema 2020-12 is the values contract.** Helm 4 validates `values.schema.json` with `santhosh-tekuri/jsonschema/v6` (draft 2020-12 default when `$schema` is absent); compose-go and kubeconform use the same library. Cross-value rules should be CEL (helm-cel pattern, VAP variable set) evaluated through `k8s.io/apiserver/pkg/cel/environment` — never import `cel.dev/cel-go` directly while k8s.io still pins `github.com/google/cel-go` (slice: schema-validation-tech; slice: gap-go-embedding-and-license-audit).
- **Adopt a three-tier fidelity ladder with one interface.** Tier 0: in-process render + schema + CEL + policy + quota arithmetic in <2 s per keystroke. Tier 1: envtest/KWOK hydrated from the snapshot in seconds-to-a-minute for real built-in admission. Tier 2: `kubectl --dry-run=server`/Argo SSD/`flux diff` when credentials exist. Tag every finding with its tier (slice: inner-loop-pseudo-cluster; slice: cluster-state-import).
- **The developer↔deployer contract already has a reference implementation: Replicated.** KOTS `Config` (values contract) + Troubleshoot `Preflight` (environment requirements evaluated against the target, with fail/warn/pass + `strict`) is the only clean split found. Copy the split; emit `values.schema.json`, `Chart.yaml kubeVersion` and a Troubleshoot-compatible analyzer list so the contract survives without zhi at runtime (slice: persona-vendor-to-customer).
- **Managed distributions need versioned profiles, not just snapshots.** OpenShift SCC mutation and UID pre-allocation, GKE Autopilot's opaque Warden webhook and resource-ratio mutations, AKS Deployment Safeguards mutators, EKS Auto Mode NodePools and Rancher PSACT are the constraints consultants actually hit; the declarative half is snapshot-able, the opaque half must be encoded as `openshift-4.20`/`aks-automatic-2026-06`-style profiles (slice: gap-deployer-environments-openshift-managed-regulated).
- **Shrink the plugin system.** Every brief says "shallow". Thriving tools (Flux, Kyverno, Renovate, Helm 4) extend via data, CEL, OCI-pinned CLI binaries or Wasm; sidecar/gRPC plugins inside the deploy path are universally painful (Argo CMP #15006, Kustomize alpha for five years). Recommendation: fixed Go core; declarative loaders/renderers; CEL validators; at most two loadable kinds (secret/cluster *resolvers* over gRPC, pure *checks* over Wasm/exec); no UI or store plugins; no marketplace before third-party plugins exist (slice: plugin-architectures; slice: k8s-2026-features-and-trends).
- **Compose is a first-class second target with a host snapshot, not an afterthought.** compose-go v2.15.0 is the model; the "pseudo host" is Engine/Compose versions, listening ports, networks/volumes, images, bind-source existence, swarm/podman flags; nobody validates Compose against host facts today (slice: docker-compose-landscape).
- **Secrets are references, never values.** ESO `ExternalSecret` (v1), SOPS, Sealed Secrets (cert in snapshot); validation runs with secrets absent; typed `secretRef` fields populate from the snapshot's store inventory (slice: environments-secrets-drift).
- **Regulated buyers want evidence, not dashboards.** Signed DSSE/SLSA VSA report bound to rendered-manifest digest + snapshot digest, CycloneDX ≥1.6/SPDX ≥3.0.1 SBOMs, `GOFIPS140` variant, zero egress, offline cosign v3 verification with pinned trusted roots, everything as OCI artifacts that move through Zarf/oc-mirror (slice: gap-deployer-environments-openshift-managed-regulated).
- **Licensing is clean in the embed set, dirty around it.** Everything to link is Apache-2.0/MIT/BSD; AGPL (flux-operator, schema-catalog, Nuon), GPL (Komodo, helm-docs, podman-compose) and BUSL (Vault, VSO, Terraform) may be exec'd, read as data, or talked to over HTTP, never imported (slice: gap-go-embedding-and-license-audit).
- **Lab spikes are required before promising numbers.** KWOK/envtest boot times, kubectl-validate CEL on a failing rule, Kyverno CLI paramRef for MAP, KWOK + VAP interaction, and multi-minor emulation from one k8s.io version are asserted from source reading, not measured (slice: inner-loop-pseudo-cluster; slice: gap-k8s-offline-fidelity-facts).

---

## 2. How software is deployed in 2026

### 2.1 The dominant GitOps flow, end to end

Two CNCF-graduated pull controllers own the last mile and are not being replaced: Argo CD v3.5.2 (2026-08-27) and Flux v2.9.5 (2026-08-31). Promotion moved one layer up (Kargo v1.11.4, Flux Operator ResourceSets, argoproj-labs/gitops-promoter) and "what is in Git" moved one layer down (rendered-manifests / Source Hydrator, beta in Argo 3.5). Progressive delivery (Argo Rollouts v1.10.0, Flagger v1.45.0) is a separate, stable-but-slow layer (slice: gitops-controllers).

```mermaid
flowchart LR
  subgraph DEV["Developer side (app repo)"]
    A[Author code + Helm chart / Compose file] --> B[values.yaml + optional values.schema.json]
    B --> C[CI builds image, pushes OCI]
  end
  subgraph ENV["Deployer side (GitOps repo, trunk, folder-per-env)"]
    D[Edit values-env.yaml / overlay / .env in plain editor] --> E[Open PR]
    E --> F{CI lint bolt-ons\nhelm template | kubeconform\nkyverno apply / gator / conftest}
    F -->|green, no cluster context| G[Merge to main]
    C -.->|Renovate / Image Updater / Kargo bumps tag| D
  end
  subgraph CTRL["Controller (in cluster)"]
    G -->|Argo poll 120s+60s jitter, Flux interval, or webhook| H[Render: helm template / kustomize build]
    H --> I[SSA dry-run vs live apiserver]
    I --> J{Admission chain\nschema, defaulting, LimitRanger, PSA,\nquota, VAP/MAP, Kyverno/Gatekeeper, webhooks}
    J -->|deny| K[App OutOfSync / Degraded\nHelmRelease NotReady]
    J -->|admit| L[Apply, health check, notify]
    L --> M{Pods admitted?\nPSA / quota apply to Pods, not Deployment}
    M -->|no| K
    M -->|yes| N[Running]
  end
  K --> O[git revert or argocd app rollback\nFlux remediation]
  O --> D
  subgraph HYD["Optional: rendered manifests"]
    G -.-> P[Source Hydrator writes hydrated YAML\nto env branch + git notes]
    P -.-> H
  end
```

**Where values live.** On one trunk branch, one folder per environment: (a) Helm values files layered `common → variant → env`, referenced from Argo `Application.spec.source.helm.{valueFiles,valuesObject,parameters}` or Flux `HelmRelease.spec.values/valuesFrom` (last-wins merge, `null` deletes a key, schema validated against the final merged `.Values` including subcharts); (b) Kustomize overlays with small patches plus Flux `postBuild.substitute/substituteFrom` `${VAR:=default}` (strict missing-var failure since kustomize-controller 1.9.0); (c) ApplicationSet git-files generator `config.json/yaml` per env; (d) Compose `.env` + `compose.<env>.yaml` overrides. Branch-per-environment is explicitly an anti-pattern (Akuity, Codefresh/Octopus, cloudogu, Red Hat). Some inputs live *in the cluster* (Flux `substituteFrom` ConfigMaps, Argo cluster-Secret labels), so Git alone cannot reproduce the rendered output (slice: gitops-controllers; slice: environments-secrets-drift).

**Where validation happens today.** Almost entirely CI bolt-ons over rendered output: `helm lint`/`helm template`/`kustomize build`/`flux build kustomization --dry-run` → kubeconform (JSON Schema only, no CEL) → `kyverno apply`/`gator test`/conftest with hand-written context stubs → optional rendered diff via argocd-diff-preview (ephemeral kind + Argo, 60–90 s) or `argocd app diff --local --server-side-generate` (needs live Argo + login). The controller's own check is the SSA dry-run at reconcile time — post-merge by construction. Argo's Server-Side Diff (stable since v3.1.0, still opt-in unless the ServerSideApply sync option is set) runs admission at *diff* time but "will not be performed during the creation of new resources" and excludes mutating webhooks unless `IncludeMutationWebhook=true` (slice: gap-k8s-offline-fidelity-facts; slice: gitops-controllers).

**Where it is missing.** Nothing between the editor and the merge knows (1) the values-file schema, (2) the target cluster's admission posture, quotas, CRD versions and classes, or (3) the identity (`request.userInfo`) the controller will present. Nobody previews HelmRelease values diffs (`flux diff` covers only `kustomization` and `artifact`); nobody does quota arithmetic; nobody validates a first deploy's *new* resources against admission before merge (slice: gitops-controllers; slice: inner-loop-pseudo-cluster).

### 2.2 The developer/deployer handoff as it exists today

At most organisations the handoff artifact is a Helm chart with `values.yaml`, a README generated by helm-docs, and — in a minority of charts — a `values.schema.json` (Bitnami's redis schema has descriptions and defaults only, no titles, enums or UI hints). The deployer copies `values.yaml` into `values-<env>.yaml`, edits it "with no guidance whatsoever" (KubeForge HN thread), and learns the cluster's constraints from the pipeline (slice: platform-engineering-config-uis; slice: devops-pain-points).

The only formalised split found is Replicated: developers author a `kots.io/v1beta1 Config` (groups, items, types, `when`, `required`, `validation.regex`) mapped to Helm values through `HelmChart.spec.values/optionalValues`, plus a Troubleshoot `Preflight` whose analyzers (clusterVersion, storageClass, customResourceDefinition, nodeResources, ingress, imagePullSecret, distribution…) may reference the deployer's *chosen* values (`storageClassName: {{ .Values.persistence.storageClass }}`) and are evaluated against the target cluster before install. Its weaknesses are the proprietary SaaS control plane ($2–3k/month base), a heavy in-cluster admin console, and preflights running at install time in the customer cluster rather than offline (slice: persona-vendor-to-customer).

Everything else stops at per-field schema validation inside a bigger product: Rancher `questions.yaml` (cluster-aware `storageclass`/`secret`/`pvc` pickers from a live cluster), OLM CSV `x-descriptors`, Kubeapps `form:true` (archived), Glasskube `valueDefinitions.targets` (archived), Devtron's per-scope JSON schema with locked keys and approval workflow (Enterprise tag), Backstage/Port/Cortex/OpsLevel JSON-Schema forms that fire a webhook and open a PR with "no diff/preview shown to user beforehand" (slice: platform-engineering-config-uis; slice: gap-closest-competitors-deep-dive).

---

## 3. The feedback-loop problem, with evidence

### 3.1 Failure classes

| # | Class | Typical cause | When detected today | Detectable pre-push? How |
|---|---|---|---|---|
| 1 | YAML syntax / implicit typing | Indentation, `NO`/`ON` coerced to bool, trailing spaces in `include:` (GitLab #388091); KEP-5295 KYAML exists for this | At render or CI lint; sometimes never (silently valid YAML) | Yes — strict YAML parse, KYAML/JSON emit for generated files (slice: devops-pain-points) |
| 2 | Values type/unit errors | `1000M` vs `1000m` CPU, string where int expected; "values.yaml has no type schema, just vibes" (HN) | At `helm install` if schema shipped; else Pod Pending/OOM at runtime | Yes if `values.schema.json`; else infer from defaults + `# @schema` comments, let author enrich (slice: k8s-config-languages; slice: schema-validation-tech) |
| 3 | Rendered-manifest schema (unknown field, wrong type, wrong apiVersion) | Template typo, chart written for older API | SSA dry-run at reconcile (post-merge) or CI kubeconform | Yes — OpenAPI v3 + CRDs from snapshot; native types approximate, CRDs exact (slice: cluster-state-import; slice: gap-k8s-offline-fidelity-facts) |
| 4 | Removed/deprecated API on target minor | Ingress→Gateway, v1beta2 Flux APIs removed in 2.9 | Reconcile error | Yes — discovery document in snapshot (replaces pluto/kubent) (slice: cluster-state-import; slice: k8s-2026-features-and-trends) |
| 5 | CRD CEL rule violation (`x-kubernetes-validations`) | Gateway API / Crossplane / Flux CRs with cross-field rules | Admission at sync | Yes, exact — `apiextensions-apiserver/pkg/apiserver/schema/cel` with server cost budgets (slice: gap-k8s-offline-fidelity-facts) |
| 6 | Admission policy denial (VAP, Kyverno, Gatekeeper, Kubewarden) | Image registry not allowed, missing labels, privileged container; "developers will only discover policy violations after merging" (dev.to, Apr 2026); Argo app OutOfSync/Degraded (CNCF blog, Apr 2026) | Post-merge sync failure | Yes with policy snapshot + synthesized admission context; partial when policy needs `authorizer`, referential data or external HTTP (slice: policy-engines-offline-validation) |
| 7 | Pod Security Admission | Chart written for privileged dev cluster deployed into `restricted` namespace | **Deployment accepted, Pods rejected** — replicas never appear | Yes — `k8s.io/pod-security-admission/policy` + namespace labels; apiserver exemptions not observable (slice: cluster-state-import; slice: gap-k8s-offline-fidelity-facts) |
| 8 | ResourceQuota exceeded | "exceeded quota: cpu-request, requested: 2000m, available: 1000m" | Pods fail at creation; Deployment succeeds | Approximate — `spec.hard − status.used` arithmetic; `status.used` is stale by definition (slice: devops-pain-points; slice: inner-loop-pseudo-cluster) |
| 9 | LimitRange min/max/ratio, defaults applied | Missing requests get defaulted; over-max rejected | Pod admission | Yes — reimplement LimitRanger (mutate then validate, ~200 LOC) (slice: cluster-state-import; slice: gap-go-embedding-and-license-audit) |
| 10 | Missing referenced object (Secret/ConfigMap key, StorageClass, IngressClass, PriorityClass, ServiceAccount) | Typo in `secretKeyRef`, RWX on a class that cannot do it | Runtime: `CreateContainerConfigError`, PVC Pending "silently hangs" | Yes — snapshot inventory of names/keys (never values), classes, CSIDriver capabilities (slice: persona-vendor-to-customer; slice: devops-pain-points) |
| 11 | Image tag nonexistent / registry blocked / unsigned | Typo, unmirrored image in air-gap, ClusterImagePolicy | Runtime `ImagePullBackOff` | Partial — allowlist/IDMS/ITMS offline; tag existence needs registry HEAD when online (slice: gap-deployer-environments-openshift-managed-regulated; slice: docker-compose-landscape) |
| 12 | Controller RBAC | "forbidden: User system:serviceaccount:argocd:argocd-application-controller cannot create resource" | Sync failure | Yes — RBAC snapshot + `RulesAllow` for the deployer identity (slice: cluster-state-import) |
| 13 | Immutable field / SSA managedFields conflict | Selector change, `clusterIP`, PVC storageClass; Helm 4/Flux/Argo/MAP ownership fights | Sync failure | Yes if snapshot carries last-known live object (slice: k8s-2026-features-and-trends; slice: cluster-state-import) |
| 14 | Opaque webhook logic (cert-manager, vendor mutators, GKE Warden) | Unknown | Sync failure; "identifying the blocking webhook is its own debugging exercise" | No — only *which* webhook matches can be predicted; adapters may claim known services (slice: cluster-state-import) |
| 15 | Scheduling / capacity / distribution rules | No node matches taints/selectors, Autopilot CPU:memory ratio, EKS NodePool mismatch | Pending pods | Partial — node snapshot + scheduler framework; distribution profiles (slice: inner-loop-pseudo-cluster; slice: gap-deployer-environments-openshift-managed-regulated) |
| 16 | Wrong effective value across merge layers / env drift | "You need to manually run Helm in your head"; base < env < cluster < substituteFrom < machine-managed | Only after the controller renders | Yes — effective-value explorer with source layer per env (slice: environments-secrets-drift; slice: gitops-controllers) |
| 17 | Compose: unset variable → `""`, port already allocated, bind source missing, `deploy.*` ignored outside Swarm, cross-project `container_name` collision | Typo in `.env`, override path resolved relative to first `-f` file | At `docker compose up` or later | Yes with strict interpolation + host snapshot; `config -q` sees none of it (slice: docker-compose-landscape) |

### 3.2 Feedback-loop timings

- **Git → controller notices:** Argo CD `timeout.reconciliation` 120 s + 60 s jitter (`argocd-cm.yaml`, settled by the gap slice; gitops-controllers had assumed 180 s), ApplicationSet PR generator `requeueAfterSeconds: 1800`; Flux `spec.interval` (commonly 1–10 min; docs recommend 60 m in production; effective landing = GitRepository interval + Kustomization interval, ~11 min at 1 m/10 m), worst case 2× `spec.timeout`; hydrator failures retry ~2 min (slice: gap-k8s-offline-fidelity-facts; slice: devops-pain-points; slice: k8s-2026-features-and-trends).
- **PR → CI green:** "30–60 minutes waiting for CI" per PR (HN, Jan 2026); LeadDev's merge-train case: median merge-to-main <20 min after optimisation vs half a day before; 3–5 pushes per PR producing 2–4 redundant runs (slice: devops-pain-points).
- **Preview diff:** argocd-diff-preview 60–90 s on an ephemeral kind cluster vs <10 s against pre-installed Argo CD; vcluster ~62 s; kind/k3d tens of seconds; "when you have multiple clusters, it's unclear which cluster you should pick" (slice: inner-loop-pseudo-cluster; slice: gitops-controllers).
- **Outcome:** Komodor 2025 (thousands of incidents): 79 % of production issues originate from a recent change, median MTTD ~40 min, MTTR >50 min, 38 % of companies see high-impact outages weekly; DORA 2025 added Rework Rate and found AI adoption correlates with *more* change failures. Net: edit-to-"known wrong" is 10 min to hours; known-to-rolled-back adds ~50 min (slice: devops-pain-points).

### 3.3 What practitioners already do, and why it is insufficient

The incumbent answer is a pre-commit/CI stack: yamllint + kubeconform (+ datreeio/CRDs-catalog) + `kustomize build`/`helm lint` + conftest/`kyverno apply`, sometimes `helm diff` or Argo SSD, increasingly the rendered-manifests pattern. Each is explicitly scoped: kubeconform's README says controllers "perform additional server-side validations not part of the OpenAPI specifications"; kubectl-validate calls native OpenAPI "a best-effort replication"; `kyverno apply` needs `--values-file`/`--context-file` mocks and in v1.12.x exited 0 on `FAIL` (making CI green on violations); gator "cannot determine if a type is Namespace-scoped" and its CEL engine has no referential data; server-side dry-run needs credentials and RBAC for the real verb, skips side-effect webhooks and cannot see controllers ("a Deployment passes while its Pods are rejected by PodSecurity or quota"). Typed languages (CUE/Timoni/Holos) fix the values half but meet "colleagues are reluctant to adopt" and require rewriting charts. Nobody snapshots the cluster's constraints into a file a values editor can consult (slice: devops-pain-points; slice: policy-engines-offline-validation; slice: cluster-state-import; slice: k8s-config-languages).

---

## 4. Landscape by category

### 4.1 GitOps controllers and promotion

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| Argo CD | v3.5.2 2026-08-27; Helm 4.2 + Kustomize 5.8 | Helm valueFiles/valuesObject/parameters, Kustomize patches, CMP params; imperative `argocd app set` overrides drift | Sync-time dry-run; SSD stable since 3.1.0, opt-in, skips new resources | `argocd app diff --local --server-side-generate` needs live Argo + cluster; REST `manifestsWithFiles`, `server-side-diff`, sync `{dryRun}` | CMP sidecars (painful, #15006), UI extensions, Lua, Notifications | Integrate: read CRs to locate values, optional live check, PR into DRY source |
| Argo Source Hydrator | Beta since 3.5.0 (GA tracked in #28143); git notes since 3.3 | Values stay DRY; hydrated YAML on `syncSource`/`hydrateTo` branch | Rendering only; no secrets during hydration; deterministic | Hydrated branch is a lintable artifact; Git-only, no OCI | Write-creds Secret, GPG signing Secret | Validate the `hydrateTo` branch; copy `hydrator.metadata` provenance |
| Flux | v2.9.5 2026-08-31; K8s 1.34–1.36; Helm 4 + SSA default since 2.8 | HelmRelease `values/valuesFrom`, Kustomization `postBuild.substitute(From)`, OCI sources | SSA dry-run each interval; drift detection; CEL health | `flux build kustomization --dry-run` offline; `flux diff kustomization` needs live cluster; **no** `flux diff helmrelease` | CRDs only in controllers ("no user code executes"); 2.9 CLI plugin system, OCI binaries by digest | Fill HelmRelease-diff gap; copy OCI plugin distribution and PR-comment providers |
| Flux `schema` plugin + Ecosystem Schema Catalog | flux-schema v0.13.0 2026-09-09 (Apache-2.0); catalog AGPL-3.0, ~9 000 schemas, daily | n/a | Structural schema + CEL via `apiextensions-apiserver` code (parity claim verified in source), strict YAML, list-map topology | Fully offline, pinned to K8s minor; `extract crd` from `kubectl get crds -o yaml` | Flux CLI plugin; `.fluxschema.yml`; custom catalogs | Closest OSS overlap on the schema half; no VAP/MAP/Kyverno/PSA/quota/UI/Git |
| Kargo (OSS) | v1.11.4 2026-09-03; v1.12 REST-only | Edits values files via `yaml-update`, `kustomize-set-image`; `git-open-pr` | Post-promotion verification only; no validation step | No | `PromotionTask` composes built-in steps; **custom container steps are Enterprise-only** (`ee.kargo.akuity.io/v1alpha1`, Akuity Platform ≥ v1.10) | Complement: emit PRs Kargo consumes; offer an `http` step target |
| gitops-promoter | v0.38.1 2026-09-10, experimental | Hydrated manifests per env branch | External CommitStatus gates | Provides the hook only | Write a CommitStatus controller | Cleanest insertion point on the hydrated path |
| Argo Rollouts / Flagger | v1.10.0 2026-08 / v1.45.0 2026-09-01 | n/a | Post-deploy metrics; abort rolls back in-cluster, Git untouched | No | go-plugin (Rollouts), webhooks (Flagger) | Downstream only; warn about Git↔cluster divergence |
| Weave GitOps | Dormant; last stable v0.38.0 2023-12-06, RCs only since | n/a | Former Rego policy, unmaintained | No | n/a | Cautionary: "the UI for Flux" died with Weaveworks (Feb 2024) |
| ConfigHub | Active SaaS + self-hosted Enterprise (closed server, MIT CLI/SDK); $4 M seed 2025 | Config Units in a DB; **no Git write**, OCI-pull delivery only since Aug 2026 | `vet-schemas` (kubeconform), `vet-celexpr`, Triggers as gates | Server-side on edit; no admission/quota import | Go SDK functions, Workers, `cub plugin` | Competes for source of truth; zhi differentiates by Git-native + snapshot |

The controllers are closed and declarative; their validation is a server-side dry-run *after* merge, and their own docs say so. The action for a pre-push tool is at the CLI/CI edge and in the PR: read the controller CRs to discover which file and merge layer a value lives in, validate the rendered output, write a PR the promotion layer (Kargo, gitops-promoter, Flux ResourceSets) already consumes, and subscribe to controller status afterwards. Kargo's OSS/Enterprise split is now settled: custom container steps are Enterprise-only, so "zhi as a Kargo step" is an Enterprise-customer integration, while "zhi opens the PR Kargo's `git-wait-for-pr` waits on" works in OSS (slice: gitops-controllers; slice: gap-closest-competitors-deep-dive).

### 4.2 Config languages and packaging

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| Helm 4 / 3 | v4.3.0 2026-09-09; v3.22.0 2026-09-10 last v3 feature release | Untyped values.yaml + `-f/--set/--set-json`; optional `values.schema.json` | `santhosh-tekuri/jsonschema/v6` (drafts 4–2020-12; **2020-12 default without `$schema`**); merged `.Values` vs all subchart schemas; `helm lint` | `--dry-run=client` offline; `--dry-run=server` needs cluster; Capabilities faked via `--kube-version/--api-versions` | `plugin.yaml apiVersion v1`: cli/getter/postrenderer types, subprocess or extism/v1 Wasm, signed by default | Embed `helm.sh/helm/v4` SDK (HIP-0004 stability); copy typed plugin kinds |
| Kustomize | v5.8.1 2026-02-09, quiet | None — overlays, components, patches, replacements | Structural only; KRM functions alpha-gated for years | Build offline; no constraint validation | KRM ResourceList (exec/container), never graduated | Embed `krusty` in-process; map `components` |
| Timoni | v0.34.0 2026-08-30 (per three slices; persona slice's v0.29.0 is superseded); bus factor 1, "APIs may change" | Typed CUE `#Config`, bundles per env | `timoni mod vet` validates against vendored CRDs incl. CEL | Schema/CRD yes; policies/quota no | None; CUE modules via OCI | Copy CRD vendoring and `mod show config`; do not build on |
| CUE | v0.17.1 2026-07-16; Go API concurrency-safe | Typed lattice, defaults, cross-field constraints | `cue vet`; JSON Schema/OpenAPI round-trips; `cue get crd` | Fully offline | `cuelang.org/go` (Apache-2.0) | Optional import/export engine; see contradiction in §10 |
| KCL / Pkl / Jsonnet-Tanka / cdk8s | KCL cli v0.12.10 (core stalled at v0.11.2); Pkl 0.32.1; Tanka v0.39.0; cdk8s daily | Typed (KCL/Pkl), untyped (Jsonnet), code (cdk8s) | Compile-time types; cluster-only diff (Tanka) | KCL needs CGO; Pkl spawns a JVM binary; cdk8s needs Node | Various | Subprocess adapters at most; excluded from a `CGO_ENABLED=0` core |
| Carvel ytt | v0.55.2 2026-08-14, maintenance | `@data/values-schema` with validations | Blocking-only rules with messages; `--data-values-schema-inspect -o openapi-v3` | Yes | `@library` | Copy schema export idea; support as form source |
| Nelm / werf | Nelm v1.26.2 stable; werf v2.78.x 2026-09-09 | Helm values + secret values | `nelm release plan install` (SSA dry-run, stricter than Helm); giterminism | Plan needs cluster; lint offline | Go `pkg/action` | Best "plan before push" UX; copy giterminism as a product rule |
| kpt / KRM functions | kpt v1.0.0 2026-08-31 | Setters, functionConfig | Validator functions with `results[].severity` | Pipeline offline (Docker for containers) | KRM Functions Spec | Adopt `results[]` shape as wire format |
| helm-values-schema-json / helm-schema / helm-cel | losisin v2.6.0, dadav 0.23.5 (both 2026-08-24); helm-cel MIT | `# @schema` comment annotations; `values.cel.yaml` | Generates 2020-12 schemas; CEL rules over values | Offline | Helm plugins | Copy annotation vocabulary; adopt CEL-over-values pattern |

Helm remains the gravitational centre and Helm 4's changes are in the deploy path (SSA, kstatus waits, OCI-by-default, Wasm plugins) plus one quiet values-path change: the JSON Schema library. Typed languages are mature but niche and each brings an embedding cost (CGO, JVM, Node). The practical conclusion is to *consume* every schema format the ecosystem produces (values.schema.json, `# @schema`, ytt, Timoni `#Config`, kro SimpleSchema, XRD schemas) into one internal JSON-Schema-2020-12 model, and never ask a team to migrate its charts (slice: k8s-config-languages; slice: schema-validation-tech; slice: devops-pain-points).

### 4.3 Policy engines and offline validation

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| VAP / MAP (native) | VAP GA 1.30; MAP GA 1.36 (`v1`), stored as v1 in 1.37; manifest-based admission beta 1.37 (`ManifestBasedAdmissionControlConfig`) | paramKind objects via bindings | CEL over `object/oldObject/request/params/namespaceObject/authorizer`; Deny/Warn/Audit | `k8s.io/apiserver/pkg/admission/plugin/policy/{validating,mutating}` + `plugin/cel`; Gatekeeper's `k8scel` driver is the proven informer-free pattern | In-tree | Semantic target of the pseudo cluster; static `.static.k8s.io` policies are invisible to the API by design |
| Kyverno + CLI | v1.19.1 2026-09-10; ValidatingPolicy GA in **1.17** (2026-02-02); ClusterPolicy deprecated, removal in 1.20 (~Nov 2026) | Policies as CRs; params via ConfigMap/CEL | CEL types + legacy JMESPath; Deny/Audit/Warn | `kyverno apply` fully offline: `--context-file` (kind `Context`, `spec.resources`, `apiCallResponses`, `globalContextEntries`), `--parameter-resource` for VAP/MAP params, `--userinfo`, `--policy-report`; in-memory dynamic client from supplied manifests | None (14 fixed CEL libraries) | Primary third-party engine; shell out, generate side files from the snapshot |
| Gatekeeper + gator | v3.23.1 2026-08-27; VAP/VAPB generation beta + on by default since **v3.20** (needs K8s ≥1.30) | Constraints = params | Rego or K8sNativeValidation CEL; deny/warn/dryrun | `gator test/verify/expand/sync test`; inventory files; CEL engine has no referential data; objects need `metadata.namespace` | External data (HTTP), OCI bundles; `frameworks/constraint` Go client | Prefer consuming the *generated* VAP/VAPB; embed `k8scel` driver + OPA for Rego templates |
| Kubewarden / kwctl | v1.37.2 2026-08-17; kwctl repo archived 2026-01-19, moved into monorepo | Settings JSON; cel-policy mirrors VAP | Wasm `validate(request, settings)` | `kwctl run --request-path AR.json`, `--record/--replay-host-capabilities-interactions` | Sigstore-signed Wasm from OCI (Rust host) | Shell out; copy record/replay as per-request snapshot |
| PSA (+ psa-checker) | Stable 1.25 | Namespace labels | Fixed checks per minor | `k8s.io/pod-security-admission/policy` `EvaluatePod` — no client needed; apiserver exemptions not discoverable (no `/configz` on kube-apiserver) | In-tree | Embed; model exemptions as declared assumptions with vendor presets |
| kubeconform | v0.8.0 2026-06-04 (settled: 2026, not 2024) | n/a | JSON Schema only, no CEL/defaults | Yes with schema dirs; CRDs via openapi2jsonschema/CRDs-catalog | Go `pkg/validator` | Floor to exceed; emit a kubeconform-compatible schema dir |
| kubectl-validate | Last tag v0.0.4 (2024-05-29); main tracks k8s 1.35 (merged 2026-01-05), builtins 1.23–1.35; Kyverno consumes a 2026-01 pseudo-version | n/a | Real `customresource.NewStrategy` + `rest.BeforeCreate`: structural + defaulting + **CRD CEL (confirmed in source)** | `--local-schemas` (`/api/<v>.json`, `/apis/<g>/<v>.json`), `--local-crds` | Go lib `pkg/validator`, `pkg/openapiclient` composite | Embed by pseudo-version; its layout is the snapshot's schema layer |
| Conftest / OPA | v0.69.0 2026-08-03; OPA v1.20.2 2026-09-03, pure Go since 1.19 | Arbitrary files | Rego deny/warn; SARIF | Yes; no K8s semantics | OPA embeds cleanly | Generic Rego for Compose/env; Gatekeeper Rego templates |
| kube-linter / Polaris / Datree | v0.8.3 2026-03-10 / v10.2.2 2026-08-10 / archived 2024-06-06 (company closed 2023-07) | Opinionated checks | Static | Local | Compiled templates | Copy template+params authoring; Datree is the cautionary SaaS tale |

The centre of gravity is "CEL over an admission-shaped request", with every third-party engine re-platformed on CEL and Kubernetes 1.37 making "policies as files on disk" official. The offline evaluators exist but each needs hand-assembled context (Kyverno `Values/UserInfo/Context`, gator inventory + AdmissionReview wrappers, kwctl replay files) that nobody generates from a cluster — that generation is precisely the importer this tool should own. Go embeddability is good where it matters (apiserver packages, OPA, Gatekeeper's driver) and bad for Kyverno (fork replace, CVE cadence) — hence subprocess isolation for Kyverno and kwctl (slice: policy-engines-offline-validation; slice: gap-go-embedding-and-license-audit; slice: gap-k8s-offline-fidelity-facts).

### 4.4 Cluster-state import and pseudo clusters

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| KWOK / kwokctl | v0.8.0 2026-06-23; apiserver default 1.36.1, supports 1.31–1.36; admission on by default | n/a | Real apiserver + scheduler on fake nodes | `snapshot export --path` (experimental) — **default filter excludes CRDs, VAP/MAP, webhooks, ResourceQuota, classes**; `--filter` extends; restore re-links ownerReferences and restores status | Stages (CEL/go-template) | Optional high-fidelity backend; do not adopt its export as the snapshot format |
| envtest (controller-runtime) | v0.25.0 2026-09-03; binaries per minor (`KUBEBUILDER_ASSETS`) | n/a | Real apiserver; no controllers/scheduler/GC | `APIServer.Configure()` accepts any flag (static admission config mechanically possible, UNVERIFIED); 20 s default start timeout | Go library | Lighter in-process alternative when scheduling is not needed |
| `kubectl --dry-run=server` / `diff --server-side` / `helm --dry-run=server` / `flux diff` / Argo SSD | Core GA; Helm 4.3.0; Flux 2.9; Argo SSD stable 3.1 | n/a | Full chain incl. SSA conflicts; webhooks only with `sideEffects: None|NoneOnDryRun`; blind to controllers, scheduling, referenced objects | Needs target apiserver + RBAC for the real verb | n/a | Fidelity ceiling; final optional tier |
| datreeio/CRDs-catalog + crd-extractor | Catalog active (commits 2026-09-08); extractor bash+python, lossy JSON Schema | n/a | Feeds kubeconform | Needs cluster once | n/a | Store raw CRDs instead; catalog as fallback |
| pluto / kube-no-trouble | pluto v5.24.3 2026-08-10; kubent dormant (rules to 1.32) | n/a | Static deprecation tables | Yes | none | Subsumed by a discovery-document check |
| kube-scheduler-simulator / cluster-capacity | dormant (v0.4.0 2024-11) / v0.30.0 2024-05 with Red Hat commits 2026 | n/a | Scheduling feasibility | Simulator imports a cluster; cluster-capacity is live-only | Scheduler plugins | Reference for an optional node layer |
| Troubleshoot.sh (Replicated) | v0.134.0 2026-09-04, Apache-2.0 | n/a | 25+ analyzers on a collected bundle (clusterVersion, storageClass, CRD, nodeResources, ingress, imagePullSecret, distribution…) | `analyze --bundle <tgz> <spec>` runs offline on a saved bundle | Go library; `runPod`/`http` collectors | Embed analyzers as the environment-requirement vocabulary; collectors = snapshot |
| Upstream Go packages | k8s.io/* v0.37.0 2026-08-26, no replace directives; `k8s.io/kubernetes` needs 33 replaces | n/a | `pod-security-admission/policy`, `apiserver/.../resourcequota.CheckRequest`, `plugin/cel`, `policy/validating`, `apiextensions customresource.Strategy`; LimitRanger/RBAC/quota evaluators only in `k8s.io/kubernetes` | In-memory | Go | Embed the staging modules; vendor-fork the three in-tree packages |

Five layers of decreasing offline emulability: (L1) API surface and schemas — aggregated discovery, `/openapi/v3` (lossless, hash-addressed), raw CRDs; (L2) built-in admission plugins — deterministic given ResourceQuota, LimitRange, PSA labels, default StorageClass/IngressClass annotations, PriorityClass/RuntimeClass existence; (L3) CEL policies — pure functions except `authorizer.*`; (L4) webhooks — configs predict *which* request hits *whom*, logic opaque; (L5) authorization and live state — RBAC rules evaluable, existing resources, node capacity, `status.used`. Nobody ships a "pseudo cluster" as a file format; the closest substrates are `kwokctl snapshot export` (whose defaults omit exactly the objects that matter) and envtest (slice: cluster-state-import; slice: gap-k8s-offline-fidelity-facts).

### 4.5 Docker Compose and single-host

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| compose-go v2 | v2.15.0 2026-09-03, Apache-2.0; used by Compose, nerdctl, kompose, Tilt, Uncloud, Docker LSP | `${VAR:-def}/${VAR:?err}` interpolation, shell > `--env-file` > `.env`; overrides, profiles, include, extends, `x-*` | 2020-12 schema + ~28 consistency checks; unset vars silently `""` | Fully offline, no daemon; no port/bind/image/capacity knowledge | Library: `WithExtension`, `ResourceLoaders`, custom `SubstituteFunc`, `WithImagesResolved` | **The** Compose model; embed |
| Docker Compose CLI v5 | v5.5.1 2026-09-03; v5.2 reconciliation, v5.5 digest reconciliation, config hashing | `config --variables/--hash/--lock-image-digests` | `config -q` full pipeline; `--dry-run` needs daemon | `config -q` offline only | Provider services (`docker-<type>` exec, JSON-lines protocol, `metadata` param schema); Bridge transformer images | Target runtime; copy provider protocol; `publish` → OCI |
| Portainer CE/BE | 2.45.0 (STS per GitHub/docs numbering; one slice labels it LTS — LTS is 2.39.7) 2026-08-27 | Env vars in UI; App Template `env` schema (`label/description/default/preset/select`) | Errors at deploy; BE-only host security policies at API time (not exportable) | None | REST, webhooks | Compete + import via API; copy `env` schema and policy dimensions |
| Komodo | v2.3.3 2026-09-01, Rust, **GPL-3** | TOML `[[stack]]` + `environment` → `.env` | Resource diff before sync; Compose not validated | No | TS Actions, REST | Closest "values in Git + diff"; integrate by emitting TOML+compose+.env; never embed |
| Coolify / Dokploy | v4.3.19 / v0.30.6 (Sep 2026) | Magic vars (`SERVICE_FQDN_*`), `template.toml` helpers (`${password:32}`) | None pre-deploy; rewrite Compose | No | None | Hobby/SMB PaaS; typed helpers are a values-kind data point |
| Uncloud | v0.20.0 2026-06-26, pre-1.0, single maintainer | Compose + `x-ports/x-caddy/x-machines` | compose-go + plan diff vs cluster | Plan needs cluster | None | Plan UX is the bar; apply target |
| Podman / Quadlet | v6.1.1 2026-09-02; podman-compose v1.6.0 (GPL-2, partial) | systemd units, `kube play --validate ignore|warn|strict` | Generator `--dryrun`, `systemd-analyze verify` | Offline schema-ish | None | Secondary flavour flag; exporter post-interpolation |
| Watchtower / WUD, DCLint, Docker Language Server | containrrr archived 2025-12-17; DCLint v3.1.0 (2025); LSP v0.20.1 | Labels / raw YAML | 15 style rules; edit-time schema via compose-go | Offline | JS rules / LSP | Anti-pattern (drift), rule ideas, proof schema drives edit UX |

Compose has no controller, no admission layer and no promotion or drift story; GitOps-for-Compose is real but fragmented and UI-first (Portainer Git stacks, Komodo ResourceSync, CI over SSH). The whitespace is identical in shape to Kubernetes: a host/fleet snapshot (Engine/Compose versions, listening ports, networks/volumes/containers, images with digests, bind-source existence, cgroup/rootless flags, swarm/podman) plus a policy pack over the normalized compose-go JSON, checked before push, with `.env`/`compose.<env>.yaml` written back so plain `docker compose` still works (slice: docker-compose-landscape; slice: k8s-2026-features-and-trends).

### 4.6 IDPs, config UIs and vendor-to-customer tooling

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| Devtron | v2.2.0 2026-07-21, Apache-2.0 open-core; Freemium $0 → Enterprise | Per-scope JSON schema GUI ↔ YAML, base + env overrides, locked JSONPath keys (Enterprise), approval drafts | Schema + Dry Run manifest preview; **no policy engine** | Server-side only inside Devtron | CI-stage plugins, not validators | Strongest living OSS incumbent; copy schema-per-scope; it owns the manifest repo (no PR to user's repo) |
| Cyclops | v0.21.1 2026-06-26, 2-month gap, roadmap basics unshipped | Module CR from `values.schema.json` | JSON Schema at edit | None; in-cluster controller | Templates from Git/OCI | Closest OSS "UI with validation"; Git write is direct commit, PR flow UNVERIFIED |
| Backstage Scaffolder / Port / Cortex / OpsLevel | Backstage v1.54.7 2026-09-11; SaaS portals active | JSON Schema forms + `ui:*`, jq/CEL conditionals, secrets masked | Form-local (pattern/enum/async validators) | None; "no diff/preview shown to user beforehand" (Port) | npm field extensions; webhooks | Integrate as field extension / webhook target; never previews manifests |
| Replicated KOTS + Troubleshoot + Distr | KOTS v1.130.2 2026-05; Distr OSS Apache-2.0 | KOTS `Config` (typed form DSL, `when`, regex) → Helm values; Distr per-target values | Preflight analyzers fail/warn/pass + `strict` against target cluster at install | Analyzers run offline on a bundle | Custom collectors/analyzers (Enterprise), `runPod` | Copy the Config+Preflight split; Distr is the OSS BYOC shape without validation |
| Rancher / OLM / Glasskube (archived 2026-06-17) / Kubeapps (archived 2025-08-25) | Rancher dashboard v2.15.1 2026-08-28 | `questions.yaml` with cluster-aware pickers; CSV `x-descriptors`; `valueDefinitions.targets` | UI constraints only | None (live pickers) | Chart-level | Copy cluster-aware field types and schema→chart-path `targets` |
| Crossplane / Kratix / KubeVela / Score | Crossplane v2.4.0 2026-08-20; score-k8s 0.18.0 | XRD openAPIV3Schema + CEL; Promise CRD; CUE→UI schema; Score `resources` | Admission on platform cluster | `crossplane composition render` + `resource validate` — best-in-class offline | gRPC functions as OCI | Copy render→validate; ingest XRD/Score as schema sources |
| Headlamp / Freelens / Helm Dashboard / k9s | v0.45.0 / v1.10.3 / v2.1.3 / v0.51.0 | Imperative YAML | apiserver on apply | None | TS plugins; `plugins.yaml` shell commands (k9s) | Integrate as panel; k9s is the cheapest viable plugin model |
| RJSF / JSON Forms | v6.10.0 2026-09-09 / v3.8.0 | formData + `ui:*` / UI schema with SHOW/HIDE rules | ajv8; `extraErrors` for server findings | Client-side | Widgets/renderers | Use; `extraErrors`/ErrorSchema is the target shape |

"Configure a deployment through a form" has been solved many times, always inside a bigger product and always with only schema validation. The K8s-UI category is consolidating and imperative (Kubernetes Dashboard archived 2026-01-21 pointing to Headlamp; Lens dormant); form-first K8s UIs die before shipping GitOps write-back. Prior post-mortems agree on the causes: value locked in a SaaS (Datree), validation outside the push path (Monokle), cluster-attached UI that cannot write back (Kubevious, Kubeapps), enterprise UI on a free controller (Weave GitOps) (slice: platform-engineering-config-uis; slice: gap-closest-competitors-deep-dive; slice: persona-vendor-to-customer).

### 4.7 Secrets and multi-environment

| Tool | Status (verified) | Values model | Validation | Pre-cluster validation | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| ESO | v2.10.0 2026-08-28, stable `external-secrets.io/v1`, 40+ providers | `ExternalSecret{secretStoreRef, data[].remoteRef{key,property,version}}` | CRD schema; `SecretSyncedError` after apply | Manifest yes; key existence needs provider | Providers compiled in; Webhook escape hatch | Reference-in-Git model; snapshot includes `(Cluster)SecretStore` inventory |
| SOPS | v3.13.3 2026-07-23; Flux-native decryption | Encrypted leaves in Git; `.sops.yaml` rules | Structure visible, values opaque | Yes with key; Compose-friendly (`exec-env`, dotenv output) | gRPC keyservice | Only model identical for Compose and K8s |
| Sealed Secrets | v0.40.0 2026-09-10; repo moved to `bitnami/`; decryption-oracle fixes in 0.39/0.40 | Ciphertext per cluster cert | Strict fails at apply | Seal offline with cert | None | Import cert with snapshot; seal on save |
| Vault / VSO / argocd-vault-plugin | Vault v2.1.0 (BUSL); VSO v1.5.1 (BUSL); AVP dormant since 2024 | Refs (mount, path, version); AVP `<path:…#key>` placeholders | CRD | No | go-plugin (Vault), CMP (AVP) | `hashicorp/vault/api` client stays MPL — fine to embed; recognise AVP placeholders on import |
| Helmfile / Kustomize Components / ApplicationSet / Flux substitution | Helmfile v1.7.4; Components since v3.7.0; matrix ≤2 generators | Per-env values + precedence lists | None env-aware | Template offline | Helm plugins | Copy precedence and `${VAR:=default}` strictness |
| Flux image automation / Argo Image Updater / Renovate / Dependabot | v1 API / v1.3.0 2026-08-13 / 44.82.3 / active | Marker comments; `.argocd-source-<app>.yaml`; in-place edits | None | n/a | Regex/JSONata managers | Machine-managed fields read-only in UI; comment-preserving YAML mandatory |
| Argo SSD / selfHeal / Flux driftDetection | SSD stable 3.1; `spec.driftDetection.mode` | n/a | Live | No | n/a | Drift as read-only third state; correlate hydrated SHA ↔ dry SHA ↔ author |

Multi-env values are layered by three unchanged mechanisms (Helm file stacking, Kustomize overlays, controller substitution) and promotion consensus in the Argo world is the hydrated-branch pattern; on the Flux side it remains DRY-repo-centric. Every "valid for *this* cluster" check needs a live API server; secret references are unverifiable at review (ESO typos, Bitwarden UUIDs); machine edits break comments (Image Updater fixed "preserved YAML formatting" only in 1.3.0). Docker Compose has zero equivalents (slice: environments-secrets-drift).

### 4.8 Schema and validation libraries for Go

| Library | Status (verified) | Values model | Validation | Offline | CGO / replace | Relevance |
|---|---|---|---|---|---|---|
| `santhosh-tekuri/jsonschema/v6` | v6.0.3 2026-06-28, Apache-2.0; Helm 4, compose-go, kubeconform | any/map | Drafts 4→2020-12; `InstanceLocation`+`KeywordLocation`; Flag/Basic/Detailed output; custom vocabularies, formats, loaders | Yes | none | Primary validator; parity with Helm 4 and Compose |
| `kubectl-validate` `pkg/validator` + `pkg/openapiclient` | main (pseudo-version 2026-01), Apache-2.0 | Unstructured | Structural + defaulting + list-type invariants + CRD CEL via apiserver code; `field.ErrorList` paths | Yes (`NewLocalSchemaFiles/NewLocalCRDFiles/NewComposite/NewOverlay`) | none | Embed; snapshot layout |
| `k8s.io/apiserver` + `apiextensions-apiserver` + `pod-security-admission` | v0.37.0 2026-08-26 | Admission attributes / structural | VAP/MAP validators, `plugin/cel`, `cel/environment.MustBaseEnvSet(version)`, `schema/cel.NewValidator` with server cost constants, PSA `EvaluatePod` | Yes | none | Core of Tier 0; pin latest minor, warn on skew |
| `k8s.io/kubernetes` (limitranger, rbac, quota evaluators) | v1.37.0 | API types | Pure functions | Yes | **33 replaces** | Vendor-fork ~2 k LOC |
| cel-go | `cel.dev/cel-go` v0.32.0 2026-08-19; k8s.io still pins `github.com/google/cel-go v0.29.2`; Kyverno v0.31.0 | Activations | Type-check, cost estimation | Yes | none | Import only through k8s.io packages to avoid two runtimes |
| OPA (`v1/rego`) + `frameworks/constraint` + Gatekeeper `k8scel` | OPA v1.20.2 (wazero since 1.19); frameworks pseudo 2026-09-08; Gatekeeper v3.23.1 | input + data | Rego; `k8scel` reuses apiserver validating packages | Yes | none | Embed for Gatekeeper clusters |
| Kyverno Go module | v1.19.1 | — | `pkg/cel/policies/vpol/engine.NewEngine` is interface-only | Yes in principle | PSA fork replace; ~416 requires; 10 GHSAs in 2026 | **Shell out** to the CLI |
| CUE `cuelang.org/go` | v0.17.1 | CUE | Cross-field native; JSON Schema/CRD import | Yes | none | Optional |
| KCL (`kcl-go`) / Pkl (`pkl-go`) / Starlark | v0.12.5 / v0.14.0 / untagged | — | — | — | **CGO** / external JVM binary / n/a | Excluded from core |
| compose-go v2, kustomize `api/krusty`, `helm.sh/helm/v4` | v2.15.0 / v0.21.1 / v4.3.0 | native | native | Yes | none | Renderers in-process |
| wazero / Extism go-sdk / dyff / Troubleshoot | v1.12.0 2026-05-28 / v1.7.1 **2025-03-02** (18 months no tag) / v1.12.0 / v0.134.0 | — | — | Yes | none | wazero directly with a zhi-owned ABI; embed dyff and Troubleshoot analyzers |
| yaml-language-server + SchemaStore | 1.24.0 2026-07-07; catalog Apache-2.0 | Plain YAML | Drafts 04–2020-12 via `# yaml-language-server: $schema=` modeline | Yes | n/a | Emit schema + modeline for free editor integration |
| xeipuuv/gojsonschema | Dead (2019, draft-07) | — | — | — | — | Avoid |

Draft 2020-12 is the interchange format; the canonical error record is the 2020-12 output unit (`instanceLocation` + `keywordLocation` + message) into which apiserver `field.ErrorList`, CEL `fieldPath`, Kyverno PolicyReport results and Rego violations (which carry no pointer unless the policy emits one) are normalised (slice: schema-validation-tech; slice: gap-go-embedding-and-license-audit).

---

## 5. The "pseudo cluster" concept

### 5.1 What can be snapshotted from a cluster

Read-only, one pass with client-go discovery + dynamic client, degrading gracefully per layer ("quota unknown: no list on resourcequotas") (slice: cluster-state-import; slice: policy-engines-offline-validation; slice: environments-secrets-drift; slice: inner-loop-pseudo-cluster):

1. **Provenance:** cluster identity, kube-context, server version (`major.minor.patch`), capture timestamp, capturing identity, per-layer hash/resourceVersion; signed.
2. **Discovery:** APIGroupDiscoveryList (preferred versions, namespaced-vs-cluster scope per GVK, deprecations) — replaces pluto/kubent and feeds Helm `Capabilities`.
3. **OpenAPI v3** per group/version in kubectl-validate layout (`/api/<v>.json`, `/apis/<g>/<v>.json`), keyed by `?hash=` for incremental refresh.
4. **Raw CRDs** (keep `x-kubernetes-validations`, defaults, pruning, ratcheting flags); derive JSON Schema only on export.
5. **Admission policies:** VAP + VAPB, MAP + MAPB (v1 on ≥1.36, v1beta1/v1alpha1 fallback on 1.34/1.35), their paramKind objects; Validating/MutatingWebhookConfiguration *metadata* (rules, selectors, matchConditions, failurePolicy, sideEffects) marked "cannot evaluate offline".
6. **Engine policies:** Kyverno ClusterPolicy/Policy + `policies.kyverno.io/v1` types + PolicyExceptions; Gatekeeper ConstraintTemplates/Constraints + Config/SyncSet (and the generated VAP/VAPB); Kubewarden (Cluster)AdmissionPolicy(+Group) settings; engine chart versions.
7. **Namespaces** with labels/annotations (PSA `enforce/audit/warn[-version]`, OpenShift `openshift.io/sa.scc.*` ranges, Rancher project annotations), plus ResourceQuota (spec + `status.used`), LimitRange, default NetworkPolicies, ClusterResourceQuota (OpenShift).
8. **Cluster catalogs:** StorageClass (+ CSIDriver capabilities, default annotation), IngressClass, GatewayClass, PriorityClass, RuntimeClass, SCCs (OpenShift), ComputeClass/NodePool/NodeClass (GKE/EKS Auto Mode).
9. **RBAC:** Roles/ClusterRoles/bindings, plus SelfSubjectRulesReview for the deploying identity (Argo/Flux service account).
10. **Referential inventory (allowlisted):** existing Secret/ConfigMap *names and keys* (never values), ServiceAccounts, Services/Ingress/Routes (host collisions), existing workloads for `oldObject` and immutable-field diffs, Helm `lookup` targets.
11. **GitOps inputs:** Argo Applications/ApplicationSets (cluster-Secret labels), Flux Kustomizations/HelmReleases (`substituteFrom` ConfigMaps redacted-but-keyed), `(Cluster)SecretStore` names/providers, Sealed Secrets cert, Kargo Stages.
12. **Optional:** Nodes (allocatable, labels, taints), per-node aggregates or Pods (requests, nodeName), image digests/tags reachable via registry.
13. **Declared, not observed** (operator-supplied overlay): PSA `AdmissionConfiguration` defaults/exemptions (no API exposes them; EKS/GKE default privileged with no exemptions; OpenShift defaults readable from operator bindata), `.static.k8s.io` manifest-based policies (invisible by design in 1.36/1.37), webhook stubs, Rancher PSACT template, distribution profile id.

Compose analogue: Engine/API version, OS/arch, cgroup version, rootless/userns, seccomp/apparmor defaults, address pools, CPU/mem/GPU, listening host ports, existing networks/volumes/containers/projects, images with digests, registry reachability, bind-source existence, swarm/podman flags; same shape via Portainer API and Komodo Periphery (slice: docker-compose-landscape).

### 5.2 Fidelity tiers, start-up cost and credentials

| Tier | Mechanism | Fidelity (what it sees) | What it misses | Start-up / latency | Credentials |
|---|---|---|---|---|---|
| **T0-a Pure schema** | JSON Schema over values; OpenAPI/CRD structural via kubectl-validate lib; discovery | Types, required, enum, unknown fields, list-map topology, removed APIs; CRDs incl. CEL near-exact | Native-type `+k8s:` rules, immutability, unions, cross-field (KEP-5073 not published to OpenAPI); all admission | ms; none beyond snapshot on disk | None at validate time |
| **T0-b Schema + policy engines in-process/CLI** | MAP then VAP via `k8s.io/apiserver` with snapshot params/namespaceObject/RBAC-backed authorizer; PSA lib; LimitRanger + quota arithmetic; Kyverno CLI with generated Context/Values/UserInfo; Gatekeeper `k8scel`/OPA with inventory; kwctl replay | Policy denials incl. parameterised; PSA on expanded Pod templates; quota headroom; RBAC of controller SA; mutated-object diff | Opaque webhooks; live `status.used`; external HTTP/image metadata lookups unless mocked; PSA exemptions; static policies; Gatekeeper referential CEL | 10s–100s ms (estimate); Kyverno subprocess adds process start | None |
| **T1 Real apiserver without kubelet** | envtest (apiserver+etcd) or kwokctl binary runtime hydrated from snapshot, webhooks rewritten to Ignore/stubbed, `--admission-control-config-file` for static policies (untested) | Exact built-in admission for the minor, defaulting, quota consumption (KWOK also schedules on fake nodes) | Webhook backends, controllers (envtest), image pulls, runtime | "Seconds" (KWOK README), envtest 20 s default timeout — **not benchmarked**; ~150–400 MB binaries per minor cached | None; needs binary downloads once |
| **T2 Live server-side dry-run** | `kubectl apply --dry-run=server --validate=strict`, `helm --dry-run=server`, `flux diff kustomization`, Argo `server-side-diff`/sync `{dryRun}` | Full chain incl. webhooks with `sideEffects: None/NoneOnDryRun`, SSA conflicts, current quota | Side-effect webhooks ("does not support dry run"), controllers, Pod-level checks for workloads, scheduling | RTT; Argo SSD skips new resources | kubeconfig + RBAC for the real verb; leaks pre-merge intent to prod |
| **T3 Real/preview cluster** | kind/k3d/vcluster (30–90 s), Argo PR generator, Flux ResourceSet previews, Compatibility Matrix | Everything, but generic clusters lack target policies unless mirrored | Cost scales linearly with services | Minutes | Cluster creds |

(slice: inner-loop-pseudo-cluster; slice: cluster-state-import; slice: gap-k8s-offline-fidelity-facts; slice: persona-vendor-to-customer)

### 5.3 Recommended tiering

- **Default = T0 (a+b) on every save**, with each finding tagged `schema-valid` / `apiserver-valid (CRDs exact, native approximate)` / `policy-valid (N rules skipped needing live data)` and the unobservable inputs shown as badges ("PSA exemptions unknown", "authorizer() evaluated as allow", "3 webhooks match, logic unknown"); a `--strict` mode turns unknowns into Warnings (slice: policy-engines-offline-validation; slice: schema-validation-tech).
- **Simulate admission order faithfully:** expand workloads to Pods (gator-expand/Kyverno-autogen style) → strict fields + structural schema + defaulting + CRD CEL → NamespaceLifecycle → LimitRanger (mutate, then validate) → PSA → quota (`CheckRequest` against `status.used`, Warning when stale) → MAP/Kyverno mutations (show the mutated diff as "what the cluster will store") → VAP/Kyverno/Gatekeeper/Kubewarden validation → webhook match prediction → RBAC (slice: cluster-state-import; slice: k8s-2026-features-and-trends).
- **T1 is opt-in** (`zhi verify --engine apiserver`) for pre-commit and CI, pinned to the snapshot's minor; benchmark before promising "<1 min"; KWOK when scheduling fit matters, envtest otherwise (slice: inner-loop-pseudo-cluster).
- **T2 is the final optional gate** when a kubeconfig exists; label live findings separately and explain the delta (side-effect webhooks, mutations) rather than merging results (slice: devops-pain-points).
- **Version policy:** pin the latest k8s.io minor (v0.37.0); `environment.MustBaseEnvSet(serverVersion)` emulates older clusters' CEL library availability downward; native schemas always come from the snapshot's OpenAPI, never embedded builtins; emit a skew Warning when the snapshot minor exceeds the pinned minor. One binary suffices (slice: gap-go-embedding-and-license-audit).
- **Freshness like a lockfile:** cluster identity + capture time + digest in every report; refuse to clear Blocking findings on snapshots older than N days; hash-addressed OpenAPI keeps `zhi snapshot refresh` cheap (a scheduled in-cluster/CI job pushing a signed OCI bundle) (slice: environments-secrets-drift; slice: inner-loop-pseudo-cluster).

---

## 6. The developer ↔ deployer contract

### 6.1 What existing formats let an author declare

| Concern | Existing formats (verified) | Consume natively? | Define ourselves? |
|---|---|---|---|
| Value types, defaults, enums, ranges, patterns | Helm `values.schema.json` (2020-12 in Helm 4, draft-07 legacy); `# @schema` comments (losisin/dadav), helm-docs `# --`, Bitnami `## @param`; ytt `@schema/*`; Timoni `#Config`; kro SimpleSchema (`replicas: integer | default=1 minimum=1`); XRD/CRD openAPIV3Schema; Score `score-v1b1.json`; Compose `config --variables` + `${VAR:?}`; Portainer App Template `env`; Dokploy `template.toml` helpers; KOTS `Config` items; Rancher `questions.yaml`; Glasskube `valueDefinitions`; Massdriver `params` (draft-07); Nuon `inputs.toml` | **Yes** — all of the above ingest into one internal JSON Schema 2020-12; priority: values.schema.json → `# @schema` → kro/Timoni/XRD/CRD → Compose variables → inferred types | Only the internal model and `x-zhi-*` extensions; write back `values.schema.json` so Helm 4 enforces it everywhere |
| UI hints (widget, order, group, secret, help, cluster-aware pickers) | Backstage/RJSF `ui:*`; JSON Forms UI schema rules SHOW/HIDE/ENABLE/DISABLE; VelaUX `uiType` (SecretSelect, CPUNumber, ImageInput); Rancher `storageclass/secret/pvc/hostname` types; OLM `x-descriptors` URNs (`io.kubernetes:Secret`, `fieldDependency:<path>:<value>`); Kubeapps `form:true`, `render: slider`; Port `jqQuery` conditionals; KOTS `when/hidden/readonly/repeatable`; Palette `${string:/regex/}`, `password`, `ipv4` | Partially (map Rancher/OLM/Kubeapps hints on import) | **Yes** — a documented vendor vocabulary (`x-ui-widget`, `x-ui-order`, `x-secret`, `x-component`, `x-help`, `x-cluster-ref: storageclass|namespace|secret-key|ingressclass|image`) registered as a custom vocabulary so hints are type-checked and ignored as annotations by other validators |
| Cross-value rules | JSON Schema `if/then`, `dependentRequired`, `oneOf` (weak); Timoni/CUE native; XRD/CRD `x-kubernetes-validations`; helm-cel `values.cel.yaml`; KOTS `when`; Rancher `show_if` (Ember vs Jexl divergence bug #4706) | helm-cel and CRD CEL yes | **CEL** with the VAP variable set (`self`, `oldSelf`, `values`, `rendered`, `env`) and the k8s CEL library; no bespoke `when` DSL |
| Severity | JSON Schema none; ytt/Timoni/KCL blocking-only; helm lint ERROR/WARNING/INFO; KRM `results[].severity error|warning|info`; Crossplane FATAL/WARNING/NORMAL; Troubleshoot fail/warn/pass + `strict`; VAP Deny/Warn/Audit; Gatekeeper deny/warn/dryrun; Kyverno Enforce/Audit | Map all onto Info/Warning/Blocking | Keep zhi's three levels as `x-zhi-severity` / CEL attribute |
| Environment requirements (min K8s version, required CRDs/APIs, storage capability, node resources, ingress class, registry access) | Troubleshoot `Preflight` analyzers (clusterVersion, customResourceDefinition, storageClass — presence only, nodeResources, ingress, imagePullSecret, distribution, registryImages); Helm `Chart.yaml kubeVersion`; OLM CSV `minKubeVersion`, `customresourcedefinitions.required`, `nativeAPIs`; Palette `pack.json constraints.resources` (derived from values via `replicaCountParamRef`); Score `resources: {type: postgres}`; Massdriver `connections` | **Yes** — Troubleshoot analyzer vocabulary (embed the Go library), `kubeVersion`, OLM `required` | Extend with missing analyzers: RWX-capable StorageClass via CSIDriver, ResourceQuota headroom, PSA namespace level, IngressClass/GatewayClass, CRD *version*, egress reachability; allow requirements to reference the deployer's chosen values |
| Component toggles | Helm `enabled`/`condition` + subcharts; Kustomize `Component`; Compose `profiles`; Timoni bundles; Holos components | Yes — map zhi components onto native switches | Only the grouping metadata |
| Environment/tenant model | Octopus Project × Environment × Tenant with variable templates (string/sensitive/select/checkbox/certificate/account); Nuon installs; Distr deployment targets; Replicated customer → license → instance; ApplicationSet generators; Flux `clusters/<env>`; Kargo Stages | Derive on import from ApplicationSet/Flux dirs/Kargo Stages | Define environment metadata: name, promotion order, target clusters (1:N), namespaces, GitOps tool + path, snapshot ref, secret-store bindings, matrix dimensions |
| Secrets | ESO `remoteRef`, SOPS, SealedSecret, VSO/Infisical/1Password refs; KOTS `password`; Nuon/Octopus `sensitive`; Backstage `ui:field: Secret` | Yes — recognise all reference forms on import | Typed `ref://<store>/<path>#<key>[@version]` never carrying values; render to ESO by default |

(slice: persona-vendor-to-customer; slice: k8s-config-languages; slice: schema-validation-tech; slice: platform-engineering-config-uis; slice: environments-secrets-drift; slice: docker-compose-landscape)

### 6.2 Recommendation

Keep two developer-authored artifacts, as KOTS does, and never merge them into Helm templates: (1) a **values contract** — JSON Schema 2020-12 with `x-zhi-*` hints and CEL cross-value rules, compiled to/from `values.schema.json` and `values.cel.yaml`; (2) an **environment contract** — a Troubleshoot-compatible analyzer list plus `kubeVersion`/required-API declarations, evaluated against the *imported snapshot* rather than at install time in the customer cluster. The deployer fills values in a form generated from (1), with cluster-aware pickers populated from the snapshot (works air-gapped), and gets (2) plus the full admission simulation before the PR opens. The same artifacts serve the DevOps transition unchanged: a developer deploying to their own environment simply picks up the deployer surface. Emit the standard forms (`values.schema.json`, `Chart.yaml kubeVersion`, a Troubleshoot `Preflight` Secret) so downstream users get value without zhi at runtime (slice: persona-vendor-to-customer; slice: k8s-2026-features-and-trends).

---

## 7. Plugin-depth analysis

### 7.1 Lessons from comparable tools

- **Out-of-process gRPC (hashicorp/go-plugin v1.8.0):** Terraform/OpenTofu (36 RPCs, capability-flag versioning, lockfile + GPG/OCI mirrors), Vault (now OCI images under runc/gVisor rather than Wasm), Pulumi (polyglot runtimes required), Crossplane functions (one Deployment per function, mTLS, `required_schemas`, offline `composition render` needs Docker). Wins for long-lived, credentialed integrations owned by a vendor binary; over-built for a validation tool whose users edit YAML (slice: plugin-architectures).
- **Wasm:** Kubewarden (waPC + WASI, OCI + Sigstore, `kwctl run` offline) is the strongest model; Helm 4's extism/v1 runtime shipped in 4.0.0 with no further Wasm work in 4.1–4.3 and the Extism Go SDK untagged since 2025-03-02; Envoy publicly regrets proxy-wasm's ABI/copy overhead and now invests in unsandboxed "dynamic modules"; kpt `--allow-alpha-wasm` never graduated. Wins for small, pure, untrusted transforms/policies; loses when the plugin needs network/FS/credentials (slice: plugin-architectures; slice: gap-go-embedding-and-license-audit).
- **In-process same-language modules:** Backstage (sustained versioning pain, experimental dynamic plugins), Grafana (frontend sandbox off by default, cannot sandbox signed plugins), VS Code (separate extension-host process, frozen additive API). Maximises DX, maximises breakage; argues against a pluggable UI (slice: plugin-architectures).
- **Declarative/expression-only:** Kyverno moved from JMESPath to CEL with ~14 fixed libraries and *no* user-defined libraries; Renovate has no code plugins (regex/JSONata managers, and regrets losing source positions); Flux controllers refuse Kustomize plugins ("no user code executes") and extend only via CRDs, while the Flux 2.9 CLI plugin system distributes OCI binaries by digest (SHA-256 only, cosign deferred, three plugins today); Compose extends via `docker-<type>` exec providers with a JSON-lines protocol and a `metadata` parameter schema. This is where 2026 rewards extension (slice: plugin-architectures; slice: docker-compose-landscape; slice: gap-closest-competitors-deep-dive).
- **Anti-patterns:** Argo CD CMP v2 sidecars (issue #15006 open; errors cached in Redis; one sidecar per plugin version); Kustomize plugins alpha-gated for five years (KEP-2953 deprecations, KEP-2906 unimplemented, #5808 closed not-planned); jsPolicy (bespoke engine without offline tooling, silent since 2024) (slice: plugin-architectures; slice: policy-engines-offline-validation).
- **Vendor-to-customer evidence:** the extensibility that mattered was custom collectors/analyzers (Replicated Enterprise upsell), `runPod`-style arbitrary checks, and field renderers (slice: persona-vendor-to-customer).

### 7.2 Concern × mechanism matrix

| Concern | Fixed core | Declarative config | CEL / CUE | Wasm | Out-of-process gRPC | CLI exec (JSON-lines / ResourceList) |
|---|---|---|---|---|---|---|
| Source loaders (YAML/JSON/TOML/env, Helm values, Kustomize, Compose, Argo/Flux CR discovery) | **Yes** — first-party Go with source positions (yaml.v3 nodes) | File patterns / merge-layer maps (Renovate shape) | — | — | Only for foreign stores (Consul/etcd) if ever needed | — |
| Renderers (Helm v4 SDK, krusty, compose-go; Timoni/KCL/Pkl/ytt adapters) | **Yes** for Helm/Kustomize/Compose | Output mappings, target flavours (swarm/podman/quadlet) | — | Phase-2 option for pure transforms (KRM-style) | — | **Yes** for Timoni/KCL/Pkl/ytt/`flux build` (`ResourceList` in/out) |
| Validators / policies | Apiserver chain emulation, PSA, quota, LimitRanger, RBAC, VAP/MAP, Gatekeeper `k8scel`, OPA | Schema packs, policy packs as data (VAP/ValidatingPolicy manifests, ConstraintTemplates), distribution profiles | **Yes** — user/developer rules in CEL with fixed host library; CUE optional import | Phase-2 `check/v1` for third-party pure checks (bytes in → diagnostics out), OCI + Sigstore, declared capabilities | **No** — engines need in-process performance and shared type environments | Kyverno CLI, kwctl, gator, kubeconform, flux-schema, Trivy/Checkov/conftest scanners auto-detected on PATH; exit codes owned by zhi |
| Cluster / host importers | K8s dynamic client, Docker API, Portainer/Komodo APIs, Rancher/OpenShift/GKE/AKS/EKS API adapters | Kind allowlists, filters, redaction rules | — | — | `resolver/v1` for vendor collectors needing credentials (cloud policy APIs, Binary Authorization) | Troubleshoot collectors, `kwokctl snapshot export` as inputs |
| Secret resolvers | SOPS/age, Sealed Secrets cert, ESO reference rendering | Store bindings per environment | — | — | **Yes** — `resolver/v1` for Vault/KMS/Infisical/1Password (credentialed, long-lived; Vault API client is MPL) | Provider CLIs (`infisical run`, `sops exec-env`) for Compose export |
| Targets / publishers | Git branch + PR (GitHub/GitLab), OCI push (cosign v3 bundles), PR comment, GitHub Check/commit status, gitops-promoter CommitStatus | Templates for PR body/report, Kargo-compatible files, Compose `publish` | — | — | — | Compose provider protocol, Bridge transformer images (`/in`→`/out`) for exotic exporters |
| UI panels / forms | **Yes** — RJSF/JSON Forms with `extraErrors`, rendered-manifest + diff pane, findings panel; LSP server; MCP tools | Widget ids, order, groups, show/hide rules in the schema vocabulary | — | — | **No** | — |

### 7.3 Recommendation and reasoning

Reduce the current four gRPC types (config/transform/store/ui) to a fixed Go core plus **at most two loadable kinds**, both optional and both distributed as digest-pinned, signed OCI artifacts with a lockfile (`.terraform.lock.hcl` model) rather than a marketplace:

1. `resolver/v1` — out-of-process (go-plugin gRPC, first-party compiled in) for **credentialed, heterogeneous integrations only**: secret stores and vendor cluster/cloud collectors. This is the one place every brief still wants a process boundary (environments-secrets-drift, persona, cluster-state-import) and the place Vault itself chose isolation over Wasm.
2. `check/v1` — sandboxed (wazero with a zhi-owned ABI, or exec with the KRM `ResourceList`/output-unit contract) for **pure validators CEL cannot express**, phase 2, only if demand appears. Kubewarden proves the shape; Helm 4's slow Wasm uptake and the Extism SDK's cadence say do not depend on it in phase 1.

Everything else is data or expressions: loaders by file pattern, renderers as fixed engines plus exec adapters speaking `ResourceList`, validators as JSON Schema + CEL + imported policy manifests (evaluated untranslated by the same apiserver CEL environment), importers as core Go, targets as core Git/OCI writers, and the UI/LSP/MCP as *output surfaces*, not extension points. Store and transform plugin types are dropped: Git is the store, and transforms in the GitOps path are exactly what Flux forbids and Argo suffers from. Version like Terraform/Crossplane/LSP — `zhi.plugin.v1`, additive RPCs gated by capability flags, tolerant enums, plugins request schemas from the host. Keep zhi's existing cosign/OCI signing pipeline and air-gap mirror *format*, but ship a curated krew-index-style catalog file, not a rating/search marketplace, until third-party plugins exist. This is the intersection of all eight slice recommendations on plugin depth, and it resolves their disagreement about transports by assigning each concern to the mechanism the ecosystem has already validated for it (slice: plugin-architectures; slice: gitops-controllers; slice: policy-engines-offline-validation; slice: cluster-state-import; slice: docker-compose-landscape; slice: platform-engineering-config-uis; slice: persona-vendor-to-customer; slice: environments-secrets-drift; slice: k8s-2026-features-and-trends).

---

## 8. Competitor overlap and whitespace

**Who does parts of this today** (slice: gap-closest-competitors-deep-dive unless noted):

- **ConfigHub** — config-as-data SaaS/self-hosted Enterprise (closed server) with `vet-schemas` (kubeconform), `vet-celexpr` and blocking Triggers on every Unit change; since August 2026 delivery is OCI-pull only and it does **not** write Git — it replaces Git as the source of truth. No admission/quota import, no snapshot. Overlap: validation-on-edit; divergence: source-of-truth model.
- **Devtron v2.2.0** — the strongest living OSS incumbent: schema-driven GUI ↔ YAML, base/env overrides, locked keys and approval workflow (Enterprise tags), Dry Run, Argo CD write-through into a Devtron-owned manifest repo. No policy engine, no snapshot, no PR into the customer's repo, validator plugins absent.
- **Flux Schema + Ecosystem Schema Catalog + Flux CLI plugins** — offline structural + CEL validation pinned to a minor, `extract crd`, public MCP; reuses `apiextensions-apiserver` code (parity claim verified in `internal/validator/cel.go`). No values model, VAP/MAP/Kyverno/Gatekeeper/PSA/quota, UI or Git write; catalog is AGPL-3.0 (binary Apache-2.0). Overlap: the CRD/CEL layer of a pseudo cluster.
- **Replicated KOTS + Troubleshoot Preflight** — the only clean developer-contract/environment-requirement split; proprietary control plane, install-time checks in the customer cluster, flat form DSL (slice: persona-vendor-to-customer).
- **Cyclops** — Helm `values.schema.json` forms in an in-cluster UI; Git write is a direct commit since v0.17 (PR flow undocumented); no policy awareness; slowing (v0.21.1 2026-06-26, 2-month gap).
- **Timoni v0.34.0** — typed CUE values, `mod vendor crds` + `mod vet` with CEL; CLI-only, single maintainer, no policies/quotas/UI (slice: k8s-config-languages).
- **Kargo** — promotion orchestrator that already edits values files and opens PRs; no validation step; custom container steps Enterprise-only. Host/complement rather than competitor.
- **argocd-diff-preview / Argo SSD / Nelm plan** — incumbent "see the rendered diff before merge" UX; needs a live or ephemeral cluster; SSD skips new resources (slice: inner-loop-pseudo-cluster).
- **Kyverno Playground + CLI side files** — closest "UI + offline engine with mocked cluster context" precedent; mocks only, no importer (slice: policy-engines-offline-validation).
- **Komodo / Portainer** — Compose-side "values in Git + diff before sync" and the only host-level policy concept (BE, not exportable); neither validates Compose against host facts (slice: docker-compose-landscape).
- **Distr, Humanitec/Score, Backstage/Port scaffolder forms** — BYOC control plane without validation; plan-only previews; JSON-Schema form-to-PR flows that never look at the target cluster (slice: platform-engineering-config-uis; slice: persona-vendor-to-customer).
- **kubectl-validate, Monokle, Datree, kat, helm-cel, helm-values-manager** — the right approaches that stalled or stayed tiny; sources of code and UX to mine.

**What nobody does** — after 10+ targeted searches across slices, no product was found that:

1. Imports VAP/MAP + Kyverno + Gatekeeper + PSA labels + ResourceQuota/LimitRange + CRDs + classes + RBAC from a live cluster into a **portable, versioned, signed snapshot** usable without cluster access at edit time.
2. Renders Helm/Kustomize/Compose to manifests and runs **all** engines (apiserver CEL, VAP/MAP, Kyverno, Gatekeeper/OPA, PSA, quota arithmetic across the namespace, RBAC) in **one** pre-push report with Info/Warning/Blocking and per-finding fidelity.
3. Lets **one persona** author value schemas + CEL cross-value rules + environment requirements that **another persona** fills in a form, with the target cluster's snapshot selected per environment.
4. Outputs a **PR** onto the repo/branch Argo CD/Flux/Kargo already watch — no new source of truth, no in-cluster controller.
5. Treats **Docker Compose** as a first-class target with the same model.
6. Ships validator extensibility that is **OSS by default** (contrast Kargo Custom Steps, Devtron Enterprise tags, Replicated Enterprise analyzers).

The gap this tool fills is therefore "the pre-push validator the controllers lack", positioned above Helm/Kustomize/Compose (never fighting them, as Glasskube did), beside Argo/Flux/Kargo (never becoming their UI, as Weave GitOps did), and stateless with respect to the cluster (never cluster-attached, as Kubevious/Kubeapps/Cyclops were).

---

## 9. Design implications

1. **[must] Git is the only write target.** Commit/PR into the env folder or hydrated branch; never apply. Emit `hydrator.metadata`-style provenance and commit trailers with snapshot digest (slice: gitops-controllers; slice: environments-secrets-drift).
2. **[must] Discover the values model from controller CRs** (Argo `source(s).helm.*`, ApplicationSet git-files, `sourceHydrator.drySource`; Flux `HelmRelease.spec.values/valuesFrom`, `postBuild.substitute(From)`, Kustomize patches; Kargo Stages) and map each key to file + path + merge layer; show the effective value per environment with its source layer (slice: gitops-controllers; slice: environments-secrets-drift).
3. **[must] Render in-process with the real engines:** `helm.sh/helm/v4` (HIP-0004 stability, snapshot-driven `--kube-version/--api-versions`, snapshot-backed `lookup`), `sigs.k8s.io/kustomize/api/krusty`, `compose-go/v2`; subprocess adapters for Timoni/KCL/Pkl/ytt/`flux build`. Keep `CGO_ENABLED=0` (slice: k8s-config-languages; slice: gap-go-embedding-and-license-audit).
4. **[must] Versioned ClusterSnapshot format** (directory or OCI artifact, signed, redactable, air-gap importable) with the layers enumerated in §5.1, hash/resourceVersion per layer, provenance, staleness TTL, and a "declared, not observed" overlay for PSA exemptions, static policies, webhook stubs (slice: cluster-state-import; slice: policy-engines-offline-validation; slice: gap-k8s-offline-fidelity-facts).
5. **[must] Importer is a kubectl-free, read-only Go client** over discovery + OpenAPI v3 + a fixed GVR list; degrades gracefully; `--filter` by workspace namespaces; records engine versions and feature-gate state/server version (MAP v1 vs v1beta1) (slice: cluster-state-import; slice: gap-k8s-offline-fidelity-facts).
6. **[must] Ordered apiserver-chain emulation** (Tier 0-b) as specified in §5.3, with workload→Pod expansion and mutation before validation (slice: cluster-state-import; slice: k8s-2026-features-and-trends).
7. **[must] Embed** `k8s.io/{apiserver,apiextensions-apiserver,pod-security-admission}` v0.37.0, kubectl-validate `pkg/validator` by pseudo-version, OPA `v1/rego`, `frameworks/constraint` + Gatekeeper `pkg/drivers/k8scel`; **vendor-fork** LimitRanger, RBAC `RulesAllow`, quota evaluators from `k8s.io/kubernetes@v1.37.0`; **shell out** to Kyverno CLI, kwctl, flux-schema; CEL only via `k8s.io/apiserver/pkg/cel/environment` (slice: gap-go-embedding-and-license-audit; slice: schema-validation-tech).
8. **[must] Three fidelity tiers, one interface, tier tagged on every finding**; T0 default, T1 opt-in, T2 optional live gate; never present T0 as "will apply" (slice: inner-loop-pseudo-cluster; slice: devops-pain-points).
9. **[must] Severity mapping:** schema/CEL/Deny/Enforce/Preflight fail → Blocking; VAP Warn, Kyverno Audit, Gatekeeper warn/dryrun, PSA warn/audit, webhook match predictions, stale quota, native best-effort checks → Warning; annotations/deprecation-in → Info; honour PolicyExceptions and `enforcementAction`; preserve `messageExpression` output and cite the source policy (slice: policy-engines-offline-validation; slice: plugin-architectures).
10. **[must] JSON Schema 2020-12 + `santhosh-tekuri/jsonschema/v6`** as values contract and validator, replicating Helm 4's `$ref` loaders and merged-`.Values`-vs-all-subchart-schemas semantics; synthesize schemas from defaults + `# @schema` comments when absent; write `values.schema.json` back to the chart repo (slice: schema-validation-tech; slice: k8s-config-languages).
11. **[must] Canonical error record = 2020-12 output unit** (instanceLocation + keywordLocation + message + severity + tier + source engine); normalise apiserver `field.ErrorList`, CEL `fieldPath`, PolicyReport, Rego `path`; back-map rendered JSONPath → template → values key for inline UI errors (slice: schema-validation-tech; slice: policy-engines-offline-validation).
12. **[must] CEL as the single user-facing rule language** (VAP variable set + k8s library + `values`/`rendered`/`env`); store user rules as VAP/ValidatingPolicy manifests so they can later be promoted to the cluster (slice: policy-engines-offline-validation; slice: plugin-architectures).
13. **[must] Comment- and order-preserving YAML round-trip** (yaml.v3 Node/kyaml); machine-managed fields (image-automation markers, Renovate) read-only; emit KYAML/JSON only for generated manifests (slice: environments-secrets-drift; slice: devops-pain-points; slice: gap-k8s-offline-fidelity-facts).
14. **[must] Secrets as typed references** (`ref://store/path#key@version`), never values; render to ExternalSecret (ESO v1) by default, SealedSecret/SOPS/VSO optional; Blocking if the store is absent from the snapshot; validation runs with secrets absent (slice: environments-secrets-drift; slice: platform-engineering-config-uis).
15. **[must] Environment metadata first-class** (name, promotion order, clusters 1:N, namespaces, GitOps tool + path, snapshot ref, secret bindings, matrix dimensions); evaluate every change against every environment's snapshot and show a matrix (slice: environments-secrets-drift; slice: devops-pain-points).
16. **[must] Developer contract = values schema (+CEL) and environment requirements (Troubleshoot analyzers + `kubeVersion` + required APIs)**, both signed separately from deployer-filled values; requirements may reference chosen values; cluster-aware pickers fed from the snapshot (slice: persona-vendor-to-customer).
17. **[must] Compose first-class:** compose-go model, strict interpolation (missing var → Blocking), host snapshot importers (Docker API, Portainer, Komodo), host-aware checks (port collisions, bind sources, `deploy.*` without swarm, capacity), policy pack over normalised JSON, write-back as `.env`/overrides/`x-zhi-*` (slice: docker-compose-landscape).
18. **[must] Distribution profiles** for OpenShift (SCC matching via `apiserver-library-go/sccmatching` + snapshot RBAC authorizer, UID pre-allocation placeholders, Routes, IDMS/ITMS, project template injection), GKE Autopilot (Warden constraints, request mutation/ratios), AKS (Deployment Safeguards mutators/validators, Azure Policy Gatekeeper objects), EKS Auto Mode (NodePool matching, Pod Identity), Rancher (PSACT from management cluster, project quotas); versioned with changelogs (slice: gap-deployer-environments-openshift-managed-regulated).
19. **[must] Three surfaces, one core:** pre-commit hook + CI mode with reliable exit codes (`zhi validate --env prod --snapshot prod.tgz --exit-code`), local Web UI over a Git worktree (schema form + rendered preview + diff + findings), JSON/SARIF/PolicyReport for PR checks and comments (slice: devops-pain-points; slice: platform-engineering-config-uis).
20. **[must] Plugin depth as §7.3:** fixed core; declarative loaders/renderers; CEL validators; ≤2 loadable kinds (`resolver/v1` gRPC, `check/v1` Wasm/exec, phase 2); no UI/store/transform plugins; signed OCI + lockfile; curated catalog, no marketplace until needed (slice: plugin-architectures; slice: k8s-2026-features-and-trends).
21. **[must] License hygiene:** never import AGPL (flux-operator, schema-catalog, Nuon), GPL (Komodo, helm-docs, podman-compose) or BUSL server code; exec/read/HTTP only; bundle `third_party/NOTICES`; respect LF/CNCF trademark rules in naming; run `govulncheck` in CI (slice: gap-go-embedding-and-license-audit).
22. **[should] Emit interoperable artifacts:** kubeconform schema dir, kubectl-validate `--local-schemas` dir, Kyverno Context/Values/UserInfo files, gator inventory + GVKManifest, Troubleshoot `Preflight` Secret, `values.schema.json`; accept AdmissionReview JSON (slice: cluster-state-import; slice: persona-vendor-to-customer).
23. **[should] Decidability report per policy** (gator `sync test` idea): list referential data each policy needs (apiCall/`resource.Get`, inventory GVKs, paramRef, namespaceObject, authorizer) and whether the snapshot has it; prompt to fetch (slice: cluster-state-import).
24. **[should] Live-check and post-merge feedback integration:** Argo REST `manifestsWithFiles`/`server-side-diff`/sync `{dryRun}`, `flux diff kustomization`, `kubectl --dry-run=server` with the controller's fieldManager; subscribe to Argo app status/Notifications and Flux commit-status providers; optionally fire `/api/webhook`/Receiver to skip the poll (slice: gitops-controllers).
25. **[should] Promotion hooks:** gitops-promoter CommitStatus provider; Kargo `http` step target and PR compatibility (`git-wait-for-pr`); validate `hydrateTo` branches as PR checks; Flux ResourceSet/Argo PR-generator preview links (slice: environments-secrets-drift; slice: gap-closest-competitors-deep-dive).
26. **[should] Signed evidence for regulated buyers:** DSSE + SLSA VSA v1 bound to rendered-manifest digest + snapshot digest + profile version, cosign v3 bundles as OCI referrers, CycloneDX ≥1.6/SPDX ≥3.0.1 SBOMs, `GOFIPS140` build variant, zero egress by default, offline verification with `--trusted-root`, append-only hash-chained audit log, deterministic reports (slice: gap-deployer-environments-openshift-managed-regulated).
27. **[should] Quota/capacity as first-class Warnings:** `(used + delta) ≤ hard`; LimitRange defaulting shown as diff; optional scheduler-framework fit against snapshot nodes — the one check no OSS tool offers (slice: inner-loop-pseudo-cluster; slice: devops-pain-points).
28. **[should] Editor integration for free:** write `.zhi/schema.json` and inject the `# yaml-language-server: $schema=` modeline; expose an LSP server (diagnostics, hover, code actions applying `proposedValue`) (slice: schema-validation-tech; slice: plugin-architectures).
29. **[should] MCP server** (stdio + streamable HTTP, loopback bind, read-only default, token separated from cluster credentials) with `validate/explain/diff/import_snapshot`; ship SKILL.md; MCP is an outbound surface, not an internal boundary (slice: k8s-2026-features-and-trends; slice: plugin-architectures).
30. **[should] Giterminism as a product rule:** reports computed only from committed inputs + a versioned snapshot; record snapshot identity in the PR (slice: k8s-config-languages).
31. **[should] Instrument outcome metrics:** count Blocking findings caught pre-push per PR and the "would have failed at" stage (admission/scheduling/runtime) — the deployer persona's core metric and the adoption argument for platform teams (slice: devops-pain-points; slice: persona-vendor-to-customer).
32. **[could] Tier 1 apiserver backend** via envtest/KWOK hydrated from the snapshot with static-manifest admission, pinned binaries per minor (~200–400 MB cached) — after benchmarking (slice: inner-loop-pseudo-cluster).
33. **[could] CUE import/export** (`cue get crd`, `encoding/jsonschema`) for teams already on Timoni/Holos; not a runtime dependency (slice: schema-validation-tech; slice: k8s-config-languages).
34. **[could] Podman-kube/Quadlet profile** on the Kubernetes path (unsupported kinds = Blocking, quadlet generator `--dryrun`); Ansible/Nomad out of scope by decision (slice: gap-deployer-environments-openshift-managed-regulated).
35. **[could] Wasm `check/v1` and OCI-image transformer contract** (`/in/compose.yaml` → `/out`) for exotic exporters, phase 2 (slice: plugin-architectures; slice: docker-compose-landscape).

---

## 10. Contradictions and open questions

### 10.1 Contradictions between briefs (with verdicts)

| Topic | Positions | Verdict / stronger evidence |
|---|---|---|
| kubectl-validate status and CEL | "dormant, schemas 1.23–1.27, CEL undocumented (treat as no)" (k8s-2026, devops-pain-points, environments) vs "main tracks 1.35, CEL via `customresource.NewStrategy`" (cluster-state-import, schema-validation-tech) | **Main is alive (merge 2026-01-05, builtins 1.23–1.35) and CEL runs** — confirmed in `pkg/validator` source and the 1.37 strategy (`cel.NewValidator` inside `Validate`); last *tag* is still v0.0.4. Embed by pseudo-version (slice: gap-go-embedding-and-license-audit). |
| Declarative validation of native types | "KEP-5073/4153 GA 1.36, rules published in OpenAPI" (cluster-state-import, k8s-2026) vs "rollout 1.33–1.36, publishing is a non-goal" (schema-validation-tech) | **KEP-5073 (4153 superseded); `DeclarativeValidation` gate GA/locked 1.36; rules NOT published to `/openapi/v3`** (explicit non-goal; GA blog calls it a "future ability"). Native offline validation stays approximate (slice: gap-k8s-offline-fidelity-facts). |
| Argo CD Server-Side Diff stability | "3.4+" (gitops-controllers) vs "stable since v3.1.0" (five slices) | **Stable since 3.1.0**, still opt-in; 3.4/3.5 changed no diff defaults (slice: gap-k8s-offline-fidelity-facts). |
| MutatingAdmissionPolicy maturity | "alpha 1.36" (devops-pain-points) vs "GA 1.36" (four slices) | **GA `v1` in 1.36**, stored as v1 in 1.37 (KEP-3962 kep.yaml, CHANGELOG-1.36) (slice: gap-k8s-offline-fidelity-facts). |
| Kyverno ValidatingPolicy GA; ClusterPolicy removal | GA "1.17" (plugin-architectures) vs "1.19" (schema-validation-tech, k8s-2026); removal "1.20" vs "v1.18–v1.20" | **GA in 1.17 (2026-02-02)**; 1.19 adds "full parity" and formal deprecation; **removal planned 1.20 (~Nov 2026)** (slice: gap-go-embedding-and-license-audit). |
| Gatekeeper VAP/VAPB generation | "since v3.17"/"stable since v3.18" (cluster-state-import) vs "beta since 3.20, on by default" (policy-engines, k8s-2026) | **Beta and on by default since v3.20**, requires K8s ≥1.30 (docs) (slice: gap-k8s-offline-fidelity-facts). |
| Kargo custom container steps | "Enterprise-only" (environments) vs OSS integration path (gitops-controllers, inner-loop, k8s-2026) | **Enterprise-only** (`ee.kargo.akuity.io/v1alpha1 CustomPromotionStep`, Akuity Platform ≥ v1.10); OSS `PromotionTask` composes built-in steps only (slice: gap-closest-competitors-deep-dive). |
| kubeconform v0.8.0 date | 2024-06-04 vs 2026-06-04 | **2026-06-04** (previous v0.7.0 2025-05-12); active, not maintenance (slice: gap-k8s-offline-fidelity-facts). |
| Timoni latest | v0.29.0 (persona) vs v0.34.0 | **v0.34.0 (2026-08-30)**; persona slice's fetch predates the August releases. |
| Helm 3 end of life | "security fixes end 2026-11-11" (k8s-config-languages, persona) vs "until Feb 2027" (k8s-2026) | The helm.sh EOL page now reads "security fixes end February 10th 2027 (extended from November 2026)"; final feature/bugfix 2026-09-09 (slice: gap-k8s-offline-fidelity-facts). Both were true at different fetch times; use Feb 2027. |
| Weave GitOps last stable | "v0.38.0 Dec 2024" vs "no stable since 2023-12" vs "RC-only since 2025" | **v0.38.0 2023-12-06**, v0.39.1-rc.1 2026-01-25, not archived (slice: gap-k8s-offline-fidelity-facts). |
| Argo default reconciliation | 180 s (gitops-controllers assumption) vs 120 s + 60 s jitter | **120 s + 60 s jitter** (`argocd-cm.yaml`) (slice: gap-k8s-offline-fidelity-facts). |
| Datree archive date; Portainer 2.45.0 label | "Apr 2024" vs "2024-06-06"; STS vs LTS | **Archived 2024-06-06** (company closed July 2023). Portainer: docs number 2.39.7 LTS; 2.45.0 is most plausibly STS (docker-compose slice checked docs; platform-engineering did not) — verify. |
| Kyverno engine: embed or shell out | Embed playground-style Processor (policy-engines, platform-engineering) vs CLI only (schema-validation-tech) vs "wrap, don't shell out blindly" (devops) | **Shell out to the CLI** with generated side files and zhi-owned exit codes; the module's PSA fork replace, 400+ requires and 2026 CVE cadence outweigh in-process convenience (slice: gap-go-embedding-and-license-audit). |
| CUE's role | Internal unification engine (k8s-config-languages) vs optional import/export (schema-validation-tech) vs "CEL first, CUE second" (plugin-architectures) | **CEL first; CUE optional import/export.** CEL is what deployers already meet in VAP/CRDs and what the snapshot's policies are written in; CUE adds a second runtime and hard error messages. Evidence for CEL is stronger (three slices plus every engine's migration). |
| Plugin kinds/transport | Eight different lists (see §7) | Resolved by concern-to-mechanism assignment in §7.3; all agree on "shallow". |
| Snapshot on-disk anchor | kubectl-validate layout vs kwokctl export vs flux-schema catalog layout vs 1.37 static-manifest form | **Own format**: kubectl-validate layout for schemas, raw CRDs, 1.37 static-manifest form for VAP/MAP, plain object dumps for the rest; *export* adapters to kwokctl/kubeconform/flux-schema layouts. kwokctl's default export omits the objects that matter (slice: gap-k8s-offline-fidelity-facts). |
| Primary Git flow | Hydrated branches first-class (gitops-controllers, inner-loop) vs DRY-repo-centric (environments) | Present both; **DRY-centric as the primary write target** (works for Flux, Compose, Kargo OSS) with hydrated-branch validation as a supported read/check path. Owner decision below. |

### 10.2 Open questions for the owner

1. **Hydrated-branch vs DRY-repo primacy** — which flow does the company's Argo/Flux estate actually run, and should zhi validate `hydrateTo` branches as a PR check in phase 1?
2. **Unknowns policy** — when PSA exemptions, static `.static.k8s.io` policies, `authorizer()` results or webhook logic are unobservable, default to "assume permissive + badge" or "Warning" or "Blocking in strict mode"? Which is the CI default?
3. **Multi-minor emulation** — accept the latest-k8s.io-minor + skew-warning policy, or invest in per-minor evaluators (multiple binaries or Tier 1 only)?
4. **Tier 1 investment** — is a KWOK/envtest backend (200–400 MB per minor, benchmarks pending) worth phase-1 effort, or is Tier 0 + optional live dry-run enough for the first release?
5. **Gatekeeper/OPA in v1** — embed Rego (OPA + `frameworks`) now, or ship VAP/MAP + Kyverno + consuming Gatekeeper's *generated* VAPs first?
6. **Kubewarden** — shell out to kwctl in v1 or defer?
7. **Which distribution profiles first** — OpenShift, AKS Automatic, GKE Autopilot, EKS Auto Mode, Rancher: which do the consultants actually deploy into this year?
8. **Secrets ownership** — ESO-only references (no Compose story) vs SOPS in-UI editing (client-side keys) vs both?
9. **Swarm, Podman/Quadlet** — support as flavour flags, or Docker Compose single-host only?
10. **Plugin lanes** — retain a gRPC `resolver` lane beyond secret stores and cluster importers? Ship Wasm `check/v1` in phase 1 given CEL coverage, or phase 2?
11. **Marketplace and mirror** — retire zhi's marketplace/rating/search and keep only signed-OCI distribution + lockfile + air-gap export/import?
12. **UI stack** — RJSF vs JSON Forms (show/hide rules) in a JS front end served by the Go binary; is a Go-native form renderer worth exploring?
13. **Argo/Flux write-through vs PR-only** — should zhi ever push directly to a branch the controller syncs (bypassing review) for dev environments?
14. **Regulated features in v1** — FIPS build, DSSE/VSA reports, SBOMs, offline Sigstore roots: required for the first customers or later?
15. **Telemetry and instrumentation** — how to measure "Blocking findings caught pre-push" without any egress by default?
16. **Lab spikes to schedule before architecture freeze:** kubectl-validate CEL on a failing rule; Kyverno CLI `--parameter-resource` with MAP; KWOK + VAP + quota interplay; envtest with `--admission-control-config-file`; Helm 4.3 accepting a 2020-12 `values.schema.json` end-to-end; boot-time benchmarks; whether Gatekeeper's `k8scel` driver exposes `request.userInfo` offline; Flux schema plugin cosign verification on install.
17. **Practitioner validation** — Reddit was unreachable and several search budgets were exhausted; run a short survey/telemetry on failure-class frequency (syntax vs schema vs policy vs quota vs refs vs image vs drift) at the owner's company before prioritising checks.

---

## 11. Sources

Deduplicated, grouped by category; drawn from the full slices on disk and the gap briefs (primary sources preferred).

### Kubernetes platform, KEPs and admission
- https://kubernetes.io/releases/
- https://kubernetes.io/blog/2026/08/26/kubernetes-v1-37-release/
- https://kubernetes.io/blog/2026/04/22/kubernetes-v1-36-release/
- https://kubernetes.io/blog/2026/05/05/kubernetes-v1-36-declarative-validation-ga/
- https://kubernetes.io/blog/2026/05/04/kubernetes-v1-36-manifest-based-admission-control/
- https://www.kubernetes.dev/blog/2026/08/27/kubernetes-v1-37-declarative-validation/
- https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/
- https://kubernetes.io/docs/reference/using-api/declarative-validation/
- https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/
- https://kubernetes.io/docs/reference/access-authn-authz/mutating-admission-policy/
- https://kubernetes.io/docs/reference/access-authn-authz/manifest-admission-control/
- https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/
- https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/
- https://kubernetes.io/docs/reference/access-authn-authz/authorization/
- https://kubernetes.io/docs/concepts/security/pod-security-admission/
- https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-admission-controller/
- https://kubernetes.io/docs/concepts/policy/resource-quotas/
- https://kubernetes.io/docs/concepts/policy/limit-range/
- https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/
- https://kubernetes.io/docs/reference/using-api/api-concepts/
- https://www.kubernetes.io/docs/reference/using-api/server-side-apply/
- https://kubernetes.io/docs/reference/kubectl/generated/kubectl_apply/
- https://kubernetes.io/docs/reference/kubectl/generated/kubectl_diff/
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/5073-declarative-validation-with-validation-gen/README.md
- https://www.kubernetes.dev/resources/keps/4153/
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/5793-manifest-based-admission-control-config/README.md
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/3962-mutating-admission-policies/README.md
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/3488-cel-admission-control/README.md
- https://github.com/kubernetes/enhancements/issues/4008
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-cli/5295-kyaml/README.md
- https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-1.37.md
- https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-1.36.md
- https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/apis/cel/config.go
- https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation/validation.go
- https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.37/staging/src/k8s.io/apiextensions-apiserver/pkg/registry/customresource/strategy.go
- https://github.com/kubernetes/kubernetes/issues/95407
- https://github.com/kubernetes-sigs/gateway-api/releases
- https://kubernetes.io/blog/2026/03/20/ingress2gateway-1-0-release
- https://opensource.googleblog.com/2026/02/the-end-of-an-era-transitioning-away-from-ingress-nginx.html
- https://endoflife.date/kubernetes

### Upstream Go packages (embedding)
- https://proxy.golang.org/k8s.io/apiserver/@v/v0.37.0.mod
- https://proxy.golang.org/k8s.io/kubernetes/@v/v1.37.0.mod
- https://pkg.go.dev/k8s.io/apiserver/pkg/admission/plugin/policy/validating
- https://pkg.go.dev/k8s.io/apiserver/pkg/admission/plugin/cel
- https://pkg.go.dev/k8s.io/apiserver/pkg/admission/plugin/resourcequota
- https://pkg.go.dev/k8s.io/apiserver/pkg/cel/environment
- https://pkg.go.dev/k8s.io/apiserver/pkg/cel/library
- https://pkg.go.dev/k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel
- https://pkg.go.dev/k8s.io/pod-security-admission/policy
- https://pkg.go.dev/k8s.io/kubernetes/plugin/pkg/admission/limitranger
- https://pkg.go.dev/k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac
- https://pkg.go.dev/k8s.io/kube-openapi/pkg/validation/validate
- https://github.com/kubernetes-sigs/kubectl-validate
- https://raw.githubusercontent.com/kubernetes-sigs/kubectl-validate/main/pkg/validator/validator.go
- https://pkg.go.dev/sigs.k8s.io/kubectl-validate/pkg/openapiclient
- https://github.com/kubernetes-sigs/kubectl-validate/tree/main/pkg/openapiclient/builtins
- https://github.com/kyverno/kubectl-validate
- https://pkg.go.dev/cel.dev/cel-go
- https://github.com/cel-expr/cel-go
- https://raw.githubusercontent.com/cel-expr/cel-go/master/go.mod
- https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6
- https://raw.githubusercontent.com/santhosh-tekuri/jsonschema/master/draft.go
- https://github.com/kaptinlin/jsonschema
- https://github.com/google/jsonschema-go
- https://github.com/invopop/jsonschema
- https://github.com/xeipuuv/gojsonschema
- https://pkg.go.dev/github.com/open-policy-agent/opa/v1/rego
- https://raw.githubusercontent.com/open-policy-agent/frameworks/master/constraint/go.mod
- https://pkg.go.dev/github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel
- https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/pkg/drivers/k8scel/driver.go
- https://raw.githubusercontent.com/kyverno/kyverno/main/go.mod
- https://raw.githubusercontent.com/kyverno/kyverno/main/pkg/cel/policies/vpol/engine/engine.go
- https://github.com/kyverno/kyverno/security/advisories
- https://osv.dev/list?ecosystem=Go&q=github.com%2Fkyverno%2Fkyverno
- https://pkg.go.dev/helm.sh/helm/v4
- https://raw.githubusercontent.com/helm/helm/main/go.mod
- https://raw.githubusercontent.com/helm/helm/main/pkg/chart/common/util/jsonschema.go
- https://github.com/helm/community/blob/main/hips/hip-0004.md
- https://pkg.go.dev/sigs.k8s.io/kustomize/api/krusty
- https://pkg.go.dev/github.com/compose-spec/compose-go/v2
- https://raw.githubusercontent.com/compose-spec/compose-go/main/loader/validate.go
- https://pkg.go.dev/cuelang.org/go
- https://pkg.go.dev/kcl-lang.io/kcl-go
- https://pkl-lang.org/go/current/evaluation.html
- https://pkg.go.dev/github.com/tetratelabs/wazero
- https://pkg.go.dev/github.com/extism/go-sdk
- https://github.com/homeport/dyff
- https://pkg.go.dev/github.com/replicatedhq/troubleshoot
- https://raw.githubusercontent.com/replicatedhq/troubleshoot/main/cmd/troubleshoot/cli/analyze.go
- https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest
- https://github.com/kubernetes-sigs/kwok
- https://kwok.sigs.k8s.io/docs/generated/kwokctl_snapshot_export/
- https://kwok.sigs.k8s.io/docs/generated/kwokctl_create_cluster/
- https://raw.githubusercontent.com/kubernetes-sigs/kwok/main/pkg/kwokctl/components/kube_apiserver.go
- https://github.com/k3s-io/kine
- https://github.com/openshift/apiserver-library-go (pkg/securitycontextconstraints/sccmatching)
- https://go.dev/doc/security/fips140

### GitOps controllers and promotion
- https://github.com/argoproj/argo-cd/releases
- https://argo-cd.readthedocs.io/en/latest/user-guide/diff-strategies/
- https://github.com/argoproj/argo-cd/blob/master/docs/operator-manual/argocd-cm.yaml
- https://github.com/argoproj/argo-cd/blob/master/docs/operator-manual/argocd-cmd-params-cm.yaml
- https://argo-cd.readthedocs.io/en/latest/user-guide/source-hydrator/
- https://argo-cd.readthedocs.io/en/stable/proposals/manifest-hydrator/
- https://github.com/argoproj/argo-cd/issues/28143
- https://argo-cd.readthedocs.io/en/latest/operator-manual/upgrading/3.2-3.3/
- https://argo-cd.readthedocs.io/en/stable/operator-manual/upgrading/3.3-3.4/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/upgrading/3.4-3.5/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/webhook/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/Generators-Git/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/Generators-Pull-Request/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/Generators-Matrix/
- https://argo-cd.readthedocs.io/en/latest/user-guide/commands/argocd_app_diff/
- https://argo-cd.readthedocs.io/en/latest/user-guide/parameters/
- https://argo-cd.readthedocs.io/en/latest/user-guide/helm/
- https://argo-cd.readthedocs.io/en/stable/operator-manual/config-management-plugins/
- https://argo-cd.readthedocs.io/en/stable/proposals/config-management-plugin-v2/
- https://github.com/argoproj/argo-cd/issues/15006
- https://argo-cd.readthedocs.io/en/stable/operator-manual/secret-management/
- https://argo-cd.readthedocs.io/en/latest/operator-manual/security/
- https://argo-cd.readthedocs.io/en/latest/developer-guide/api-docs/
- https://github.com/argoproj/argo-cd/issues/27773
- https://github.com/fluxcd/flux2/releases
- https://fluxcd.io/blog/2026/06/flux-v2.9.0/
- https://fluxcd.io/blog/2026/02/flux-v2.8.0/
- https://fluxcd.io/blog/2026/07/flux-schema-validation/
- https://fluxcd.io/flux/cli-plugins/flux-schema/
- https://github.com/fluxcd/flux-schema
- https://raw.githubusercontent.com/fluxcd/flux-schema/main/go.mod
- https://raw.githubusercontent.com/fluxcd/flux-schema/main/internal/validator/cel.go
- https://github.com/controlplaneio-fluxcd/schema-catalog
- https://github.com/fluxcd/plugins
- https://github.com/fluxcd/flux2/blob/main/rfcs/0013-cli-plugin-system/README.md
- https://fluxcd.io/flux/cmd/flux_plugin/
- https://fluxcd.io/flux/cmd/flux_build_kustomization/
- https://fluxcd.io/flux/cmd/flux_diff_kustomization/
- https://fluxcd.io/flux/cmd/flux_diff/
- https://fluxcd.io/flux/components/kustomize/kustomizations/
- https://fluxcd.io/flux/components/helm/helmreleases/
- https://fluxcd.io/flux/components/source/ocirepositories/
- https://fluxcd.io/flux/components/notification/providers/
- https://github.com/fluxcd/kustomize-controller/blob/main/CHANGELOG.md
- https://fluxcd.io/flux/faq/
- https://fluxcd.io/flux/guides/repository-structure/
- https://fluxcd.io/flux/cheatsheets/oci-artifacts/
- https://github.com/controlplaneio-fluxcd/flux-operator
- https://fluxoperator.dev/docs/resourcesets/github-pull-requests/
- https://github.com/akuity/kargo
- https://docs.kargo.io/user-guide/reference-docs/promotion-steps/
- https://docs.kargo.io/user-guide/reference-docs/promotion-steps/custom-steps
- https://akuity.io/blog/kargo-custom-steps-gitops-promotion
- https://akuity.io/blog/kargo-v1-10-custom-steps-http-notifications-new-promotions
- https://github.com/akuity/kargo/issues/4590
- https://akuity.io/pricing
- https://akuity.io/blog/the-rendered-manifests-pattern
- https://akuity.io/blog/gitops-best-practices-whitepaper
- https://github.com/argoproj-labs/gitops-promoter
- https://gitops-promoter.readthedocs.io/en/latest/crd-specs/
- https://github.com/dag-andersen/argocd-diff-preview
- https://dag-andersen.github.io/argocd-diff-preview/
- https://codefresh.io/blog/argo-cd-preview-diff/
- https://github.com/argoproj/argo-rollouts/releases
- https://github.com/fluxcd/flagger/blob/main/CHANGELOG.md
- https://github.com/weaveworks/weave-gitops/releases
- https://siliconangle.com/2024/02/05/kubernetes-automation-startup-weaveworks-shuts/
- https://octopus.com/blog/30-argo-cd-antipatterns-for-gitops
- https://octopus.com/blog/how-to-model-your-gitops-environments
- https://platformengineering.org/blog/gitops-architecture-patterns-and-anti-patterns
- https://github.com/cloudogu/gitops-patterns
- https://octopus.com/blog/argo-cd-verified-deployments
- https://octopus.com/news/octopus-acquires-codefresh
- https://docs.confighub.com/
- https://docs.confighub.com/guide/functions/
- https://docs.confighub.com/enterprise/self-hosting/
- https://github.com/confighub/cub-scout/issues/505
- https://techcrunch.com/2025/03/26/cloud-veterans-launch-confighub-to-fix-configuration-hell/

### Config languages and packaging
- https://github.com/helm/helm/releases
- https://helm.sh/blog/helm-4-released/
- https://helm.sh/blog/helm-v3-end-of-life/
- https://helm.sh/docs/topics/charts/
- https://helm.sh/docs/sdk/gosdk/
- https://helm.sh/docs/plugins/overview/
- https://helm.sh/docs/plugins/migrate/
- https://helm.sh/community/hips/hip-0026/
- https://github.com/helm/community/blob/main/hips/hip-0020.md
- https://helm.sh/docs/chart_template_guide/functions_and_pipelines/
- https://helm.sh/docs/helm/helm_template/
- https://github.com/helm/helm/pull/13283
- https://github.com/helm/helm/issues/13069
- https://github.com/helm/helm/issues/31260
- https://github.com/helm/helm/issues/7756
- https://github.com/helm/helm/issues/12994
- https://github.com/helm/helm/issues/11176
- https://github.com/losisin/helm-values-schema-json
- https://github.com/dadav/helm-schema
- https://github.com/norwoodj/helm-docs
- https://github.com/idsulik/helm-cel
- https://github.com/helm/chart-testing
- https://blog.artifacthub.io/blog/helm-values-schema-reference/
- https://github.com/bitnami/charts/blob/main/bitnami/redis/values.schema.json
- https://github.com/kubernetes-sigs/kustomize/releases
- https://kubectl.docs.kubernetes.io/guides/config_management/components/
- https://kubectl.docs.kubernetes.io/guides/extending_kustomize/
- https://github.com/kubernetes-sigs/kustomize/blob/master/cmd/config/docs/api-conventions/functions-spec.md
- https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/2953-kustomize-plugin-graduation
- https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/2906-kustomize-function-catalog
- https://github.com/kubernetes-sigs/kustomize/issues/5808
- https://github.com/kptdev/kpt/releases
- https://github.com/cue-lang/cue/releases
- https://github.com/stefanprodan/timoni/releases
- https://timoni.sh/cue/module/custom-resources/
- https://timoni.sh/gitops-flux
- https://timoni.sh/bundle/
- https://github.com/kcl-lang/kcl/releases
- https://www.kcl-lang.io/docs/tools/cli/kcl/vet
- https://github.com/apple/pkl/releases
- https://github.com/grafana/tanka/releases
- https://tanka.dev/diff-strategy/
- https://github.com/cdk8s-team/cdk8s/releases
- https://cdk8s.io/docs/latest/cli/synth/
- https://github.com/carvel-dev/ytt/releases
- https://carvel.dev/ytt/docs/latest/how-to-write-schema/
- https://github.com/holos-run/holos/releases
- https://github.com/werf/nelm/releases
- https://blog.werf.io/nelm-1-0-released-helm-chart-compatible-alternative-to-helm-3-5648b191f0af
- https://werf.io/docs/v2/usage/project_configuration/giterminism.html
- https://github.com/kro-run/kro/blob/main/website/docs/api/specifications/simple-schema.md
- https://github.com/yokecd/yoke

### Policy engines and offline validation
- https://github.com/kyverno/kyverno/releases
- https://kyverno.io/blog/2026/02/02/announcing-kyverno-release-1.17/
- https://kyverno.io/blog/2026/04/24/announcing-kyverno-release-1.18/
- https://kyverno.io/blog/2026/08/20/announcing-kyverno-release-1.19/
- https://kyverno.io/docs/subprojects/kyverno-cli/
- https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
- https://kyverno.io/docs/kyverno-cli/reference/kyverno_test/
- https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/commands/apply/command.go
- https://kyverno.io/docs/policy-types/cel-libraries/
- https://kyverno.io/docs/policy-types/validating-policy/
- https://kyverno.io/docs/installation/releases/
- https://github.com/kyverno/playground/blob/main/README.md
- https://pkg.go.dev/github.com/kyverno/playground/backend/pkg/engine
- https://github.com/kyverno/kyverno-json
- https://github.com/kyverno/kyverno/issues/5476
- https://dev.to/sodiqjimoh/your-kyverno-ci-is-lying-to-you-why-kyverno-cli-exits-0-on-policy-violations-1gg9
- https://nirmata.com/2026/07/26/kyverno-policy-migration-to-cel-based-policies/
- https://github.com/open-policy-agent/gatekeeper/releases
- https://open-policy-agent.github.io/gatekeeper/website/docs/gator/
- https://open-policy-agent.github.io/gatekeeper/website/docs/validating-admission-policy/
- https://open-policy-agent.github.io/gatekeeper/website/docs/constrainttemplates/
- https://open-policy-agent.github.io/gatekeeper/website/docs/sync/
- https://github.com/open-policy-agent/gatekeeper/issues/3337
- https://github.com/open-policy-agent/opa/releases
- https://github.com/open-policy-agent/conftest/releases
- https://github.com/kubewarden/kubewarden-controller/releases
- https://github.com/kubewarden/kwctl
- https://docs.kubewarden.io/reference/kwctl-cli
- https://docs.kubewarden.io/admission-controller/1.35/en/explanations/context-aware-policies.html
- https://github.com/vicenteherrera/psa-checker
- https://github.com/yannh/kubeconform
- https://github.com/yannh/kubeconform/releases
- https://github.com/yannh/kubernetes-json-schema
- https://github.com/datreeio/CRDs-catalog
- https://github.com/datreeio/CRDs-catalog/blob/main/Utilities/crd-extractor.sh
- https://github.com/steadforce/crds-catalog
- https://github.com/datreeio/datree
- https://github.com/stackrox/kube-linter
- https://github.com/FairwindsOps/polaris
- https://github.com/zegl/kube-score
- https://github.com/FairwindsOps/pluto
- https://github.com/doitintl/kube-no-trouble
- https://github.com/bridgecrewio/checkov
- https://trivy.dev/latest/docs/scanner/misconfiguration/
- https://github.com/loft-sh/jspolicy
- https://github.com/zemanlx/kat
- https://oneuptime.com/blog/post/2026-02-09-server-side-dry-run-validate-manifests/view
- https://oneuptime.com/blog/post/2026-09-04-trace-blocking-admission-webhook/view
- https://bex.co/blog/2026/09/07/mutating-admission-policies-kubernetes-136
- https://www.cncf.io/blog/2026/04/02/gitops-policy-as-code-securing-kubernetes-with-argo-cd-and-kyverno/

### Pseudo cluster, inner loop and preview environments
- https://kwok.sigs.k8s.io/docs/user/kwokctl-snapshot/
- https://kwok.sigs.k8s.io/docs/user/kwokctl-admission/
- https://github.com/kubernetes-sigs/kwok/releases/tag/v0.8.0
- https://book.kubebuilder.io/reference/envtest.html
- https://github.com/kubernetes-sigs/controller-runtime/releases
- https://github.com/kubernetes-sigs/kube-scheduler-simulator
- https://github.com/kubernetes-sigs/cluster-capacity
- https://github.com/robscott/kube-capacity
- https://github.com/kubernetes-sigs/kind/releases
- https://github.com/k3d-io/k3d/releases
- https://github.com/loft-sh/vcluster/releases
- https://www.vcluster.com/blog/pull-request-testing-on-kubernetes
- https://www.signadot.com/blog/telepresence-mirrord-okteto-alternatives/
- https://www.okteto.com/docs/previews/
- https://github.com/UffizziCloud/uffizzi
- https://github.com/databus23/helm-diff
- https://github.com/itaysk/kubectl-neat
- https://github.com/vince-riv/argo-diff
- https://github.com/argocd-diff-action/argocd-diff-action
- https://github.com/kubeshop/monokle
- https://monokle.io/blog/end-of-life-announcement-for-monokle-cloud
- https://github.com/kubevious/kubevious
- https://www.kubernetes.io/blog/2019/01/14/apiserver-dry-run-and-kubectl-diff/
- https://github.com/prometheus-operator/prometheus-operator/issues/3704

### Docker Compose and single-host
- https://github.com/docker/compose/releases
- https://github.com/compose-spec/compose-go/releases
- https://raw.githubusercontent.com/compose-spec/compose-spec/main/schema/compose-spec.json
- https://raw.githubusercontent.com/compose-spec/compose-spec/main/spec.md
- https://docs.docker.com/reference/cli/docker/compose/config/
- https://docs.docker.com/reference/cli/docker/compose/publish/
- https://docs.docker.com/compose/how-tos/provider-services/
- https://raw.githubusercontent.com/docker/compose/main/docs/extension.md
- https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/
- https://docs.docker.com/compose/how-tos/environment-variables/envvars-precedence/
- https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/
- https://docs.docker.com/reference/compose-file/services/
- https://docs.docker.com/reference/compose-file/extension/
- https://docs.docker.com/compose/how-tos/production/
- https://docs.docker.com/compose/bridge/
- https://docs.docker.com/engine/swarm/
- https://docs.docker.com/engine/deprecated/
- https://github.com/docker/docker-language-server
- https://github.com/kubernetes/kompose/releases
- https://docs.portainer.io/release-notes
- https://docs.portainer.io/advanced/app-templates/format
- https://docs.portainer.io/admin/environments/policies.md
- https://docs.portainer.io/user/docker/stacks/add
- https://github.com/moghtech/komodo
- https://github.com/moghtech/komodo/blob/main/docsite/docs/automate/sync-resources.md
- https://github.com/coollabsio/coolify/releases
- https://coolify.io/docs/knowledge-base/docker/compose
- https://github.com/Dokploy/dokploy/releases
- https://docs.dokploy.com/docs/core/docker-compose
- https://github.com/psviderski/uncloud
- https://github.com/basecamp/kamal
- https://github.com/containrrr/watchtower/discussions/2135
- https://github.com/nicholas-fedor/watchtower
- https://github.com/getwud/wud
- https://github.com/zavoloklom/docker-compose-linter
- https://docs.kics.io/latest/platforms/
- https://github.com/containers/podman/releases
- https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html
- https://docs.podman.io/en/latest/markdown/podman-kube-play.1.html
- https://github.com/containers/podman-compose
- https://github.com/containers/podlet
- https://oneuptime.com/blog/post/2026-02-08-how-to-debug-why-docker-compose-services-wont-start/view
- https://blog.gntech.me/posts/2026-05-24-docker-compose-env-variables/
- https://www.portainer.io/blog/docker-swarm-still-works-but-does-it-still-have-a-future

### IDPs, config UIs and vendor-to-customer tooling
- https://github.com/devtron-labs/devtron/releases
- https://docs.devtron.ai/docs/user-guide/app-management/policies/lock-deployment-config
- https://docs.devtron.ai/docs/devtron/v1.8/user-guide/global-configurations/approval-policy
- https://docs.devtron.ai/docs/user-guide/creating-application/base-config/deployment-template
- https://devtron.ai/pricing
- https://github.com/cyclops-ui/cyclops/releases
- https://www.cyclops-ui.com/docs/installation/git-write/
- https://github.com/glasskube/glasskube
- https://github.com/glasskube/distr
- https://glasskube.dev/products/package-manager/docs/reference/package-manifest/
- https://github.com/sap/kubeapps
- https://github.com/vmware-tanzu/kubeapps/issues/5982
- https://backstage.io/docs/features/software-templates/writing-templates/
- https://backstage.io/docs/features/software-templates/writing-custom-field-extensions/
- https://backstage.io/docs/overview/versioning-policy/
- https://docs.port.io/actions-and-automations/create-self-service-experiences/setup-ui-for-action/user-inputs/
- https://docs.port.io/guides/all/let-developers-enrich-services-using-gitops/
- https://docs.cortex.io/streamline/workflows.md
- https://docs.opslevel.com/docs/configuring-custom-actions-manual-inputs
- https://ranchermanager.docs.rancher.com/how-to-guides/new-user-guides/helm-charts-in-rancher/create-apps
- https://github.com/rancher/rancher/wiki/Understanding-How-Rancher-Configures-Helm-Charts
- https://github.com/rancher/dashboard/issues/4706
- https://github.com/openshift/console/blob/main/frontend/packages/operator-lifecycle-manager/src/components/descriptors/reference/reference.md
- https://docs.replicated.com/reference/custom-resource-config
- https://docs.replicated.com/reference/custom-resource-preflight
- https://docs.replicated.com/vendor/preflight-defining
- https://docs.replicated.com/vendor/helm-optional-value-keys
- https://docs.replicated.com/vendor/testing-about
- https://www.replicated.com/pricing
- https://www.replicated.com/blog/introducing-the-state-of-self-hosted-survey-2025
- https://github.com/replicatedhq/troubleshoot
- https://troubleshoot.sh/docs/analyze/
- https://github.com/replicatedhq/kots/releases
- https://docs.spectrocloud.com/registries-and-packs/pack-constraints/
- https://octopus.com/docs/tenants
- https://github.com/nuonco/nuon
- https://docs.nuon.co/configuration-files
- https://docs.omnistrate.com/usecases/byoc/
- https://docs.massdriver.cloud/bundles
- https://docs.plural.sh/how-to/deploy/pr-automation
- https://docs.crossplane.io/latest/cli/command-reference
- https://docs.crossplane.io/latest/composition/composite-resource-definitions/
- https://github.com/crossplane/crossplane/releases
- https://docs.kratix.io/main/reference/statestore/gitstatestore
- https://kubevela.io/docs/reference/ui-schema/
- https://github.com/score-spec/score-k8s/releases
- https://docs.score.dev/docs/score-specification/score-spec-reference/
- https://humanitec.com/products/platform-orchestrator
- https://github.com/kubernetes-sigs/headlamp/releases
- https://headlamp.dev/docs/latest/development/plugins/
- https://github.com/kubernetes/dashboard
- https://github.com/komodorio/helm-dashboard
- https://github.com/freelensapp/freelens/releases
- https://k9scli.io/topics/plugins/
- https://github.com/rjsf-team/react-jsonschema-form/releases
- https://rjsf-team.github.io/react-jsonschema-form/docs/usage/validation/
- https://jsonforms.io/docs/uischema/rules
- https://tag-app-delivery.cncf.io/whitepapers/platform-eng-maturity-model/
- https://www.qovery.com/blog/gitops-tools-kubernetes-ai-agents-compared

### Secrets and multi-environment
- https://github.com/external-secrets/external-secrets/releases
- https://external-secrets.io/latest/introduction/overview/
- https://github.com/getsops/sops/releases
- https://getsops.io/docs/
- https://fluxcd.io/flux/guides/mozilla-sops/
- https://github.com/bitnami/sealed-secrets/releases
- https://github.com/bitnami/sealed-secrets/issues/1982
- https://github.com/hashicorp/vault-secrets-operator
- https://github.com/hashicorp/vault-secrets-operator/blob/main/LICENSE
- https://github.com/argoproj-labs/argocd-vault-plugin
- https://infisical.com/docs/integrations/platforms/kubernetes/infisical-secret-crd
- https://github.com/DopplerHQ/kubernetes-operator
- https://github.com/1Password/onepassword-operator
- https://helmfile.readthedocs.io/en/latest/environments/
- https://fluxcd.io/flux/guides/image-update/
- https://github.com/argoproj-labs/argocd-image-updater/releases
- https://docs.renovatebot.com/modules/manager/helm-values/
- https://docs.renovatebot.com/modules/manager/regex/
- https://docs.github.com/en/code-security/dependabot/ecosystems-supported-by-dependabot/supported-ecosystems-and-repositories
- https://github.com/snyk/driftctl

### Plugin architectures and extension protocols
- https://github.com/hashicorp/go-plugin
- https://developer.hashicorp.com/terraform/plugin/terraform-plugin-protocol
- https://developer.hashicorp.com/terraform/internals/provider-registry-protocol
- https://developer.hashicorp.com/terraform/plugin/framework/functions
- https://opentofu.org/docs/cli/oci_registries/provider-mirror/
- https://developer.hashicorp.com/vault/docs/plugins/containerized-plugins
- https://www.pulumi.com/docs/iac/guides/building-extending/providers/implementers/protocol-reference/
- https://raw.githubusercontent.com/crossplane/crossplane/main/proto/fn/v1/run_function.proto
- https://github.com/crossplane/function-sdk-go
- https://docs.dagger.io/features/modules
- https://krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/
- https://extism.org/docs/concepts/manifest/
- https://github.com/extism/go-sdk/releases
- https://github.com/knqyf263/go-plugin
- https://github.com/envoyproxy/envoy/issues/35420
- https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/dynamic_modules
- https://grafana.com/docs/grafana/latest/administration/plugin-management/plugin-frontend-sandbox/
- https://backstage.io/api/next/modules/_backstage_backend-dynamic-feature-service.html
- https://code.visualstudio.com/api/advanced-topics/extension-host
- https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/
- https://blog.modelcontextprotocol.io/posts/2026-07-28/
- https://github.com/modelcontextprotocol/registry
- https://github.com/renovatebot/renovate/discussions/34567
- https://github.com/argoproj-labs/mcp-for-argocd (verify vs https://github.com/akuity/argocd-mcp)
- https://fluxoperator.dev/mcp-server/
- https://github.com/fluxcd/agent-skills
- https://github.com/containers/kubernetes-mcp-server

### Schema, forms and editor integration
- https://json-schema.org/draft/2020-12/json-schema-core
- https://github.com/redhat-developer/yaml-language-server
- https://www.schemastore.org/api/json/catalog.json
- https://mcp.schemastore.org/
- https://schemas.fluxoperator.dev/catalog/versions/kubernetes/v1.35
- https://registry.npmjs.org/@rjsf/core/latest
- https://registry.npmjs.org/@jsonforms/core/latest

### Managed distributions and regulated environments
- https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/authentication_and_authorization/managing-pod-security-policies
- https://docs.okd.io/4.20/applications/projects/configuring-project-creation.html
- https://docs.okd.io/4.20/networking/ingress_load_balancing/configuring_ingress_cluster_traffic/ingress-gateway-api.html
- https://docs.okd.io/latest/rest_api/config_apis/imagedigestmirrorset-config-openshift-io-v1.html
- https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/nodes/nodes-sigstore-using
- https://developers.redhat.com/articles/2025/06/02/manage-operators-clusterextensions-olm-v1
- https://github.com/openshift/cluster-kube-apiserver-operator/blob/master/bindata/assets/config/defaultconfig.yaml
- https://docs.cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security
- https://docs.cloud.google.com/kubernetes-engine/docs/concepts/autopilot-resource-requests
- https://docs.cloud.google.com/kubernetes-engine/docs/concepts/about-autopilot-privileged-workloads
- https://docs.cloud.google.com/kubernetes-engine/policy-controller/docs/overview
- https://docs.cloud.google.com/kubernetes-engine/docs/how-to/podsecurityadmission
- https://learn.microsoft.com/en-us/azure/aks/deployment-safeguards
- https://learn.microsoft.com/en-us/azure/governance/policy/concepts/policy-for-kubernetes
- https://aws.amazon.com/blogs/containers/implementing-pod-security-standards-in-amazon-eks/
- https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html
- https://docs.aws.amazon.com/eks/latest/userguide/automode.html
- https://docs.aws.amazon.com/whitepapers/latest/security-overview-amazon-eks-auto-mode/workloads.html
- https://documentation.suse.com/cloudnative/rancher-manager/latest/en/security/psact.html
- https://ranchermanager.docs.rancher.com/how-to-guides/advanced-user-guides/manage-projects/manage-project-resource-quotas/about-project-resource-quotas
- https://github.com/rancher/webhook/blob/main/docs.md
- https://slsa.dev/spec/v0.1/verification_summary
- https://github.com/in-toto/attestation/tree/main/spec/predicates
- https://github.com/sigstore/cosign/releases
- https://github.com/sigstore/cosign/blob/main/specs/BUNDLE_SPEC.md
- https://edu.chainguard.dev/open-source/sigstore/cosign/verifying-in-air-gapped-environments/
- https://some-natalie.dev/blog/cosign-disconnected/
- https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03183/TR-03183_node.html
- https://github.com/zarf-dev/zarf/releases
- https://github.com/hauler-dev/hauler/releases
- https://docs.replicated.com/embedded-cluster/v2/installing-embedded-air-gap
- https://github.com/oras-project/oras/releases
- https://www.linuxfoundation.org/legal/trademark-usage
- https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license
- https://developer.hashicorp.com/nomad/docs/commands/job/plan
- https://docs.podman.io/en/latest/markdown/podman-quadlet.1.html

### Pain-point and adoption evidence
- https://komodor.com/blog/komodor-2025-enterprise-kubernetes-report-finds-nearly-80-of-production-outages/
- https://www.businesswire.com/news/home/20250917424603/en/Komodor-2025-Enterprise-Kubernetes-Report-Finds-Nearly-80-of-Production-Outages-are-Due-to-System-Changes
- https://www.cncf.io/announcements/2026/01/20/kubernetes-established-as-the-de-facto-operating-system-for-ai-as-production-use-hits-82-in-2025-cncf-annual-cloud-native-survey/
- https://www.cncf.io/announcements/2025/07/24/cncf-end-user-survey-finds-argo-cd-as-majority-adopted-gitops-solution-for-kubernetes/
- https://dora.dev/research/2025/dora-report/
- https://leecampbell.com/2026/01/12/navigating-dora-2025-a-guide-to-rework-rate-and-visualising-archetypes/
- https://www.redhat.com/en/blog/state-kubernetes-security-2024
- https://www.cncf.io/blog/2024/01/26/2024-kubernetes-benchmark-report-the-latest-analysis-of-kubernetes-workloads/
- https://leaddev.com/technical-direction/we-halved-our-continuous-integration-pipeline
- https://leanopstech.com/blog/ci-cd-pipeline-costs-github-actions-circleci-gitlab-2026/
- https://gitlab.com/gitlab-org/gitlab/-/issues/388091
- https://k8s.af/
- https://github.com/hjacobs/kubernetes-failure-stories
- https://news.ycombinator.com/item?id=44608856
- https://news.ycombinator.com/item?id=40789862
- https://news.ycombinator.com/item?id=44752469
- https://news.ycombinator.com/item?id=42366665
- https://komodor.com/learn/kubernetes-troubleshooting-the-complete-guide/
- https://adhdecode.com/debugging/helm/error-webhook-denied-the-request/
- https://medium.com/kotaicode/gitops-rollbacks-why-git-revert-isnt-always-enough-8281e16a14ba
- https://dev.to/oleksandr_kuryzhev_42873f/argo-cd-rollback-checklist-for-safe-gitops-sync-ab5
- https://oneuptime.com/blog/post/2026-02-26-argocd-pre-commit-hooks-manifests/view
- https://github.com/PrivateBin/helm-chart/issues/56
- https://github.com/kubernetes-sigs/headlamp/issues/2579
- https://www.docker.com/blog/2025-docker-state-of-app-dev/
- https://survey.stackoverflow.co/2025/technology