# S16 result: PASS on the criterion, with one identity-rule change (substitute inputs)

Date: 2026-09-17. Input: a synthetic three-file Compose project with `.env`
instead of a company project; rerun when one is available.

## Versions

| Component | Version |
|---|---|
| Go | go1.27.1 linux/amd64, `CGO_ENABLED=0` build verified |
| github.com/compose-spec/compose-go/v2 | v2.15.0 (no replace directives) |
| github.com/santhosh-tekuri/jsonschema/v6 | v6.0.3 |
| gopkg.in/yaml.v3 | v3.0.1 (positions only) |
| YAML modules pulled in by compose-go | go.yaml.in/yaml/v4 v4.0.0-rc.4 (direct), go.yaml.in/yaml/v3 v3.0.5 (indirect) |

## Measurements

| Check | Result |
|---|---|
| Pass 1 keeps `${VAR}` in typed fields | `services.api.image` is `${REGISTRY:-docker.io}/shop/api:${API_TAG:?…}` after the model load; override and include are already merged |
| `ExtractVariables` | 10 variables, 3 required (`API_TAG`, `DATA_DIR`, `DB_PASSWORD`), 5 with defaults; `API_PORT` is absent because `compose.override.yaml` replaces the `ports` list with `!override`, which is compose's merge semantics |
| Usage positions | every variable mapped to `{file, service, line, col}` across the three files, including the included one (the loader deletes `include:` from the model; the raw file is read for it) |
| `values.schema.json` over the variables | 2020-12, `required` from `:?`, `default` from `:-`, `x-fathom-secret` for `*PASSWORD*`, `x-fathom-compose.usages` per variable; 3789 bytes |
| Pass 2 with `.env` only | fails exactly like `docker compose config`: `required variable API_TAG is missing a value: API_TAG must be set` (both usages named) |
| Pass 2 with `API_TAG` supplied | project loads; `!override` ports applied; profile `*` includes `worker`; included `metrics` present |
| Findings | 12, all twelve ids unique, golden file stable across runs |

Findings by rule on the fixture:

| Rule | Severity | Count | Anchor |
|---|---|---|---|
| `variable-required-unset` (from the schema's 2020-12 output unit) | Blocking, `wouldFailAt: schema` | 2 (api, worker) | `instanceLocation ""`, `keywordLocation /required`, `valuesPointer /API_TAG`, `source compose.yaml:12:44` |
| `variable-unset-empty` (compose substitutes `""` and only logs a warning) | Blocking, `runtime` | 1 (`SMTP_HOST`) | `keywordLocation /properties/SMTP_HOST` |
| `variable-default-used` | Warning, `runtime`, `proposedValue` = the default | 4 | one per usage service, including the included file |
| `extension-unknown` (`x-depends-on`; `x-fathom-*` allow-listed) | Info, `developer` side | 1 | `instanceLocation /x-depends-on` |
| `deploy-swarm-only` (`placement`, `update_config`, `mode`, `endpoint_mode`; `replicas` and `resources` are honoured by compose and not flagged) | Warning, `unobserved: [host:engine]` | 4 | `/services/<svc>/deploy/<key>`; suppressed when the host is Swarm |

JSON keys used by the twelve findings: `admissionContext, engine, fidelity,
id, instanceLocation, keywordLocation, layer, lineage, message, policyRef,
proposedValue, resource, ruleId, severity, side, source, valuesPointer,
wouldFailAt`. All are section 2.4 fields; `resource` uses the
`{file, service}` variant. **No field was added.**

## Findings that change the design

1. **Finding identity (design 2.2) must include `keywordLocation` and
   `valuesPointer`.** `hash(engine, ruleId, resourceId, instanceLocation)`
   collides for JSON Schema output units that anchor at the parent instance:
   `required` (two missing variables in one service) and defaulted
   properties both have `instanceLocation ""`. The same collision hits any
   Kubernetes values-schema finding with two missing required keys. The spike
   hashes the six-tuple; no field changes. This is the one domain-model rule
   change and it applies to every engine, not only Compose.
2. **Compose is silent where fathom is strict.** compose-go interpolates an
   unset variable without default to `""` and emits only a logrus warning
   (`The "SMTP_HOST" variable is not set. Defaulting to a blank string.`),
   not a structured result; fathom's `variable-unset-empty` is the structured
   form. The design's severities (unset → Blocking, defaulted → Warning) are
   implemented as written; consider Info for the defaulted case, since a
   default is the author's intent (owner decision, not measured here).
3. **Variable set follows compose's merge, positions follow the raw files.**
   `!override`/`!reset` and `include` change what is effective; the raw scan
   keeps a usage the merge removed (`API_PORT`). The product should map
   usages through the merged model's paths (`services.api.ports[0]`) rather
   than raw lines, or mark raw-only usages as inactive in the editor.
4. **The deployer mapping is an explicit input.** `cli.WithOsEnv` was not
   used; `.env` plus the UI mapping are passed through `cli.WithEnv`. That is
   what keeps `Validate` pure for Compose (design 0 item 5).
5. **Three YAML libraries in one binary.** compose-go v2.15.0 depends on
   `go.yaml.in/yaml/v4` (a release candidate) and transitively on
   `go.yaml.in/yaml/v3`; Helm and S6 use `gopkg.in/yaml.v3`. Acceptable for a
   static binary, but `internal/values` should standardise on one module, and
   `gopkg.in/yaml.v3` (frozen at v3.0.1 since 2022) is the wrong one to freeze
   on; `go.yaml.in/yaml/v3` has the same API.
6. **Two passes are enough; a third is not needed.** The design's worry (C2
   minor) that typed fields would reject uninterpolated strings holds only for
   `LoadWithContext`; `LoadModelWithContext` returns the merged map before
   typing, so pass 1 needs no typed model at all.

Design impact: section 2.2 (finding identity tuple), section 6 (severity of
the defaulted case is an owner decision; usage mapping through the merged
model), section 3.1 (one YAML module). Section 2.4 unchanged.
