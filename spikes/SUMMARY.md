# Spike sprint summary (interim, 2026-09-18)

Written at shutdown after week 2 of the sprint. Seven of sixteen spikes have a
RESULT.md; three were interrupted by the API spend limit with their fixtures
committed; the rest have not started. Every result below used substitute inputs
(Bitnami charts, synthetic values, a synthetic Compose project) because no
company charts, values, Compose project or managed cluster exists.

| Spike | Result | Design sections that must change |
|---|---|---|
| S1 kubectl-validate | PARTIAL: 6/8 server-identical via the module API, 7/8 with an 80-line `ValidateUpdate` port; budget error identical; upstream `TestHasUptoDateBuiltinSchemas` fails at 0.37 (embed stops at 1.35) | 3.1: port `pkg/validator` (~1k LOC) into `internal/admit/port/kubectlvalidate` instead of importing; 3.3 S-stage: update path with the ratcheting validator, ratcheted errors are Warnings, `fieldValidation` mode is a stage input |
| S2 VAP/MAP offline | PASS: 9/9 verdicts and mutated objects identical; VAP 35 to 96 µs, MAP 0.2 to 0.3 ms per object | 3.1: the port is ~880 LOC of VAP dispatch and MAP compile internals (`mapcompile` alone is not enough; `compilation.go` is in staging); 3.3 M/V wording |
| S3 Kyverno CLI | PASS: 8/8 in-cluster verdicts reproduced; fathom exit codes from the JSON report | 3.3 V: ClusterPolicy context via `--values-file`, CEL policies via `--context-file`; 3.5 engines/: CLI exits 1 for fail and error alike; 3.6: cold start 0.2 to 0.9 s feeds the S7 decision |
| S4 ports | PASS: 14/14 LimitRange and quota dry-runs, 23/23 RBAC verdicts; 1,889 LOC ported; 2.5 to 4 h per minor | 3.1: `MatchingScopes` is not in staging and is part of the port list; 3.3 M/V/Z1 unchanged |
| S5 Helm render | PASS: byte-identical deterministic renders; Argo's Helm 4.2.1 identical on 15/15 docs; 2020-12 schema accepted offline | 3.3 P2: `CustomTemplateFuncs` carries lookup too, longer replacement list, UTC clock; 3.8/4: no remote `$ref`, `.helmignore` check, subchart merge rules for emitted schemas |
| S6 yaml.v3 write-back | PASS: byte-range splice, untouched regions identical; naive re-encode changes 10/65 lines | 3.6: alias/merge-key state in the form, flow-container residual; 5.1: gotmpl layers read-only |
| S16 Compose | PASS on fields; identity rule extended | 2.2 (applied 2026-09-18): identity hashes `keywordLocation` and `valuesPointer`; 6 (applied): defaulted variable is Info; 3.1: one YAML module |
| S14 survey | drafted EN + DE (Google Forms, Du); not sent | decisions pre-registered in the annex; nothing until responses |
| S7 latency | interrupted (fixtures partly built) | pending; the "<2 s" copy in 0.15 stays unmeasured |
| S8 import | interrupted after identities (kind only) | pending; capped at PARTIAL without EKS/AKS/OpenShift |
| S13 Troubleshoot | interrupted after scout | pending |
| S9, S10, S11, S12, S15 | not started | S11 and S12 have no OpenShift or AKS cluster to run against; S15 is the phase-3 gate |

## Recommendation so far

Nothing measured refutes the architecture: the admission emulation core (S2,
S3, S4) reproduces the apiserver's and Kyverno's verdicts on every scenario
tried, the render and write-back paths (S5, S6) meet their byte-identity
criteria, and the domain model survived Compose (S16) with one identity-rule
change. The two open risks are S1 (kubectl-validate must be a port, not an
import, and strict-decode wording differs) and S7 (the latency budget is still
a promise). Freeze the architecture after S7 runs; rescope only the
"import kubectl-validate" line in 3.1 and the port list, which already grew
from four ports to six (kubectl-validate, VAP dispatch, MAP compile, quota
scopes, plus the four planned).

## What the owner still has to provide

Company charts and layered values (S6, S9, S10), a Compose project (S16),
cluster access for S8, S11, S12, and the survey send-out (S14).
