# S1 result: PARTIAL

Date: 2026-09-18. Oracle: kind `kind-fathom-oracle`, server v1.37.0.
Offline: kubectl-validate `v0.0.5-0.20260105161640-a97ccfaca20b` under MVS at
`k8s.io v0.37.0`, no `replace` directives, `CGO_ENABLED=0 go build` passes.

The criterion has three parts. Judged strictly:

| Part | Result |
|---|---|
| Server-identical error text vs kind | **Not met.** 5 of 8 scenarios identical through kubectl-validate's own API (`Parse` + `Validate`). `structural-ignore` is text-identical only with the spike's own plain-YAML decode standing in for `--validate=ignore` (`Parse` is strict-only; the offline object keeps the unpruned `spec.colour` that the server prunes, a difference the text comparison cannot see): 6 of 8 counting it. `structural` (unknown field under `fieldValidation=Strict`) differs in wrapper and item text; `ratcheting-replicas` is rejected offline because `Validate` has no old object, while the server ratchets and warns. The ~80-line strategy port written in this spike (`ValidateUpdate` via `rest.BeforeUpdate`) brings it to 7 of 8; only the decode-level `structural` text stays different. |
| Compiles and passes its own tests under MVS at k8s.io v0.37.0 | **Compiles: yes. Own tests: no.** `TestHasUptoDateBuiltinSchemas` fails (`Missing builtin version v1.36`, `v1.37`): the embedded builtin schemas stop at 1.35 and the test pins them to the linked `k8s.io/api` minor. Every other upstream test passes at v0.37.0. |
| Fork-readiness note | **Met**, below. |

A mismatch is a mismatch: parts 1 and 2 are not met, so this is not a
PASS. It is PARTIAL rather than FAIL under the tracker's verdict rule
(`spikes/README.md`, Process): part 3 is met, and every unmet part is
measured and bounded (a strict decoder with the server's error wording; a
non-strict decoder that prunes; the builtins embed) and named as a design
change below. Both gaps are reasons to port the ~950 to 1,200 LOC of
`pkg/validator` rather than import the module, see "What it changes in the
design".

## Versions

| Component | Version |
|---|---|
| Go | `go1.27.1-X:nodwarf5 linux/amd64` (`go version`), `CGO_ENABLED=0` build verified |
| sigs.k8s.io/kubectl-validate | `v0.0.5-0.20260105161640-a97ccfaca20b` = commit `a97ccfaca20b164819afdae780392496f6ed6bbd`, 2026-01-05T16:16:40Z; upstream `go.mod` pins k8s.io v0.35.0 |
| k8s.io/api, apimachinery, apiserver, apiextensions-apiserver, client-go | v0.37.0 (resolved by MVS, `go list -m all`) |
| github.com/google/cel-go | v0.29.2 (the path `k8s.io/apiserver` pins; `cel.dev/cel-go` is not in the dependency graph) |
| k8s.io/kube-openapi | v0.0.0-20260721132016-d427ff9ee9ad |
| kind | v0.33.0 |
| kubectl | client v1.36.4, server v1.37.0 |
| node image | `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5` |
| `CRDValidationRatcheting` | enabled in `utilfeature.DefaultFeatureGate` by default (GA, locked on since 1.33 in `apiextensions-apiserver/pkg/features/kube_features.go:70-74`; registered by the package's `init`) |

## Scenarios: server text vs offline text

Server text is kubectl's stderr verbatim (`testdata/golden/*.server.txt`).
Offline text is kubectl-validate's `Parse` + `Validate` rendered by the
spike's `render` (`validate/validate.go`, a re-implementation of kubectl's
`StandardErrorMessage`/`MultilineError` shape) so that both sides go through
the same normaliser; the raw kubectl-validate error reads
`Widget.spike.fathom.dev "cel-fail" is invalid: ...`. `↵` marks a newline. "Identical" is
the normalised comparison (wrappers stripped, `* ` items split, sorted).
The last column is the strategy port from this spike (`rest.BeforeCreate` /
`rest.BeforeUpdate` on `customresource.NewStrategy`, real old object).

| Scenario | Server (`--dry-run=server`) | Offline (kubectl-validate `pkg/validator`, kubectl-shaped by the spike's `render`) | Identical | Port identical |
|---|---|---|---|---|
| cel-fail | `The Widget "cel-fail" is invalid: ↵* spec: Invalid value: replicas must not exceed maxReplicas↵* spec.name: Invalid value: "UPPER": name must be lowercase, got UPPER` | `The Widget "cel-fail" is invalid: ↵* spec: Invalid value: replicas must not exceed maxReplicas↵* spec.name: Invalid value: "UPPER": name must be lowercase, got UPPER` | **yes** | yes |
| cel-pass | (empty, exit 0) | (empty, accepted) | **yes** | yes |
| structural (strict) | `Error from server (BadRequest): error when creating "objects/structural.yaml": Widget in version "v1" cannot be handled as a Widget: strict decoding error: unknown field "spec.colour"` | `spec.colour: Invalid value: value provided for unknown field` (from `Parse`; `Validate` never runs) | **no** | no (no decoder in the port: it reports the `structural-ignore` text instead) |
| structural-ignore (`--validate=ignore`) | `The Widget "structural" is invalid: ↵* spec.replicas: Invalid value: "string": spec.replicas in body must be of type integer: "string"↵* <nil>: Invalid value: null: some validation rules were not checked because the object was invalid; correct the existing errors to complete validation` | same text, but **not through kubectl-validate's API**: `Parse` is strict-only, so the spike's plain-YAML `decode` (`CreateIgnoreUnknown`) feeds `Validate`; `spec.colour` is not pruned, only invisible to the validator, while the server prunes it (`TestIgnoreUnknownIsNotParse` pins both facts) | **yes** (text only) | yes (same decode) |
| structural-type | `The Widget "structural-type" is invalid: ↵* spec.replicas: Invalid value: "string": spec.replicas in body must be of type integer: "string"↵* <nil>: Invalid value: null: some validation rules were not checked because the object was invalid; correct the existing errors to complete validation` | same text | **yes** | yes |
| ratcheting-replicas (update, only `spec.replicas` changes) | `Warning: spec.name: Invalid value: "UPPER": name must be lowercase` (accepted, exit 0) | `The Gadget "ratchet" is invalid: spec.name: Invalid value: "UPPER": name must be lowercase` (rejected) | **no** | yes (`Warning: spec.name: Invalid value: "UPPER": name must be lowercase`, accepted) |
| ratcheting-name (update, `spec.name` UPPER -> OTHER) | `The Gadget "ratchet" is invalid: spec.name: Invalid value: "OTHER": name must be lowercase` | same text | **yes** | yes |
| budget | `The Blob "budget" is invalid: spec: Invalid value: "object": validation failed due to running out of cost budget, no further validation rules will be run` | same text | **yes** | yes |

Both kubectl-validate mismatches, in detail:

- **structural.** The server's `fieldValidation=Strict` decoder rejects the
  request before validation with a `BadRequest` whose message is
  `Widget in version "v1" cannot be handled as a Widget: strict decoding
  error: unknown field "spec.colour"`. kubectl-validate's strict `Parse`
  also stops before validation but builds its own error from the structural
  schema walk: `spec.colour: Invalid value: value provided for unknown field`
  (`crd_decoder.go:116-125`, `field.Invalid(path, OmitValueType{}, ...)`).
  Same verdict, same field, different wording and different error class
  (decode error, not `StatusError`). A port can reproduce the server's
  wording by calling the serializer's strict decoder and keeping its
  `runtime.StrictDecodingError` text.
- **ratcheting-replicas.** `Validate` calls `rest.BeforeCreate`, so the
  strategy's `Validate` runs with `oldObj == nil`: no `CorrelatedObject`, no
  `WithRatcheting`, and the unchanged invalid `spec.name` is an error. The
  server runs `ValidateUpdate`, ratchets the unchanged field and emits the
  error as a warning (`schema/cel/validation.go:469`,
  `warning.AddWarning(ctx, "", e.Error())`).

## Ratcheting: what offline `Validate` returns, and the `ValidateUpdate` port

kubectl-validate on the updated object alone (`objects/ratcheting-replicas.yaml`)
returns `Gadget.spike.fathom.dev "ratchet" is invalid: spec.name: Invalid
value: "UPPER": name must be lowercase` and rejects it. On
`ratcheting-name.yaml` it returns the same text with `"OTHER"`, which happens
to match the server because a changed value is never ratcheted.

`validate.NewStrategy` + `Strategy.ValidateUpdate` (about 80 lines including
comments and the `Strategy` type, `validate/validate.go`) is what
kubectl-validate would need:

1. decode the CRD with `apiextensionsv1.CustomResourceDefinition`, convert
   to the internal type, `apiextensions.GetSchemaForVersion`;
2. `structuralschema.NewStructural(props)` and
   `validation.NewSchemaValidator(props)`, which returns a
   `RatchetingSchemaValidator` because the gate is on (kubectl-validate's
   `basicValidatorAdapter` discards the old object and would not ratchet
   schema-level errors even through `BeforeUpdate`);
3. `customresource.NewStrategy(crdserverscheme.NewUnstructuredObjectTyper(),
   namespaced, gvk, sv, nil, ss, nil, nil, nil)`;
4. `rest.BeforeUpdate(strategy, ctx, new, old)` with
   `request.WithNamespace` and `warning.WithWarningRecorder` on the context.

Two details the port had to get right, both of which the server gets for
free from its request path:

- Numbers: `sigs.k8s.io/yaml.Unmarshal` into a map gives `float64`, and
  every integer CEL rule then fails with `invalid data, expected int, got
  float64 evaluating rule: ...`. Decoding through
  `Unstructured.UnmarshalJSON` (`UnstructuredJSONScheme`) gives `int64` like
  the apiserver. kubectl-validate's `Parse` does this correctly; a port that
  accepts already-decoded objects must document the contract.
- `metadata.resourceVersion`: `rest.ValidateUpdate` requires it
  (`must be specified for an update`); kubectl's patch carries the stored
  one, so the port copies it from the old object when the new one has none.
  UID and creationTimestamp are copied by `BeforeUpdate` itself.

With that, the port **ratchets like the server**: `ratcheting-replicas` is
accepted with the identical `Warning:` line, `ratcheting-name` is rejected
with the identical error. No feature gate had to be set: `k8s.io/apiserver`
0.37 registers `CRDValidationRatcheting` as GA and locked to `true`, and
`utilfeature.DefaultFeatureGate.Enabled` returns `true` in the test binary
(`TestRatchetingGateDefault`).

## Budget

The oracle reproduced the budget error (`budgetReproduced: true`): 14 rules
of `self.values.all(x, self.values.all(y, x + y >= 0))` over 350 integers
exhaust the 10,000,000 `RuntimeCELCostBudget` after 11 rules. Offline,
kubectl-validate's strategy hard-wires the same `celconfig.PerCallLimit` and
`celconfig.RuntimeCELCostBudget` (`customresource/strategy.go:78,221`), and
the text is **identical**: `spec: Invalid value: "object": validation failed
due to running out of cost budget, no further validation rules will be run`.
The strategy port gives the same text. Sub-criterion measured, not "not
measured".

Cost: the scenario takes about 1.65 s offline without the race detector
(`go test -count=1 -run 'TestOfflineVsServer/budget$'`: 1.65 to 1.67 s over
three runs; server round trip through kubectl: 1.63 to 1.65 s) and 33 s under
`-race`. A CEL budget exhaustion is a real per-object latency risk for the S7
chain budget; it is bounded by the same constants the server uses, so it is
the same ~1.6 s a real apply would spend.

## Upstream test result

Given input (module cache copy, read-only):

```
cd .../kv-scout && CGO_ENABLED=0 go test -count=1 sigs.k8s.io/kubectl-validate/...
ok   sigs.k8s.io/kubectl-validate/pkg/cmd        2.246s
panic: exit status 1
  sigs.k8s.io/kubectl-validate/pkg/openapiclient_test.init.func1
  .../pkg/openapiclient/hardcoded_builtins_test.go:18
FAIL sigs.k8s.io/kubectl-validate/pkg/openapiclient  0.712s
ok   sigs.k8s.io/kubectl-validate/pkg/utils      0.003s
ok   sigs.k8s.io/kubectl-validate/pkg/validator
```

The panic is environmental: the test's package `init` runs
`go list -m -mod=mod -f '{{if eq .Path "k8s.io/api"}}{{.Version}}{{end}}' all`
from the package directory, which tries `updating go.sum: open
.../kubectl-validate@v0.0.5-.../go.sum: permission denied` in the read-only
module cache. Re-run on a writable copy of the module with
`go mod edit -require=k8s.io/{api,apiextensions-apiserver,apimachinery,apiserver,client-go}@v0.37.0`
and `go mod tidy` (MVS resolves cel-go v0.29.2, no `replace`):

```
ok   sigs.k8s.io/kubectl-validate/pkg/cmd            2.130s
--- FAIL: TestHasUptoDateBuiltinSchemas (0.00s)
    hardcoded_builtins_test.go:25: 0.37.0
    hardcoded_builtins_test.go:41: Missing builtin version v1.36
    hardcoded_builtins_test.go:41: Missing builtin version v1.37
FAIL sigs.k8s.io/kubectl-validate/pkg/openapiclient  0.800s
ok   sigs.k8s.io/kubectl-validate/pkg/utils          0.002s
ok   sigs.k8s.io/kubectl-validate/pkg/validator      0.066s
```

`TestNewLocalCRDFiles`, `Test_localCRDsClient_Paths`,
`TestNewLocalSchemaFiles`, `Test_localSchemasClient_Paths` and
`TestGitHubBuiltins` (live GitHub call) pass. The one failure is a real
consequence of MVS at v0.37.0: the test requires an embedded builtin schema
set for every minor up to the linked `k8s.io/api`, and the embed ends at
1.35. Nothing in the failing test touches `pkg/validator`.

Side effect of importing `pkg/openapiclient` at all: the 110 MB
`//go:embed builtins` is linked; the spike's test binary is 189 MB.

## Fork-readiness note

Module root `M` = `/home/luk/go/pkg/mod/sigs.k8s.io/kubectl-validate@v0.0.5-0.20260105161640-a97ccfaca20b` (read-only; not modified).

### 1. Identity, pin, build, license
- Pseudo-version `v0.0.5-0.20260105161640-a97ccfaca20b` = commit `a97ccfaca20b164819afdae780392496f6ed6bbd`, 2026-01-05T16:16:40Z ("Merge PR #175 eddycharly/kube-1.35"); module cache `.info` confirms origin. Last tag `v0.0.4` = `fac15fd6e…`, 2024-05-29 (pinned k8s v0.30.1).
- Non-test LOC (wc -l, 26 files): **3060**; `pkg/` only 2892 (validator 1366, openapiclient 859 incl. `groupversion/`, utils 346, cmd 321). Tests 748 LOC. `pkg/validator/structural.go` (447) is self-declared DEPRECATED dead code (`M/pkg/validator/structural.go:14-18`; no callers outside the file).
- k8s pin `M/go.mod:9-13`: `k8s.io/{apiextensions-apiserver,apimachinery,apiserver,client-go} v0.35.0`, `kube-openapi 20251125` → **2 minors behind** fathom's v0.37.0. Builds under MVS at v0.37.0: **yes** — `kv-scout` (`CGO_ENABLED=0 go build` OK; `go list -m` resolves apiserver/apiextensions/apimachinery to v0.37.0, cel-go v0.29.2). `replace` directives: **0** in both go.mod files. License: Apache-2.0 (`M/LICENSE:1-3`). Go `1.25.0` (`M/go.mod:3`).

### 2. `pkg/validator` surface and the missing update path
- Public: `Validator` (`validator.go:29`), `New(openapi.Client)` (`:34`, calls `client.Paths()`), `Parse([]byte) (GVK, *Unstructured, error)` (`:51`; strict YAML decode through a structural-schema coercing decoder: pruning + defaulting + unknown-field paths, `crd_decoder.go:77-129,288-303`), `Validate(*Unstructured) error` (`:90`).
- `Validate` (`validator.go:90-127`): clones the object, defaults namespace to `default` (`:104-107`), rewrites core `v1` → `core/v1` (`:109-114`), then `customresource.NewStrategy(typer, namespaced, gvk, SchemaValidator(), nil, ss, nil, nil, nil)` (`:121-123`) and `rest.FillObjectMetaSystemFields` + **`rest.BeforeCreate`** (`:125-126`). Create-only.
- Why update is needed: `customResourceStrategy.Validate` passes `oldObj == nil` to the CEL validator (`apiextensions-apiserver@v0.37.0/pkg/registry/customresource/strategy.go:221`), so `oldSelf` transition rules are never evaluated and no ratcheting happens. Both live only in `ValidateUpdate` (`strategy.go:274-319`): `common.NewCorrelatedObject(new, old, …)`, `validation.WithRatcheting`, `cel.WithRatcheting` (`:287-291`) and `celValidator.Validate(…, uNew, uOld, RuntimeCELCostBudget, celOptions…)` (`:309`), all gated on `CRDValidationRatcheting`, GA + locked-on since 1.33 (`pkg/features/kube_features.go:70-74`). Entry point is `rest.BeforeUpdate(strategy, ctx, obj, old)` (`apiserver@v0.37.0/pkg/registry/rest/update.go:114`): copies generation/UID/timestamps from old, `PrepareForUpdate`, `ValidateUpdate`, warnings.
- Second gap: kubectl-validate's `basicValidatorAdapter.ValidateUpdate` discards `old` and options (`validator_gvk.go:136-138`), so even through `BeforeUpdate` schema-level errors would not ratchet; the server uses `validation.NewRatchetingSchemaValidator` / `NewSchemaValidatorFromOpenAPI` (`apiextensions-apiserver/pkg/apiserver/validation/validation.go:120-125`, `ratcheting.go:45`).
- Unexported pieces a `ValidateUpdate` needs (export or copy): `validatorEntry` (`validator_gvk.go:19`), `(*Validator).infoForGVK` + `validatorCache` (`validator.go:129`, `:31`), `validatorEntry.StructuralSchema()` (`validator_gvk.go:98`), `.SchemaValidator()` (`:35`), `.ObjectTyper()` (`:44`), `.IsNamespaceScoped()` (`:31`), `newUnstructuredObjectTyper` (`crd_decoder.go:261`; the type `UnstructuredObjectTyper` itself is exported, `:256`), `basicValidatorAdapter` (`validator_gvk.go:128`). `Validator` has no exported fields; nothing can be reached from outside → upstream PR, fork, or port.
- Port estimate (Validate + ValidateUpdate into `internal/admit/port/kubectlvalidate`, Apache header + NOTICES): `validator.go` 318 + `validator_gvk.go` 138 + `schema_patcher.go` 160 + `utils/schema.go` 76 + `utils/schema_visitor.go` 159 = **~850 LOC**, plus ~50 for the typer (`crd_decoder.go:256-286`) and ~60 new for `ValidateUpdate` → **~950**; **~1,200** if `Parse`'s coercing decoder (`crd_decoder.go`, 303) is ported too, which design §3 wants (decode-level defaulting/pruning). `structural.go` is dropped.

### 3. CEL cost constants
- `PerCallLimit = 1000000` and `RuntimeCELCostBudget = 10000000` are Go `const`s in `k8s.io/apiserver/pkg/apis/cel/config.go:22,26` (identical in v0.35.0 and v0.37.0). The strategy hard-wires them: `cel.NewValidator(ss, true, celconfig.PerCallLimit)` (`strategy.go:78`) and `celValidator.Validate(…, celconfig.RuntimeCELCostBudget…)` (`strategy.go:221,309`).
- kubectl-validate never names them (grep of `M/pkg` empty) and `NewStrategy` returns the unexported value type `customResourceStrategy` with unexported `celValidator` (`strategy.go:56-68,76`): **no caller can change them**. A port could pass its own values via `cel.NewValidator(s, true, perCallLimit)` (`schema/cel/validation.go:84`) and `(*Validator).Validate(…, costBudget, …)` (`:205`), but design §3 wants the server constants, so the default is the right one.
- Related and not tunable either: the CEL env is `environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion())` (`schema/cel/validation.go:98`) = the build's min-compatibility version (`apiserver/pkg/cel/environment/base.go:49-55`), not the snapshot minor that design's skew policy names.

### 4. `pkg/openapiclient` — which client fits a snapshot
- `NewLocalSchemaFiles(fs)` (`local_schemas.go:23`): layout `/apis/<group>/<version>.json` + `/api/<version>.json` (`:20-22,40-74`), JSON only (`groupversion/file.go:18`). This is the layout `openapi/` in the snapshot copies (design §3, line 242). **Fits**: each `/openapi/v3/apis/<g>/<v>` (and `/openapi/v3/api/v1`) document written verbatim — components carry `x-kubernetes-group-version-kind` and the paths give scope (`validator.go:279-289`); installed CRDs are already in the server's v3 docs. Importer must strip `?hash=` into the file name.
- `NewLocalCRDFiles(fs...)` (`local_crds.go:40`): decodes CRD YAML/JSON, `NewStructural(...).ToKubeOpenAPI()` (`:120-124`), injects GVK + `x-kubectl-validate-scope` (`:135-137`), `metadata/apiVersion/kind` (`:145-171`) and embedded ObjectMeta defs (`:22,:190`); keys only `apis/<g>/<v>` (`:193`). Use for CRDs not yet installed (the chart's own `crds/`), composed after the snapshot: `NewComposite(NewLocalSchemaFiles(snap), NewLocalCRDFiles(chartCRDs))`.
- `NewHardcodedBuiltins(v)` (`hardcoded_builtins.go:37`): `//go:embed builtins` for 1.23–1.35, **110 MB** on disk (`:15`; `du -sh`). `NewGitHubBuiltins(v)` (`github_builtins.go:26`): live `api.github.com` on `release-<v>`, unpinned, rate limited (`:39`). Neither is deterministic; not for fathom.
- `NewComposite` (`composite.go:15`): union, first client wins per schema key (`groupversion/composite.go:42-46`), client errors joined but non-fatal (`:24-27`). `NewFallback` (`fallback.go:36`): first `Paths()` without error, sticky. `NewOverlay` + `PatchLoaderFromDirectory` (`overlay.go:23,44`): JSON merge patches per GV path (`groupversion/overlay.go:34`).
- Caveat: importing `pkg/openapiclient` at all links the 110 MB embed into fathom's static binary. Write fathom's own `openapi.Client` over the snapshot dir instead (`local_schemas.go` is 84 LOC; `local_crds.go` 196) — `pkg/validator` only needs the `openapi.Client` interface (`validator.go:34`).

### 5. Upstream health and open risk
- Tags: v0.0.1, v0.0.2, v0.0.3, v0.0.4 (proxy `@v/list`); `@latest` = v0.0.4 (2024-05-29). No tag since; consumers must pin pseudo-versions.
- Commits 2025-01 → 2026-01 (GitHub API, committer date): 13 total — 2025-01 (2, k8s bump), 2025-02 (1, k8s bump), 2025-05 (1, dependabot merge), 2025-09 (5: "fix: local CRDs for 1.34", new approver eddycharly, testify bump), 2025-12 (3: "upgrade to kubernetes 1.35", cobra bump), 2026-01 (1, merge). Cadence: one k8s-minor bump plus dependabot; no feature work. Approvers list has seven names (`M/OWNERS`); dependabot ignores k8s minor bumps (`.github/dependabot.yml`), so minor upgrades are manual.
- Risk: (a) create-only API with no ratcheting or `oldSelf` — fathom needs a port or an upstream PR either way; (b) two minors behind and manual bumps — MVS at v0.37.0 works today but is unverified per upstream test; (c) `Parse` core-group rewrite `v1`→`core/v1` and the `default` namespace default (`validator.go:104-114`) shape error text; (d) CEL env pinned to build compat version, not snapshot minor; (e) 110 MB embed if `openapiclient` is imported. Recommendation: import `pkg/validator` for S1 calibration only, plan the ~950–1,200 LOC port under `internal/admit/port/` with `ValidateUpdate` as the first fathom-owned addition.

Spike-measured addenda to the note: risk (b) is now verified (builds, one
upstream test fails on the builtins embed); the ratcheting gap (a) is closed
by about 80 lines over exported APIs; the `structural` wording is a third gap
the note did not list (`Parse` builds its own unknown-field error instead of
the serializer's `strict decoding error: unknown field "..."`); and `Parse`
being strict-only is a fourth: there is no pruning, non-strict decode for
`fieldValidation=Ignore`/`Warn`, which the port's coercing decoder must add.

## What it changes in the design

1. **3.1 Binary and packages.** "kubectl-validate by pseudo-version (S1
   verifies it compiles and passes its own tests under MVS at v0.37.0)" is
   half true: it compiles, one upstream test fails on the builtin embed, and
   importing `pkg/openapiclient` adds 110 MB to the static binary. Replace
   the module dependency with a port: `internal/admit/port/kubectlvalidate`
   (Apache-2.0 header, `third_party/NOTICES`), ~950 LOC for
   `Validate`/`ValidateUpdate` plus the typer, ~1,200 with the coercing
   decoder. fathom writes its own `openapi.Client` over the snapshot's
   `openapi/` directory (84 + 196 LOC upstream to look at). kubectl-validate
   stays on the export-adapter list (3.8) and out of `go.mod`.
2. **3.3 S-stage.** "via kubectl-validate's `customresource.NewStrategy`
   path with the server's cost constants" holds for the text and the cost
   constants (cel-fail, structural-type, budget: identical), but the S-stage
   must be the **update** path when an old object exists in the snapshot:
   `rest.BeforeUpdate` with `validation.NewSchemaValidator` (ratcheting
   validator) and a warning recorder, so ratcheted errors surface as
   Warning findings, not Blocking. Two contract lines the stage needs:
   objects enter as `int64`-typed Unstructured (the apiserver's JSON decode,
   not a YAML map), and updates carry the stored `resourceVersion`. The
   `fieldValidation` mode is a stage input (`Strict`/`Warn`/`Ignore`,
   default `Strict` like kubectl apply); Strict stops at the decoder with
   the server's `strict decoding error: unknown field` wording, which the
   port takes from the serializer rather than from kubectl-validate's
   `Parse`.
3. **10.1 S1 row.** Status PARTIAL, 2026-09-18: 5/8 identical through the
   module's API, 6/8 with the spike's non-strict decode standing in for
   `--validate=ignore` (text only; the object keeps the unpruned field), 7/8
   with the spike's strategy port, the eighth is the decode wrapper; builds under MVS at v0.37.0, one upstream test fails
   (builtins embed ends at 1.35); fork-readiness note written, decision is
   port, not fork. Open owner decision to add to section 12: whether to send
   the `ValidateUpdate` + ratcheting-validator change upstream as a PR in
   parallel (small, self-contained, and the project takes k8s bumps only).
