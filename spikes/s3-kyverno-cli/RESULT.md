# S3 result: PASS

Date: 2026-09-18, revised the same day after review (see "Revision"). Oracle:
kind `kind-fathom-oracle`, server v1.37.0, Kyverno chart 3.9.1 (appVersion
v1.19.1) in namespace `kyverno`. Offline: Kyverno CLI v1.19.1 pinned by
sha256, run as a subprocess with a minimal explicit environment
(`HOME=<work>/home`, `PATH=/usr/bin:/bin`; nothing from the caller's process
environment); no Kyverno Go module, no `replace`, `CGO_ENABLED=0 go build`
passes.

The criterion has two parts. Judged strictly:

| Part | Result |
|---|---|
| Reproduces a known in-cluster denial | **Met.** 8 of 8 scenarios give the server's verdict (allowed/denied) and, for every denial, the identical `{policy, rule, message}` after stripping only the webhook/apiserver wrapper text. The mutate pass reproduces the cluster-stored mutation exactly (leaf delta identical), and the pass-2 verdict is shown to depend on the pass-1 output (`TestTwoPassValidateSeesMutation`). The full chain (6 policies, 5 objects, admin identity) reports exactly the five server denials, including both denials of the object the server reports nondeterministically. |
| fathom-owned exit codes | **Met.** The exit code is computed from the parsed report plus enforcement derived from the policy files. The CLI's exit status is never used as a verdict; it is used only as a consistency gate (an exit outside {0, 1}, or a 0/1 that contradicts the report summary, is a tool error, `TestCLIContractViolations`). Demonstrated where the two differ: Audit ClusterPolicy fail (CLI 1, fathom 0), VAP binding `validationActions: [Warn]` (CLI 1, fathom 0), evaluation errors (CLI 1, fathom 3), no policy matched (CLI 0 with no report, fathom 0 flagged `NoMatch`), digest mismatch / missing binary / timeout (no CLI verdict, fathom 3). |
| Tracker scope "incl. a MAP paramRef" (not part of the pass criterion) | **Met with a caveat.** A MutatingAdmissionPolicy with a ConfigMap `paramRef` mutates offline exactly as the server does (leaf delta identical to a `--dry-run=server -o yaml` of the same policy applied at v1, `TestMAPParamRef`). The caveat: CLI 1.19.1 parses MAP only at `admissionregistration.k8s.io/v1alpha1` and silently drops v1/v1beta1 ("Applying 0 policy rule(s)", exit 0), and a missing MAP parameter is equally silent (`parameterNotFoundAction` ignored). The adapter refuses both instead of reporting clean. |

`--crd-paths` (named in design.md 10.1 but not in the tracker's criterion) was
not exercised: no scenario admits a CRD-typed object. Carried to the S7/S8
fixtures, see "Not measured".

## Revision

The review of the first version found one adapter defect and several claims
that were stronger than the evidence. Retracted or corrected:

- **Defect.** When no validate rule matched any admitted object, the CLI
  prints no report at all under `--policy-report --output-format json`
  (only the deprecation warnings, no summary line, exit 0) and the adapter
  returned tool error 3 for a clean object. It now returns 0 with
  `Result.NoMatch = true` and no findings (`TestNoMatchIsClean`). The
  no-match output is indistinguishable, from the CLI alone, from a side-file
  mistake that makes every selector miss, so the product must surface it.
- **False statement.** "`-o <dir>` writes `<name>-mutated.yaml` only for
  matched objects": a ClusterPolicy mutate pass writes the file for every
  object it was given, matched or not (reformatted, same leaves). `Changed`
  was therefore true for untouched objects; it is now a canonical leaf
  comparison (`TestUnmatchedObjectUnchanged`). A MAP-only pass does write
  only matched objects, and with several matching mutating policies the file
  holds one cumulative document per policy (the last is the final object).
- **Untested rows** of the exit-code table (CLI exit outside {0, 1},
  exit/report mismatch, no or unparsable report, duplicate names) are now
  tested (`TestCLIContractViolations` with fake binaries,
  `TestDuplicateNamesRefused`); the duplicate-name check runs before the
  CLI.
- **Scope gap.** The tracker row says "incl. a MAP paramRef"; only the VAP
  paramRef had been exercised. Added (above).
- **Unverified default.** "ValidatingPolicy without `validationActions` is
  Deny (Kyverno's default)": the served CRD has no default. Verified on the
  oracle instead: the webhook denies (`engine/testdata/
  vpol-no-validation-actions.server.txt`, `TestValidatingPolicyWithoutValidationActions`).
- **Wording.** "Empty environment" (it is `HOME` + `PATH`); "the CLI's exit
  code is never used" (it is a consistency gate, never a verdict); "sha256
  matches the release checksums.txt" (the checksums file lists the tarball;
  the binary digest is that of the binary extracted from the
  checksum-verified tarball).

The verdict stays PASS: both parts of the criterion are met by the revised
adapter, and every claim in this document is now covered by a test or an
oracle golden.

## Versions

| Component | Version |
|---|---|
| Go | `go1.27.1-X:nodwarf5 linux/amd64`; `CGO_ENABLED=0 go build ./...` verified; tests run with `-race` |
| Module deps | `sigs.k8s.io/yaml v1.6.0` only (no `k8s.io/*` needed); no `replace` |
| Kyverno CLI | 1.19.1, git `40ec788d48bb28d83dbf85538e962a59db9d45c6`, built 2026-09-10, `/home/luk/go/bin/kyverno`, 265,433,250 bytes, sha256 `dfa1ffe747e43d0d5a34cbc676ff96ecafdf7ef979eee1c6d9d6606e3613c138`. The release `checksums.txt` lists only the tarball (`kyverno-cli_v1.19.1_linux_x86_64.tar.gz` = `b38228f367fc0fdc2b08f4c83ea50ac5f16c60ff8d62d76a66157c33c47b70ae`); the binary digest above is that of the binary extracted from that checksum-verified tarball, byte-identical to the installed file. Re-verified by `VerifyBinary` before every adapter run |
| Kyverno in kind | chart `kyverno/kyverno` 3.9.1, appVersion v1.19.1, release `kyverno` revision 3, `admissionController.replicas=1`, `features.policyExceptions.enabled=true`, `features.policyExceptions.namespace=s3` (chart default is `--enablePolicyException=false`) |
| kubectl | client v1.36.4, server v1.37.0 |
| kind | v0.33.0 |
| node image | `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, containerd 2.3.4 |
| Policy API versions served | `kyverno.io/v1` ClusterPolicy (deprecated warning on every CLI run), `kyverno.io/v2` PolicyException, `policies.kyverno.io/v1` ValidatingPolicy (`vpol`), `admissionregistration.k8s.io/v1` VAP + binding and MAP + binding (enforced by the apiserver itself; the CLI parses MAP only at v1alpha1) |

## Scenarios: server verdict vs offline verdict

Server = kubectl stdout+stderr verbatim (`testdata/golden/*.server.txt`).
Offline = the adapter's findings reduced to `{policy, rule, message}` for
every Blocking finding (`OfflineVerdict`). Normalisation on the server side
strips only: `Error from server: error when creating "<file>": admission
webhook "validate.kyverno.svc-fail" denied the request: \n\nresource
<Kind>/<ns>/<name> was blocked due to the following policies \n\n` plus the
`<policy>:` / `  <rule>: ` line structure; `admission webhook
"vpol.validate.kyverno.svc-fail" denied the request: Policy <name> failed: `;
`The deployments "<name>" is invalid: : ValidatingAdmissionPolicy '<p>' with
binding '<b>' denied request: `. The message text is compared byte for byte.
Every row: `go test -run TestScenariosVsServer -v`.

| Scenario | Policies | Server | Offline (two-pass adapter, generated side files) | Identical | CLI exit | fathom exit |
|---|---|---|---|---|---|---|
| violating-team | (a) | denied `require-team-label` / `team-label-allowed`: `label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)` | Blocking, same policy/rule/message (ConfigMap via generated `--values-file`) | **yes** | 1 | 1 |
| passing-mutated | (b), (a) | `deployment.apps/web-passing created`; stored object has label `fathom.dev/mutated-by=add-default-securitycontext` and `spec.template.spec.securityContext.runAsNonRoot: true` | pass 1 wrote `web-passing-mutated.yaml`; leaf delta `{/metadata/labels/fathom.dev~1mutated-by, /spec/template/spec/securityContext/runAsNonRoot}` equals the server delta minus apiserver-owned fields; every CLI leaf present in the stored object; pass 2 `pass` | **yes** | 0 | 0 |
| excused | (a), (e) | `deployment.apps/legacy-app created (server dry run)` | `skip` (`rule is skipped due to policy exceptionss3/legacy-app-team-label`, sic) via `--exception`; clean | **yes** | 0 | 0 |
| protected-admin | (c) | denied `restrict-protected-deployer` / `argocd-only`: `... requester kubernetes-admin has groups ["kubeadm:cluster-admins","system:authenticated"]` | Blocking, same message, identity from `snapshot/identity-admin.yaml` (SelfSubjectReview) via generated `--userinfo` | **yes** | 1 | 1 |
| protected-argocd | (c) | `deployment.apps/web-protected created (server dry run)` (impersonated argocd SA) | `skip` (precondition false) with `--userinfo` from `identity-argocd.yaml` + `clusterRoles: [edit]` from `rbac-argocd.yaml`; clean | **yes** | 0 | 0 |
| vap-violating | (d) | apiserver VAP `s3-replica-limit` / binding `s3-replica-limit-binding`: `replicas 5 exceeds maxReplicas 3 from ConfigMap replica-limits` | Blocking, same message, binding from `properties.binding`; `--parameter-resource` generated from `cm-replica-limits.yaml`, `namespaceSelector[]` from `namespace.yaml` | **yes** | 1 | 1 |
| vpol-excused | (f) | denied `Policy vpol-require-team-label failed: label team=MISSING is not ... (ConfigMap s3/allowed-teams)` (the v2 PolicyException does not cover a ValidatingPolicy) | Blocking, same message, ConfigMap via generated `--context-file` | **yes** | 1 | 1 |
| vpol-violating-team | (a), (f) | **nondeterministic**: the two Kyverno validating webhooks are called in parallel and kubectl prints whichever denies first (`golden/vpol-violating-team.server.txt` = vpol text, `.alt.server.txt` = ClusterPolicy text). A review rerun of ten dry runs produced only the vpol text; the alternation was reproduced the same day with the `validationActions`-less probe below (ClusterPolicy text in 1 of 3 runs) | two Blocking findings = the union of both goldens | **yes** (union) | 1 | 1 |

Full chain (`TestFullChain`: (a)–(f), all five objects, admin identity):
pass 1 mutated all five objects; pass 2 summary `pass 10, fail 5, skip 1,
error 0`; the five Blocking findings are exactly the five server denials
(the two ClusterPolicy ones, the VAP one, both ValidatingPolicy ones).

### Spike-owned oracle probes (`engine/testdata/probes.sh`)

Recorded after the review with the same oracle and the state `commands.sh`
leaves behind; everything the script creates it deletes.

| Probe | Server | Offline | Identical |
|---|---|---|---|
| (f) without `spec.validationActions` (`f-vpol-no-validation-actions.yaml`, applied as `vpol-default-action-probe`), `violating-team` | denied: `Policy vpol-default-action-probe failed: label team=pirates is not ...` (`vpol-no-validation-actions.server.txt`; the ClusterPolicy (a) text in `.alt.server.txt` from the webhook race) | adapter with (a) + the probe: two Blocking findings = the union of both goldens; enforcement derived as Deny for the absent field | **yes** (union) |
| MAP (g) `s3-max-replicas-label` + binding with `paramRef` ConfigMap `s3/replica-limits` (`g-map-max-replicas-label.yaml`, applied at v1), `web-map` (`map-object.yaml`), `--dry-run=server -o yaml` | object with labels `fathom.dev/max-replicas: "3"` (MAP, from the ConfigMap) and `fathom.dev/mutated-by` + `runAsNonRoot: true` (Kyverno (b), still on the cluster) (`map-paramref.dryrun.yaml`) | pass 1 with (b) + (g) at v1alpha1, `--parameter-resource` and `--values-file namespaceSelector[]` generated from the snapshot; `-o` file holds two cumulative documents; leaf delta `{/metadata/labels/fathom.dev~1max-replicas: "3", /metadata/labels/fathom.dev~1mutated-by, /spec/template/spec/securityContext/runAsNonRoot}` equals the server delta minus apiserver-owned fields | **yes** |

## CLI exit code vs fathom exit code

Every row is pinned by the test named in the last column. "n/a" = the CLI
never ran or its exit is not a verdict. `TestCLIContractViolations` uses
fake binaries (shell scripts pinned by their own sha256) for the rows the
real CLI cannot be made to produce.

| Case | Report result | CLI exit | fathom exit | Test |
|---|---|---|---|---|
| pass | pass | 0 | 0 | ExitCodeMapping, ScenariosVsServer |
| no validate rule matched any admitted object (`engine/testdata/other-namespace.yaml`) | **no report at all**: stdout holds only the deprecation warnings, no summary line | 0 | 0, `Result.NoMatch = true`, no findings | NoMatchIsClean, ExitCodeMapping |
| rule skipped by PolicyException | skip | 0 | 0 | ExitCodeMapping, ScenariosVsServer |
| rule skipped by false precondition (argocd identity) | skip | 0 | 0 | ScenariosVsServer |
| Enforce ClusterPolicy violated | fail | 1 | 1 | ExitCodeMapping, ScenariosVsServer |
| Audit ClusterPolicy violated (`engine/testdata/a-require-team-label-audit.yaml`) | fail | 1 | 0 (Warning finding) | ExitCodeMapping |
| VAP binding `validationActions: [Deny]` violated | fail | 1 | 1 | ExitCodeMapping, ScenariosVsServer |
| VAP binding `validationActions: [Warn]` violated (`engine/testdata/d-vap-replica-limit-warn.yaml`) | fail (the CLI ignores the binding action) | 1 | 0 (Warning finding) | ExitCodeMapping |
| ValidatingPolicy `validationActions: [Deny]` violated | fail | 1 | 1 | ScenariosVsServer |
| ValidatingPolicy without `validationActions` violated (the oracle denies) | fail | 1 | 1 | ValidatingPolicyWithoutValidationActions |
| ClusterPolicy ConfigMap context missing from the snapshot | error (`JMESPath query failed: Invalid type for: <nil>`) | 1 | 3 | ExitCodeMapping |
| VAP parameter missing from the snapshot | error (`composited variable "maxReplicas" fails to evaluate: no such key: data`, not `parameterNotFoundAction: Deny`) | 1 | 3 | ExitCodeMapping |
| MAP parameter missing from the snapshot | the CLI would print `pass: 0 ... skip: 0`, write no file and exit 0 (silent); refused before the CLI runs | n/a | 3 | MAPParamRef |
| MAP at `admissionregistration.k8s.io/v1` or v1beta1 | the CLI would drop it (non-fatal `kind MutatingAdmissionPolicy not found in ... groupversion`, "Applying 0 policy rule(s)", exit 0); refused before the CLI runs | n/a | 3 | MAPParamRef |
| binary sha256 differs from the pin | n/a | n/a | 3 | ExitCodeMapping |
| binary missing | n/a | n/a | 3 | ExitCodeMapping |
| timeout | n/a | killed | 3 | ExitCodeMapping |
| CLI exit not in {0, 1} | any | 2 | 3 | CLIContractViolations |
| CLI exit 0/1 inconsistent with the report summary (validate) or the summary line (mutate) | any | 0 or 1 | 3 | CLIContractViolations |
| no JSON report on stdout other than the no-match output (exit 1, or extra stdout, or anything on stderr), or an unparsable one (not JSON, wrong kind, no summary) | any | any | 3 | CLIContractViolations |
| mutate pass without a summary line, or with `error > 0` | any | any | 3 | CLIContractViolations |
| unsupported policy document kind | n/a | n/a | 3 | ExitCodeMapping |
| two admitted objects share `metadata.name` and a mutate pass runs (`-o` names files by name only); refused before the CLI runs | n/a | n/a | 3 | DuplicateNamesRefused |

Kyverno's own rule (v1.19.1): exit 1 on any `fail` or any `error`, exit 0
otherwise, `--warn-exit-code N` for warnings. `--audit-warn` is **not**
used: it rewrites Audit ClusterPolicy failures to `warn` as documented, but
in 1.19.1 it also rewrites VAP Deny and ValidatingPolicy Deny failures to
`warn` in the report while still exiting 1 (observed: full chain summary
`warn: 3`, exit 1). Enforcement therefore comes from the policy files.

## Which flag carries the ConfigMap context

| Policy kind | Flag that works | Flags that do not |
|---|---|---|
| kyverno.io/v1 ClusterPolicy `context[].configMap` (JMESPath) | `--values-file`: `policies[].rules[].values.<contextName>` = `{data: <cm.data>, metadata: {...}}` | `--resource <configmap>` (CLI logs `disabled loading of ConfigMap context entry` at `-v 4`; context nil, result `error`); `--context-file` (feeds CEL policies only) |
| policies.kyverno.io/v1 ValidatingPolicy `resource.get('v1','configmaps',ns,name)` (CEL) | `--context-file`: cli.kyverno.io/v1alpha1 Context `spec.resources[]` | `--resource <configmap>`, `--values-file` (both: result `error`) |
| admissionregistration VAP binding `paramRef` | `--parameter-resource <configmap file>` **plus** `--values-file` `namespaceSelector[]` with the namespace's labels; without the selector entry the binding matches nothing (pass 0 / fail 0 / skip 0, exit 0, i.e. a silent non-evaluation) | passing the Namespace through `--resource` makes the CLI evaluate the VAP against the Namespace too (`no such key: spec`): `matchConstraints.resourceRules` are not honoured offline |
| `request.userInfo` | `--userinfo`: cli.kyverno.io/v1alpha1 UserInfo `{roles[], clusterRoles[], userInfo{username, groups[]}}` | without it the rule still runs with `requester UNKNOWN has groups []` |
| kyverno.io/v2 PolicyException | `--exception <file>` (matched rule -> `skip`) | in-cluster the chart's `--enablePolicyException=false` default would ignore the same exception |
| admissionregistration MAP binding `paramRef` | `--parameter-resource <configmap file>` **plus** `--values-file` `namespaceSelector[]`, and only with the policy written at `admissionregistration.k8s.io/v1alpha1` | v1 / v1beta1 documents (what the 1.37 server serves) are dropped with a non-fatal parse error; without the parameter, or without the selector entry, the CLI applies nothing and exits 0 with `pass: 0`; `parameterNotFoundAction: Deny` is ignored |

The adapter generates all of them from the same snapshot objects and passes
them on both passes (`values.yaml`, `context.yaml`, `userinfo.yaml`,
`param-configmap-<ns>-<name>.yaml`); `TestGeneratedSideFilesMatchOracle`
checks them against the oracle's hand-written, CLI-verified files.

## Cold start

`TestColdStart`, five runs, policy (a) on `passing-mutated.yaml` with the
ConfigMap from the snapshot, validate pass only; wall clock around the
`exec.Cmd` (process start to exit) and around the whole `Run` call:

| Measurement | Runs (ms) | min | median |
|---|---|---|---|
| `kyverno apply` pass 2 only | 226, 227, 226, 225, 237 | **224.9 ms** | **226.5 ms** |
| adapter `Run` (sha256 of the 265 MB binary + side files + pass 2) | 397, 396, 396, 393, 409 | 392.8 ms | 395.9 ms |
| oracle's own `date +%s%N` timing of the same command (manifest `coldStartMs`) | 226, 224, 235, 231, 226 | 224 ms | 226 ms |
| `kyverno apply` pass 2 only, rerun with the revised adapter | 235, 214, 219, 233, 223 | 214.4 ms | 223.3 ms |
| adapter `Run`, same rerun | 407, 389, 391, 406, 396 | 388.9 ms | 396.2 ms |

Full chain (6 policies, 5 objects): pass 1 487 ms, pass 2 423 ms, about 0.9 s
plus the digest check. The sha256 of the binary costs about 170 ms per run
(the difference between the two rows): verify once per process (or per
`fathom.lock` entry keyed on path, size and mtime), not per invocation.

## Other observations that matter for the product

- **Two invocations are required, not optional.** One `kyverno apply` with
  mutate and validate policies does validate the mutated object
  (oracle probe xi), but the product needs the patched objects for the S
  stage, PSA, quota and the "what the cluster will store" diff between the
  passes; `-o <dir>` gives them as `<name>-mutated.yaml` (4-space indent,
  trailing `---`, reparsed fine as `--resource`). A ClusterPolicy mutate
  pass writes that file for **every** object it was given, matched or not
  (the unmatched one comes back reformatted with identical leaves), a
  MAP-only pass only for matched objects, and when several mutating
  policies match the file holds one cumulative document per policy, in
  application order: take the last one, and decide "changed" by comparing
  leaves, never by the file's presence. The output file name is
  `metadata.name` only: kind and namespace are lost, so the adapter refuses
  two admitted objects with the same name before a mutate pass runs. The
  product should write one object per input file and map by index instead.
- **Nothing matched is silent.** Under `--policy-report --output-format
  json` a run in which no validate rule matches any object prints no report
  and no summary line, only the deprecation warnings, and exits 0. From the
  CLI output alone this is indistinguishable from a side-file mistake that
  makes every selector miss (a `namespaceSelector` without the namespace's
  labels in `values.yaml` behaves exactly so, see the VAP row above). The
  adapter flags it (`NoMatch`); the product must emit a finding for it
  (which policies were given, which objects, and that none matched) rather
  than a bare "clean".
- **MAP through the CLI is version-bound and silent on error.** CLI 1.19.1
  reads MutatingAdmissionPolicy only at v1alpha1 while the 1.37 server
  serves v1; a snapshot's MAP objects would have to be rewritten. A missing
  parameter yields no error result, no file and exit 0. With a working
  parameter the mutation is exactly the server's. This confirms design 3.3:
  MAP (like VAP) belongs to the in-process path from S2; the CLI evaluates
  only Kyverno's own kinds.
- **Report every denial.** Two Kyverno validating webhooks deny the same
  object in parallel and the apiserver returns whichever fails first; the
  cluster's verdict text is nondeterministic while the offline report is
  complete. fathom's copy must not promise "the same message as the server",
  it reports the set.
- **Enforcement is not in the report.** The report has `scored` but not the
  binding action or `failureAction`; severity (Blocking vs Warning) must be
  read from the policy objects in the snapshot's `engines/` layer.
- **PolicyExceptions depend on server flags.** The chart ships
  `--enablePolicyException=false`; the CLI honours `--exception` regardless.
  Offline parity needs the admission controller's flags
  (`enablePolicyException`, `exceptionNamespace`) in the snapshot so that
  fathom passes `--exception` only when the cluster would honour them.
- **Evaluation errors are not verdicts.** A missing ConfigMap context or VAP
  parameter yields result `error` and CLI exit 1, the same exit as a
  violation. The adapter maps `error` to exit 3; the product maps it to a
  Warning finding with an `unobserved` fidelity marker for the missing
  object (design 3.4) rather than a Blocking one, and never to "clean".
- **VAP belongs to the in-process validator.** The CLI evaluates VAPs but
  does not honour `matchConstraints.resourceRules`, `parameterNotFoundAction`
  or the binding's `validationActions`; S2's `validating.NewValidator` path
  does. Routing: VAP/VAPB to the in-process V stage, Kyverno kinds only to
  the CLI (design 3.3 "per-engine routing", C2-M2 dedupe).
- **Deprecation is a stdout warning, not an error.** Every run prints
  `Warning: <file>: kyverno.io/v1 ClusterPolicy is deprecated and will be
  removed in a future release` (and the same for kyverno.io/v2
  PolicyException) on **stdout** before the JSON report; the adapter skips
  to the first `{` line. The product surfaces these lines as Info findings.

## Not measured

- `--crd-paths` (custom-resource-typed objects under Kyverno policies).
- Kyverno `generate` rules, `mutate existing` (`--target-resource`), image
  verification (`--registry`) and MutatingPolicy (policies.kyverno.io):
  routed but not exercised.
- MAP beyond one `ApplyConfiguration` mutation with a ConfigMap paramRef:
  JSONPatch mutations, `reinvocationPolicy: IfNeeded` and the interaction
  with Kyverno's own mutate ordering were not compared with the server.
- A Kyverno 1.20 CLI against these fixtures (ClusterPolicy removal): not
  available; the pinning consequence is reasoned below, not observed.

## What it changes in the design

- **3.3 (V stage, Kyverno second pass; engines routing).** Confirmed as
  written: Kyverno mutate is the first CLI pass with `-o` output taken;
  Kyverno validate is the second pass on the mutated objects with
  `--values-file`, `--context-file`, `--userinfo`, `--parameter-resource`
  and `--exception`, all generated from the snapshot. Add to the text:
  (1) `--values-file` is a required fourth side file, it is the only carrier
  of ClusterPolicy ConfigMap contexts and of namespace labels for
  `namespaceSelector` matching; (2) ValidatingAdmissionPolicy and
  MutatingAdmissionPolicy are routed to the in-process path, never to the
  CLI, because the CLI ignores `resourceRules`, `parameterNotFoundAction`
  and the binding's `validationActions`, and reads MAP only at v1alpha1;
  (3) severity comes from the policy objects, not from the report; (4) the
  stage reports every denial (the server's message is one of several under
  a webhook race); (5) `error` results are Warning findings with
  `unobserved` fidelity, never clean; (6) a validate pass that matched
  nothing is an Info finding naming the policies and objects, never a bare
  clean, because the CLI's no-match output is identical to a side-file
  mistake.
- **3.5 (snapshot `engines/` layer; CLI pinning across the 1.20 ClusterPolicy
  removal).** The `engines/` layer needs, besides policies and exceptions,
  the Kyverno admission controller's feature flags (`enablePolicyException`,
  `exceptionNamespace`) and the Kyverno version (`engines.kyverno.version`
  is already in the envelope). ConfigMap `data` in the `full` slice is
  confirmed necessary (3.5 already says so). `fathom.lock` pins one CLI
  (sha256, as this spike's `VerifyBinary`) **per snapshot engine version**:
  the 1.19 CLI evaluates both `kyverno.io/v1` and `policies.kyverno.io/v1`;
  a 1.20 CLI will refuse the ClusterPolicy kinds the same cluster may still
  serve, so the lockfile entry follows the cluster's Kyverno minor and the
  deprecation warnings on stdout become Info findings that name the removal.
  The digest check should be done once per process, not per invocation
  (170 ms).
- **3.6 (cadence; Kyverno stage save-time if over budget, together with
  S7).** Median cold start 226.5 ms for one policy and one object, 0.9 s for
  the six-policy/five-object chain, so the Kyverno stage fits the
  500 ms–1 s debounce at this size; the save-time decision stays with S7's
  50-policy measurement. If S7 exceeds the budget, the two passes can be
  split: mutate on the debounce (it feeds the diff the deployer sees),
  validate at save.
- **10.1.** S3 row -> PASS; `--crd-paths` moves to the S7/S8 fixtures.
