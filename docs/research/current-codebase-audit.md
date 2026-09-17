# zhi Audit Brief: Carry-Forward Analysis for a GitOps Config-Validation Rewrite

**Subject:** `/home/luk/Desktop/git/zhi` @ `da4bb0b` (Go 1.26, 42,487 LOC non-test non-generated, 42,290 LOC test, 10,790 LOC generated protobuf, 24 direct dependencies)

**Audience:** architects rewriting this from scratch as a GitOps-oriented, cluster-policy-aware config validation tool for Kubernetes and Docker Compose.

## 0. Size map

| Area | Non-test LOC | Verdict headline |
|---|---|---|
| `internal/core` (engine, components, export, apply, diff, drift, registry, discovery) | 4,544 | Highest-value area |
| `internal/cli` (Cobra, ~48 files) | 5,793 | Surface bloat from plugin/marketplace verbs |
| `internal/ui` (driver 595 + TUI 4,536) | 5,627 | TUI cost is real, payoff thin |
| `pkg/zhiplugin` (config 923, store 1,618, ui 1,429, labels 1,573, transform 263, launch 478) | 6,422 | Keep config + labels, drop store/ui |
| `pkg/providers` (webui 3,779, store providers 2,768, structuredfile 396, httpapi 500) | 7,443 | webui patterns reusable |
| `pkg/sharing` (OCI client 1,204, verify 1,236, marketplace 565, manifest 288, update 268, metadata 212, registry 208, semver 166, lockfile 134) | 4,281 | Keep OCI+lockfile, drop the rest |
| `cmd/zhi-marketplace` (server 1,164, storage 787, main 188, auth 61) | 2,200 | Drop |
| `cmd/zhi-mirror` (server 1,049, airgap 615, storage 454, main 284) | 2,402 | Drop or defer |
| `examples/` (11 example plugins + workspaces) | 2,063 | Reference only |
| generated `*.pb.go` | 10,790 | Consequence of the plugin choice |
| tests (`*_test.go`, 138 files) | 42,290 | Near 1:1 with production code |

Roughly **8,900 LOC (21%) is plugin distribution infrastructure** with no analogue in a GitOps validator. Another **~11,000 LOC is gRPC plumbing** (proto definitions, generated stubs, `grpc_client.go`/`grpc_server.go` pairs) that exists only because plugins are separate processes.

Commands to reproduce: `find <dir> -name '*.go' -not -name '*_test.go' -not -name '*.pb.go' | xargs wc -l`

---

## 1. Core domain model

### 1.1 The tree

`config.Tree` (`pkg/zhiplugin/config/config.go:117-188`) is a **flat `map[string]*Value` keyed by slash-delimited paths**, guarded by a `sync.RWMutex`. It is not a tree. Nesting exists only as a string convention, reconstituted on demand by `TreeData.Nested` (`internal/core/export_data.go:96`) for templates and by `buildNestedTree`/`insertTreeNode` (`pkg/providers/ui/webui/tree.go`) for display.

Surface: `Set`, `Get` (returns a **copy**, cloning Metadata and Validators), `GetPtr` (returns the live pointer for in-place mutation — documented as caller-synchronized), `Delete`, `List`, `Validate`. `TreeReader` is the read-only pair `{Get, List}`.

**Path rules** — `ValidatePath` (`pkg/zhiplugin/config/config.go:20-34`) splits on `/` and enforces each segment against:

```
^[a-z](?:[a-z0-9._-]*[a-z0-9])?$
```

**This is fatal for the rewrite.** Kubernetes keys are camelCase (`imagePullPolicy`, `terminationGracePeriodSeconds`, `apiVersion`), and list indices have no representation at all. A flat-path model cannot address `spec.containers[0].resources.limits.memory` without inventing an escaping scheme. Compose likewise carries uppercase environment keys. The example fixtures work only because they are lowercase by construction (`pkg/providers/config/structuredfile/testdata/basic.yaml`).

### 1.2 Value

`Value` (`pkg/zhiplugin/config/config.go:79-105`):

```go
type Value struct {
    Val        any                 `json:"value" yaml:"value"`
    Metadata   map[string]any      `json:"metadata,omitempty"`
    Version    string              `json:"-" yaml:"-"`   // transient; store CAS token
    Validators []ValidateFunc      `json:"-" yaml:"-"`   // never crosses the wire
}
```

The `any` typing forces `Value.TryConvert` (`pkg/zhiplugin/config/convert.go`, 192 lines of hand-written numeric coercion across int/uint/float64/bool/string) **and** a second coercion layer, `tryNumericConvert` (`internal/core/numeric.go`, 138 lines), invoked in `Engine.LoadTree` (`internal/core/engine.go:150-163`) because a JSON store returns `float64` where the YAML config provider returned `int`. When coercion fails the engine logs `stored value has different type and won't be updated` and **silently keeps the stale value**. A third coercion site exists in `Engine.LoadStoredTree` driven by a `core.semantictype` label (`engine.go:~285`).

**This entire class of bug disappears if values carry a schema-derived type.** 330+ LOC of coercion is the price of `any`.

### 1.3 Validation

Three severities (`config.go:36-46`): `Info`, `Warning`, `Blocking`. `Severity.String()` renders `"info"`/`"warning"`/`"blocking"`.

```go
type ValidationResult struct {
    Severity Severity
    Message  string
    Metadata map[string]any
}
```

**`ValidationResult` has no `Path` field.** The web UI recovers the path via `r.Metadata["path"].(string)` (`pkg/providers/ui/webui/validation_handlers.go:120, 155, 185`) — a stringly-typed side channel every plugin must populate by convention, undocumented in the type. The TUI (`internal/ui/tui/validation_view.go`) does not recover it at all and groups only by severity.

**Fix in the rewrite:** a finding must be `{Severity, Message, Path/JSONPointer, Rule ID, Source file + line + column, Fix hint}`. GitOps users need "which file, which line, which policy, how do I fix it." This type answers none of those, and the omission has already produced two inconsistent frontends.

**Cross-value validation** works via:

```go
type ValidateFunc func(value any, tree TreeReader) []ValidationResult
```

Every validator gets read access to the whole tree. The concept is right and directly reusable — cluster policy is inherently cross-document ("every Deployment must reference an existing ConfigMap"). The implementation is not: `Tree.Validate` (`config.go:190-215`) constructs an `unsafeTreeReader` over the raw map to avoid re-entrant `RLock` deadlock when a validator calls `Get`. That workaround is a signal the lock is at the wrong granularity.

**Validators are attached in two incompatible places:**

1. **In-process**, as Go closures on `Value.Validators`, run by `Value.Validate`/`Tree.Validate`. Explicitly excluded from serialization.
2. **Out-of-process**, via `config.Plugin.Validate(ctx, path, tree)` (`pkg/zhiplugin/config/plugin.go:19-24`).

`Engine.Validate` (`internal/core/engine.go:196-218`) calls **only the second**. So `Value.Validators` is dead weight for any external plugin. Two mechanisms, one used, both maintained.

Component dependency violations are injected into the same result stream as `Blocking` findings under a pseudo-path `_components/<name>` (`engine.go:207-213`) — another stringly-typed channel.

**The O(N²) problem.** `Engine.Validate` loops over every path calling `configPlugin.Validate(ctx, path, tree)`, and `GRPCClient.Validate` (`pkg/zhiplugin/config/grpc_client.go`) calls `TreeToProto(tree)` — serializing the **entire tree as JSON** into every single call:

```go
func (c *GRPCClient) Validate(ctx context.Context, path string, tree TreeReader) ([]ValidationResult, error) {
    entries, err := TreeToProto(tree)          // <-- whole tree, every call
    resp, err := c.client.Validate(ctx, &pb.ValidateRequest{Path: path, Tree: entries})
    ...
}
```

A 500-key tree means 500 gRPC round trips each carrying 500 JSON-encoded values. For a repository of Kubernetes manifests this is unusable. **The rewrite must validate the whole document set in one pass.**

### 1.4 The policy language is arbitrary Go interpreted at runtime

The built-in config provider (`pkg/providers/config/structuredfile/`) reads YAML/JSON files where each leaf is a map containing `val`, optional `metadata`, optional `imports`, and optional `validation` — the last being **a Go function body as a string**. `compileValidation` (`pkg/providers/config/structuredfile/validate.go:47-95`) wraps it in a package, evaluates it under Traefik's Yaegi interpreter, and loads **the full Go standard library**. The code documents this at line 65:

```go
// SECURITY: Yaegi loads the full Go stdlib (stdlib.Symbols), which means
// validation code embedded in configuration files can execute arbitrary
// system calls (os/exec, net/http, os.Remove, etc.). This is a deliberate
// trade-off for maximum flexibility: configuration authors are considered
// trusted. If untrusted input is ever accepted, a restricted symbol set
// must replace stdlib.Symbols.
```

Example fixture (`.../testdata/with_imports.yaml`):

```yaml
pokedex:
  trainer.name:
    val: Ash
    imports: [strings, regexp]
    validation: |-
      name, ok := v.Val.(string)
      if !strings.HasPrefix(name, "A") { ... }
```

This is remote code execution by design, gated on "configuration authors are considered trusted." **In a GitOps tool, config arrives in a pull request from a fork.** The assumption inverts.

**This must be replaced by a sandboxed declarative policy language — CEL, Rego, or JSON Schema plus a constraint layer.** Every rule then becomes cacheable, explainable, statically analyzable, diffable, and safe to evaluate on untrusted branches. This is the single most important lesson in the repository.

### 1.5 Components

`internal/core/component.go` (429 lines).

```go
type ComponentDef struct {
    Name, Description string
    Paths             []string
    Mandatory         bool
    Dependencies      []string
}
```

`ComponentManager` does genuinely careful work:

- **Unique-name check** and dependency-reference validation (`NewComponentManager:45-62`).
- **Cycle detection** via Kahn's algorithm (`detectCycles:88-127`).
- **Segment-aware overlapping-prefix rejection** so `app` and `app-backend` do not collide (`prefixOverlaps`/`pathHasPrefix:155-170`).
- **Mandatory components start enabled together with their transitive dependencies** — with a comment explaining that enabling only the mandatory ones immediately fails dependency validation (`NewComponentManager:74-83`).
- `EnableWithReport` returns which dependencies were auto-enabled (for UI messaging).
- `DisableCascade` refuses when a transitively-dependent component is mandatory, avoiding the broken "mandatory enabled, its dependency disabled" state (`:320-345`).
- `FilterTree` produces a tree containing only enabled-component paths plus unowned paths — this drives component-aware export.
- `LoadState`/`SaveState` persist enable/disable across sessions; mandatory components are force-enabled on load.

This is the **best-engineered part of the codebase**, and the concept maps cleanly onto GitOps: components ≈ Kustomize overlays, Helm subcharts, Compose profiles. **Carry the dependency/cycle/cascade logic forward almost verbatim.** Change only the addressing: ownership should be by file or manifest identity (`apiVersion/kind/namespace/name`), not string prefix.

### 1.6 Judgment summary for section 1

| Concept | Good / awkward |
|---|---|
| Severity triad | **Good.** Right granularity; maps to CI gate vs. advisory. |
| Cross-value validation with full-tree read | **Good concept, bad delivery.** Essential for policy; must be one pass, not N. |
| Component graph semantics | **Good.** Best code in the repo. |
| `TreeReader` read-only interface | **Good.** Clean seam; keep it. |
| Flat slash-path addressing | **Awkward.** Cannot express K8s keys or list indices. |
| `[a-z]`-only segment regex | **Broken for the domain.** |
| `Val any` | **Awkward.** 330 LOC of coercion, silent type-mismatch drops. |
| `ValidationResult` without Path/Rule | **Awkward.** Forced a `Metadata["path"]` convention honored by one frontend. |
| Two validator attachment mechanisms | **Awkward.** One is dead. |
| `Value.Validators` non-serializable | **Awkward.** Guarantees the in-process and plugin paths can never converge. |

---

## 2. Plugin system

Four plugin types over `hashicorp/go-plugin` v1.8.0 with gRPC transport over stdio. Handshake is trivial (`pkg/zhiplugin/plugin.go`, 12 lines): magic cookie `ZHI_PLUGIN` = `zhiplugin-v1`, protocol version 1. Each type exports a `PluginMap` keyed by type name and a `GRPCPlugin` implementing `GRPCServer`/`GRPCClient`.

### 2.1 Interface sizes tell the story

| Interface | Methods | File |
|---|---|---|
| `config.Plugin` | 4 (`List`, `Get`, `Set`, `Validate`) | `pkg/zhiplugin/config/plugin.go:14-24` |
| `transform.Plugin` | 3 (`BeforeDisplay`, `AfterSave`, `ValidatePolicy`) | `pkg/zhiplugin/transform/plugin.go:13-27` |
| `store.Plugin` | **27** | `pkg/zhiplugin/store/plugin.go` |
| `ui.Controller` | **25** | `pkg/zhiplugin/ui/plugin.go:17` |

`store.Plugin` spans capabilities negotiation, 4 auth RPCs, tree CRUD, tree-level versioning (list/get/rollback/delete), value-level versioning (same four again), encryption init/rotate, and grant/revoke/list access control. The interface documents its own failure at line 17:

> "Plugins may support different subsets of the interface. Methods that do not apply to a plugin's capabilities should return a descriptive error (e.g. 'versioning not supported'). The Capabilities method lets the host discover which features are available."

That is an interface that should have been five interfaces with compile-time capability detection.

`ui.Controller` methods: `LoadTree`, `SetValue`, `Validate`, `SaveTree`, `ExportTemplates`, `Export`, `Apply`, `ListComponents`, `EnableComponent`, `DisableComponent`, `WorkspaceName`, `SearchMarketplace`, `GetMarketplaceDetail`, `InstallPlugin`, `UninstallPlugin`, `ListInstalledPlugins`, `CheckUpdates`, `UpdatePlugin`, `RatePlugin`, `StoreAuthMethods`, `StoreLogin`, `StoreLoginInteractive`, `StoreLoginInteractiveCallback`, `StoreAuthStatus`, `StoreLogout`.

**Every Controller method costs eight implementations:** Go interface, proto RPC, `controller_client.go` (415 LOC), `controller_server.go` (393 LOC), TUI call site, webui handler, httpapi handler, MCP tool. 25 × 8 is why `pkg/zhiplugin/ui` is 1,429 LOC and generated protobuf is 10,790.

### 2.2 The proto surface

`api/proto/zhiplugin/v1/`: `config.proto` (81 lines), `transform.proto` (61), `store.proto` (319), `ui.proto` (506) — 967 lines of IDL producing 10,790 lines of Go.

**JSON-over-gRPC for values.** Every message carries `bytes value_json` and `bytes metadata_json` (`config.proto:38-40`, `TreeEntry` at `:53-61`):

```protobuf
message TreeEntry {
  string path = 1;
  bytes value_json = 2;
  bytes metadata_json = 3;
  string version = 4;
}
```

Values are marshalled to JSON, wrapped in protobuf, unwrapped, unmarshalled to `any`, then type-coerced back. **Three serialization layers**, and the round trip is lossy for numbers — which is precisely why `convert.go` and `numeric.go` exist. Protobuf's type system is bypassed entirely; it is being used as a dumb envelope.

`transform.proto` ships the **entire tree in both directions** on every `BeforeDisplay`/`AfterSave` call. `ui.proto` has exactly one streaming RPC (`Apply` returns `stream CtrlApplyResponse`); everything else is unary, so `LoadTree` returns the whole tree in one message with no pagination.

### 2.3 TTY-only UI plugins

`ui.Capabilities.RequiresTTY` (`pkg/zhiplugin/ui/ui.go:70-78`) exists because an external gRPC child process has no terminal. Any TUI must be **compiled into the host binary** and registered as built-in. The package doc states it outright:

> "UI plugins that require direct terminal (TTY) access (such as TUI implementations) must report RequiresTTY: true ... These plugins must be registered as builtin plugins since external gRPC processes do not have access to the host's terminal."

The plugin system therefore cannot deliver the one plugin type users would most want to swap. This is a leaky abstraction admitted in the source.

### 2.4 Meta-plugin SDK

`pkg/zhiplugin/launch` (478 LOC): `LaunchConfig`/`LaunchTransform`/`LaunchStore` spawn child plugin processes and return `(Plugin, cleanup func(), error)`. `audit.go` (153 LOC) resolves symlinks via `filepath.EvalSymlinks`, rejects `..` after resolution, SHA-256 hashes the binary and compares against the digest recorded at install time, and warns on world-writable files. **The security hygiene is good and cheap — carry the pattern.**

Composition primitives:
- `DelegatingPlugin` (`config/delegate.go` 38 LOC, `store/delegate.go` 134, `transform/delegate.go` 38) — decorator: forward everything, override selectively.
- `MergedPlugin` (`config/compose.go`, 139 LOC) — composite: mount child plugins under distinct `prefix/` namespaces, validate non-overlap, fan out `List` in parallel, route `Get`/`Set`/`Validate` by prefix, and hand each child a `prefixedTreeReader` scoped to its own namespace.
- `MirroredPlugin` (`store/compose.go`, 234 LOC) — primary + backup store.

Elegant, well-tested, and solving a problem the rewrite will not have.

### 2.5 Discovery and registry

`internal/core/discovery.go` (219 LOC) scans `~/.zhi/plugins/` (overridable via `plugins.directories` in `zhi.yaml`) for either flat `zhi-<type>-<name>` binaries or `<type>/<name>` subdirectory layout, flat taking precedence. It rejects directories containing path traversal, expands `~`, skips non-executables, and dedupes by `type/name`.

`internal/core/registry.go` (336 LOC) maps provider names to factories per plugin type, holds discovered external plugins, lazily instantiates and caches them, and accumulates cleanup funcs. Built-in providers register at startup (`internal/core/builtin.go`).

### 2.6 Sharing, marketplace, mirror (8,900 LOC combined)

**`pkg/sharing` (4,281 LOC).**

- **Manifest** (`manifest/manifest.go`): `zhi-plugin.yaml` = `schemaVersion`, `name` (`[a-z][a-z0-9-]*`), `type`, `version` (semver), `zhiProtocolVersion`, `description`, `author`, `license`, `homepage`, `keywords[]`, `runtime{type,version,bundled}`, `minZhiVersion`, `binaries` map. `workspace.go` adds a parallel `WorkspaceManifest` with `dependencies[]{ref,type,optional}` and `tools[]{name,version}`.
- **Lockfile** (`lockfile/lockfile.go`, 134 LOC): `zhi-plugins.lock`, `version: 1`, `generatedAt`, `zhiVersion`, and per plugin: name, type, ref, **digest**, platforms map, `signed`, `signingIdentity`. Pins the OCI digest. **Exactly the right shape for CI reproducibility.**
- **OCI client** (`client/`, 1,204 LOC): `oras.land/oras-go/v2`. Custom media types (`media.go`): `vnd.zhi.plugin.{config,binary,runtime,readme,license}.v1(+json)`, `vnd.zhi.workspace.{config,bundle,readme}.v1`. Multi-platform = one OCI Image Index with per-platform manifests sharing a single config blob (`push.go:171-256`); pull uses `MapRoot` to fetch only the matching platform subgraph (`client.go:172-176`). Atomic install via temp-file + rename (`client.go:343-383`). Workspace push tars a `.gitignore`-respecting bundle (`push.go:468-566`). **Clean, standard, reusable.**
- **Verification** (`verify/`, 1,236 LOC): four policy levels (None/Signed/VerifiedPublisher/Strict). `signing.go` + `verify.go` implement **real** Sigstore keyless signing — Fulcio via `sign.NewFulcio`, optional Rekor transparency log, optional TSA timestamping, TUF-fetched trusted root — and `VerifySignature()` performs genuine cryptographic verification.

  **It is never called.** `grep` finds zero callers outside `verify.go` itself. The actual install chokepoint is `Client.PullPlugin → Verifier.VerifyArtifact` (`verify/verify.go:148-189`), which checks only the local policy (blocklist, allowed registries) and, per its own doc comment, "does NOT perform cryptographic signature verification" — it hardcodes `Signed: false`. Consequences: `policy.requireSignatures: true` **rejects every install** (`:174-181`), and non-strict mode **silently trusts everything**. The signing identity is the literal placeholder string `"keyless-identity"` (`signing.go:211`) rather than the Fulcio certificate SAN. **This is the single biggest leaky abstraction in the repository: a fully-built crypto subsystem not wired to the feature it names.**

**`cmd/zhi-marketplace` (2,200 LOC).** Auth (`auth/auth.go`, 61 LOC) is a flat map of static API keys → publisher IDs, constant-time compared, supplied via `--api-keys`/`--api-keys-file` (`main.go:40-65`). No signup, no OAuth, no JWT. Storage (`storage/sqlite.go`, ~620 LOC) is **not SQLite** despite the filename, the `NewSQLiteStore(dsn)` constructor, and `main.go:40` documenting `-db` as "SQLite database path" — it is a `JSONFileStore` that loads everything into memory and rewrites the whole file on every mutation (`:22-53`), silently rewriting a `.db` suffix to `.json`. No transactions, no indices, no multi-instance support. Endpoints (`server/routes.go`): `GET /.well-known/zhi-marketplace.json`, `GET /api/v1/search`, `GET|POST /api/v1/plugins`, `GET|POST /api/v1/plugins/{type}/{pub}/{name}/versions`, `GET .../resolve`, `POST .../download`, `GET .../stats`, `GET|POST .../ratings`, `POST .../ratings/{id}/helpful`, `GET|POST /api/v1/advisories`, `GET /api/v1/publishers/{name}`, `POST .../verify-request`. Ratings dedupe helpful-votes (`storage/models.go:26-28`); advisories require ownership of the target plugin (`advisory.go:60-75`). **A well-organized reference implementation, not a production service.**

**`cmd/zhi-mirror` (2,402 LOC).** Air-gap format (`airgap/export.go` 420, `import.go` 197): a gzipped tar containing `oci-layout`, `bundle.json` (`{version, createdAt, [{ref,digest}]}`), and `blobs/sha256/<hex>`. Recursively walks image indexes (depth-capped at 10) and verifies every blob digest before writing; import re-verifies and rejects incomplete bundles atomically (`import.go:80-94`). `storage/oci_layout.go` (350 LOC) is a real on-disk OCI Image Layout with atomic fsync'd writes and digest-validated paths. **Solid, well-hardened code.**

But: `SyncRule.Schedule` is typed and documented as a cron expression, while `parseSyncInterval` (`server/sync.go:157-188`) only extracts an interval from `*/N` and falls back to 24h for anything else — day-of-week and specific-hour semantics are silently unimplemented. `RetentionPolicy{UnusedDays, MaxStorage}` (`server/policy.go:43-49`) is documented as LRU eviction and has **zero references anywhere** — dead config, no cleanup logic exists.

**Duplicated logic:** tar-extraction hardening (zip-slip, decompression bombs) is implemented twice, independently — `client/push.go:579-664` and `airgap/import.go:116-196`, ~90 lines each.

### 2.7 Keep / keep-conceptually / drop

| Piece | LOC | Verdict |
|---|---|---|
| Handshake + go-plugin transport | ~12 + 10,790 generated | **Drop.** Start in-process. |
| `config.Plugin` (4 methods) | 923 | **Keep conceptually** as an in-process interface. |
| `transform.Plugin` + `ValidatePolicy` tri-state | 263 | **Drop.** Three-way ordering knob for a two-hook pipeline nobody exercised. |
| `store.Plugin` (27 methods) | 1,618 | **Drop.** Git supersedes it. |
| `ui.Controller` (25 methods) + broker callback | 1,429 | **Drop to one in-process interface.** |
| `labels` registry | 1,573 | **Adapt** → JSON Schema + `x-` hints. |
| `launch` + binary audit | 478 | **Keep the audit hygiene** for downloaded policy bundles. |
| Delegate / Merged / Mirrored | ~400 | **Drop.** Solves a problem the rewrite won't have. |
| Discovery + Registry | 555 | **Keep conceptually**, much smaller. |
| OCI client + media types | 1,204 | **Keep.** Right vehicle for policy/schema bundle distribution. |
| Lockfile | 134 | **Keep.** Digest pinning is what CI needs. |
| Sigstore verify/sign | 1,236 | **Drop or rebuild wired.** Currently unreachable; shell out to cosign. |
| Marketplace server | 2,200 | **Drop.** Static OCI registry + index file replaces it. |
| Mirror + air-gap | 2,402 | **Defer.** Good code, only if regulated customers appear. |

---

## 3. UI layer

### 3.1 The abstraction

`UIDriver` is one method (`internal/ui/driver.go:21-26`):

```go
type UIDriver interface {
    Run(ctx context.Context, controller *UIController) error
}
```

`UIController` (595 LOC) wraps `*core.Engine`, caches the tree, holds a `marketplace.Client`, and tracks `dirtyPaths map[string]bool` so that a reload does not clobber unsaved edits — `loadTreeLocked` (`:72-96`) re-applies dirty values over the freshly merged tree. Methods mirror the 25-method `ui.Controller` plus `FilteredTree`, `ExportPreview`, `ExportAll`, `PathBelongsToComponent`, `ComponentDefinition`, `ComponentDependents`, `SaveComponentState`.

Three frontends consume it (TUI, webui, httpapi) plus an MCP bridge. `internal/ui/adapter.go` (234 LOC) adapts `*UIController` to the wire-level `zhiui.Controller`.

### 3.2 Web UI — `pkg/providers/ui/webui` (3,779 Go + 1,522 template + 2,758 CSS + 380 JS)

Server-rendered Go `html/template` + htmx (vendored `htmx.min.js`), **no build step, no SPA framework**. `Server` wraps a Go 1.22+ `ServeMux` with `{path...}` wildcards and a `templateEngine` supporting embedded `embed.FS` in production and `os.DirFS` hot reload in dev mode. One handler serves both full page and fragment by checking `isHTMX(r)` (the `HX-Request` header) — e.g. `handleTree` (`routes.go:97-148`).

Largest files: `editor.go` 475, `middleware.go` 397, `templates.go` 374, `marketplace_handlers.go` 312, `tree.go` 286, `auth_handlers.go` 285, `plugins_handlers.go` 228, `routes.go` 212.

**30 routes:** `GET /static/*` (content-hashed, immutable-cached); `GET|POST /login`, `POST /login/interactive`, `POST /logout`, `GET /auth/status`; `GET /{$}` → `/tree`; `GET /tree`; `GET /tree/edit/{path...}`; `POST /tree/values/{path...}`; `GET /tree/display/{path...}`; `POST /validate/inline/{path...}`; `GET /validation`; `POST /validate`; `POST /tree/save`; `GET /components`; `POST /components/{name}/toggle`; `GET /export`; `POST /export/preview`; `POST /export`; `POST /export/all`; `GET /apply`; `POST /apply/run` (SSE); `GET /shortcuts`; `GET /marketplace`; `GET /marketplace/{type}/{publisher}/{name}`; `POST .../rate`; `GET /plugins`; `POST /plugins/install`; `POST /plugins/update-all`; `POST /plugins/{name}/uninstall`; `POST /plugins/{name}/update`; catch-all 404.

**Tree navigation** (`tree.go`): flat paths are split and folded into nested `[]*treeNode`, sorted by `ui.order` → `ui.section` → name, with synthetic section headers injected between leaves. Text filtering matches path or `ui.displayName`. `ui.showIf` conditional visibility is evaluated against the **unfiltered** tree. Component ownership resolves per node by longest-prefix match.

**Value editing** (`editor.go` + `templates/fragments/value_form.html`): click-to-edit swaps a display fragment for a form fragment via htmx (`hx-get /tree/edit/{path}` → `hx-post /tree/values/{path}` → `value_display`). Editors are selected from `labels` metadata plus Go-type inference (`valueType`: string/number/bool/map/list/other):

| Condition | Widget |
|---|---|
| default string | text input |
| numeric Go type | `<input type=number>` |
| bool | checkbox-styled toggle |
| `ui.enum` present | `<select>` |
| `ui.password` | masked `<input type=password>` |
| `ui.multiline` or `ui.format: yaml` | `<textarea>`; `ui.yamlSchema` shown as a hint string only — **no YAML parsing or schema validation client-side** |
| map | add/remove rows of `map_key`/`map_value` field pairs (vanilla JS) |
| list | add/remove `list_item[]` rows (vanilla JS) |
| `ui.readonly` / `config.immutable` | static text, **re-enforced server-side** in `handleSaveValue` and `handleInlineValidation` |
| `ui.confirm` | `hx-confirm` dialog before submit |

**Validation surfacing — three layers:**

1. **Inline/live.** Every non-trivial input carries `hx-post /validate/inline/{path}` on `input changed delay:500ms`. The handler **sets the value, runs the full `ctrl.Validate`, filters to that path/subtree, reverts the mutation**, and swaps in a `validation_badge` fragment. Live feedback with zero duplicated client-side rule logic, and invalid state never persists. **Steal this.**
2. **On save.** `handleSaveValue` sets, validates, and if any `Blocking` result exists for that path, reverts and re-renders the form with inline error text. Warnings and Info save through.
3. **Aggregate page.** `/validation` groups all results by severity with icon + CSS class (`groupValidationResults`), each linking back to a `PathID` anchor in the tree.

Toasts (`HX-Trigger: showNotification`) are reserved for export/apply/install outcomes, not validation.

**Export/Apply display.** `POST /export/preview` renders a dry run into a preview fragment with a format-based CSS class; `POST /export` and `/export/all` write files and report via toast. **Apply is the one genuinely dynamic surface**: `POST /apply/run` opens a `text/event-stream`, disables the write deadline, unwraps the middleware chain to reach the underlying `http.Flusher`, and streams `event: output` / `event: done` frames into a log pane (`apply.html`, 180 lines). The same SSE pattern is duplicated near-verbatim in `httpapi.handleApply`.

**Middleware chain** (`server.go:83-90`, outermost first): gzip (skips `text/event-stream`) → ETag (SHA-256 weak ETag on buffered GET bodies; skips HTML, SSE, and 4xx+) → CSRF (double-submit cookie, HMAC-SHA256 signed, validated on POST/PUT/DELETE via header or form field) → security headers (per-request CSP nonce, `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`, COOP) → recovery → logging → response time → request ID. Auth is a per-route `requireAuth` wrapper keyed off `ctrl.StoreAuthStatus`, not global. TLS optional via `internal/tlsutil`, with mutual-TLS client-CA support.

**Non-obvious constraints already solved here** (worth not re-deriving): HTML cannot be ETag-cached because CSP nonces change per request; gzip must wrap ETag, not the reverse; every wrapper must implement `Unwrap()`/`Flush()` or SSE dies silently.

### 3.3 TUI — `internal/ui/tui` (4,536 LOC, Bubbletea)

`app.go` 807, `value_editor.go` 797, `login.go` 439, `tree_view.go` 370, `marketplace.go` 326, `component_view.go` 288, `export_view.go` 285, `apply_view.go` 276, `installed.go` 267, `plugindetail.go` 248, `validation_view.go` 198, `styles.go` 137, `notification.go` 89.

`value_editor.go` alone implements separate map and list sub-editors with their own key handling (`updateMapEditor`, `updateListEditor`), `coerceToOriginalType`, `validatePattern`, `validateType`, and a confirmation state machine. `validation_view.go` groups by severity but **cannot link a finding to a path** (no Path field to link on). High cost, capability duplicated by the web UI, and forced in-process by `RequiresTTY`.

### 3.4 httpapi — `pkg/providers/ui/httpapi/httpapi.go` (500 LOC, one file)

A pure JSON REST mirror of the same Controller with **no auth, no CSRF, no CSP, no TLS, no session** — demo-grade despite living under `pkg/providers`. 21 endpoints under `/api/*`: workspace, tree (GET/PUT value), validate, save, export templates/export, apply (SSE JSON events), components list/enable/disable, marketplace search/detail/install/uninstall/list/updates/update/rate. No pagination or streaming for tree reads. Errors are cleaned by stripping gRPC `desc = ` prefixes — a leak of the transport into the HTTP surface.

### 3.5 MCP — `pkg/mcpbridge` (1,168 LOC) + `internal/ui/mcp` (60)

Official `github.com/modelcontextprotocol/go-sdk/mcp`. Two transports share one bridge: stdio (`internal/ui/mcp/plugin.go`, built-in, `gomcp.IOTransport`) and Streamable HTTP (`examples/zhi-ui-mcp-sse`, despite the "sse" name it uses `mcp.NewStreamableHTTPHandler`, with optional constant-time Bearer auth). Both wrap the Controller in a `SafeController` (`safe_controller.go`) that mutex-serializes every method, because MCP sessions share one controller and the tree cache is not otherwise synchronized.

**Tools** — read-only: `reload_tree`, `validate`, `check_updates`, `marketplace_search`, `marketplace_detail`. Write (omitted when `ReadOnly`): `set_value`, `save`, `apply` (`DestructiveHint: true`), `export`, `enable_component`, `disable_component`, `marketplace_install`, `marketplace_uninstall`, `marketplace_update`, `marketplace_rate`, `store_login`, `store_logout`.

**Resources** — static: `zhi://workspace/name`, `zhi://tree`, `zhi://components`, `zhi://validation`, `zhi://export/templates`, `zhi://marketplace/installed`, `zhi://store/auth/{status,methods}`. Templated: `zhi://tree/{+path}`, `zhi://export/{name}`, `zhi://marketplace/{type}/{publisher}/{name}`.

**Prompts** — `explore-workspace`, `review-config`, `apply-changes`, `setup-component`, `audit-validation`: canned multi-step instructions chaining resource reads to tool calls.

A near 1:1 projection of `ui.Controller` onto MCP primitives — architecturally right, and genuinely the same engine the human UIs use rather than a fork. But it is the **third** re-marshalling of the same 25 methods.

### 3.6 Judgment: did the multi-frontend abstraction pay off?

**Partly.** The Controller discipline genuinely kept business logic out of transport code — that is real and worth keeping even with one frontend. The three concrete frontends did not pay off: most of `httpapi.go`'s 500 lines and `mcpbridge`'s 1,168 are boilerplate re-marshalling the same structs, and every new Controller method (the marketplace set alone is 8) must be implemented four times.

**Recommendation:** build one web UI plus the REST endpoints its own htmx calls consume. Add MCP only once the domain model stabilizes, and generate it from the same surface description rather than hand-writing it. Drop the TUI.

**Specific UX verdicts:**
- **Value editing:** metadata-driven widget selection is the right pattern; the flat map/list row editors are fragile (`%v` stringification, no key dedup, no order guarantee) and must be replaced by a recursive structured editor or an embedded code editor for nested YAML.
- **Validation surfacing:** mutate-validate-revert is the best idea in the UI layer. The severity grouping page is good. The inability to link a finding to a source location is the gap.
- **Export/apply display:** SSE log pane is exactly right for `kubectl apply` / `docker compose up`.

---

## 4. Export and apply

### 4.1 Export — `internal/core/export.go` (725 LOC)

Engine: `text/template` with `sprig.TxtFuncMap()` (100+ functions) plus zhi additions `toJSON`, `toJSONCompact`, `toYAML`, `toTOML`, `toDotenv`, `shellQuote`, and **two side-effecting functions** — `fileACL(entry)` and `fileMode(int)` mutate capture state while rendering and return `""` (`export.go:540-568`). Rendering a template therefore has out-of-band effects on how the output file is written (`writeExportFile`, `:636`). Clever, but a trap.

**Built-in formats:** `json`, `yaml`, `toml`, `dotenv` (`renderBuiltinFormat:484`, `export_formats.go`). Note the asymmetry documented at `export.go:77-82`: json/yaml/toml strip the prefix themselves via `.Nested <prefix>`, but dotenv renders `.All` and so must be pre-filtered through `newPrefixedTreeData` or unrelated subtrees leak into `.env`.

**Template dot value** is `*TreeData` (`internal/core/export_data.go`, 223 LOC) exposing `.Get(path) string`, `.GetOr(path, default)`, `.Has(path) bool`, `.All() map[string]any`, `.Prefix(p) map[string]any` (flat, prefix stripped), `.Nested(p) map[string]any` (split on `/`), and `.ComponentEnabled(name)`.

**Iterate exports** (`ExportIterate:132`): render once per direct child of a tree prefix, with `IterateData{Key, Value *TreeData}` as dot and a Go-template `output-pattern` producing the filename. This is how the Ansible example emits per-host files.

**Component awareness:** `PrepareTreeData` (`:385`) filters the tree through `ComponentManager` unless `AllComponents` is set.

### 4.2 How Kubernetes and Compose are reached today: string interpolation

`internal/cli/init-template/templates/docker-compose.yaml.tmpl`:

```yaml
services:
  pokedex-web:
    image: nginx:1.25
    ports:
      - "{{ .Get "pokedex-web/external_port" }}:80"
    networks:
      - {{ .Get "pokedex-web/network_name" }}
{{ if .ComponentEnabled "trainer-info" }}
    environment:
      TRAINER_NAME: {{ .Get "trainer-info/name" | quote }}
{{ end }}
```

`deploy/vault/templates/k8s-statefulset.yml.tmpl`:

```yaml
kind: StatefulSet
metadata:
  namespace: {{ .Get "vault-deploy/k8s/namespace" }}
spec:
  replicas: {{ .Get "vault-deploy/k8s/replicas" }}
  ...
          image: hashicorp/vault:{{ .Get "vault-deploy/vault-version" }}
```

**zhi has no knowledge that the output is a Compose file or a StatefulSet.** It cannot validate that a port is a valid port, that `replicas` is an integer, that `resources.limits.memory` parses as a Kubernetes quantity, that `apiVersion/kind` is a real GVK, or that the emitted document is schema-valid. `.Get` returns `fmt.Sprintf("%v", v.Val)` — a string, always.

**This is the entire gap the rewrite exists to close, and it argues for inverting the direction of flow:** parse real manifests into a typed, schema-aware model; validate against schema + cluster policy; render or write back. Do not template text out of an untyped key-value bag.

`deploy/vault/` is the most elaborate existing workspace: seven export templates including `k8s-namespace`, `k8s-configmap`, `k8s-statefulset`, `k8s-service`, `k8s-ingress`, `vault-config.hcl`, `docker-compose.yml`, and an `apply.sh` that deletes secret-bearing files after execution. Worth reading as the target use case.

### 4.3 Apply — `internal/core/apply.go` (313 LOC)

`sh -c <command>` with production-grade process handling:

- `SysProcAttr{Setpgid: true}` and `cmd.Cancel = syscall.Kill(-pid, SIGTERM)` so cancellation tears down the **entire process group**, not just the direct child.
- `cmd.WaitDelay = 5 * time.Second` before Go closes pipes and SIGKILLs.
- **Both pipes drained to EOF before `cmd.Wait()`**, with a comment explaining that `Wait` closes the pipe read ends and racing it drops buffered output as "file already closed."
- Scanner buffer raised from bufio's 64 KiB default to 1 MiB (`maxScanLineSize`) for tools emitting single-line JSON (terraform `-json`, ansible callbacks).
- On scan error, emit a diagnostic line and `io.Copy(io.Discard, r)` to drain the rest, so the child can never deadlock writing into a full unread pipe.
- **Non-zero exit is a result, not an error** — `ApplyResult{ExitCode, Duration, Err}`; `Err` is reserved for failure to start or context cancellation.
- Environment: `os.Environ()` + workspace `env` + CLI `--env` overrides + `ZHI_WORKSPACE`, `ZHI_ENABLED_COMPONENTS`, `ZHI_DISABLED_COMPONENTS` (sorted for determinism).
- `RunPreChecks` (`:190`) runs gating commands sequentially with the same workdir/env, aborting on the first non-zero exit.

**Copy this file's subprocess handling essentially line for line.** Every subtlety in it is a fixed bug.

### 4.4 Environments, targets, diffs

**Targets exist.** `ApplyConfig` (`internal/core/workspace.go:70-118`) supports a simple form (top-level `command`/`workdir`/`env`/`timeout`/`pre-check`/`pre-export`) and named targets (`targets: {default: {...}, destroy: {...}}`), resolved by `ResolveTarget(name)`.

**Environments do not exist.** There is no dev/staging/prod concept anywhere, no cluster or kube-context notion, no per-environment value overlay. Components are the only axis of variation.

**Drift detection exists and is the closest thing to GitOps in the repo.** `internal/core/drift.go` (179 LOC) re-renders every workspace export template in dry-run mode and compares against the files on disk, returning `DriftCheckResult{Drifted, InSync, Errors, Warnings}` with `HasDrift()`/`HasErrors()`. `internal/core/diff.go` (273 LOC) implements LCS-based unified diff with 3 lines of context and a `/dev/null` new-file form. `zhi drift` is a CLI command (`internal/cli/drift.go`).

**Generalize drift from "rendered vs. files on disk" to "desired vs. live cluster state" and it becomes the spine of the new product.**

---

## 5. Store layer

27-method interface (`pkg/zhiplugin/store/plugin.go`, `store.proto` 319 lines) spanning:

- **Capabilities** — `VersioningMode{None,Tree,Value}`, `EncryptionStatus{None,Supported,Active}`, `Auth bool`, `AccessControl bool`.
- **Auth** — `AuthMethods`, `Login`, `LoginInteractive` (returns `InteractiveChallenge{ChallengeID, AuthURL, ExpiresAt}`), `LoginInteractiveCallback`. A local callback server lives at `internal/core/authcallback/server.go` (118 LOC) for the OIDC redirect. `internal/core/session.go` (173 LOC) manages sessions; `Engine` auto-logs-in from workspace options at startup (`engine.go:78-89`).
- **Tree ops** — `ListTrees`, `DeleteTree`, `GetValues(id, paths)`, `PutValues(id, values, *PutOptions)`, `DeleteValues`. Path-explicit reads avoid recursive traversal — a good design choice for Vault.
- **Versioning** — tree-level and value-level, each with list/get/rollback/delete (8 methods).
- **Encryption** — `InitEncryption`, `RotateEncryption`.
- **Access control** — `GrantAccess`, `RevokeAccess`, `ListAccess` with `Permission{Path, Actions[]}` where `Action ∈ {Read, Write, Delete}`.

**Optimistic locking is real.** `Value.Version` round-trips from reads, and `PutOptions{CASVersion, CASVersions map[string]string}` implements tree-level and per-path check-and-set.

**Providers** (`pkg/providers/store`, 2,768 LOC): Vault (`vault/vault.go` 1,277 + `httpclient` 190 + `client.go` 39), a Vault credential/policy manager (`vaultmanager`, 769), and a plaintext JSON file fallback (`jsonfile`, 293). The engine **silently installs the plaintext fallback** when no store is configured (`engine.go:66-74`), logging:

> `values will be stored in PLAINTEXT on disk — do not use for secrets in production`

### 5.1 Relevance when Git is the store of record

**Almost none of it survives.**

| Store feature | Git equivalent | Verdict |
|---|---|---|
| Tree/value versioning | `git log`, commit SHAs | **Drop.** Git is better — it carries authorship, message, review. |
| Rollback | `git revert` | **Drop.** |
| CAS / optimistic locking | "validate against the SHA you read" | **Adapt.** Re-express, don't reimplement. |
| Access control (grant/revoke/list) | Branch protection, CODEOWNERS | **Drop.** |
| `ListTrees` / `DeleteTree` | Repository/directory | **Drop.** |
| `Capabilities` negotiation | — | **Drop.** Exists only to paper over heterogeneous backends. |
| Encryption at rest | SOPS / age / sealed-secrets | **Adapt.** |
| Vault client + `store.writeonly` label | Secret *references* in values files | **Keep the seed.** |
| Interactive OIDC login + callback server | Cluster / secret-manager auth | **Keep if the tool talks to a cluster.** |

**What must survive:** a GitOps values file must never hold a plaintext secret. It holds a **reference** — `vaultRef: kv/data/prod#db_password`, a SOPS-encrypted blob, or an External Secrets reference. The Vault HTTP client and the `store.writeonly` label concept are the seeds of that feature; the 27-method interface around them is not.

---

## 6. Tests and CI conventions worth keeping

138 test files, 42,290 LOC — near 1:1 with production code.

**Conventions:**
- **`go test -race -count=1` everywhere**, no exceptions. `-count=1` defeats the result cache so flakes surface instead of hiding.
- 50 files use table-driven subtests (`tests := []struct{...}` + `t.Run`). Only 10 use `t.Parallel()` — an easy win the rewrite should take from the start.
- `testdata/` fixture directories for parsers (`internal/core/testdata`, `pkg/providers/config/structuredfile/testdata`, `examples/zhi-config-ansible/testdata`).
- Plugin tests use in-process gRPC via `goplugin.TestPluginGRPCConn` — no process spawning in unit tests.
- Web UI tests use a `startTestServer(t)` helper binding port 0.
- `errorMockController` wraps `mockController` for systematic error-path coverage of the 25-method interface.
- Benchmarks use `b.Loop()` (Go 1.25+).

**CI** (`.github/workflows/ci.yml`), five jobs:
1. **Lint** — runs `make fmt` then fails if `git diff --name-only` is non-empty. Stricter and clearer than `gofmt -l`. Then `go vet ./...` and `golangci-lint-action@v9` (v2, all default linters except `errcheck`).
2. **Test** — matrix ubuntu + macOS, `gotestsum --junitfile junit.xml --format testdox -- -race -count=1 -coverprofile=coverage.txt -timeout 10m ./...`.
3. **Build** — cross-compile all three binaries for linux/darwin × amd64/arm64 with `CGO_ENABLED=0`.
4. **Proto Check** — `make proto-check` fails if generated `*.pb.go` is stale. Keep the equivalent for any codegen.
5. **Integration** — builds binaries, runs `test/` if it contains test files (currently empty).

`make check` = fmt + vet + lint + test, the single local gate.

**Add for the rewrite:** golden-file tests over real Kubernetes and Compose fixtures (including intentionally-invalid ones), and a policy conformance suite expressed as a table of `rule → manifest → expected findings`, runnable in CI and shippable to users as the contract for custom rules.

---

## 7. Carry-forward recommendations

### 7.1 Concept table

| Concept | Verdict | Reason |
|---|---|---|
| Severity triad (Info/Warning/Blocking) | **Keep** | Right granularity; maps to CI gate vs. advisory |
| Cross-value validation via full-tree read access | **Keep (concept)** | Essential for cluster policy; reimplement as one whole-document pass, not per-path |
| `TreeReader` read-only seam | **Keep** | Clean interface; lets rules be pure functions |
| `ComponentManager` dependency graph, cycle detection, cascade disable | **Keep** | Best code in repo; maps to overlays / profiles / subcharts |
| Drift detection (`drift.go` + `diff.go`) | **Keep and promote to core** | Already the GitOps primitive; extend to live cluster state |
| Subprocess streaming in `apply.go` | **Keep verbatim** | Process groups, WaitDelay, pipe-drain ordering, 1 MiB scanner all hard-won |
| Pre-check gating commands | **Keep** | Natural home for `kubectl diff` / `docker compose config` |
| Named apply targets | **Keep** | Extend into environments (dev/staging/prod) |
| Inline mutate-validate-revert UX | **Keep** | Live linting with zero duplicated rule logic |
| Severity-grouped validation page | **Keep** | Add source-location links |
| Schema/metadata-driven editor selection | **Adapt** | Replace bespoke `labels` registry with JSON Schema + `x-` hints |
| Server-rendered templates + htmx + middleware chain | **Keep** | No build step; CSRF/CSP/ETag/SSE interactions already solved correctly |
| SSE apply log pane (flusher unwrap, `event: output`/`done`) | **Keep** | Maps directly to streaming `kubectl apply` output |
| Binary/bundle integrity audit (`launch/audit.go`) | **Adapt** | Same hygiene applies to downloaded policy bundles |
| OCI artifact client + custom media types | **Keep** | Right vehicle for distributing policy/schema bundles |
| Digest-pinning lockfile | **Keep** | Exactly what CI reproducibility needs |
| Air-gap OCI export/import | **Defer** | Solid, well-hardened code; only if regulated customers appear |
| `-race -count=1`, proto-check, fmt-diff CI, testdata fixtures | **Keep** | Cheap, high-signal |
| Secret *references* (Vault client, `store.writeonly`) | **Adapt** | Values files must reference, never contain, secrets |
| Interactive OIDC login + local callback server | **Adapt** | Reuse if the tool authenticates to a cluster or secret manager |
| Flat slash-delimited path model | **Drop** | Cannot express list indices or camelCase Kubernetes keys |
| `[a-z]`-only path segment regex | **Drop** | Rejects valid K8s and Compose keys outright |
| `Val any` + hand-written type coercion (`convert.go`, `numeric.go`) | **Drop** | 330 LOC that a schema makes unnecessary; silent type-mismatch drops |
| Path carried in `Metadata["path"]` | **Drop** | Make Path, Rule ID, and source location first-class fields |
| `Value.Validators` (non-serializable closures) | **Drop** | Dead for external plugins; guarantees two divergent paths |
| Yaegi-interpreted Go as the policy language | **Drop, urgently** | Full-stdlib RCE from a config file; use CEL / Rego / JSON Schema |
| Four gRPC plugin types + 10,790 LOC generated stubs | **Drop** | Start in-process; add one extension point when a user demands it |
| JSON-over-protobuf value encoding | **Drop** | Three serialization layers, lossy for numbers |
| `transform.Plugin` + `ValidatePolicy` (before/after/both) | **Drop** | Three-way ordering knob for a two-hook pipeline nobody exercised |
| 27-method `store.Plugin` (versioning, CAS, ACL, encryption) | **Drop** | Git provides history, rollback, review, authorship |
| Plaintext JSON fallback store | **Drop** | Silently writes secrets to disk when misconfigured |
| `ui.Controller` (25 methods) × 3 frontends + MCP | **Drop to one** | Same CRUD re-marshalled four ways |
| TUI (4,536 LOC) | **Drop** | Web UI covers it; `RequiresTTY` forced it in-process anyway |
| `RequiresTTY` capability flag | **Drop** | Symptom of a boundary that shouldn't exist |
| Marketplace server + ratings + advisories + publisher tiers | **Drop** | Speculative social infrastructure; a static OCI index suffices |
| Sigstore signing stack (1,236 LOC) | **Drop or rebuild wired** | Unreachable from the install path today; shell out to cosign instead |
| `text/template` + Sprig as the K8s/Compose bridge | **Drop** | String interpolation with no schema awareness is the problem to solve |
| `fileACL`/`fileMode` side-effecting template funcs | **Drop** | Rendering with out-of-band effects on file writing |
| Meta-plugin SDK (delegate/merge/mirror) | **Drop** | Elegant solution to a problem the rewrite won't have |

### 7.2 Ten concrete lessons

1. **A flat string-keyed map is not a tree.** Every consumer — export (`TreeData.Nested`), web UI (`buildNestedTree`), components (prefix matching) — rebuilds nesting on the fly, and list indices remain inexpressible. Model the document structure once, natively, and let paths be derived views over it.

2. **Untyped `any` values cost more than a schema.** `convert.go` (192 LOC), `numeric.go` (138 LOC), a `core.semantictype` label, a third coercion in `LoadStoredTree`, and a silent `stored value has different type and won't be updated` warning — all downstream of not knowing the type. Kubernetes and Helm both ship schemas; use them.

3. **Never make a config file executable.** Yaegi with `stdlib.Symbols` turns a pull request into remote code execution. The code knows and says so. Declarative policy (CEL/Rego/JSON Schema) is safer, cacheable, explainable, diffable, and statically analyzable.

4. **Put the path in the finding.** Omitting `Path` from `ValidationResult` forced a `Metadata["path"]` convention that the web UI honors and the TUI silently ignores. A finding needs path, rule ID, source file, line, and a fix hint — anything less and the CI output is not actionable.

5. **Don't ship the whole corpus per item.** Per-path validation that serializes the entire tree into each of N gRPC calls is O(N²) in wire bytes before any real work happens. Validate the document set in one pass.

6. **A process boundary costs ~11,000 LOC.** Four plugin types produced 10,790 lines of generated stubs plus eight hand-written client/server pairs — and still could not host a TUI, the one plugin type users most want to swap. Earn that boundary with a real second implementor before paying for it.

7. **Half-built security is worse than none.** A 1,236-line Sigstore integration with zero callers means `requireSignatures: true` rejects everything while the default trusts everything, and the recorded signer is the string `"keyless-identity"`. Wire the chokepoint first; add ceremony later.

8. **Name things what they are.** `storage/sqlite.go` is a JSON file. `SyncRule.Schedule` is typed as cron and only understands `*/N`. `RetentionPolicy` is documented as LRU eviction and has no implementation and no references. Each is a trap for the next reader and a lie to the operator.

9. **One abstraction, one frontend, until proven otherwise.** The `UIDriver`/`UIController` seam was the right idea and genuinely kept business logic out of transport code. Three concrete implementations of its 25 methods (plus MCP) was four times the maintenance for one product, and the marketplace feature set alone had to be built four times.

10. **The best code here solved concrete, observed problems; the worst solved imagined ones.** Process-group teardown, pipe-drain ordering, 1 MiB scanner buffers, ETag-versus-CSP-nonce exclusion, `Unwrap()`/`Flush()` chains, component cycle detection, mandatory-dependency pre-enabling — every one carries a comment explaining the bug that motivated it, and every one is worth keeping. Ratings, advisories, air-gapped retention policies, value-level ACLs, WASM plugin plans in `TODOS.md` — none were driven by a user. Build toward observed failure.

### 7.3 Suggested shape of the rewrite (implied by the above)

- **Domain:** parse Kubernetes manifests and Compose files into a typed, schema-aware document model with stable identity (`file + GVK + namespace/name` for K8s; `file + service` for Compose) and JSON Pointer addressing including list indices. Keep source position on every node so findings can cite file:line.
- **Policy:** declarative rules (CEL or Rego) evaluated in one pass over the whole document set, with cross-document queries. Rules ship as OCI bundles, pinned by digest in a lockfile.
- **Severity + components:** reuse the triad and the component dependency graph verbatim, re-addressed to manifest identity.
- **GitOps loop:** `validate` (policy over the repo) → `diff`/`drift` (rendered desired vs. live cluster) → `apply` (reuse `apply.go` subprocess handling) with named environments replacing named targets.
- **UI:** one server-rendered htmx web app reusing the middleware chain, the mutate-validate-revert inline linting, the severity-grouped findings page, and the SSE apply log pane. Schema-driven form widgets. No TUI, no plugin-marketplace surface.
- **Storage:** Git. Secrets by reference only.
