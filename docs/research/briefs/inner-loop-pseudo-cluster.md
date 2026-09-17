# inner-loop-pseudo-cluster

## State of the slice (Sept 2026)

The dominant production practice is still "render → diff → server-side dry-run → merge → controller syncs". Argo CD v3.5.2 (2026-08-27) ships the rendered-manifests pattern as the Source Hydrator, **Beta since v3.5, not GA** (a wener.me note claiming GA in Feb 2026 contradicts official docs; GA tracked in issue #28143): drySource → hydrated YAML committed to syncSource/hydrateTo branches with commit trailers, Git notes, templated READMEs; Git-only (no OCI), no secrets during hydration, no non-deterministic external lookups. Flux v2.9.5 (2026-08-31) has `flux build kustomization` (offline via `--kustomization-file`, `--local-sources Kind/ns/name=path`, `--dry-run`) and `flux diff kustomization` (server-side dry-run against a LIVE cluster; exit 0/1/>1). PR-diff bots (argocd-diff-preview v0.2.14 Aug 2026, 743★, ArgoCon EU 2026 talk; argo-diff 2.14.0; argocd-diff-action 0.7.3) post rendered diffs as PR comments; argocd-diff-preview documents 60–90 s overhead for an ephemeral kind+Argo CD cluster vs <10 s against pre-installed Argo CD. Kargo v1.11.4 (2026-09-03) is the promotion layer (helm-template/kustomize-build/git-open-pr/argocd-update steps) with **no validation step**. Helm 4.3.0 (2026-09-09) keeps `--dry-run=none|client|server`; `lookup` only returns data with `--dry-run=server`.

Diffing and server-side dry-run are mature but cluster-bound. `kubectl diff` / `kubectl apply --dry-run=server --validate=strict` run the full admission chain (schema, server-side field validation, defaulting, mutating webhooks with sideEffects None/NoneOnDryRun, validating webhooks, VAP (GA 1.30), MAP (GA 1.36), PodSecurity, ResourceQuota, LimitRanger, RBAC) but never run controllers, scheduling, image pulls, PVC binding, or ordering checks; webhooks with side effects are skipped; quota is checked against current usage only. Argo CD Server-Side Diff (stable since v3.1) uses the same SSA dry-run but **skips new resources** and mutation webhooks unless `IncludeMutationWebhook=true`. helm-diff v3.15.13 (2026-09-11) and dyff v1.12.0 (2026-04-26) are the de-facto semantic diff stack; kubectl-neat is unmaintained (last release v2.0.4, Jul 2024).

Offline validators form a fidelity ladder, not one tool: (1) pure schema (kubeconform v0.8.0; kubectl-validate as a Go library); (2) offline policy engines (Kyverno CLI v1.19.1, gator v3.23.1, kwctl) — none evaluate ResourceQuota/LimitRange/PSA/node capacity; (3) real API server without kubelet (envtest; KWOK v0.8.0, June 2026, default kube-apiserver v1.36.1, supports 1.31–1.36, **all default admission plugins live** — verified in `pkg/kwokctl/components/kube_apiserver.go`; `kwokctl snapshot export --kubeconfig … --path snapshot.yaml` + `snapshot restore/replay` is the closest existing "validate against a cluster snapshot" primitive); Kubernetes 1.37 (2026-08-26) adds Beta manifest-based admission (`ManifestBasedAdmissionControlConfig`, `--admission-control-config-file` with `staticManifestsDir`, files `*.static.k8s.io`, no paramKind/paramRef); (4) scheduler/capacity tools are dormant or live-only; (5) real clusters (kind v0.33.0, k3d v5.9.0, vcluster v0.36.1 at ~62 s vs 5–7 min GKE) and preview products. Direction: policy converging on CEL inside the API server (offline evaluation with `k8s.io/apiserver` CEL libs feasible); rendered manifests + PR diff comments becoming the default review surface; preview envs moving from "clone the stack" to "shared cluster + delta" (Signadot, vcluster sleep) due to cost and agent-driven change volume; the static-analysis UI generation (Datree archived Apr 2024, Monokle unmaintained, Kubevious 2022, kubepug/kubent 2023/2024) is dead, leaving whitespace. **Nobody ships one artifact = cluster snapshot (OpenAPI v3 + CRDs + VAP/MAP + Kyverno/Gatekeeper + quotas/limits + namespaces/PSA labels + nodes/allocatable + classes + workloads) plus an in-process evaluator <2 s that escalates to KWOK/envtest <1 min.**

Fidelity/cost matrix (measured where noted, else estimates): pure schema — ms, type/required/enum/CRD-CEL only; schema + offline policy — 10s–100s ms, policy denials incl. parameterised if params in snapshot, misses quota/PSA-exemptions/webhooks; envtest + CRDs + static admission — ~2–10 s boot, full built-in admission minus webhook backends and quota usage unless seeded; KWOK binary runtime + restored snapshot — seconds ("almost instantly", 20 nodes/pods per second claimed), sees everything API server + quota controller + scheduler with fake nodes see, no image pulls/webhook backends/runtime; vcluster/kind/k3d — 30–90 s, real kubelet but not target policies unless mirrored; real cluster `--dry-run=server` — RTT, highest admission fidelity, no scheduling, needs credentials, leaks pre-merge intent to prod.

## Tools

| Tool | Status (verified via GitHub API 2026-09-12 unless noted) | Values model | Validation | Pre-cluster/offline | Plugin model | Relevance |
|---|---|---|---|---|---|---|
| Argo CD Source Hydrator | Beta since v3.5 (v3.5.2, 24.1k★); disabled by default (`hydrator.enabled`) | Values in drySource; hydrated YAML on separate branch | None at hydration | No; produces the artifact validators consume | CMP sidecars for renderers | Integrate: emit hydrator-shaped output (branch+trailers); Argo owns rendering, zhi owns values+validation |
| Kargo | v1.11.4, 3.6k★, Apache-2.0 | Freight + expressions mutate values files | None; only custom-step/http | No | Custom steps = arbitrary images | Whitespace: `zhi validate` step image before git-open-pr |
| argocd-diff-preview | v0.2.14, 743★ | Whatever Apps point at | Render errors only | Render yes, validate no | — | Copy UX: PR diff comment with filtering, two-tier speed |
| argo-diff / argocd-diff-action | 2.14.0 (59★) / 0.7.3 (28★) | — | Live Argo diff | No | — | Reference PR-comment plumbing |
| `argocd app diff` / Server-Side Diff | stable since v3.1; flags `--local`, `--server-side-generate`, `--exit-code`, `--diff-exit-code`, `--server-side-diff-max-batch-kb` (250) | — | SSA dry-run admission; skips new resources | No (needs Argo API + cluster even with --local) | — | Output must stay Argo-diffable; do "admission in diff" against snapshot |
| Flux build/diff kustomization | v2.9.5, 8.4k★ | Kustomize + postBuild.substitute/substituteFrom; HelmRelease values | build: kustomize+substitution; diff: live dry-run | build yes; diff no | — | Call/embed `flux build` for rendering, own the validation |
| Flux Operator ResourceSetInputProvider | v0.60.0 (2026-09-11), 749★ | — | Real deployment | No | — | Trigger preview via PR label after validation |
| Argo ApplicationSet PR generator | GA in 3.x; polls every 30 min default | — | Real deployment | No | — | Downstream minutes tier |
| kubectl diff / `--dry-run=server --validate=strict` | K8s v1.37.0 | — | Full admission; no controllers/scheduling/capacity/images/PVC/ordering | No | — | Fidelity ceiling; oracle for offline evaluator tests |
| Helm `--dry-run` / template / lookup | v4.3.0, 30.2k★; `--kube-version`, `--api-versions`, `--include-crds`, `--is-upgrade` | — | client: syntax+optional OpenAPI; server: cluster OpenAPI+lookup; no admission | Yes if Capabilities/lookup faked | — | Feed Capabilities/lookup from snapshot |
| helm-diff | v3.15.13, 3.5k★; `--three-way-merge`, `--output dyff`, `--show-secrets` | — | Optional OpenAPI | `diff local`/client yes | — | Copy redaction, normalization, dyff output |
| dyff | v1.12.0, 1.9k★, MIT, Go 1.23+; `--output github\|gitlab` | — | None | N/A | Go library | Embed for semantic diffs |
| kubectl-neat | Unmaintained (v2.0.4, 2024-07-12) | — | None | N/A | — | Reimplement normalization; no binary dep |
| kubectl-validate | v0.0.4 (2024-05-29); main bumped to k8s 1.35 (2026-01-05); 276★ | Full manifests | Schema+defaults+CRD CEL (inferred from source); no admission/quota | Yes; `--local-schemas` layout `/api/<v>.json`, `/apis/<g>/<v>.json`; `--local-crds` | Go lib `pkg/validator`, `openapi.Client` interface | Embed for <2 s tier; vendor/fork |
| kubeconform | v0.8.0 (2026-06-04), 3.2k★ | — | JSON schema only; CRDs via datreeio/CRDs-catalog | Yes | — | Compete/replace: match speed, exceed fidelity |
| Kyverno CLI | v1.19.1 (2026-09-10), 8.1k★ | — | ClusterPolicy, CEL ValidatingPolicy/MutatingPolicy, native VAP+VAPB; `--values-file`/`--context-file` fake context; `--cluster` | Yes with hand context | — | Import policies into snapshot; auto-generate context |
| Gatekeeper gator | v3.23.1 (2026-08-27), 4.3k★ | — | Rego/CEL; inventory for referential data (not for CEL); needs metadata.namespace | Yes | — | Export templates/constraints+inventory |
| Kubewarden kwctl | repo archived Jan 2026, moved into kubewarden-controller monorepo (1.32.0+) | — | Wasm on AdmissionReview | Yes | — | Low priority |
| VAP/MAP/manifest-based admission | VAP GA 1.30; MAP GA 1.36; manifest-based Beta on-by-default 1.37 | — | In-apiserver CEL; offline via `k8s.io/apiserver/pkg/admission/plugin/policy/validating` | Policy format snapshot must capture | — | Implement <2 s tier; pre-seed pseudo cluster |
| KWOK/kwokctl | v0.8.0, 3.2k★; `--kube-admission` default true; runtimes binary/docker/podman/kind | N/A | Stock apiserver admission incl. VAP/MAP/quota/PSA; scheduler on fake nodes | Yes after one `snapshot export` | Stages (CEL/go-template) | <1 min tier core |
| kube-scheduler-simulator | dormant: v0.4.0 (2024-11-09), last commit 2025-09-21, deps k8s 1.32 | — | Scheduling only | Yes with Docker+kubeconfig import | — | Copy import list + debuggable-scheduler UX |
| cluster-capacity | v0.30.0 (2024-05-30); Red Hat commits Aug–Sept 2026 (k8s 1.36) | — | CPU/mem/ephemeral fit only | No (live) | — | Copy: embed scheduler framework in-process |
| kube-capacity | v0.8.0 (2024-02-21) | — | Observational | No | — | Capture same allocatable/requests data |
| envtest | controller-runtime v0.25.0 (2026-09-03) | — | API server only; no kcm/scheduler/GC; quota usage only if seeded | Yes once binaries cached (20 s default timeout) | — | Alt <1 min tier without scheduling |
| kine | v0.17.0 (2026-08-26) | — | N/A | Enabler: apiserver+SQLite, no etcd | — | Lightest real-apiserver pseudo cluster; in-process apiserver unsupported upstream |
| kind / k3d | v0.33.0 (15.5k★) / v5.9.0 (6.6k★) | — | Real cluster, none of target policies | Tens of seconds | — | CI/e2e only |
| vcluster | v0.36.1, 11.3k★; 62 s create (vendor) | — | Real; host policies apply to synced pods, not virtual API | No | — | Preview tier; does not mirror host VAP/Kyverno |
| Okteto | CLI 3.23.1 | — | None | No | — | Full-stack clone model (cost critique) |
| Signadot | CLI v1.8.0 | — | None | No | — | Delta-not-clone direction |
| Uffizzi | dormant (last commit 2024-08-07) | — | None | No | — | Evidence clone-per-PR OSS didn't sustain |
| Garden/Tilt/Skaffold/DevSpace | 0.14.20 (slowing) / v0.37.7 / v2.24.0 / v6.3.21 | — | Dev cluster | Render only | — | Not competitors; post-validation apply |
| Static linters | kube-linter v0.8.3, Polaris v10.2.2, Pluto v5.24.3, Kubescape v4.0.14 active; kube-score, kubent, kubepug stale | — | Opinionated rules | Yes | — | Optional plugins; Pluto-style deprecation belongs in snapshot validator |
| Datree/Monokle/Kubevious | archived / "not able to maintain" (v2.4.8 2024-05) / 2022 | — | Schema+policy; Monokle cluster compare | Partial | — | Whitespace evidence; learn Monokle feature list |
| Cyclops | stalled: v0.21.1 (2025-06-26), 3.3k★ | Helm values.schema.json forms | Values JSON schema only | No cluster awareness | — | Closest OSS "UI with validation" competitor; gap = cluster-policy awareness |

## Strongest pain-point evidence

- Values-only diffs hide blast radius: PRs "show only parameter changes, not the resulting manifest differences reviewers actually need"; can't tell a two-line from a two-hundred-line change (Akuity, akuity.io/blog/the-rendered-manifests-pattern).
- No audit trail: rendered manifests "live briefly in the repo-server's cache and are then gone"; compliance questions "Git can't answer" (Akuity).
- Accurate diffs need a cluster with Argo CD: 60–90 s ephemeral vs <10 s pre-installed; tool sells "no access to production" (dag-andersen.github.io/argocd-diff-preview).
- Server-side dry-run needs live access and misses dependencies, eventual consistency, timing; "always test in a staging environment" (oneuptime.com 2026-02-09).
- Argo SSD "skips new resources"; mutation webhooks excluded by default — first deploys least validated (argo-cd diff-strategies docs).
- Offline policy tools need hand-authored context: Kyverno `--values-file/--context-file`; gator inventory files, "cannot determine if a type is Namespace-scoped" (gator docs).
- Schema-only validators miss server rejections: kubeconform "controllers still perform additional server-side validations"; kubectl-validate "best-effort replication" (kubeconform README).
- Flux diff needs a live cluster; flux build fetches the Kustomization from cluster unless `--kustomization-file` (fluxcd.io).
- Helm offline lies: lookup empty except `--dry-run=server`; Capabilities faked via `--kube-version/--api-versions` (helm.sh functions_and_pipelines).
- Source Hydrator: "Cannot use tools injecting secrets during hydration"; "No deterministic external lookups" (argo-cd source-hydrator docs).
- Clone-per-PR cost "scales linearly with the number of services"; request routing "keeps cost flat… as coding agents multiply the number of changes" (signadot.com blog).
- Desktop validators abandoned: Monokle "not able to maintain or evolve"; Datree archived; kubectl-neat "concluded".
- No offline scheduling/capacity answer: cluster-capacity "operates live… not offline against snapshots"; scheduler-simulator dormant.
- PR generator polls "defaulting to every 30 minutes" without webhooks (Argo docs).

## Design implications

1. **Pseudo cluster = versioned snapshot bundle** (OCI artifact or directory, read-only RBAC): `/openapi/v3` docs in kubectl-validate `--local-schemas` layout; all CRDs; server version + served API groups (deprecation checks, Helm Capabilities); VAP/VAPB + MAP/MAPB + param objects; Validating/MutatingWebhookConfigurations metadata (guarded GVKs, sideEffects, failurePolicy); Kyverno policies/exceptions; Gatekeeper templates/constraints + inventory; Namespaces with `pod-security.kubernetes.io/*` labels; ResourceQuota/LimitRange with `status.used`; Nodes (allocatable, labels, taints); Pods (requests, nodeName) or per-node aggregates; Storage/Priority/Ingress/RuntimeClasses; objects Helm `lookup` reads. Reuse `kwokctl snapshot export --filter` format so it replays into KWOK.
2. **Three-tier ladder with explicit fidelity labels.** Tier 0 (<2 s, every keystroke): render (helm template with faked Capabilities + snapshot-backed lookup, kustomize build, `flux build --kustomization-file`) then in-process: kubectl-validate lib, VAP/MAP CEL via k8s.io/apiserver policy packages with snapshot params/namespaceObject, Kyverno/Gatekeeper engines as libs with auto-generated context, PodSecurity admission lib, quota arithmetic + LimitRange defaulting, scheduler-framework fit check. Tier 1 (<60 s, pre-commit/`zhi verify`): kwokctl binary runtime (or envtest+kine when no scheduling) with restored snapshot + manifest-based admission (1.37+), `kubectl apply --dry-run=server --validate=strict` plus real apply to observe scheduling/quota; cache the booted cluster. Tier 2 (minutes, CI): real-cluster dry-run via CI creds, argocd-diff-preview/flux diff, or Argo PR-generator / Flux ResourceSet previews. Never present Tier 0 as "will apply"; show what each tier cannot see.
3. **Ship rendered manifests, not just values**: every save yields hydrated YAML + semantic diff (embed dyff; GitHub/GitLab markdown). Optionally commit to a Source Hydrator-compatible branch (trailers, README) or Kargo-compatible files.
4. **API server is the primary policy "plugin"**: support VAP/MAP CEL, Kyverno, Gatekeeper, OpenAPI/CRD schema natively by importing cluster objects. Plugin surface for renderers (helm/kustomize/flux/plain/compose), snapshot importers (K8s, Compose host facts), value sources/stores, output sinks (git branch, PR comment). Validator plugins = escape hatch (exec/gRPC returning Severity), not the main path.
5. Reuse kubectl-validate's `openapi.Client` (composite/overlay/fallback) as the schema source model; vendor it (releases lag, main tracks 1.35).
6. Helm rendering with snapshot-driven `--kube-version`/`--api-versions` and snapshot-backed `lookup`; warn when a chart looks up a kind absent from the snapshot.
7. Quota/capacity as first-class Warning results: (new/changed requests + `status.used`) vs hard; scheduler framework against snapshot nodes → "fits on N nodes" — the one check no offline tool offers.
8. First-class CREATE paths: snapshot carries namespaced-vs-cluster scope per GVK from discovery.
9. `zhi snapshot refresh` job (in-cluster or CI, scheduled) pushing a signed OCI bundle; show snapshot age; block Tier 1/2 claims on stale snapshots.
10. Docker Compose analogue: host facts (Docker/Compose version, ports, networks, volumes, image digests) + compose-spec schema; `docker compose config` is render; no dry-run admission.
11. Budget: Tier 0 needs no Docker/binaries; Tier 1 needs ~200–400 MB control-plane binaries per K8s minor (`~/.kwok/cache`), pinned to snapshot server version.
12. Severity mapping: VAP validationActions Deny→Blocking, Warn→Warning, Audit→Info; Kyverno Enforce/Audit; honour PolicyExceptions and Gatekeeper enforcementAction.
13. PR-comment output in argocd-diff-preview style (collapsible per-app, path filters) plus SARIF/JSON for CI gates.
14. Do not build preview-env orchestration; integrate via PR labels (Flux ResourceSet) or Argo PR generator, link preview URL back.

## Uncertainties

- Start-up times not benchmarked: KWOK "almost instantly" (README), envtest 20 s timeout/typically seconds, kind 60–90 s (argocd-diff-preview incl. Argo), vcluster 62 s (vendor). Benchmark before promising "<1 min pre-commit".
- kubectl-validate CEL support inferred from source (`customresource.NewStrategy`, cel-go in go.mod), not docs; test with `x-kubernetes-validations`.
- KWOK + VAP: verified by reading `kube_apiserver.go`, not run empirically; unknown whether fake-node controller interferes with ResourceQuota/LimitRanger status.
- envtest passing `--admission-control-config-file` (1.37 beta) via KubeAPIServer args untested; setup-envtest availability of 1.37 binaries unchecked.
- Source Hydrator GA timing unknown (issue #28143); trusted official Beta status.
- kubectl-validate: v0.0.4 confirmed latest tag; pkg.go.dev shows newer pseudo-version on main — decide whether to vendor main.
- Search budget exhausted: Okteto/Signadot/Garden/Tilt/Skaffold/DevSpace statuses from GitHub API + vendor docs only; Uffizzi company vs repo status ambiguous.
- Kyverno CLI paramRef support for native VAPs undocumented; needs a test.
- MAP GA in 1.36 from current kubernetes.io; 1.32–1.35 clusters have it alpha/beta behind gates — snapshot must record feature-gate state or server version.
- Helm 4.3.0 `--dry-run=server`/lookup behaviour not re-verified beyond docs; Helm 4 SDK changes may affect embedding.
- Demand for scheduling-fit checks is indirect (cluster-capacity/scheduler-simulator niche and dormant) — product decision.