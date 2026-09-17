# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this is

fathom is the ground-up successor to `github.com/MrWong99/zhi` (the sibling folder
`../zhi` is the old codebase; reference only). The product: **the PR check that
already knows your cluster**. It renders Helm/Kustomize/Compose values with the
real engines, runs the target cluster's admission chain offline against an
imported snapshot, and opens a pull request. Git is the only write target; fathom
never applies to a cluster and has no in-cluster component.

Two personas: developers author the chart and the contract (`values.schema.json`,
`values.cel.yaml`, `fathom/requirements.yaml`); consultants and supporters fill
environment values in the GitOps repo and get validated against the target
cluster's snapshot before the PR opens.

## Naming

The design and research documents were written under the working name **zhi**.
Read `zhi` as `fathom` everywhere: command `fathom ...`, workspace directory
`.fathom/`, lockfile `fathom.lock`, schema keywords `x-fathom-*`, commit trailers
`Fathom-*:`, API groups `fathom.dev/v1`. Do not rewrite the documents; they are
the record.

## Source of truth

- `docs/design/design.md` is the plan. Section 0 is the decision summary, section 3
  the architecture and pipeline order, section 8 the plugin decision, section 10
  the roadmap and spikes, section 12 the open owner decisions. Do not re-litigate
  settled decisions; if the code must diverge, update the design first.
- `docs/research/` is the evidence behind it. `docs/research/landscape-report.md`
  section 9 lists the tagged design implications.
- `spikes/README.md` tracks the spike sprint. Product code in `pkg/` and `internal/`
  is not written until the calibration spikes (S1, S2, S7) and the survey (S14)
  have a `RESULT.md`.

## Non-negotiable constraints

- `CGO_ENABLED=0`, one static binary, one Go module for the product.
- **No `replace` directives** in the product `go.mod` and **never the
  `k8s.io/kubernetes` module**. Embed the staging modules (`k8s.io/apiserver`,
  `k8s.io/apiextensions-apiserver`, `k8s.io/pod-security-admission`, ...) at one
  pinned minor. LimitRanger, quota evaluators, RBAC rule matching and MAP compile
  are **ports** to external types under `internal/admit/port/` with NOTICES.
- CEL only through `k8s.io/apiserver/pkg/cel/environment` at the cel-go path
  Kubernetes pins; never import `cel.dev/cel-go` directly.
- Kyverno, kwctl and flux-schema are **subprocesses** (digest-pinned, fathom-owned
  exit codes), never Go dependencies.
- Never import AGPL (flux-operator, schema-catalog, Nuon), GPL (Komodo, helm-docs,
  podman-compose) or BUSL server code. Exec, read as data, or talk HTTP instead.
- Values are addressed by RFC 6901 JSON Pointer; findings are JSON Schema 2020-12
  output units extended with severity, fidelity tier, lineage, admission context
  and persona side. Severity is Info / Warning / Blocking.
- The pipeline runs in the apiserver's order: mutators, then defaulting and
  structural/CRD-CEL validation, then validators with ResourceQuota last.
- Plugin system: shallow. Zero loadable plugins in v1; `Check` and `Resolver`
  are Go interfaces. No gRPC plugin types, no marketplace, no transform hooks.
- Secrets are references, never values; validation runs with secrets absent.
- Determinism: `pkg/pipeline.Validate` is a pure function of committed inputs plus
  a digest-pinned snapshot; two renders must be byte-identical.

## Planned layout (design section 3.1)

```
cmd/fathom              Cobra; all surfaces
pkg/pipeline            Validate(ctx, Workspace, Env, Options) -> (Report, error)
pkg/report              versioned finding/report types, canonical JSON, exporters
pkg/check, pkg/resolver the two doors
internal/workspace      workspace, environment bindings, policy files, fathom.lock
internal/layout/*       Argo/Flux/Helmfile/plain layer discovery
internal/values         yaml.v3 nodes, JSON Pointer, layer fold, write-back
internal/contract       schema ingest, x-fathom keywords, CEL, overlay, synthesis
internal/render/*       Helm v4 SDK, krusty, compose-go
internal/snapshot       format, importer tiers, redaction, verification, OCI/bundle
internal/admit          the ordered chain; internal/admit/port/* ports
internal/engines        exec adapters (kyverno first)
internal/profile        distribution profiles (openshift data tier in MVP)
internal/require        Troubleshoot analyzers + fathom analyzers
internal/finding        canonical record, severity, back-mapping, suppression
internal/scm/*          generic exit codes + files; github Check Run
internal/git            system git runner (carried from old zhi apply.go)
internal/ui             htmx shell, form renderer, session API
spikes/sNN-<slug>/      throwaway spikes, each its own Go module
```

Do not pre-create empty packages; add directories when code lands.

## Commands

```sh
make build        # static binaries to bin/
make test         # go test -race -count=1 ./...
make test-cover   # coverage summary (bin/coverage.out)
make fmt          # gofmt -s
make vet          # go vet
make lint         # golangci-lint (v2, errcheck disabled)
make check        # fmt + vet + lint + test
make spikes       # run every spikes/<name>/go.mod module
make tidy         # go mod tidy
```

Run one test: `go test -race -count=1 -run TestName ./path/...`

## Conventions

- Tests live beside code as `*_test.go`, table-driven, `t.Parallel()` where safe;
  golden fixtures under `testdata/`. The calibration oracle is a real
  `--dry-run=server` on kind; keep its coverage definition (design section 3.4).
- `gofmt -s`; CI fails on unformatted files.
- Copyright header `// Copyright 2026 Lukas Schmidt` + `// SPDX-License-Identifier: MIT`.
- Ported upstream code keeps its Apache-2.0 header and is listed in
  `third_party/NOTICES` (generate with go-licenses once dependencies exist).
- Commit messages: imperative subject; reference the spike or design section.

## Spike workflow

1. `mkdir spikes/sNN-<slug> && cd $_ && go mod init github.com/MrWong99/fathom/spikes/sNN-<slug>`
2. Write `README.md` with goal, pass criterion (copy from `spikes/README.md`), method.
3. Implement; `go test -race -count=1 ./...` must run it.
4. Write `RESULT.md` (PASS / FAIL / PARTIAL, measurements, date, design impact) and
   update the tracker row in `spikes/README.md`.
