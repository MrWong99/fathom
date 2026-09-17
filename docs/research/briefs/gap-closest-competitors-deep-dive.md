# gap-closest-competitors-deep-dive

Access date for all sources: 2026-09-13. Note on dates: GitHub omits the year for current-year dates; several fetches mis-inferred "2024/2025" for releases that are actually 2026 (Kargo v1.11.4, flux-schema v0.13.0, Cyclops v0.21.1, Devtron v2.2.0). I cross-checked with second sources (newreleases.io, release blogs, "© 2026" page footers) and report the corrected years below; where only one source exists I flag it.

## TASK A — comparison table

| Tool | Model / hosting | License / price | Where validation runs | What it validates | Cluster-awareness | Git integration | Values UI | Plugin surface | Maintained? |
|---|---|---|---|---|---|---|---|---|---|
| **ConfigHub** | SaaS (auth.confighub.com) + self-hosted Enterprise (K8s + PostgreSQL + optional Keycloak; image `ghcr.io/confighubai/confighub`) | Server closed-source, "Evaluation License" for self-host; `cub` CLI, SDK, cub-scout, flux-bridge, examples = MIT. Pricing page is JS-only, no numbers retrievable → **UNVERIFIED** | Server-side on Unit change via **Triggers** (blocking gate or warning), or locally with `cub function local` | `vet-schemas` (kubeconform against K8s schemas), `vet-celexpr` (CEL over the unit), `vet-placeholders`, `vet-approvedby`; custom functions via SDK in Workers | Live-state *reporting* (argobot force-syncs Argo app and reports status back); **no import of VAP/Kyverno/Gatekeeper/quotas**; no admission simulation | **No Git write.** As of Aug 2026 "delivery is now OCI-pull only (ConfigHub publishes a tagged Release to its OCI registry; Argo CD / Flux pull it)"; Bridge push sunset | Web UI exists; unit editing is data-centric (YAML/JSON/TOML/INI/Env/Properties, OpenTofu) not schema-driven forms — form details **UNVERIFIED** | SDK (Go) for custom functions/workers; `cub plugin install` (e.g. `confighub/cub-server`, `cub-commander`) | Yes: founded 2024, $4M seed Mar 2025 (Crane, Pear, Encoded), self-hosted GA Apr 2026, very active GitHub org |
| **Devtron** | Self-hosted in-cluster platform (Helm install); "Devtron SaaS" mentioned | Apache-2.0 core, open-core: Lock Deployment Config, Approval Policy, Deployment Window carry "Enterprise" tag. Pricing: Freemium $0 (1 cluster/50 vCPU/10 users), Starter $999/mo (5 clusters), Growth $2,000, Scale $2,500–3,500, Accelerate $4,000–6,000, Enterprise Plus custom | Edit time in UI (GUI form ↔ YAML, JSON-schema-driven), Dry Run manifest preview, then approval flow | Custom JSON schema per scope (App-Env > App > Env > Chart-ref > Cluster > Global, via `PUT /orchestrator/deployment/template/schema`); locked JSONPath keys; approvals (who/how many), **not** policy simulation | Live connection to managed clusters; no policy/quota import; validation is against schema, not admission | GitOps mode = Devtron-managed manifest repo synced by Argo CD (gitops docs 404'd twice → repo model **UNVERIFIED**); no PR-to-user-repo flow | Yes, best-in-class form/YAML toggle | Devtron "plugins" are CI/CD pipeline steps, not validators | Yes: v2.2.0 (native sidecars) Jul 21 2026; 5.6k stars |
| **Flux Schema** (`fluxcd/flux-schema`) | CLI plugin (`flux plugin install schema`), GitHub Action, container image; offline | Apache-2.0 | CI / pre-commit / editor time (offline) | JSON Schema (santhosh-tekuri/jsonschema v6) + `x-kubernetes-validations` CEL via **k8s apiserver code**; strict YAML, DNS-1123 names, list-map/set topology, defaults+pruning before CEL | **Snapshot via `extract crd`** (CRD YAML from files or `kubectl get crds -o yaml \|`), `extract k8s` from swagger. **No VAP/MAP, Kyverno, Gatekeeper, PSA, quotas, LimitRanges** | None (validator only) | None | `.fluxschema.yml` config, custom catalogs layered over defaults | Yes: v0.13.0 Sep 9 2026, weekly cadence since Jul 2026 |
| **Ecosystem Schema Catalog** (`controlplaneio-fluxcd/schema-catalog`) | CDN at schemas.fluxoperator.dev (Cloudflare) + public MCP endpoint | **AGPL-3.0** repo; ~9,093 schemas / 123 projects / 653 MB; GitHub Artifact Attestations | n/a | n/a | n/a | n/a | n/a | n/a | Rebuilt daily |
| **Flux CLI plugin system** (RFC-0013, Flux 2.9, `fluxcd/plugins`) | `~/.fluxcd/plugins/flux-<name>`; catalog.yaml `PluginCatalog` (fields: name, description, homepage, source, license) | Apache-2.0 catalog; plugins: schema (Apache), mirror (Apache), operator (**AGPL-3.0**, ControlPlane) | — | — | — | — | — | Third parties submit PR to catalog and "are responsible for maintaining their plugin definitions"; **SHA-256 only**, "Cosign/SLSA signature verification ... can be added later"; `@sha256:` digest pinning | RFC status "implementable" (2026-04-13); GA in 2.9 (Jun 2026) |
| **Kargo** (OSS) | In-cluster controller + API/UI | Apache-2.0; 3.7k stars | Promotion time (post-merge) | Built-in steps only (git-clone/open-pr/merge-pr, helm-update-image, kustomize, argocd-update, http, file-write in 1.11) | Reads Argo CD app health; no policy/quota awareness | **Opens PRs** (`git-open-pr`, `git-wait-for-pr`, `git-merge-pr`) or commits directly to hydrated branches | No values editor; UI for freight/stages | `PromotionTask`/`ClusterPromotionTask` = reusable sequences of built-in steps only | Yes: v1.11.0 Jul 24 2026, v1.11.4 Sep 3 2026, v1.12 in progress |
| **Kargo Enterprise / Akuity Platform** | SaaS control plane, self-hosted agent; self-hosted/on-prem control plane only on Enterprise | Pro $495/mo (1 Argo CD CP, 50 apps, 1 Kargo CP, 50 stages, 25M AI tokens; +$99/mo per 10 apps + 10 stages + 5M tokens, ≤1,500 apps); Enterprise contact sales | Promotion time | **Custom Steps** (any container, e.g. "OPA / Kyverno" scan of rendered manifests) | Same as OSS | Same | Same | `CustomPromotionStep` (cluster-scoped, `ee.kargo.akuity.io/v1alpha1`), alpha, "no config validation at the moment" | Yes |
| **Cyclops** | In-cluster controller + UI; `Module` CRD; MCP server (v0.20) | Apache-2.0; 3.3k stars | Edit time in UI form | Helm `values.schema.json` (types/required/enum) — no cross-field/CEL, no admission | Live cluster (applies via controller); no policy import | Since v0.17 "commit your Module as yaml to the repository" on a branch (GitHub token w/ contents write); direct commit, **PR flow UNVERIFIED/not documented**; Argo CD then syncs Module → controller renders | Yes, auto-generated from schema | Templates from Git/OCI Helm charts; `cyctl` | Alive but slowing: v0.21.1 Jun 26 2026, last commit Jul 7 2026; roadmap still lists auth/RBAC, Kustomize |
| **Qovery** | SaaS control plane, managed or BYOK clusters | Commercial | Deploy time | Helm value overrides stored in Qovery or Git, `qovery.env.*` macro interpolation; no schema/policy validation | Live BYOK cluster | Auto-deploy on Git change; no PR authoring | Basic override editor (shows default values.yaml) | — | Yes (blog Dec 2023; product active) |
| **Octopus (+Codefresh)** | SaaS/self-hosted Octopus; Codefresh GitOps (Argo). Octopus acquired Codefresh Feb 2024; **Codefresh "GitOps Cloud" discontinued** | Commercial (free tier); tier for Argo features **UNVERIFIED** | Codefresh: Argo `Application` editor validates spec before commit. Octopus: steps "Update Argo CD Application Image Tags/Manifests", "Wait for Argo CD Applications"; verified deployments (2026.1) and PR-merged verification (2026.2, Mar 24 2026) | Application spec / image tags; no values-schema, no admission | Live Argo CD connection; drift + Live Object Status | Commit direct **or PR**; verification fails if PR closed | Codefresh Form/YAML editor for Application, not chart values | Octopus step templates | Yes |

### ConfigHub — deep dive
- Model: "System of Record" DB of config Units in Spaces, Links for apply ordering, Revisions, Releases. Roles: Workers = service identity + custom-function execution in your network ("You do not need a running Worker to deploy configuration. Deployment happens by publishing a Release that a GitOps operator pulls; nothing is pushed from ConfigHub into your infrastructure").
- GitOps posture (corrects earlier slice): it does **not** write to Git; it is an *alternative source of truth* delivering OCI artifacts to Flux (ExternalArtifact via flux-bridge) / Argo CD. Issue confighub/cub-scout#505 (2 Aug 2026, by monadic = Alexis Richardson): "Bridge sunset — delivery is now OCI-pull only".
- Validation: Functions (`get-`/`set-`/`vet-`) + Triggers with blocking gates; CEL via `vet-celexpr`; schemas via kubeconform. No admission-policy or quota awareness.
- Cannot do vs reqs: (1) schema-driven values form — partial/UNVERIFIED; (2) pseudo-cluster admission/quota — **no**; (3) import cluster policies — **no** (cub-scout maps Flux/Argo/Helm/Crossplane/kro *state*, not policies); (4) open PRs — **no**, replaces Git as source; (5) plugins — yes (SDK functions, cub plugins).
- Verdict: **integrate** (ConfigHub could be a store backend / OCI publisher) but it competes for "source of truth"; brand risk that Weaveworks founders push OCI-not-Git.

### Devtron — deep dive
- The 404'd docs exist at new paths: `docs.devtron.ai/docs/user-guide/app-management/policies/lock-deployment-config` (v2.0, "Enterprise" tag, JSONPath key locking, super-admin only) and `docs.devtron.ai/docs/devtron/v1.8/user-guide/global-configurations/approval-policy` ("Enterprise" tag; Request → Approve → Execute; covers Deployments, Deployment Template, ConfigMap, Secret changes; editor cannot self-approve; single draft at a time).
- Cannot do: (2) no admission/quota simulation (Dry Run only renders the manifest); (3) no policy import; (4) no PR to the customer's own GitOps repo (Devtron owns the repo, Argo CD syncs); (5) no validator plugin API. Its GUI JSON-schema-per-scope mechanism is the closest analogue to zhi's developer-defines/deployer-fills split — copy that, not the platform.

### Flux Schema — the four questions
1. **Reuse vs reimplement:** go.mod direct deps `k8s.io/apiextensions-apiserver v0.36.3`, `k8s.io/apimachinery`, `k8s.io/kube-openapi`, `santhosh-tekuri/jsonschema/v6`; `cel-go` and `k8s.io/apiserver` indirect. `internal/validator/cel.go` imports `k8s.io/apiextensions-apiserver/pkg/apiserver/schema`, `.../schema/cel`, `.../schema/defaulting` and calls `apiextschemacel.NewValidator(structural, true, math.MaxUint64)` after prune-nulls + apply-defaults. **It reuses the apiserver CEL/structural code; the parity claim is real for CRD `x-kubernetes-validations`.** JSON-Schema-level validation (built-ins) is a generic JSON Schema engine, not `kubectl-validate`'s declarative validation.
2. **Live cluster needed?** No. `extract crd` reads CRD YAML from stdin/files; docs example pipes `kubectl get crds -o yaml`. Validation is fully offline.
3. **AGPL-3.0 catalog:** license is on the `schema-catalog` repo (extraction tooling + indexes + hosted JSON). The `flux-schema` binary is Apache-2.0 and ships its own built-in Kubernetes/OpenShift/Gateway/Flux catalogs; the ecosystem catalog is a *fallback* over CDN (v0.11.0). Consuming JSON schema files over HTTP does not link AGPL code; whether ControlPlane treats the schema files as "covered work" is **UNVERIFIED** (no statement found). Safe path for zhi: generate your own catalog with `flux schema extract` (Apache) from the customer cluster's CRDs — which is the "import from live cluster" feature anyway.
4. **Third-party plugins:** yes — PR to `fluxcd/plugins` catalog.yaml; author maintains entry; polling picks up releases; SHA-256 mandatory, cosign explicitly deferred; digest pinning available. Flux 2.9 GA Jun 2026. Only 3 plugins today (schema, mirror, operator).
- Cannot do: (1) no UI; (2) no VAP/MAP/Kyverno/Gatekeeper/PSA/quotas — schema+CEL-on-CRD only; (3) CRD import only; (4) none; (5) none. **Integrate** (embed as a library-ish validator or shell out) — it is the CRD/CEL layer of a pseudo-cluster, not the whole thing.

### Kargo — settled
- **Custom container promotion steps are Enterprise-only.** docs.kargo.io custom-steps page: "This promotion step is only available in Kargo on the Akuity Platform, versions v1.10 and above"; CRD `CustomPromotionStep` in `ee.kargo.akuity.io/v1alpha1` (the `ee.` group is the tell); alpha; needs Promotion Controller + self-hosted agents. Akuity blog 31 Mar 2026: "Custom Steps is available as part of Kargo Enterprise"; v1.10 blog 13 Apr 2026 lists it under "Kargo Enterprise Features". OSS `PromotionTask` only composes built-in steps. Issue #4590 (Jul 2025) is closed without an OSS commitment; OSS roadmap page says enterprise roadmap is "get in touch". Any earlier slice claiming OSS custom containers was wrong.
- Cannot do: (1)(2)(3) nothing at edit time; (4) yes — PR opening is native; (5) OSS: no. **Integrate:** zhi should produce commits/PRs that Kargo's `git-open-pr`/hydrated-branch flows consume, and offer a Kargo `http` step target for a zhi validation service.

### Cyclops
Closest UI analogue (schema → form → Git). Gaps: JSON-schema only, cluster-attached controller/CRD required even in Git mode, direct commit not PR (UNVERIFIED), no policy awareness, roadmap items (RBAC) still open after 2 years, 2-month commit gap. **Compete** on UI, take the "template = Helm chart + schema" idea.

## TASK B — 2025–2026 entrants and adjacent projects (one-line verdicts)
- **kat** (github.com/zemanlx/kat, Apache-2.0, 1 star): local VAP/MAP tester using "official Kubernetes CEL libraries"; no cluster snapshot import — proof the offline-VAP engine is small; borrow, don't fear.
- **kubectl-validate** (SIG-CLI subproject, now hosted at github.com/kyverno/kubectl-validate): server-parity declarative validation for built-ins + CRDs from cluster or local dir; no admission policies — strong candidate library for the built-in-types layer.
- **Kyverno CLI `apply`** (offline by default; `--cluster` for live; `--parameter-resource` for VAP/MAP params; GlobalContextEntry evaluated offline from in-memory resources): the reference for Kyverno-policy simulation — embed or shell out; nobody wraps it with a snapshot importer + UI.
- **Kubewarden `kwctl run`**: same engine as admission controller, offline, context-aware dry-run — same story for Kubewarden policies.
- **KEP-5793 Manifest-Based Admission Control** (alpha 1.36, beta 1.37): VAP/MAP/webhook configs loaded from files on the apiserver — validates the idea that "policies as files" is the canonical snapshot format; align zhi's snapshot to these object kinds.
- **helm-cel** (idsulik, MIT; HN Nov 2024): CEL rules in `values.cel.yaml` instead of `values.schema.json` — adopt the pattern for developer-authored cross-value validators.
- **helm-values-manager** (Zipstack, MIT, 0 stars): vendor schema vs customer values separation — same persona split as zhi, no traction.
- **gitops-reverser** (ConfigButler, Apache-2.0, 22 stars, active Sep 2026): cluster→Git write-back with audit attribution and Kustomize re-render check — adjacent, not competing.
- **stakpak/devx** (211 stars, CUE-based config generation/validation): CUE-first, no cluster import.
- **Flux Operator Web UI** (AGPL-3.0, ControlPlane, previewed KubeCon Atlanta 2025, shipped in Flux 2.8 timeframe): read-only HelmRelease values/history views; no editing, no validation.
- **Argo CD Source Hydrator** (alpha 2.14 → beta issue #28143; Git-notes since 3.3): the hydrated-branch target zhi's PRs should land on; ArgoCon EU 2026 talk "Previewing Pull Request Changes in SECONDS!" = PR-preview demand signal.
- **ConfigHub cub-scout / argobot** (MIT, 2026): maps GitOps state in clusters — could be a snapshot-importer competitor if it grows into policies; today it does not read VAP/Kyverno.
- **KyvernoCon EU 2026 "Beyond Admission" (Boye/Fahmy, Cloudflare)**: "testing policy changes safely before deployment" — from the policy-author side, not the values-author side.
- Not found after 10+ searches: any product that imports VAP/MAP + Kyverno + Gatekeeper + PSA + ResourceQuota/LimitRange + CRDs + StorageClass/IngressClass from a live cluster into a portable snapshot and validates Helm/Compose values against it in a UI before opening a PR.

## What nobody does
1. Cluster **policy snapshot** as a versioned, shareable artifact (VAP/MAP + Kyverno + Gatekeeper constraints + PSA labels + quotas/LimitRanges + CRDs + classes) importable without cluster access at edit time.
2. Rendering Helm/Compose → manifests and running **all** engines (apiserver CEL, Kyverno, Gatekeeper/OPA, PSA, quota arithmetic across the whole namespace) in one pre-push report with Info/Warning/Blocking.
3. Developer-authored value schemas + cross-value rules (JSON Schema + CEL à la helm-cel) that a *different persona* fills in a form, with the target cluster's snapshot selected per environment.
4. Output as a **PR** onto the repo/branch Argo/Flux/Kargo already watch (no new source of truth, no in-cluster controller).
5. Docker Compose treated as a first-class deployment target with the same validation model.
6. Validator plugins that are OSS by default (contrast Kargo Custom Steps, Devtron Enterprise tags).

## TASK C — post-mortems

| Project | What it did | What happened (statements) | Why | Lesson for zhi |
|---|---|---|---|---|
| **Datree** | CLI + hosted policy registry/dashboard for K8s misconfig | "Since July 2023, the commercial company ... has been closed"; repos archived 6 Jun 2024; "centralized policy registry, automatic Kubernetes schema validation, dashboard" gone; standalone offline mode remains | Core value (policy registry, schema fetch) lived in SaaS; CLI-only free tier had no upgrade pull | Snapshot and rules must be local files in Git; hosted registry optional |
| **Monokle** (Kubeshop) | Desktop IDE, CLI, VS Code ext, admission controller, Monokle Cloud | Cloud EOL 17 Apr 2024 (Ole Lensmar): "the level of adoption has not reached a point that makes it sustainable"; repo README: "not able to maintain or evolve Monokle at this time"; Kubeshop refocused on Testkube | Desktop-IDE distribution, validation without a deploy story, accelerator portfolio triage | Validation must sit in the push path (PR), not in a separate editor people must adopt |
| **Kubevious** | In-cluster dashboard + "Guard" rules engine, CLI | Not archived; last release v1.1 12 Oct 2024; "Governance policy is yet to be defined"; ~single maintainer | Cluster-attached UI observes but cannot write back; no company | Avoid in-cluster UI as the product center; write-back to Git is the moat |
| **Cyclops** | Helm-schema-driven forms, Module CRD, Git write | Alive (v0.21.1 Jun 2026) but 2-month quiet, roadmap basics (RBAC, Kustomize) unshipped | Controller+CRD coupling; UI-first without policy value | Stateless, cluster-detached tool; borrow schema→form |
| **Glasskube package manager** | K8s package manager GUI/CLI (YC) | Repo archived 17 Jun 2026; last release v0.26.0 26 Nov 2024; company: "no longer actively maintained ... should not be used in production"; pivot to Distr | Helm gravity; package-manager business model absent | Do not fight Helm/Kustomize; sit above them |
| **Weaveworks / Weave GitOps** | Flux creator; WGO OSS + Enterprise UI | Shut 5 Feb 2024 (Richardson): "sales growth was lumpy and our cash position, consequently volatile ... M&A process ... fell through at the 11th hour"; WGO "transitioning to a community driven project", last tag 0.39.0-rc.2 Feb 2025, no GA since; Flux continued at CNCF, ControlPlane employs maintainers | Enterprise UI on top of a free controller = weak differentiation; ControlPlane now sells the same shape (AGPL Operator + UI) | Don't be "the UI for Flux/Argo"; be the pre-push validator they lack |
| **Kubeapps** | In-cluster Helm/Carvel catalog UI | Archived by VMware/Broadcom 25 Aug 2025; SAP fork "essential upkeep ... new feature development is not guaranteed" | Corporate-sponsored, cluster-attached, no GitOps write-back | Same as Kubevious; corporate sponsor risk |
| **jsPolicy** (Loft) | JS/TS admission policies on V8 | Not archived; last release v0.3.0-beta.6 7 Jun 2023; Loft all-in on vcluster | Side project of a company with a different core | A plugin ecosystem needs one sustaining product |
| **driftctl** (Snyk) | Terraform drift detection | README: "now in maintenance mode. We cannot promise to review contributions"; last release v0.40.0 1 Dec 2023 | Acquired, absorbed into Snyk IaC | Drift/observation features get commoditized; validation-before-push is the durable niche |

## Sources
1. https://itnext.io/representing-configuration-as-data-791e3921e882 (search snippet)
2. https://techcrunch.com/2025/03/26/cloud-veterans-launch-confighub-to-fix-configuration-hell/
3. https://docs.confighub.com/
4. https://docs.confighub.com/guide/functions/
5. https://docs.confighub.com/guide/workers/
6. https://docs.confighub.com/enterprise/self-hosting/
7. https://github.com/confighub
8. https://github.com/confighub/cub-scout/issues/505
9. https://itnext.io/confighub-why-your-internal-developer-platform-needs-it-bd716d415c24 (403 on fetch; search snippet only)
10. https://docs.devtron.ai/docs/user-guide/app-management/policies/lock-deployment-config
11. https://docs.devtron.ai/docs/devtron/v1.8/user-guide/global-configurations/approval-policy
12. https://docs.devtron.ai/docs/user-guide/creating-application/base-config/deployment-template
13. https://devtron.ai/pricing
14. https://github.com/devtron-labs/devtron ; https://github.com/devtron-labs/devtron/releases ; https://newreleases.io/project/github/devtron-labs/devtron/release/v2.2.0
15. https://fluxcd.io/blog/2026/07/flux-schema-validation/
16. https://fluxcd.io/flux/cli-plugins/flux-schema/
17. https://github.com/fluxcd/flux-schema ; https://github.com/fluxcd/flux-schema/releases ; https://github.com/fluxcd/flux-schema/releases/tag/v0.13.0
18. https://raw.githubusercontent.com/fluxcd/flux-schema/main/go.mod
19. https://raw.githubusercontent.com/fluxcd/flux-schema/main/internal/validator/cel.go
20. https://github.com/controlplaneio-fluxcd/schema-catalog
21. https://github.com/fluxcd/plugins ; https://raw.githubusercontent.com/fluxcd/plugins/main/catalog.yaml
22. https://github.com/fluxcd/flux2/blob/main/rfcs/0013-cli-plugin-system/README.md
23. https://fluxcd.io/blog/2026/06/flux-v2.9.0/ (search snippet)
24. https://docs.kargo.io/user-guide/reference-docs/promotion-steps/custom-steps
25. https://akuity.io/blog/kargo-custom-steps-gitops-promotion
26. https://akuity.io/blog/kargo-v1-10-custom-steps-http-notifications-new-promotions
27. https://github.com/akuity/kargo/issues/4590 ; https://docs.kargo.io/roadmap ; https://github.com/akuity/kargo ; https://github.com/akuity/kargo/releases ; https://main.docs.kargo.io/release-notes/v1.11.0
28. https://akuity.io/pricing
29. https://github.com/cyclops-ui/cyclops ; https://github.com/cyclops-ui/cyclops/releases ; https://github.com/cyclops-ui/cyclops/releases/tag/v0.21.1 ; https://github.com/cyclops-ui/cyclops/commits/main
30. https://www.cyclops-ui.com/docs/installation/git-write/
31. https://www.qovery.com/blog/deploy-your-helm-charts-with-ease
32. https://octopus.com/blog/argo-cd-verified-deployments ; https://octopus.com/docs/argo-cd ; https://octopus.com/news/octopus-acquires-codefresh
33. https://codefresh.io/docs/docs/deployments/gitops/manage-application/
34. https://github.com/zemanlx/kat
35. https://github.com/kyverno/kubectl-validate
36. https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
37. https://docs.kubewarden.io/reference/kwctl-cli (search snippet)
38. https://www.kubernetes.dev/resources/keps/5793/
39. https://github.com/idsulik/helm-cel ; https://github.com/Zipstack/helm-values-manager ; https://github.com/ConfigButler/gitops-reverser ; https://github.com/topics/config-as-data
40. https://fluxoperator.dev/web-ui/ (search snippet)
41. https://github.com/argoproj/argo-cd/issues/28143 ; https://argo-cd.readthedocs.io/en/latest/user-guide/source-hydrator/ (search snippets)
42. https://colocatedeventseu2026.sched.com/2026-03-23/list/descriptions/type/KyvernoCon ; https://colocatedeventseu2026.sched.com/overview/area/ArgoCon (search snippet)
43. https://github.com/datreeio/datree
44. https://monokle.io/blog/end-of-life-announcement-for-monokle-cloud ; https://github.com/kubeshop/monokle
45. https://github.com/kubevious/kubevious ; https://github.com/kubevious/kubevious/releases
46. https://github.com/glasskube/glasskube ; https://github.com/glasskube/glasskube/releases/tag/v0.26.0 ; https://www.ycombinator.com/companies/glasskube (search snippet)
47. https://siliconangle.com/2024/02/05/kubernetes-automation-startup-weaveworks-shuts/ ; https://github.com/weaveworks/weave-gitops ; https://github.com/weaveworks/weave-gitops/releases
48. https://github.com/sap/kubeapps
49. https://github.com/loft-sh/jspolicy ; https://github.com/loft-sh/jspolicy/releases
50. https://github.com/snyk/driftctl ; https://github.com/snyk/driftctl/releases