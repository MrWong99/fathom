# Spike sprint (weeks 0 to 6, before architecture freeze)

Sixteen spikes with pass criteria, taken from docs/design/design.md section 10.1
(the design uses the working name zhi; read it as fathom). Nothing in `pkg/` or
`internal/` is built until S1, S2, S7 and S14 have a result. Two of them decide
whether the rewrite is justified at all: the **calibration** spikes (S2 and S7:
offline findings must agree with a server-side dry run) and **S14** (the
practitioner survey at the company).

## Process

- Each spike lives in `spikes/sNN-<slug>/` as its **own Go module** so its
  dependencies never leak into the product module. `make spikes` runs them all.
- Each spike directory has a `README.md` (goal, pass criterion, method) and, when
  done, a `RESULT.md` (PASS / FAIL / PARTIAL, measurements, date, what it changes
  in the design).
- Update the status column below when a spike starts or finishes.
- A failed spike is a result, not a problem: it rescopes the design (design.md
  section 14 shows how the critique already turned S4 and S11 from forks into ports).

## Tracker

| # | Spike | Pass criterion | Status |
|---|---|---|---|
| S1 | kubectl-validate `pkg/validator` (pseudo-version) on a CRD with a failing `x-kubernetes-validations` rule, ratcheting, budget exhaustion | Server-identical error text vs kind; compiles and passes its own tests under MVS at k8s.io v0.37.0; fork-readiness note | not started |
| S2 | VAP/MAP offline: `validating.NewValidator` + `cel.NewCompositedCompiler`; MAP via `compilation.go` copied into `internal/admit/port/mapcompile`; snapshot-backed type converter and namespace lister | Identical verdicts vs `--dry-run=server` on kind; per-object latency recorded | not started |
| S3 | `kyverno apply --context-file --parameter-resource --userinfo --policy-report` from generated side files incl. a MAP paramRef; two-pass mutate/validate | Reproduces a known in-cluster denial; fathom-owned exit codes | not started |
| S4 | Port (not fork) LimitRanger mutate/validate and pod/PVC/service quota usage to `k8s.io/api/core/v1`; `RulesAllow`/`RuleAllows` + rule resolver; `MatchingScopes` | Golden test against `--dry-run=server` on kind; port size recorded; per-minor refresh task budgeted | not started |
| S5 | Helm 4.3 SDK render with snapshot Capabilities/lookup; deterministic `CustomTemplateFuncs`; 2020-12 `values.schema.json` with `x-fathom-*` and standard `$schema`; subchart merge | `helm lint`/`helm template` accept the file with no warning and no network; Argo renders it; two renders byte-identical | **PASS** 2026-09-17, substitute inputs (`s5-helm-render/RESULT.md`) |
| S6 | yaml.v3 round-trip over the company's real layered values files (anchors, markers, `null` deletes) incl. `writeTo` a chosen layer | Byte-identical untouched regions; edits land in the intended file | not started |
| S7 | Full-chain T0 latency: 40-object app, 300-CRD snapshot, 50 Kyverno policies, warm; Kyverno CLI cold start measured separately | Chain under 2 s or the copy changes; Kyverno stage moves to save-time if over budget | not started |
| S8 | Import size/time/RBAC degradation on EKS, AKS, OpenShift with discovery-only, namespace-scoped and full identities; MAP v1/v1beta1 fallback | Per-layer degradation recorded; namespace-scoped import is a supported mode | not started |
| S9 | Go-native schema-to-form on Bitnami redis + two internal charts; share of deployer-touched leaves with a picker from heuristics alone; residual vs editor path (S14 arm) | At least 95% of leaves typed, no deployer group in YAML fallback, residual demonstrated; else RJSF island | not started |
| S10 | Lineage: `{kind,name,pointer}` targets + fingerprint coverage on three charts; sentinel cost | Coverage %; sentinel go/no-go for phase 2 | not started |
| S11 | SCC port sizing: `sccmatching` + strategies + range parser re-typed to external types; offline `ConstraintAppliesTo` vs `oc adm policy scc-subject-review` on 4.18+ | Port size and identical selection on 20 fixture pods; else data tier only | not started |
| S12 | AKS Azure Policy cluster: `k8sazure*` as CEL (generated VAP) or Rego? | Decides OPA phase 2 vs 3 | not started |
| S13 | Troubleshoot analyzers on a snapshot-as-bundle adapter | `storageClass`, `clusterVersion`, `customResourceDefinition`, `nodeResources` pass on a synthetic bundle | not started |
| S14 | Practitioner survey at the company: failure-class frequency, distributions, SCM mix, how customers run Helm, task-based form-vs-editor arm | Orders the backlog; confirms OpenShift-first; decides GitLab timing and the form's residual | in progress: survey drafted 2026-09-17 (`s14-survey/SURVEY.md`), waiting to be sent; task arm weeks 5 to 6 |
| S15 | (phase-3 gate) envtest/KWOK boot with restored snapshot and `--admission-control-config-file`; Gatekeeper `k8scel` `request.userInfo` offline | Boot time measured | not started |
| S16 | Compose: compose-go two-pass load of a company project, `ExtractVariables` to schema, three findings in `pkg/report` shape | No domain-model field changes | not started |

## Suggested order

Week 1: S14 (send the survey first; it runs in the background), S5, S6, S16.
Weeks 2 to 3: S1, S2, S3, S4 (the admission emulation core).
Weeks 4 to 5: S7, S8, S9, S10, S13.
Week 6: S11, S12, results review, architecture freeze. S15 waits for phase 3.
