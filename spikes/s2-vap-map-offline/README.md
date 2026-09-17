# S2: ValidatingAdmissionPolicy / MutatingAdmissionPolicy offline

Own Go module `github.com/MrWong99/fathom/spikes/s2-vap-map-offline`
(k8s.io/* at v0.37.0, no `replace`, never `k8s.io/kubernetes`, `CGO_ENABLED=0`).

## Goal

Show that fathom can run a cluster's VAP and MAP admission chain against an
imported snapshot with no apiserver and get the apiserver's verdicts: the same
allow/deny, the same message text, the same warnings, the same mutated object.
Find out which parts of `k8s.io/apiserver`'s admission plugins are reachable
through exported symbols and how much has to be copied (design section 3.1 lists
`internal/admit/port/mapcompile` as a planned port; section 8 decision C2-m2 says
"`compilation.go` copied into `port/mapcompile`").

## Pass criterion (spikes/README.md, design section 10.1)

Identical verdicts vs `--dry-run=server` on kind; per-object latency recorded

## Method

1. **Oracle** (`testdata/`, written by the oracle agent, read-only here): a kind
   `kindest/node:v1.37.0` cluster (`kind-fathom-oracle`) with namespace `s2`
   (labels `environment=prod`, `team=shop`), a ConfigMap param, one VAP with
   three bindings (Deny / Warn+Audit / missing param), four MAPs (ApplyConfiguration,
   JSONPatch, and a reader/setter pair that needs reinvocation). `commands.sh`
   captures `kubectl apply|create --dry-run=server` output verbatim into
   `golden/`, the `-o yaml` result of mutations, the served `/openapi/v3`
   documents and the policies, bindings, namespace, param and `auth whoami` as
   the server returns them (`snapshot/`).
2. **Offline evaluator** (`offline/`): compiles every policy in the snapshot the
   way the apiserver plugins do (`cel.NewCompositedCompiler(environment.MustBaseEnvSet(1.36))`,
   `StoredExpressions`, `validating.NewValidator`, the staging MAP compile path
   with `patch.NewJSONPatcher` / `patch.NewApplyConfigurationPatcher`), then
   dispatches a dry-run CREATE with the request identity from `whoami.yaml`
   through MAP (two-pass reinvocation loop) and VAP, with:
   - the snapshot namespace behind a `client-go` `NamespaceLister` and a fake
     clientset (`matching.NewMatcher`),
   - the snapshot ConfigMap behind a typed shared informer on the fake clientset,
     so the exported `generic.CollectParams` runs unchanged,
   - an authorizer that answers as kind's RBAC does for `kubernetes-admin`
     (group `kubeadm:cluster-admins` is bound to `cluster-admin`) and records
     every question,
   - a `patch.TypeConverterManager` over `openapiclient.NewLocalSchemaFiles(testdata/openapi)`
     from kubectl-validate (synchronous, no 5 s poll).
3. **Port** (`port/`): the unexported apiserver symbols the chain needs, copied
   with their Apache-2.0 header and listed in `port/NOTICE` with upstream line
   ranges: both `compilePolicy` functions and their converters, the VAP
   `dispatcher.Dispatch` verdict loop with `reasonToCode`, `wrappedParam` and the
   audit-annotation helpers, the MAP `dispatchInvocations` / `dispatchOne` /
   `keyFor` / `policyReinvokeContext`, and `admission.reinvoker.Admit`.
4. **Comparison** (`offline/evaluator_test.go`): the offline `*StatusError` and
   warnings are rendered through a re-implementation of kubectl's `checkErr`
   formatting (kubectl itself is not run; the oracle captured only its printed
   text) and compared byte for byte with `golden/<scenario>.server.txt`
   and `.exit`; warnings are additionally compared as sets; mutated objects are
   compared after canonicalisation (drop `metadata.managedFields`,
   `resourceVersion`, `uid`, `creationTimestamp`, `generation`, `status`; sorted
   keys). Native apps/v1 and core/v1 defaulting is not part of S2 (it lives in
   `k8s.io/kubernetes`); the request object gets the observed defaults written
   down in `ApplyNativeDefaults` before admission, as the apiserver defaults the
   request body before its admission chain runs.
5. **Latency** (`offline/latency_test.go`): 200 iterations, compiled policies
   warm, median and p95 for one VAP object (`vap-deny-pass`) and one MAP object
   (`map-reinvoke`); the test logs the CPU model, governor, `GOMAXPROCS`, Go
   version and `GOGC` of the run.

```sh
cd spikes/s2-vap-map-offline
CGO_ENABLED=0 go build ./...
go test -race -count=1 -v ./...
go test -count=1 -v -run TestLatency ./offline/   # numbers without the race detector
```

Result: `RESULT.md`.
