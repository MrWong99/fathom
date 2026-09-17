# S3: Kyverno CLI as a digest-pinned subprocess, two-pass, from snapshot side files

## Goal

Decide whether the Kyverno stage of the admission chain (design section 3.3:
Kyverno mutate as the last M-stage mutator, first CLI pass with the patched
output taken; Kyverno validate as a V-stage validator, second CLI pass on the
mutated objects with `--context-file`, `--parameter-resource`, `--userinfo`,
`--policy-report`) can be an exec adapter that (1) generates every side file
the CLI needs from snapshot data (ConfigMaps, Namespaces, RoleBindings, the
requester identity), (2) reproduces the exact denials a real Kyverno v1.19.1
admission controller and the apiserver's own ValidatingAdmissionPolicy
produce on the kind oracle, and (3) reports fathom-owned exit codes computed
from the parsed report and the policies' enforcement settings, never from the
CLI's exit status as a verdict (the exit status is only checked for
consistency with the report). Kyverno is a subprocess: no Kyverno Go module
is imported (CLAUDE.md non-negotiables), the binary is pinned by sha256 and
run with a minimal explicit environment (`HOME` under the work directory,
`PATH=/usr/bin:/bin`; nothing from the caller's process environment).

## Pass criterion (tracker)

Reproduces a known in-cluster denial; fathom-owned exit codes

## Method

- `testdata/` (written by the oracle agent, read-only here): six policies
  ((a) ClusterPolicy validate reading a ConfigMap context, (b) ClusterPolicy
  mutate, (c) ClusterPolicy validate gated on `request.userInfo`, (d)
  ValidatingAdmissionPolicy + binding with a ConfigMap `paramRef`, (e)
  kyverno.io/v2 PolicyException, (f) policies.kyverno.io/v1 ValidatingPolicy
  reading the same ConfigMap through `resource.get`), five Deployments, the
  snapshot side data, `manifest.json` with kubectl's verbatim verdict per
  scenario, one golden per scenario, the cluster-stored mutated Deployment,
  every CLI probe output under `cli/out/` and `commands.sh` to replay all of
  it. Oracle: kind `kind-fathom-oracle`, `kindest/node:v1.37.0`, kubectl
  client v1.36.4, Kyverno chart 3.9.1 (appVersion v1.19.1) with
  `features.policyExceptions.enabled=true` for namespace `s3`.
- `engine/testdata/` (spike-owned): (a) with `failureAction: Audit`, (d)
  with `validationActions: [Warn]`, (f) without `validationActions`, (g) a
  MutatingAdmissionPolicy + binding with a ConfigMap `paramRef` (written at
  `admissionregistration.k8s.io/v1alpha1`, the only version CLI 1.19.1
  parses), two Deployments outside `s3` (one sharing a name with
  `web-passing`), the Deployment `web-map` for (g), and `probes.sh`, which
  records two oracle goldens the tracker's `commands.sh` does not: the
  server verdict under (f)-without-`validationActions`
  (`vpol-no-validation-actions.server.txt`, plus `.alt` for the webhook
  race) and the server dry-run object under (g) applied at v1
  (`map-paramref.dryrun.yaml`).
- `engine/engine.go`: `Run(ctx, Options, Input) (*Result, error)`.
  `Options` pins the binary (absolute path + sha256, verified before every
  run), a timeout and a fresh work directory. `Input` is policy files (any
  mix of kinds), object files and snapshot files. The CLI is started with
  `Env = [HOME=<work>/home, PATH=/usr/bin:/bin]` only.
  - `engine/policies.go`: routes each policy file by kind (mutate rules,
    MutatingAdmissionPolicy + binding and MutatingPolicy to pass 1; validate
    rules, VAP + binding and ValidatingPolicy to pass 2; PolicyException to
    `--exception` on both) and derives enforcement per policy/rule
    (ClusterPolicy `failureAction` / `validationFailureAction` = Enforce,
    VAP binding `validationActions` contains Deny, ValidatingPolicy
    `validationActions` contains Deny or is absent, which the oracle denies).
    It also records which ConfigMap context entries and which VAP/MAP
    `paramRef`s the snapshot has to satisfy. A MutatingAdmissionPolicy at any
    version but v1alpha1 is refused: the CLI drops it with a non-fatal parse
    error and "Applying 0 policy rule(s)".
  - `engine/snapshot.go`: generates `values.yaml` (cli.kyverno.io/v1alpha1
    Values: `policies[].rules[].values.<context>` from the snapshot
    ConfigMaps, `namespaceSelector[]` from the snapshot Namespaces),
    `context.yaml` (Context with every snapshot object in `spec.resources`),
    `userinfo.yaml` (UserInfo from a SelfSubjectReview plus roles and
    clusterRoles resolved from the snapshot's bindings with Kyverno's own
    rule) and one `--parameter-resource` file per VAP `paramRef`.
  - Pass 1: `kyverno apply <mutate files> --resource <objects> --exception
    ... <side flags> -o <dir>`. Two admitted objects sharing a
    `metadata.name` are refused before the CLI runs (`-o` names files by
    name only). `<dir>/<name>-mutated.yaml` is read per object: a
    ClusterPolicy mutate pass writes it for every object, matched or not
    (reformatted); a MAP-only pass only for matched ones; with several
    matching mutating policies it holds one cumulative document per
    policy, and the last one is taken. `Changed` is the canonical leaf
    comparison of the result with the input, not the file's presence.
  - Pass 2: `kyverno apply <validate files> --resource <mutated objects>
    --exception ... --values-file --context-file --userinfo
    --parameter-resource --policy-report --output-format json`; the
    openreports.io/v1alpha1 ClusterReport is taken from the first stdout
    line that starts with `{` (the ClusterPolicy deprecation warnings come
    first on stdout). When no validate rule matches any object the CLI
    prints no report at all (only the warnings) and exits 0; that is a clean
    result flagged `NoMatch`, not a tool error.
  - Findings: one per report result with policy, rule, result, message,
    resource, source, binding, enforced and severity (fail + enforced =
    Blocking; fail + not enforced, warn, error = Warning; pass, skip = Info).
  - Exit code: 0 clean (including `NoMatch`); 1 any Blocking finding; 3
    tool error (digest mismatch, missing binary, timeout, CLI exit not in
    {0, 1}, CLI exit not the one the report implies, no or unparsable report
    other than the no-match output, an unsupported policy kind, a MAP at a
    version the CLI drops, a MAP `paramRef` the snapshot lacks, two admitted
    objects sharing a name in the mutate pass, or any `error` result: a
    policy the CLI could not evaluate is not a verdict). The CLI's exit code
    is recorded per pass and used only as that consistency gate, never as
    the verdict.
- `engine/normalise.go`: `ParseServerVerdict` strips only the wrappers from
  kubectl's verbatim output (the `admission webhook "validate.kyverno.svc-fail"
  denied the request: ... was blocked due to the following policies` header
  with its `<policy>:` / `  <rule>: <message>` block; the
  `vpol.validate.kyverno.svc-fail ... Policy <name> failed: ` prefix; the
  apiserver's `ValidatingAdmissionPolicy '<p>' with binding '<b>' denied
  request: ` prefix) and keeps the rule message verbatim; `OfflineVerdict`
  reduces the findings to the same `{policy, rule, message}` set. `Leaves`
  flattens a document to RFC 6901 pointer -> JSON scalar for the canonical
  mutation comparison.
- `engine/engine_test.go` (`go test -race -count=1 ./...`; skips when the
  pinned binary is absent, `FATHOM_KYVERNO_BIN` overrides the path):
  `TestBinaryDigest` (pinned digest accepted, wrong digest / other file /
  missing / relative / empty path refused), `TestScenariosVsServer` (every
  manifest scenario plus the webhook-race scenario `vpol-violating-team`,
  where the union of both goldens is asserted), `TestMutatedObjectCanonical`
  (pass-1 output is a leaf-subset of the stored object and the mutation
  delta equals the server's delta minus apiserver-owned fields),
  `TestExitCodeMapping` (thirteen cases), `TestFullChain` (all policies on
  all five objects: the five server denials, nothing else),
  `TestGeneratedSideFilesMatchOracle` (generated side files against the
  oracle's hand-written ones), `TestMAPParamRef` (MAP paramRef mutation
  delta equals the server dry run; v1 document and missing parameter
  refused), `TestTwoPassValidateSeesMutation` (the pass-2 verdict depends on
  the pass-1 output), `TestUnmatchedObjectUnchanged`, `TestNoMatchIsClean`,
  `TestDuplicateNamesRefused`, `TestValidatingPolicyWithoutValidationActions`
  (offline verdict equals the probe golden), `TestCLIContractViolations`
  (fake binaries for the tool-error rows the real CLI cannot produce),
  `TestParseServerVerdict`, `TestParseReport`, `TestColdStart` (five runs,
  min/median of the CLI pass and of the whole adapter call).
