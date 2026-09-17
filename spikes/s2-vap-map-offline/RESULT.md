# S2 result: VAP/MAP offline

**PASS**, 2026-09-18.

Criterion (unchanged): *Identical verdicts vs `--dry-run=server` on kind; per-object
latency recorded.* All nine oracle scenarios give the server's verdict. What is
compared, exactly: the offline `*StatusError` and warnings, rendered by a
re-implementation of kubectl's `checkErr` (`RenderKubectl`, kubectl itself is not run
offline), match the captured kubectl transcript byte for byte, which covers
`Details.Kind`/`Details.Name`, the cause messages, the reason class (Invalid vs any
other), the warning headers in order and the exit code. The four mutated objects
equal the server's after canonicalisation, on a request object that was pre-defaulted
by hand (`ApplyNativeDefaults`); the identical thing is therefore the MAP delta, not an
end-to-end native-defaulting match. Fields kubectl does not print (`Status.Message`,
`Status.Code`, `Details.Group`, the causes' `Type`, `Field` beyond its printed form)
were never captured from the server and are not compared; a raw-response capture
(`kubectl --v=8` or the REST body) would close that gap. Latency is recorded below.

## Versions

| component | version |
|---|---|
| Go | go1.27.1 linux/amd64 (`CGO_ENABLED=0 go build ./...` passes) |
| k8s.io/api, apimachinery, apiserver, client-go, component-base | v0.37.0 (no `replace`, no `k8s.io/kubernetes`) |
| github.com/google/cel-go (transitive, pinned by apiserver) | v0.29.2 (cel.dev/expr v0.25.1); never imported directly |
| sigs.k8s.io/kubectl-validate | v0.0.5-0.20260105161640-a97ccfaca20b (`pkg/openapiclient.NewLocalSchemaFiles`) |
| k8s.io/kube-openapi (MVS pick) | v0.0.0-20260721132016-d427ff9ee9ad |
| sigs.k8s.io/structured-merge-diff/v6, evanphx json-patch.v4 | v6.4.2, v4.13.0 |
| kind | v0.33.0 |
| node image | `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5` |
| kubectl client | v1.36.4 (server v1.37.0, context `kind-fathom-oracle`) |
| MAP API | `admissionregistration.k8s.io/v1` (v1 only, no feature gate needed on 1.37) |

## Scenarios: server vs offline

`go test -race -count=1 ./...` passes; the table is what `offline/evaluator_test.go`
asserts per scenario. "kubectl output" is the golden `<scenario>.server.txt`
(stdout+stderr as captured) compared byte for byte with the offline verdict
rendered by `RenderKubectl`; nothing is stripped on either side, but the oracle
captured only kubectl's printed text, so the comparison is bounded by what kubectl
prints (details below).

| scenario | kind | server | offline | kubectl output | exit | warnings (set) | mutated object |
|---|---|---|---|---|---|---|---|
| vap-deny-violation | validate | denied, Invalid: `replicas 5 exceeds maxReplicas 3 from ConfigMap vap-limits` | denied, same | identical | 1 = 1 | {} = {} | n/a |
| vap-deny-pass | validate | allowed | allowed | identical | 0 = 0 | {} = {} | n/a |
| vap-deny-excluded | validate | allowed (matchCondition false) | allowed | identical | 0 = 0 | {} = {} | n/a |
| vap-warn | validate | allowed + 2 warnings | allowed + 2 warnings | identical (incl. order) | 0 = 0 | equal | n/a |
| vap-param-missing | validate | denied: `failed to configure binding: no params found for policy binding with `Deny` parameterNotFoundAction` | denied, same | identical | 1 = 1 | {} = {} | n/a |
| map-apply | mutate | allowed | allowed | identical | 0 = 0 | {} = {} | equal |
| map-jsonpatch (apply) | mutate | allowed | allowed | identical | 0 = 0 | {} = {} | equal (add-key branch) |
| map-jsonpatch-create | mutate | allowed | allowed | identical | 0 = 0 | {} = {} | equal (add-map branch) |
| map-reinvoke | mutate | allowed | allowed | identical | 0 = 0 | {} = {} | equal (`tier-seen: backend` after reinvocation) |

Audit annotations are also captured (`Result.AuditAnnotations`); vap-warn records
`validation.policy.admission.k8s.io/validation_failure` with both failures, as the
apiserver's Audit action does. The golden files cannot show that, so it is not
asserted.

**What "identical" means, exactly.** The offline chain returns the same
`*apierrors.StatusError` the apiserver plugin returns (reason, code, `Details.Kind/Name`,
`Details.Causes`). kubectl's rendering is re-implemented from
`k8s.io/kubectl/pkg/cmd/util/helpers.go` (`checkErr`, `StatusCausesToAggrError`,
`MultilineError`) and `AddSourceToErr`:

- each warning header prints as `Warning: <text>` before the result;
- admitted: `deployment.apps/<name> created (server dry run)`;
- reason Invalid: `The <details.kind> "<details.name>" is invalid: ` followed by the
  causes as `<field>: <message>` (one cause on the same line, which is where the
  `: :` in the goldens comes from; several causes as `* ` bullets). The
  `error when creating "<file>": ` wrapper only touches `Status.Message`, which this
  path never prints, so nothing has to be stripped;
- any other reason: `Error from server (<reason>): error when creating "<file>": <message>`
  (implemented, not exercised: `vap-deny-violation`'s first denied decision has reason
  Invalid, so the Forbidden decision of the second validation never surfaces, as on the
  server);
- exit code 1 on error.

Only the admitted branch and the one-cause Invalid branch are reached by the goldens.
The other branches are transcribed from kubectl v1.36 `helpers.go` (`k8s.io/kubectl` is
not a dependency of this module and was not in the module cache) and are pinned by
`offline/kubectl_test.go` for self-consistency only. Two divergences from kubectl in
those unexercised branches were found on review and fixed: with `Details == nil`
kubectl appends `: <Status.Message>` to "The request is invalid", and
`statusCausesToAggrError` drops duplicate `<field>: <message>` strings; the first
version of `RenderKubectl` did neither. Neither affects the nine goldens.

Warnings are compared as sets in addition to the byte comparison. Mutated objects are
compared as JSON maps after dropping `metadata.managedFields`, `resourceVersion`,
`uid`, `creationTimestamp`, `generation` and top-level `status` (map equality is key
order independent; diffs print with sorted keys; the goldens carry only
`creationTimestamp`, `generation`, `uid` and `status` of these). The request object is
pre-defaulted by `ApplyNativeDefaults` before admission (see below), so this equality
shows the MAP delta is identical, not that offline defaulting is.

**Request identity.** `admission.NewAttributesRecord(obj, nil, apps/v1 Deployment, "s2",
name, apps/v1 deployments, "", Create, &CreateOptions{}, dryRun=true, user)` with the
user from `snapshot/whoami.yaml` (`kubernetes-admin`, groups `kubeadm:cluster-admins`,
`system:authenticated`, credential-id extra). The request body is what kubectl sent:
`kubectl apply` (client-side, the oracle's default) adds
`kubectl.kubernetes.io/last-applied-configuration` (file content as JSON with
`metadata.annotations: {}`, trailing newline); `kubectl create` sends the file as is.
That difference is what selects the JSONPatch branch in map-jsonpatch vs
map-jsonpatch-create, and the offline evaluator reproduces both.

**Authorizer.** The policy asks exactly one question:
`kubernetes-admin [kubeadm:cluster-admins system:authenticated]: get /configmaps ns="s2" name="" -> Allow`
(`TestAuthorizerQuestion`). The snapshot has no RBAC objects; `SnapshotAuthorizer`
answers Allow for members of `kubeadm:cluster-admins` (kind's ClusterRoleBinding to
`cluster-admin`) or `system:masters`, NoOpinion otherwise, and records every check.
The real answer needs the `rbacrules` port over the snapshot's RBAC layer (design 3.1);
the `authorizer.*` verdict is only as good as that layer.

**Native defaulting is out of scope and handled explicitly.** The apiserver defaults
the request body at decode time, before admission, and `dispatchOne` re-runs the
defaulter after each patch. Those functions live in `k8s.io/kubernetes/pkg/apis/{apps,core}/v1`
(forbidden import) and the served OpenAPI v3 carries no `default` values for the
fields these fixtures need (checked in `testdata/openapi`: `progressDeadlineSeconds`,
`revisionHistoryLimit`, `strategy`, `dnsPolicy`, `restartPolicy`, `schedulerName`,
`terminationGracePeriodSeconds`, `imagePullPolicy`, `terminationMessagePath`,
`terminationMessagePolicy` have none). The documents do carry defaults elsewhere:
`default: {}` on struct fields (41 in `apis/apps/v1.json`, 77 in `api/v1.json`),
Go zero values on non-pointer scalars (`default: ""` 105 / 170, `default: 0` 18 / 20)
and a few real values (`ContainerPort.protocol` and `ServicePort.protocol` `TCP`,
`ReplicationControllerSpec.replicas` `1`, RBD/Flex volume fields), none of which
is what the apiserver's `SetDefaults_*` functions set on a Deployment, so
kubectl-validate cannot supply them either.
`ApplyNativeDefaults` writes down the observed 1.37 defaults set-if-absent
and is applied before admission; with it, the offline mutated objects equal the
server's. The post-patch defaulter call is a no-op offline (client-go scheme has no
defaulters), which matters only when a mutation creates a struct that has its own
defaults; none of the fixtures does. This is the S stage's job (design 3.3), not S2's.

## Latency

`Measure` wraps `Evaluator.Admit` (MAP two-pass loop + VAP dispatch, CEL programs and
type converter warm) in `time.Now` for 200 iterations, one warm-up call first.
Invocation: `go test -count=1 -v -run TestLatency ./offline/` (no `-race`, no
`-cpu`, no benchmark harness), which now logs the machine line below itself.
Machine (desktop): 13th Gen Intel Core i9-13900K, 24 cores / 32 logical CPUs,
cpufreq governor `powersave`, 800 to 5500 MHz (the `scaling MHz` value changes
between runs, which is why runs 1 to 5 spread and why five runs are given);
`GOMAXPROCS=32`, `GOGC` default (100), go1.27.1 linux/amd64, `CGO_ENABLED=0`,
CachyOS Linux 7.2.6. Runs 6 and 7 were taken while correcting this section, on the
same machine, with the machine line logged. VAP object `vap-deny-pass` evaluates
the match condition, two variables, three validations (params, `namespaceObject`,
`authorizer` bound) and the message expression; MAP object `map-reinvoke` runs two
ApplyConfiguration policies plus the reinvocation pass (three patch applications).

| object | run | median | p95 | min | max |
|---|---|---|---|---|---|
| vap-deny-pass | 1 | 34.7 µs | 91.6 µs | 31.3 µs | 300 µs |
| vap-deny-pass | 2 | 95.7 µs | 159 µs | 33.6 µs | 579 µs |
| vap-deny-pass | 3 | 72.0 µs | 189 µs | 34.0 µs | 327 µs |
| vap-deny-pass | 4 | 34.5 µs | 159 µs | 32.4 µs | 346 µs |
| vap-deny-pass | 5 | 62.3 µs | 140 µs | 32.8 µs | 570 µs |
| vap-deny-pass | 6 | 70.2 µs | 87.9 µs | 31.4 µs | 479 µs |
| vap-deny-pass | 7 | 33.4 µs | 82.3 µs | 31.0 µs | 894 µs |
| map-reinvoke | 1 | 189 µs | 404 µs | 174 µs | 1.24 ms |
| map-reinvoke | 2 | 304 µs | 599 µs | 175 µs | 1.54 ms |
| map-reinvoke | 3 | 226 µs | 817 µs | 175 µs | 1.68 ms |
| map-reinvoke | 4 | 207 µs | 599 µs | 174 µs | 1.30 ms |
| map-reinvoke | 5 | 191 µs | 428 µs | 175 µs | 1.20 ms |
| map-reinvoke | 6 | 194 µs | 541 µs | 176 µs | 1.07 ms |
| map-reinvoke | 7 | 187 µs | 344 µs | 174 µs | 1.22 ms |

Summary: VAP median 33 to 96 µs, p95 82 to 189 µs; MAP median 187 to 304 µs, p95 0.3
to 0.8 ms per object. Under `-race` the same loop reads VAP median 458 µs / p95 1.2 ms
and MAP median 2.1 ms / p95 3.5 ms. Against the S7 budget (whole chain under 2 s for
40 objects) VAP+MAP cost about 10 ms per 40 objects and is not the stage to worry about.
Compile cost (not in the loop): `New` compiles 1 VAP + 4 MAPs and syncs the informer in
about 60 ms cold, dominated by `MustBaseEnvSet` and the first `managedfields.NewTypeConverter`
over the 700 KB apps/v1 document.

## Copied code

`port/` holds 865 upstream lines (non-overlapping ranges and the counting rule in
`port/NOTICE`; 915 non-blank lines after the Apache headers including the mechanical
edits) from `k8s.io/apiserver@v0.37.0`. An earlier count of 881 double-counted
`policyDecisionWithMetadata` (5 lines inside the 42-69 range) and included the
exported `ValidationFailureValue` (dispatcher.go:358-367, used from upstream, not
copied). The unexported symbols that forced them:

| copied | upstream (pkg/admission/...) | lines | why it is needed |
|---|---|---|---|
| `validating.compilePolicy` + `convertv1Validations`, `convertv1MessageExpressions`, `convertv1AuditAnnotations`, `convertv1beta1Variables` | plugin/policy/validating/plugin.go:147-227 | 77 | the only path from a `v1.ValidatingAdmissionPolicy` to a `Validator`; it also fixes `HasAuthorizer:false` for message expressions and the `"policy"/"validate"` matcher labels |
| `validating.getCompositionEnvTemplateWithStrictCost` | plugin.go:52-57 | replaced | a package-level `sync.Once` over `DefaultCompatibilityVersion()`; replaced by a caller-supplied `*environment.EnvSet` |
| `validating.dispatcher.Dispatch`, `policyDecisionWithMetadata`, `publishValidationFailureAnnotations`, `auditAnnotationCollector`, `wrappedParam`, `reasonToCode` | validating/dispatcher.go:42-446 minus the exported `ValidationFailureValue` (358-367), policy_decision.go:73-87 | 403 | the verdict: binding `validationActions` → deny list / audit annotation / warning, first denied decision → `NewForbidden` with reason and code, failure-policy handling of config errors |
| `mutating.compilePolicy`, `convertV1Variables` | plugin/policy/mutating/compilation.go:31-82 (doc comment included), 84-96 | 65 | the MAP compile path (the file is in staging `k8s.io/apiserver`, so no copy from `k8s.io/kubernetes` is needed, but the function is unexported) |
| `mutating.NewDispatcher`/`dispatcher`, `dispatchInvocations`, `dispatchOne`, `keyFor` | mutating/dispatcher.go:46-305 | 252 | patch application order, `CreateContext` for `variables`, per-invocation reinvocation bookkeeping, `Dirty` write-back |
| `mutating.key`, `policyReinvokeContext` | mutating/reinvocationcontext.go:26-76 | 51 | reinvocation state kept under `ReinvocationContext.Value("MutatingAdmissionPolicy")` |
| `admission.reinvoker.Admit` | reinvocation.go:35-51 (doc comment 31-34 carried, not counted) | 17 | the two-pass loop the apiserver's chain wrapper runs |

Used as-is (exported): `cel.NewCompositedCompiler`, `CompileCondition`,
`CompileMutatingEvaluator`, `CompileAndStoreVariables`, `matchconditions.NewMatcher`,
`validating.NewValidator` and all accessor/decision types, `validating.PolicyHook`,
`mutating.PolicyEvaluator`/`PolicyHook`, `patch.NewJSONPatcher`,
`patch.NewApplyConfigurationPatcher`, `patch.TypeConverterManager` (interface),
`generic.NewPolicyDispatcher` (takes the copied delegate through assignability of the
unexported func type, so `versionedAttributeAccessor` and the failure-policy filter did
not have to be copied), `generic.NewPolicyMatcher`, `generic.CollectParams`
(unchanged, fed by a typed shared informer on `client-go/kubernetes/fake` seeded with the
snapshot ConfigMap; the 1 s `WaitForCacheSync` is satisfied immediately),
`matching.NewMatcher` (snapshot `NamespaceLister` + fake clientset as the NotFound
fallback), `admission.NewAttributesRecord`/`NewVersionedAttributes`/
`NewObjectInterfacesFromScheme(client-go scheme)`, `admissionauthorizer.NewCachingAuthorizer`,
`warning.WithWarningRecorder`.

Re-implemented (exported but informer- or poll-bound): `patch.TypeConverterManager`
as `SnapshotTypeConverterManager` (47 lines, MIT) over kubectl-validate's
`openapiclient.NewLocalSchemaFiles(os.DirFS("testdata/openapi"))`; `Paths()` is read
once, `managedfields.NewTypeConverter(components.schemas, false)` built per
group-version on first use. The layout the oracle saved (`api/v1.json`,
`apis/<group>/<version>.json`) is exactly what `NewLocalSchemaFiles` reads; no adapter
was needed. The apiserver's `NewTypeConverterManager` was not used because its cache
is filled only by the asynchronous `Run` poll.

Not used: `validating.TypeChecker` (status warnings only, never changes a verdict),
`admission.NewVersionedAttributes` conversion to internal types (the offline object
stays `*appsv1.Deployment`; `ConvertToGVK` short-circuits on equal GVK).

## Findings that matter for the design

1. **`mutating/compilation.go` lives in staging `k8s.io/apiserver`, not `k8s.io/kubernetes`.**
   Its imports are all staging packages. Design 3.1 and decision C2-m2 assumed the file
   had to be copied from kubernetes; it does not. What still has to be copied is the
   65-line unexported `compilePolicy`, and, larger, the two dispatch loops (VAP 403
   lines, MAP 320 lines) that turn evaluations into verdicts and apply patches with
   reinvocation. The port is real but it is a **dispatcher** port, not a compiler port.
2. **Compatibility version.** `environment.DefaultCompatibilityVersion()` returns 1.36
   in this non-kubernetes process too (it falls back to `DefaultBuildEffectiveVersion`,
   which derives from the linked component-base version), but the evaluator pins
   `version.MajorMinor(1, 36)` explicitly so the CEL environment does not depend on a
   process-global registry. Every policy expression is compiled with
   `StoredExpressions`, which ignores `IntroducedVersion` gating; parity therefore rests
   on using the same apiserver minor as the target cluster (snapshot must record it).
3. **TypeMeta after write-back.** The scheme's auto-registered self-conversion clears
   TypeMeta on the write-back target ("to match legacy reflective conversion"). On the
   apiserver the admission object is the internal type, so nothing is lost and the
   response encoder sets apiVersion/kind. Offline the object is external, so `Admit`
   restores the GVK after the mutation pass; the product should keep admission objects
   unstructured (the CRD path is unstructured on the server too) or do the same.
4. **Ordering.** Policies and bindings are sorted by name; the fixtures confirm MAPs run
   in that order (map-reinvoke). The server's informer-backed source also sorts.
5. **Everything runs with `matchPolicy: Equivalent` as served**, so the snapshot must
   carry the policies as the server stores them (defaults applied), not the authored
   YAML: `generic.NewPolicyMatcher` errors on a nil `namespaceSelector`/`objectSelector`,
   which the defaulted objects always have.

## Compile-path map (as handed to the spike; followed as written)

```
# S2 compile-path map: offline VAP/MAP evaluator (k8s.io/apiserver v0.37.0)

Base path `A=/home/luk/go/pkg/mod/k8s.io/apiserver@v0.37.0/pkg/admission/plugin`. All line refs are into that tree.

## 0. Environment (shared by VAP and MAP)
- Both compile with `environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion())` (validating/plugin.go:52-57 via unexported `getCompositionEnvTemplateWithStrictCost`; mutating/compilation.go:38). In v0.37.0 `MustBaseEnvSet(ver)` takes ONE arg (pkg/cel/environment/base.go:220); there is no strict flag any more: `StrictCostOpt` is baked into `baseOpts` (base.go:188-197).
- `DefaultCompatibilityVersion()` (base.go:49) = effective version's MinCompatibilityVersion = binary minor minus 1 (1.36 for a 1.37 build). Offline caller should pass `version.MajorMinor(1, 36)` explicitly rather than depend on the globals registry.
- Every policy expression is compiled with `environment.StoredExpressions` (validating/plugin.go:162-175; mutating/compilation.go:47,57,69,75). `NewExpressions` is only used by API validation (not in staging path). StoredExpressions env ignores IntroducedVersion gating, so parity mostly depends on using the same apiserver minor as the target cluster.
- `cel.NewCompositedCompiler(envSet)` (cel/composition.go:365, exported) extends env with `variables` map type and builds `cel.NewCompiler` (cel/compile.go:159). `createEnvForOpts` (compile.go:251, unexported, reached via NewCompiler) declares `object`/`oldObject`/`params` as Dyn, `namespaceObject` and `request` as hand-built DeclTypes (`BuildRequestType` :48, `BuildNamespaceType` :94), `authorizer` + `authorizer.requestResource` when HasAuthorizer, and `HasPatchTypes` adds `mutation.DynamicTypeResolver` + `library.JSONPatch` (compile.go:293-301). Nothing to copy: all reached through exported `CompositedCompiler` methods.
- Evaluation: `condition.ForInput` (cel/condition.go:999) and `mutatingEvaluator.ForInput` (cel/mutation.go:1180) call unexported `newActivation` (cel/activation.go:773); bindings: object/oldObject via `LazyObject.CELValue()`, params via `common.SchemalessTypedToVal`, authorizer via `library.NewAuthorizerVal`, `variables` via `CompositionContext.Variables`. `CompositedConditionEvaluator.ForInput` creates the composition ctx itself (composition.go:511); `CompositedEvaluator` (MAP) does NOT, so the MAP caller must wrap ctx with `PolicyEvaluator.CompositedCompiler.CreateContext(ctx)` (mutating/dispatcher.go:236-238) or `variables` resolve to nothing.
- Budgets: `celconfig.RuntimeCELCostBudget` (10M) per Validate/patch call, `RuntimeCELCostBudgetMatchConditions` (2.5M) for matchConditions (webhook/matchconditions/matcher.go:80-85), `PerCallLimit`/`CheckFrequency` at compile.

## 1. VAP: policy -> Validator
- `validating.compilePolicy(*v1.ValidatingAdmissionPolicy) Validator` — UNEXPORTED, validating/plugin.go:147-181. Steps: optionalVars `{HasParams: ParamKind!=nil, HasAuthorizer: true}`; message expressions use `{HasParams, HasAuthorizer:false}` (:153). `CompileAndStoreVariables(convertv1beta1Variables(spec.Variables), optionalVars, Stored)`; matchConditions -> `(*matchconditions.MatchCondition)` accessors -> `matchconditions.NewMatcher(CompileCondition(...), failurePolicy, "policy", "validate", name)` (:169); `NewValidator(CompileCondition(validations), matcher, CompileCondition(auditAnnotations), CompileCondition(messageExpressions), failurePolicy, nil)` (validator.go:280, exported).
- Accessor converters (unexported, trivial): `convertv1Validations` :183, `convertv1MessageExpressions` :196 (nil slot when MessageExpression==""), `convertv1AuditAnnotations` :209, `convertv1beta1Variables` :221. The accessor types themselves are exported (`ValidationCondition`, `MessageExpressionCondition` message.go:26, `AuditAnnotationCondition`, `Variable`, interface.go).
- `Validator.Validate(ctx, matchedResource GVR, versionedAttr, versionedParams, namespace, budget, authz)` (interface.go:589, exported; impl validator.go:312-495): compileError -> EvalError decision by failurePolicy; celMatcher.Match with params+authz; builds `cel.CreateAdmissionRequest(attr, GVR, VersionedKind)` (condition.go:1028) and `cel.CreateNamespaceObject` (:1095); validations with `{VersionedParams, Authorizer}`; messageExpressions with params only and remaining budget; audit annotations with params only and the RAW namespace (validator.go:442). Message fallback order: messageExpression (string, <=5KiB, no newline) -> Message -> "failed expression: ...".

## 2. VAP dispatch (what turns decisions into a verdict)
- `validating.NewDispatcher(authz, generic.PolicyMatcher)` exported (dispatcher.go:540); `dispatcher.Dispatch` UNEXPORTED method (dispatcher.go:563-811) holds the verdict logic: per hook `DefinitionMatches` -> per binding `BindingMatches` -> `generic.CollectParams` -> `admission.NewVersionedAttributes(a, matchKind, o)` -> `Validate` per param -> `binding.Spec.ValidationActions` (Deny -> denied list; Audit -> `validation.policy.admission.k8s.io/validation_failure` annotation; Warn -> `warning.AddWarning`), audit annotation Error -> deny; first denied decision -> `admission.NewForbidden` with Reason and `reasonToCode` (policy_decision.go:73, unexported). Config errors: Fail -> deny "failed to configure policy/binding", Ignore -> drop.
- Params: `generic.CollectParams` (policy_dispatcher.go:275, exported) requires `informers.GenericInformer` + cache sync + optional dynamic client; with nil informer and paramKind+paramRef set it errors "paramKind kind not known" -> config error. Offline: re-implement the 4-way switch (:325-404) over snapshot objects (nil paramKind or nil paramRef -> `[nil]`; name / selector / ParameterNotFoundAction Deny). `wrappedParam` (dispatcher.go:896) only matters for typed params lacking TypeMeta; unstructured snapshot params skip it.
- Matching: `matching.NewMatcher(namespaceLister, kubernetes.Interface)` (policy/matching/matching.go:53) with `generic.NewPolicyMatcher` (policy_matcher.go:569) is usable offline: back the lister with the snapshot and pass `client-go/kubernetes/fake` (namespace.Matcher falls back to `Client.CoreV1().Namespaces().Get` on NotFound, webhook/predicates/namespace/matcher.go:48-51; nil client would panic). `Matches` also resolves equivalent GVK/GVR via `o.GetObjectConvertor`/`admission.ObjectInterfaces` for `matchPolicy: Equivalent`.
- Namespace special case: object of kind v1 Namespace gets namespaceObject=nil (dispatcher.go:677, mutating/dispatcher.go:214).
- `TypeChecker` (typechecking.go:47, exported, needs `resolver.SchemaResolver` + RESTMapper) only produces status warnings; it never changes the verdict — optional for fathom.

## 3. MAP: policy -> PolicyEvaluator
- `mutating/compilation.go` IS in the staging module `k8s.io/apiserver` (full path given in task). Its imports are only `k8s.io/api/admissionregistration/v1`, `k8s.io/apiserver/pkg/{admission/plugin/cel, admission/plugin/policy/mutating/patch, admission/plugin/policy/validating, admission/plugin/webhook/matchconditions, cel, cel/environment}`; the module go.mod has no `k8s.io/kubernetes` requirement and no replace. Confirmed: it needs nothing from k8s.io/kubernetes.
- `mutating.compilePolicy(*v1.MutatingAdmissionPolicy) PolicyEvaluator` — UNEXPORTED, compilation.go:36-82: opts `{HasParams, HasAuthorizer:true}`; variables via `convertV1Variables` (:84, reuses `validating.Variable`); matcher identical to VAP (:57, note literal "validate"); `patchOptions.HasPatchTypes = true`; per mutation: JSONPatch -> `&patch.JSONPatchCondition{}` + `CompileMutatingEvaluator` + `patch.NewJSONPatcher`; ApplyConfiguration -> `&patch.ApplyConfigurationCondition{}` + `patch.NewApplyConfigurationPatcher`. Returns exported struct `PolicyEvaluator{Matcher, Mutators []patch.Patcher, CompositedCompiler, Error}` (plugin.go:481).
- Patchers (patch/, all exported): `Patcher.Patch(ctx, patch.Request{MatchedResource, VersionedAttributes, ObjectInterfaces, OptionalVariables, Namespace, TypeConverter}, budget)`. JSON patcher (json_patch.go:117) encodes the versioned object with a strict JSON serializer from `ObjectInterfaces` creater/typer, applies evanphx json-patch v4, decodes (Unstructured stays Unstructured); `ErrTestFailed` -> no-op. ApplyConfiguration (smd.go:304) requires `eval -> *dynamic.ObjectVal`, `CheckTypeNamesMatchFieldPathNames`, then `ApplyStructuredMergeDiff(typeConverter, live, patch)` (:357): `validatePatch` rejects atomic maps/lists/structs, `typed.Merge` with `typed.AllowDuplicates` on live.
- TypeConverter: `patch.TypeConverterManager` interface {GetTypeConverter(gvk), Run(ctx)} + `NewTypeConverterManager(static managedfields.TypeConverter, openapi.Client)` (typeconverter.go:491-507). `GetTypeConverter` tries the static converter, then `lastFetchedPaths` which is filled only by `Run` polling `openapiClient.Paths()` every 5s (:531-567) and builds `managedfields.NewTypeConverter(spec3.OpenAPI.Components.Schemas, false)` per GV (:612-626). Offline: implement the 2-method interface yourself over saved `/openapi/v3/apis/<gv>` documents (call `managedfields.NewTypeConverter` per GV, cache by GV); kubectl-validate `openapiclient.NewLocalSchemaFiles` satisfies `openapi.Client` if you prefer the manager, but `Run` is async so you must wait for the first poll. Nil converter -> ServiceUnavailable (dispatcher.go:341).

## 4. MAP dispatch and reinvocation
- `mutating.NewDispatcher(authz, *matching.Matcher, TypeConverterManager)` exported (dispatcher.go:142) wraps `generic.NewPolicyDispatcher` (policy_dispatcher.go:87, exported) with delegate `dispatchInvocations` (UNEXPORTED :169-323) and `dispatchOne` (:325-373). `generic.policyDispatcher.Dispatch` (:113-267) does hook/binding matching, `versionedAttributeAccessor` (:483, unexported, trivial map over `admission.NewVersionedAttributes`), `CollectParams`, then failure-policy filtering of `PolicyError`s into one Forbidden (:233-263).
- Per invocation order: ctx = `CompositedCompiler.CreateContext(ctx)`; `Matcher.Match(ctx, versionedAttr, param, cachingAuthz)`; key = `keyFor(invocation)` (:375, unexported; struct `key` reinvocationcontext.go:610); on a reinvoke pass skip unless `policyReinvokeCtx.ShouldReinvoke(key)`; mutations applied in spec order via `dispatchOne`: GetTypeConverter -> Patch -> (typed only) `ConvertToVersion` -> `o.GetObjectDefaulter().Default(newObj)` -> `versionedAttr.UpdateObject` (sets Dirty). After the invocation: `!Semantic.DeepEqual(before, after)` -> `RequireReinvokingPreviouslyInvokedPlugins()` + `reinvokeCtx.SetShouldReinvoke()`; `ReinvocationPolicy == IfNeeded` -> `AddReinvocablePolicyToPreviouslyInvoked(key)`. End: if Dirty, `o.GetObjectConvertor().Convert(versioned, a.GetObject())` writes back.
- Reinvocation state: `policyReinvokeContext` (reinvocationcontext.go:617-660, UNEXPORTED) stored under `a.GetReinvocationContext().Value("MutatingAdmissionPolicy")`; on entry of a reinvoke pass, if `IsOutputChangedSinceLastPolicyInvocation(a.GetObject())` all previously-invoked IfNeeded policies are re-armed (:187-191). The second pass is driven by `admission.reinvoker` (pkg/admission/reinvocation.go:23-50, unexported): Admit once; if `ShouldReinvoke()` then `SetIsReinvoke()` and Admit once more (max 2 passes). `admission.NewAttributesRecord` (attributes.go:57) already supplies a `ReinvocationContext`. Offline caller re-implements this 2-pass loop (about 10 lines) around its own chain.
- Feature gate `features.MutatingAdmissionPolicy` toggles the plugin (plugin.go:564); offline evaluator should take enablement from the snapshot (whether MAP objects exist / gate state), not from a gate registry.

## 5. What an offline caller uses vs copies
- Use as-is (exported): `cel.*` compilers/evaluators, `matchconditions.*`, `validating.NewValidator`/`Validator`/`PolicyDecision` types, `validating.TypeChecker`, `mutating.PolicyEvaluator`, `patch.*`, `matching.NewMatcher`, `generic.NewPolicyMatcher`, `generic.NewPolicyDispatcher`, `admission.NewAttributesRecord`/`NewVersionedAttributes`/`NewObjectInterfacesFromScheme`, `admissionauthorizer.NewCachingAuthorizer`, `manifest/source.NewStaticPolicySource` + `manifest.LoadFiles` (hooks without ParamInformer; only for param-less policies).
- Copy (unexported, listed in unexportedNeeded): the two `compilePolicy` functions and their converters; the VAP `dispatcher.Dispatch` verdict logic with `reasonToCode`, `auditAnnotationCollector`, `publishValidationFailureAnnotations`; the MAP `dispatchInvocations`/`dispatchOne`/`keyFor`/`key`/`policyReinvokeContext`; `generic.versionedAttributeAccessor`; `admission.reinvoker`. Re-implement (exported but informer-bound): `generic.CollectParams` over snapshot objects; `patch.TypeConverterManager` over saved OpenAPI v3.
- Authorizer: `authorizer.UnconditionalAuthorizer` must be non-nil when HasAuthorizer (always true for policies). Offline: a snapshot-RBAC authorizer (planned port under internal/admit/port) or a deterministic "NoOpinion" stub; either way the verdict of `authorizer.*` expressions is only as good as the RBAC snapshot.
```

Corrections found while following the map (the map's line numbers were from a
different listing; the v0.37.0 files are shorter): `validating.dispatcher.Dispatch` is
dispatcher.go:72-320 (446 lines total), `wrappedParam` :405-446, `reasonToCode`
policy_decision.go:73; `mutating.dispatchInvocations` :73-227, `dispatchOne` :229-277,
`keyFor` :279-305; `policyReinvokeContext` reinvocationcontext.go:33-76;
`NewTypeConverterManager` typeconverter.go:43, `Run` :77, `GetTypeConverter` :115.
`generic.CollectParams` did not need re-implementing (a fake-clientset informer
satisfies it), and `generic.versionedAttributeAccessor` did not need copying
(`generic.NewPolicyDispatcher` accepts the copied delegate).

## Design impact

- **3.1 port list.** `internal/admit/port/mapcompile` as named is no longer the right
  unit: the MAP compile file is in staging and only `compilePolicy` (65 lines) is
  unexported. The port that S2 shows is needed is `internal/admit/port/policy`
  (or `admitpolicy`): both `compilePolicy`s, the VAP `Dispatch` verdict loop, the MAP
  `dispatchInvocations`/`dispatchOne` loop with `policyReinvokeContext`, and the
  two-pass `reinvoker`, 865 upstream lines per Kubernetes minor, plus two
  MIT-licensed adapters (`SnapshotTypeConverterManager`, snapshot authorizer). Rename
  the entry and its NOTICES line; decision C2-m2's wording "`compilation.go` copied"
  becomes "`compilePolicy` and both dispatch loops copied". The `rbacrules` port is a
  hard dependency of VAP/MAP fidelity: every policy is compiled with
  `HasAuthorizer: true`, and `authorizer.*` verdicts come from it.
- **3.3 M stage.** "MutatingAdmissionPolicy (via the copied compile path, §10 S2)"
  should read "via the copied compile path and dispatch loop with the apiserver's
  two-pass reinvocation". Two conditions follow: (a) the S stage's defaulting must run
  before M as well as after it, because the apiserver defaults the request body before
  admission and MAP patches see a defaulted object (S2 had to apply the defaults
  by hand to match); (b) `dispatchOne`'s post-patch defaulter is a no-op without
  `k8s.io/kubernetes`, so a mutation that creates a struct with its own native
  defaults is `approximate` on native types until S runs again (exact on CRDs, whose
  defaults kubectl-validate applies). The M stage output object must keep its GVK
  (finding 3).
- **3.3 V stage.** ValidatingAdmissionPolicy is confirmed as written
  (`validating.NewValidator` + `cel.NewCompositedCompiler`, params, `namespaceObject`,
  RBAC-backed `authorizer`); add that Warn and Audit actions are reproduced (warnings
  become findings of severity Warning, audit annotations Info) and that the snapshot
  must store policies and bindings as served (defaults applied) and the server minor
  (compatibility version = minor − 1). `TypeChecker` stays out.
- **10.1 S2 row.** Replace "MAP via `compilation.go` copied into
  `internal/admit/port/mapcompile`" with "MAP via the staging compile path and the
  copied `compilePolicy` + dispatch loops (`internal/admit/port/policy`)". Status: PASS
  2026-09-18, 9/9 scenarios identical (kubectl-rendered verdict text, warnings, exit
  code; MAP delta on a pre-defaulted object), VAP about 35 to 100 µs and MAP about
  0.2 to 0.3 ms median per object.

## Open questions for the owner

1. Pin `MustBaseEnvSet` to the snapshot's server minor − 1 (proposed: the snapshot
   records the server version; fathom's linked apiserver minor must equal it or the
   finding is `approximate`), or to the linked apiserver's minor − 1 regardless?
2. `authorizer`: S2 used a deterministic group-based stub with a recorded question;
   the design's `rbacrules` port over the snapshot RBAC layer is the intended source.
   When the snapshot lacks RBAC, downgrade to `approximate` and record
   `unobserved: ["rbac"]`, or evaluate with NoOpinion (which turns
   `authorizer...check().allowed()` false and can deny)?
3. Copy the dispatch loops verbatim as done here (audit annotations and warnings kept,
   865 lines per minor) or re-implement a slimmer verdict loop that emits findings
   directly? S2's data: the verbatim copy is what made the messages and warnings
   byte-identical; a slimmer loop would have to re-derive the first-denied-decision,
   reason-to-code, and Warn/Audit behaviour by hand.
4. Should admission objects be unstructured throughout `internal/admit` (finding 3),
   which also removes the typed-conversion path in `dispatchOne`?
