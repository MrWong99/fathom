# gap-go-embedding-and-license-audit

Research date 2026-09-13. All facts below were pulled from live sources (pkg.go.dev, proxy.golang.org, raw GitHub go.mod/source, release pages, OSV, vendor docs); numbered citations refer to the Sources list. Where memory and the web disagreed, the web wins and the disagreement is called out.

## 0. Headline findings (read first)

1. **The k8s.io stack is embeddable without `replace` directives as long as you never import `k8s.io/kubernetes`.** The *published* `k8s.io/apiserver@v0.37.0` go.mod has zero replace/exclude directives [1]; only the in-tree (`kubernetes/kubernetes`) go.mod and the published `k8s.io/kubernetes@v1.37.0` .mod carry the `k8s.io/* => ./staging/src/...` replaces with `v0.0.0` requires [2][3]. Two things we want live *only* in `k8s.io/kubernetes`: `plugin/pkg/admission/limitranger` and `plugin/pkg/auth/authorizer/rbac` [4][5], plus the ResourceQuota *evaluators* (`pkg/quota/v1/evaluator/core`) [6]. Verdict: VENDOR-FORK those three small packages (they are pure functions over API types); do not take the `k8s.io/kubernetes` module.
2. **cel-go is mid-rename and Kubernetes has not followed yet.** Canonical module today is `cel.dev/cel-go` (go.mod `module cel.dev/cel-go`, first tag on that path v0.32.0, 2026-08-19) [7][8][9]; the GitHub org moved to `cel-expr/cel-go` in June 2026 (redirects work) [10]. `k8s.io/apiserver` v0.37 (and 1.37 in-tree) still require `github.com/google/cel-go v0.29.2` [1][2]; Kyverno main uses `github.com/google/cel-go v0.31.0` [11]; OPA frameworks `v0.29.0` [12]; flux-schema `v0.26.0` [13]. **Rule: do not import `cel.dev/cel-go` directly in the rewrite** — it would create two CEL runtimes with incompatible `*cel.Env` types. Import CEL only through `k8s.io/apiserver/pkg/cel/environment` and let the k8s pin decide the path; revisit when k8s.io/* moves to `cel.dev/cel-go` (unscheduled as of 1.37).
3. **kubectl-validate DOES evaluate CRD `x-kubernetes-validations` CEL, indirectly.** Its `pkg/validator` imports no CEL package itself but calls `customresource.NewStrategy(...)` then `rest.BeforeCreate` [14]; the apiextensions strategy at 1.37 constructs `cel.NewValidator(structuralSchema, true, celconfig.PerCallLimit)` and runs `celValidator.Validate(...)` inside `Validate` unless a blocking structural error exists [15]. Corrects any slice that said "kubectl-validate is JSON-schema only".
4. **Kyverno is not cleanly embeddable**: one `replace k8s.io/pod-security-admission => github.com/kyverno/pod-security-admission v0.0.0-20251031094455-46f20778634f` [11] (fork of a fork, last touched 2025-10-31, patches "fix: return message" on a release-1.34 branch [16]); ~416 require lines [11]; 10 GHSAs in 2026 alone including two Criticals (Sep 10 2026 batch, coincides with v1.19.1) [17][18]. Its `pkg/cel/policies/vpol/engine.NewEngine(provider, nsResolver, matcher)` is interface-only and constructible offline [19], but the CLI (`kubectl-kyverno`) is the supported offline surface. Verdict: SHELL-OUT to `kyverno apply` (CLI v1.19.1, 2026-09-10 [18]).
5. **Helm v4 SDK is embeddable and stability-promised** (HIP-0004: exported Go API compatible per Go module rules, breaking only on major or security; Kubernetes-typed surfaces exempt; `internal/` exempt) [20][21]. helm.sh/helm/v4 v4.3.0 (2026-09-09) pins k8s.io/* v0.37.0 and `santhosh-tekuri/jsonschema/v6 v6.0.3`, no replaces [22][23].
6. **License landmines are all *adjacent*, none in the embed set**: every module we would link is Apache-2.0/MIT/BSD-3. AGPL-3.0: flux-operator, schema-catalog, Nuon. GPL-3.0: Komodo, helm-docs. GPL-2.0: podman-compose. BUSL-1.1: Vault, Vault Secrets Operator, Terraform (SDKs/API clients stay MPL-2.0) [24-31].

## 1. Dependency audit table

Weight = rough count of `require` lines in the module's own go.mod (direct+indirect) or proxy .mod; "tree" figures are approximations. CVE column = OSV/GHSA entries touching the module in 2025-26 that matter to an offline validator.

| Module (packages needed) | Latest tag / date | License | replace-free? | CGO | Open CVEs (2025-26) | Verdict |
|---|---|---|---|---|---|---|
| `k8s.io/apiserver` (`pkg/admission/plugin/policy/{validating,mutating}`, `plugin/cel`, `pkg/cel/{library,environment}`, `plugin/resourcequota`) | v0.37.0, 2026-08-26; v0.36.4 / v0.35.8 (2026-08-20) [32] | Apache-2.0 | **Yes** (published .mod has no replace) [1] | No | OSV lists only GHSA-82hx-w2r5-c2wq (2022 DoS) [33] | **EMBED** |
| `k8s.io/apiextensions-apiserver` (`pkg/apiserver/schema`, `schema/cel`, `pkg/registry/customresource`) | v0.37.0 [34] | Apache-2.0 | Yes (same staging release train) | No | none listed | **EMBED** — `cel.NewValidator(s *schema.Structural, isResourceRoot bool, perCallLimit uint64)`; `Validate(ctx, fldPath, _, obj, oldObj, costBudget, opts...)`; `Compile(s, declType, perCallLimit, baseEnvSet, envLoader)` [34] |
| `k8s.io/pod-security-admission` (`policy`, `api`) | v0.37.0, 2026-08-26 [35] | Apache-2.0 | Yes | No | none listed | **EMBED** — `policy.NewEvaluator(DefaultChecks())`, `EvaluatePod(api.LevelVersion, *metav1.ObjectMeta, *corev1.PodSpec)`; no client needed [35] |
| `k8s.io/kubernetes` (`plugin/pkg/admission/limitranger`, `plugin/pkg/auth/authorizer/rbac`, `pkg/quota/v1/evaluator/core`) | v1.37.0 | Apache-2.0 | **No** — published .mod requires `k8s.io/api v0.0.0` etc. with 33 `=> ./staging/src/k8s.io/*` replaces; consumer must mirror ~33 replaces per minor [2][3][36] | No | n/a | **VENDOR-FORK** the three packages (LimitRanger: `PodValidateLimitFunc`, `PodMutateLimitFunc`, `PersistentVolumeClaimValidateLimitFunc` [4]; RBAC: `RulesAllow`, `RuleAllows`, `New(RoleGetter, RoleBindingLister, ClusterRoleGetter, ClusterRoleBindingLister)` [5]). Neither has been moved to staging as of 1.37. |
| `sigs.k8s.io/kubectl-validate` (`pkg/validator`, `pkg/openapiclient`) | last tag **v0.0.4** (K8s 1.30 era); main is untagged, go 1.25, k8s.io/* v0.35.0, no replaces [37][38]; Kyverno consumes pseudo-version `v0.0.5-0.20260105161640-a97ccfaca20b` [11] | Apache-2.0 | Yes | No | none | **EMBED (pseudo-version pin)**. Built-in schemas for 1.23–1.35 [39]; CEL via strategy (see §0.3). The `kyverno/kubectl-validate` fork is *not* what Kyverno's go.mod uses — ignore it [11][40]. |
| `sigs.k8s.io/kwok` | binary/container tool; runtimes `binary` (downloads kube-apiserver/etcd from dl.k8s.io), docker/podman/nerdctl/kind [41][42] | Apache-2.0 | n/a | n/a | n/a | **AVOID as library** (no in-process apiserver API; `pkg/kwokctl/*` only orchestrates external binaries). SHELL-OUT only if we ever need a live fake API. |
| `sigs.k8s.io/controller-runtime/pkg/envtest` | v0.25.0, 2026-09-03 [43] | Apache-2.0 | Yes | No | none relevant | **SHELL-OUT/AVOID** — requires real `etcd`+`kube-apiserver` binaries (`KUBEBUILDER_ASSETS`, `DownloadBinaryAssets` default false) [43]; breaks the single-static-binary goal. |
| `github.com/kyverno/kyverno` (`pkg/engine`, `pkg/cel`, `cmd/cli/kubectl-kyverno`) | v1.19.1, 2026-09-10 [18]; go 1.26.6; k8s.io/* v0.36.4 | Apache-2.0 | **No** (PSA fork replace) [11] | No | 10 GHSAs in 2026 (2 Critical: GHSA-5qq8-67g6-4h2w, GHSA-79gf-7frw-68m9/CVE-2026-54523; Highs: apiCall SSRF/token leak family, CEL http.Get SSRF GHSA-rggm-jjmc-3394); GHSA-cvq5-hhx3-f99p "no fix" per OSV [17][18][44] | **SHELL-OUT** to `kyverno` CLI |
| `github.com/open-policy-agent/frameworks/constraint` | untagged pseudo `v0.0.0-20260908221318-1ef3bdb53b1f` (2026-09-08) [45]; go 1.26; opa v1.20.2, k8s.io/* v0.36.4, no replaces [12] | Apache-2.0 | Yes | No | inherits OPA | **EMBED with caution** — the `k8scel` driver lives in **gatekeeper** (`github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel`, v3.23.1 2026-08-27), not frameworks [46]; it imports `apiserver/.../policy/validating`, `plugin/cel`, `cel/environment` and needs no informers [47]. Frameworks only ships `rego` and `fake` drivers [48]. |
| `github.com/open-policy-agent/opa` (`v1/rego`) | v1.20.2, 2026-09-03 [49] | Apache-2.0 | Yes | **No since v1.19.0** (wasmtime-go replaced by wazero) [49] | GHSA-9f29-v6mm-pw6w (server path bypass, Feb 2026; server-only, fixed) [50] | **EMBED** (`v1/` is a directory inside module `github.com/open-policy-agent/opa`, not a separate module [51]) |
| cel-go | `cel.dev/cel-go` v0.32.0 (2026-08-19); `github.com/google/cel-go` v0.31.0 last on old path [7][9] | Apache-2.0 + BSD-3 file [52] | Yes | No | none | **EMBED transitively only** (see §0.2) |
| `helm.sh/helm/v4` (`pkg/action`, `pkg/chart`, `pkg/registry`) | v4.3.0, 2026-09-09 (v4.2.4 2026-08-13) [22] | Apache-2.0 | Yes [23] | No | GHSA-q5jf-9vfq-h4h7, GHSA-vmx8-mqv2-9gmg (plugin install, High), GHSA-hr2v-4r36-88hr (chart extraction) — all fixed, all in 4.x [53] | **EMBED** (SDK stability via HIP-0004 [20][21]) |
| `sigs.k8s.io/kustomize/api` (`krusty`) | v0.21.1, 2026-02-09 [54] | Apache-2.0 [55] | Yes | No | none | **EMBED** |
| `github.com/compose-spec/compose-go/v2` | v2.15.0, 2026-09-03 [56] | Apache-2.0 [57] | Yes | No | none | **EMBED** |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.3, 2026-06-28 [58] | Apache-2.0 [59] | Yes | No | none | **EMBED** (drafts 4/6/7/2019-09/2020-12; default 2020-12 when `$schema` absent) [60] |
| `cuelang.org/go` | v0.17.1, 2026-07-16 [61] | Apache-2.0 [62] | Yes | No | none | optional EMBED |
| `github.com/replicatedhq/troubleshoot` | v0.134.0, 2026-09-04 [63] | Apache-2.0 [64] | Yes | No (UNVERIFIED for every collector) | none | **EMBED analyzers only**: `analyze --bundle <tgz> <spec>` calls `analyzer.DownloadAndAnalyze()` on a saved archive, no cluster [65] |
| `github.com/tetratelabs/wazero` | v1.12.0, 2026-05-28 [66] | Apache-2.0 [67] | Yes | No | none | **EMBED** |
| `github.com/extism/go-sdk` | v1.7.1, 2025-03-02 — **18 months without a tag** [68] | BSD-3-Clause [69] | Yes | No (wazero) [70] | none | EMBED with fork-readiness; or use wazero directly |
| `github.com/homeport/dyff` | v1.12.0, 2026-04-26 [71] | MIT [72] | Yes | No | none | **EMBED** |
| `github.com/yannh/kubeconform` (`pkg/validator`) | v0.8.0, 2026-06-04 [73] | Apache-2.0 | Yes | No | none | EMBED optional; no CEL, CRDs need openapi2jsonschema conversion [74] |
| `github.com/fluxcd/flux-schema` | module exists (`cmd/flux-schema`); go 1.26; k8s.io/* v0.36.3, jsonschema/v6, no replaces, no flux-operator dep [13] | Apache-2.0 [75] | Yes | No | none | Library surface is `cmd/` only → **SHELL-OUT or copy patterns**; do not depend on the AGPL catalog for embedding |
| `controlplaneio-fluxcd/schema-catalog` | hosted at schemas.fluxoperator.dev, daily rebuilt, snapshots for six most recent minors of K8s/OpenShift/Flux, MCP endpoint [76] | **AGPL-3.0** | n/a | n/a | n/a | **Network-fetch at user's option only; never vendor** (§3) |
| `controlplaneio-fluxcd/flux-operator` | v0.60.0, 2026-09-11 [77] | **AGPL-3.0** [78] | n/a | n/a | n/a | **AVOID as Go dependency**; reading its CRD YAML / talking to its MCP server from our binary is safe (§3) |
| `github.com/stefanprodan/timoni` | v0.34.0, 2026-08-29 [79] | Apache-2.0 | Yes | No | none | Only `api/` is importable; logic under `internal/`; README warns API/CLI may break [80] → AVOID as lib |

## 2. Version skew (one k8s.io/* minor vs clusters 1.34–1.37)

**What each tool actually does:**
- **kubectl-validate**: ships built-in native OpenAPI for 1.23–1.35 as data, falls back to fetching from the upstream GitHub repo for unknown versions; user selects `--version` [39][81]. The *validation code* (structural schema, CEL, defaulting) is whatever k8s.io minor it is compiled against (v0.35.0 on main).
- **kubeconform**: per-minor JSON-schema catalog (`yannh/kubernetes-json-schema`, regenerated daily, `-kubernetes-version` default `master`) [74][82]; pure JSON Schema, so version skew is purely a data problem — but it cannot express CEL, defaulting, or admission.
- **flux-schema / schema-catalog**: CLI bakes "latest" catalog into its image; the hosted catalog keeps versioned snapshots for the six most recent minors; version pinning is by pointing `--schema-location` at a snapshot (docs show no `--kubernetes-version` flag — UNVERIFIED that snapshots are addressable by the CLI directly) [76][83]. CEL evaluated "with the same engine as the Kubernetes API" at a single compiled minor (v0.36.3) [13].
- **Kyverno**: tests each release against three minors only (1.19 → K8s 1.33–1.35), "other versions may work" [84]; main already pins v0.36.4 for 1.20 [11]. No per-minor abstraction.
- **Flux (kustomize/helm-controller)**: no offline validation at all — server-side dry-run against the live cluster is the validation [85]; support matrix K8s ≥1.33 [86].

**Is `environment.MustBaseEnvSet(version)` sufficient?** Signature at v0.37: `func MustBaseEnvSet(ver *version.Version) *EnvSet`, `Env(envType Type) (*cel.Env, error)`, `Extend(options ...VersionedOptions)`; `NewExpressions` = strict n-1 compatibility for write-path, `StoredExpressions` = permissive union [87][88]. It gates *CEL libraries and language options* by `IntroducedVersion`/`RemovedVersion`, so a binary compiled against v0.37 can faithfully emulate 1.34/1.35/1.36 CEL library availability (older-than-pinned is fine). It is **not** sufficient for: (a) libraries introduced in 1.38+ (unknown to v0.37 — must emit "cluster newer than validator, CEL library set may be incomplete"); (b) native type schemas — those must come from the per-cluster snapshot (OpenAPI v3 discovery is the only correct source; do not rely on embedded `builtins`); (c) admission API *types* (VAP/MAP fields, `x-kubernetes-*` extensions) added after the pinned minor — decode as unstructured, fail soft; (d) `pod-security-admission` check versions (its `api.LevelVersion` is explicit and already covers all past versions — pass the cluster minor). Recommended policy: pin **latest k8s.io minor** (currently v0.37.0) since older-cluster emulation is downward-compatible by design (DefaultCompatibilityVersion is n-1 [87]), always validate against the imported snapshot's OpenAPI, and surface a *skew warning* (not an error) when the snapshot's `serverVersion` minor > pinned minor. Multiple binaries are not needed.

**The k8s.io minor also decides Helm, Gatekeeper and frameworks alignment**: Helm v4.3.0 = k8s v0.37.0; Gatekeeper v3.23.1 / frameworks = v0.36.4; Kyverno = v0.36.4. Go MVS resolves to the highest (v0.37.0) — verified no replace conflicts among these modules, so co-resolution builds (Kyverno excluded).

## 3. License landmines

| Item | License | Embed (link Go code) | Subprocess | Network use | Notes |
|---|---|---|---|---|---|
| flux-operator (controlplaneio-fluxcd) | AGPL-3.0 [78] | **No** (would make zhi AGPL) | OK — exec'ing `flux-operator` or reading its CRDs/`ResourceSet` YAML creates no derivative work | OK — MCP/HTTP clients of an AGPL server are not "conveying" it | Safe: parse its CRDs (data), don't import `api/v1` Go types (AGPL code; regenerate types from CRD YAML instead) |
| schema-catalog | AGPL-3.0 [76] | **No** (do not vendor generated schema files; the repo license covers the generated artefacts absent a separate data license — UNVERIFIED that schemas are dual-licensed) | n/a | OK to *fetch* at runtime as a user-configured source (AGPL §13 binds the operator of the modified service, not clients) | Default to cluster-derived OpenAPI; make schemas.fluxoperator.dev opt-in |
| Nuon | AGPL-3.0 [24] | No | OK | OK | Adjacent BYOC competitor, not a dependency |
| Komodo | GPL-3.0 [25] | No | OK | OK | Compose-deployment adjacency only |
| helm-docs (norwoodj) | GPL-3.0 [26] | **No** | OK (exec as user-installed tool) | n/a | Do not port its templates/code into a values-doc generator |
| podman-compose | GPL-2.0 [27] | No | OK | n/a | `compose-go` (Apache) is the right embed |
| Vault, Terraform | BUSL-1.1 [28][29] | Do not embed server code; internal use and CI use allowed; competing hosted/embedded offering prohibited | OK | OK | `github.com/hashicorp/vault/api` client stays MPL-2.0 [29] — fine to embed for secret refs |
| Vault Secrets Operator | BUSL-1.1 (IBM; change to MPL-2.0 after 4 years) [30] | No | reading its CRDs OK | OK | Treat like flux-operator |
| Redis | RSALv2/SSPL (memory; not verified live) | n/a | n/a | n/a | Not in stack; UNVERIFIED |

**Trademark/attribution (LF/CNCF policy) [31]**: do not put "Helm", "Kyverno", "Flux", "Kubernetes" in the product name; allowed phrasing is "zhi for Kubernetes®", "zhi compatible with Helm"; ® on first prominent use; no logos without written permission; must not imply endorsement. Apache-2.0 obligations for shipped upstream code: carry LICENSE and NOTICE texts (Helm, Kubernetes, Kyverno-CLI if redistributed) — bundle a `third_party/NOTICES` generated by `go-licenses`. If we redistribute the Kyverno CLI binary in an OCI bundle, ship its LICENSE alongside and do not rename it.

## 4. Disputed facts — verdicts

1. **Kyverno ValidatingPolicy GA version: 1.17, not 1.19.** 1.17 (2026-02-02) promoted ValidatingPolicy, MutatingPolicy, GeneratingPolicy, ImageValidatingPolicy, DeletingPolicy, PolicyException to `v1`/"stable and production-ready" and marked ClusterPolicy/Policy/CleanupPolicy deprecated [89]. 1.18 (2026-04-24): no breaking change, deprecation continues [90]. 1.19 (2026-08-20): "full feature parity", ClusterPolicy/Policy officially deprecated, **removal planned for v1.20 (est. Nov 2026)**, 1.19 is the last release with full legacy support [91]. Kyverno 1.19 supports K8s 1.33–1.35 only [84].
2. **`kyverno apply` v1.19.1 flags — CONFIRMED from source** [92]: `--parameter-resource strings` ("Path to resource files that act as ValidatingAdmissionPolicy/MutatingAdmissionPolicy parameters") ⇒ native VAP *and MAP* paramRef are supported offline; `--context-file string` ("File containing context data for CEL policies") is the **real flag**; the docs prose says `--context-path` but the docs' own example command uses `--context-file` [93] — docs typo, source wins. Context file kind is `cli.kyverno.io/v1alpha1 Context` with `spec.resources` (and `globalContextEntries`) [93]. `--crd-path` is deprecated in favour of `--crd-paths` on main (post-1.19.1, UNVERIFIED whether shipped in 1.19.1) [92]. MutatingAdmissionPolicy offline apply: supported (flag text + docs "test a MutatingAdmissionPolicy to preview the changes") [94]; the exact first version is UNVERIFIED (docs don't say; reference page exists back to 1.16).
3. **Helm 4.3 `values.schema.json` drafts.** Helm 3.18 introduced `santhosh-tekuri/jsonschema` alongside `xeipuuv/gojsonschema`, selecting the new lib only when `$schema` says so (PR #13283, merged 2025-04-14, milestone 3.18.0) [95]. Helm 4 dropped gojsonschema entirely ("Simplify the JSON Schema checking", #30754) [96]; `pkg/chart/common/util/jsonschema.go` compiles with `jsonschema/v6`, sets no `DefaultDraft`, and installs a `SchemeURLLoader` for `file:`, `http:`, `https:` (15 s timeout) and `urn:` (unresolved URNs degrade to `true` with a warning) [97]. Therefore accepted `$schema` URIs in Helm 4.3 are exactly the library's: `https://json-schema.org/draft-04/schema`, `draft-06`, `draft-07`, `draft/2019-09/schema`, `draft/2020-12/schema`, with trailing `#`/`#/` stripped and `http://` normalised to `https://`; **absent `$schema` ⇒ Draft 2020-12** (Helm 3 defaulted to draft-07 — behaviour change to document for chart authors) [60][98]. The helm.sh "Schema Files" page still shows draft-07 and carries a "not updated for Helm 4" banner [99]; GitHub issue #13069 was closed-not-planned before the feature landed via a different PR [100] — memory saying "Helm only supports draft-7" is outdated.
4. **cel-go path**: `cel.dev/cel-go` (v0.32.0+), *not* `github.com/cel-expr/cel-go` (that is only the repo location) and *not* `github.com/google/cel-go` for new code; Kubernetes 1.37 still imports the google path [1][7][9][10].
5. **Kyverno `pkg/cel/policies` without dclient**: constructor `NewEngine(provider Provider, nsResolver engine.NamespaceResolver, matcher matching.Matcher)` — all interfaces, no client [19]. Feasible in principle, but the transitive module (PSA fork replace, 400+ requires, CVE cadence) makes SHELL-OUT the recommendation.

## 5. Practical embedding recipe (for the architects)

- `go.mod`: pin `k8s.io/{api,apimachinery,apiserver,apiextensions-apiserver,client-go,component-base,pod-security-admission,kube-openapi} v0.37.0`, `helm.sh/helm/v4 v4.3.0`, `sigs.k8s.io/kustomize/api v0.21.1`, `compose-go/v2 v2.15.0`, `open-policy-agent/opa v1.20.2`, `gatekeeper/v3 v3.23.1` (for `pkg/drivers/k8scel`) + `frameworks/constraint` pseudo-version, `sigs.k8s.io/kubectl-validate` pseudo-version. No replace directives are required by any of these; `CGO_ENABLED=0` builds (OPA ≥1.19 is pure Go; wazero pure Go).
- Vendor-fork (copy with Apache header + NOTICE) from `kubernetes/kubernetes@v1.37.0`: `plugin/pkg/admission/limitranger` (validation funcs only), `plugin/pkg/auth/authorizer/rbac` (`RulesAllow`), `pkg/quota/v1/evaluator/core` (pod/PVC usage). ~2k LOC, no further in-tree deps beyond staging modules (UNVERIFIED for evaluator/core helpers — check `pkg/apis/core` imports, which are in-tree and would need type conversion to `k8s.io/api/core/v1`).
- VAP/MAP: use `validating.NewValidator` + `cel.NewCompositedCompiler` exactly as Gatekeeper's `k8scel` driver does (proven informer-free pattern) [47]; `mutating` package analogous; params come from the snapshot.
- CRDs: `schema.NewStructural` → `cel.NewValidator` → `Validate` with `celconfig.RuntimeCELCostBudget` [15][34]; or just call `customresource.NewStrategy` like kubectl-validate to get structural+CEL+defaulting in one call [14].
- Kyverno and flux-schema: subprocess, version-pinned, distributed as OCI artefacts with LICENSE; feed `--context-file`, `--parameter-resource`, `--crd-paths`.
- Run `govulncheck` in CI; the only currently-open OSV item in the embed set is none; Kyverno's GHSA-cvq5-hhx3-f99p is marked "no fix" and is an argument for subprocess isolation.

## Sources

1. https://proxy.golang.org/k8s.io/apiserver/@v/v0.37.0.mod
2. https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.37/go.mod
3. https://proxy.golang.org/k8s.io/kubernetes/@v/v1.37.0.mod
4. https://pkg.go.dev/k8s.io/kubernetes/plugin/pkg/admission/limitranger
5. https://pkg.go.dev/k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac
6. https://pkg.go.dev/k8s.io/apiserver/pkg/admission/plugin/resourcequota
7. https://raw.githubusercontent.com/cel-expr/cel-go/master/go.mod
8. https://pkg.go.dev/cel.dev/cel-go?tab=versions
9. https://github.com/cel-expr/cel-go/releases/tag/v0.32.0
10. https://github.com/cel-expr/cel-go (org move, June 2026; issue #1329)
11. https://raw.githubusercontent.com/kyverno/kyverno/main/go.mod
12. https://raw.githubusercontent.com/open-policy-agent/frameworks/master/constraint/go.mod
13. https://raw.githubusercontent.com/fluxcd/flux-schema/main/go.mod
14. https://raw.githubusercontent.com/kubernetes-sigs/kubectl-validate/main/pkg/validator/validator.go
15. https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.37/staging/src/k8s.io/apiextensions-apiserver/pkg/registry/customresource/strategy.go
16. https://github.com/kyverno/pod-security-admission/commits/master
17. https://github.com/kyverno/kyverno/security/advisories
18. https://github.com/kyverno/kyverno/releases
19. https://raw.githubusercontent.com/kyverno/kyverno/main/pkg/cel/policies/vpol/engine/engine.go
20. https://helm.sh/docs/sdk/gosdk/
21. https://github.com/helm/community/blob/main/hips/hip-0004.md
22. https://github.com/helm/helm/releases
23. https://raw.githubusercontent.com/helm/helm/main/go.mod
24. https://github.com/nuonco/nuon
25. https://github.com/moghtech/komodo/blob/main/LICENSE
26. https://github.com/norwoodj/helm-docs/blob/master/LICENSE
27. https://github.com/containers/podman-compose/blob/main/LICENSE
28. https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license
29. https://www.hashicorp.com/en/blog/hashicorp-updates-licensing-faq-based-on-community-questions
30. https://github.com/hashicorp/vault-secrets-operator/blob/main/LICENSE
31. https://www.linuxfoundation.org/legal/trademark-usage
32. https://pkg.go.dev/k8s.io/apiserver?tab=versions
33. https://osv.dev/list?ecosystem=Go&q=k8s.io%2Fapiserver
34. https://pkg.go.dev/k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel
35. https://pkg.go.dev/k8s.io/pod-security-admission/policy
36. https://github.com/kubernetes/kubernetes/issues/79384
37. https://raw.githubusercontent.com/kubernetes-sigs/kubectl-validate/main/go.mod
38. https://github.com/kubernetes-sigs/kubectl-validate/releases
39. https://github.com/kubernetes-sigs/kubectl-validate/tree/main/pkg/openapiclient/builtins
40. https://github.com/kyverno/kubectl-validate
41. https://kwok.sigs.k8s.io/docs/user/kwokctl-platform-specific-binaries/
42. https://kwok.sigs.k8s.io/docs/design/architecture/
43. https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest
44. https://osv.dev/list?ecosystem=Go&q=github.com%2Fkyverno%2Fkyverno
45. https://pkg.go.dev/github.com/open-policy-agent/frameworks/constraint?tab=licenses
46. https://pkg.go.dev/github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel
47. https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/pkg/drivers/k8scel/driver.go
48. https://github.com/open-policy-agent/frameworks/tree/master/constraint/pkg/client/drivers
49. https://github.com/open-policy-agent/opa/releases
50. https://osv.dev/list?ecosystem=Go&q=github.com%2Fopen-policy-agent%2Fopa
51. https://pkg.go.dev/github.com/open-policy-agent/opa/v1/rego
52. https://pkg.go.dev/cel.dev/cel-go?tab=licenses
53. https://osv.dev/list?ecosystem=Go&q=helm.sh%2Fhelm%2Fv4
54. https://pkg.go.dev/sigs.k8s.io/kustomize/api?tab=versions
55. https://pkg.go.dev/sigs.k8s.io/kustomize/api?tab=licenses
56. https://pkg.go.dev/github.com/compose-spec/compose-go/v2?tab=versions
57. https://pkg.go.dev/github.com/compose-spec/compose-go/v2?tab=licenses
58. https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6?tab=versions
59. https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6?tab=licenses
60. https://raw.githubusercontent.com/santhosh-tekuri/jsonschema/master/draft.go
61. https://pkg.go.dev/cuelang.org/go?tab=versions
62. https://pkg.go.dev/cuelang.org/go?tab=licenses
63. https://pkg.go.dev/github.com/replicatedhq/troubleshoot?tab=versions
64. https://pkg.go.dev/github.com/replicatedhq/troubleshoot?tab=licenses
65. https://raw.githubusercontent.com/replicatedhq/troubleshoot/main/cmd/troubleshoot/cli/analyze.go
66. https://pkg.go.dev/github.com/tetratelabs/wazero?tab=versions
67. https://pkg.go.dev/github.com/tetratelabs/wazero?tab=licenses
68. https://pkg.go.dev/github.com/extism/go-sdk?tab=versions
69. https://pkg.go.dev/github.com/extism/go-sdk?tab=licenses
70. https://github.com/extism/go-sdk
71. https://pkg.go.dev/github.com/homeport/dyff?tab=versions
72. https://pkg.go.dev/github.com/homeport/dyff?tab=licenses
73. https://pkg.go.dev/github.com/yannh/kubeconform?tab=versions
74. https://github.com/yannh/kubeconform
75. https://github.com/fluxcd/flux-schema
76. https://github.com/controlplaneio-fluxcd/schema-catalog
77. https://pkg.go.dev/github.com/controlplaneio-fluxcd/flux-operator?tab=versions
78. https://github.com/controlplaneio-fluxcd/flux-operator/blob/main/LICENSE
79. https://pkg.go.dev/github.com/stefanprodan/timoni?tab=versions
80. https://github.com/stefanprodan/timoni
81. https://raw.githubusercontent.com/kubernetes-sigs/kubectl-validate/main/README.md
82. https://github.com/yannh/kubernetes-json-schema
83. https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/manifests-validation.md
84. https://kyverno.io/docs/installation/releases/
85. https://github.com/fluxcd/kustomize-controller/blob/main/docs/spec/v1/kustomizations.md
86. https://fluxcd.io/flux/installation/
87. https://pkg.go.dev/k8s.io/apiserver/pkg/cel/environment
88. https://pkg.go.dev/k8s.io/apiserver/pkg/cel/environment#MustBaseEnvSet
89. https://kyverno.io/blog/2026/02/02/announcing-kyverno-release-1.17/
90. https://kyverno.io/blog/2026/04/24/announcing-kyverno-release-1.18/
91. https://kyverno.io/blog/2026/08/20/announcing-kyverno-release-1.19/
92. https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/commands/apply/command.go
93. https://kyverno.io/docs/subprojects/kyverno-cli/
94. https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
95. https://github.com/helm/helm/pull/13283
96. https://helm.sh/docs/changelog/
97. https://raw.githubusercontent.com/helm/helm/main/pkg/chart/common/util/jsonschema.go
98. https://raw.githubusercontent.com/santhosh-tekuri/jsonschema/master/README.md
99. https://helm.sh/docs/topics/charts/
100. https://github.com/helm/helm/issues/13069
101. https://pkg.go.dev/k8s.io/apiserver/pkg/admission/plugin/policy/validating