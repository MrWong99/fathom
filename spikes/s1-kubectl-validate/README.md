# S1: kubectl-validate `pkg/validator` vs the kind oracle

## Goal

Decide whether kubectl-validate's `pkg/validator` (pinned by pseudo-version,
no fork) can be the S-stage of the admission chain (design section 3.3:
decode-level defaulting, pruning, structural schema and CRD CEL through
`customresource.NewStrategy` with the server's cost constants). Three
questions: is the offline error text identical to `kubectl apply
--dry-run=server` on a 1.37 kind cluster for a failing
`x-kubernetes-validations` rule, for CRD validation ratcheting and for CEL
runtime cost budget exhaustion; does the module compile and pass its own
tests under MVS at `k8s.io v0.37.0`; and what would a fork or port need.

## Pass criterion (tracker)

Server-identical error text vs kind; compiles and passes its own tests under MVS at k8s.io v0.37.0; fork-readiness note

## Method

- `testdata/` (written by the oracle agent, read-only here): four CRDs, nine
  objects, `manifest.json` with kubectl's stderr per scenario, one golden file
  per scenario and `commands.sh` to replay them. Oracle: kind v0.33.0,
  `kindest/node:v1.37.0`, kubectl client v1.36.4, server v1.37.0, context
  `kind-fathom-oracle`.
- `validate/validate.go`:
  - `NewOffline`: `validator.New(openapiclient.NewLocalCRDFiles(os.DirFS("testdata/crds")))`.
    No hardcoded builtins are composed in; the local CRD client injects the
    ObjectMeta definitions itself.
  - `Offline.Create`: kubectl-validate's CLI path, strict `Parse` then
    `Validate` (`rest.BeforeCreate` on the strategy). `CreateIgnoreUnknown`
    is the spike's stand-in for `--validate=ignore`: kubectl-validate's
    `Parse` is strict-only, so a plain YAML decode (no pruning) feeds
    `Validate`; the unknown field stays on the object. `Offline.Update` is
    `Create` on the new object: kubectl-validate has no old-object path.
  - `NewStrategy` + `Strategy.ValidateUpdate`/`ValidateCreate`: the ~100-line
    piece kubectl-validate is missing, built only from exported
    `k8s.io/apiserver` and `k8s.io/apiextensions-apiserver` v0.37.0 APIs the
    way `customresource_handler.go` does it (internal CRD ->
    `GetSchemaForVersion` -> `NewStructural` + `validation.NewSchemaValidator`
    -> `customresource.NewStrategy` with the `crdserverscheme` typer ->
    `rest.BeforeUpdate` with a warning recorder on the context).
  - `render`: kubectl's stderr shape from a `StatusError` (`The <Kind>
    "<name>" is invalid: ` with one cause inline or `* ` lines; `Warning: `
    lines first), so both sides go through the same normaliser.
  - `Normalise`: strips kubectl's `The <Kind> "<name>" is invalid: `, the
    strategy's `<Kind>.<group> "<name>" is invalid: ` and kubectl's `Error
    from server (...): error when creating "...": ` wrappers, splits the
    `* ` items, keeps `Warning:` lines, sorts. `Identical` compares the lists
    order-insensitively.
- `validate/validate_test.go`:
  - `TestOfflineVsServer` and `TestStrategyPort` run every scenario and log
    server text, offline text and the normalised items; the identical /
    not-identical outcome per scenario is pinned so a change in either
    direction fails the test.
  - `TestIgnoreUnknownIsNotParse` pins that `Parse` rejects the unknown
    field and that the `CreateIgnoreUnknown` decode keeps it unpruned, the
    object-state difference the text comparison cannot see.
  - `TestRatchetingGateDefault` checks `CRDValidationRatcheting` in the
    apiserver's default feature gate.
  - `TestOracleReplay` re-runs every `--dry-run=server` against the kind
    oracle from `testdata/` and checks stderr is byte-identical to the golden;
    it skips when the context is unreachable (`FATHOM_ORACLE_CONTEXT`
    overrides the context name). It does not run `commands.sh`.
- Upstream tests: `CGO_ENABLED=0 go test ./...` on a writable copy of the
  module with `k8s.io/*` bumped to v0.37.0 (`go mod edit -require`, `go mod
  tidy`); the module cache copy is read-only and one upstream test shells out
  to `go list -m -mod=mod`, which wants to write `go.sum`.

Run: `go test -race -count=1 -v ./...` (about 35 s; the budget scenario runs
350 x 350 CEL iterations twelve times under the race detector).
