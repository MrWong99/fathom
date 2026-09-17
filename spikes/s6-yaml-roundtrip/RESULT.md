# S6 result: PASS (substitute inputs)

Date: 2026-09-17. Inputs: a synthetic four-layer fixture modelled on a Bitnami
redis deployment (common, HA variant, eu-prod, renovate-owned images) instead
of the company's real layered files; rerun on those when they arrive, in
particular on any Helmfile `*.yaml.gotmpl` layers (see open questions).

## Versions

| Component | Version |
|---|---|
| Go | go1.27.1 linux/amd64, `CGO_ENABLED=0` build verified |
| gopkg.in/yaml.v3 | v3.0.1 |

## Measurements against the pass criterion

Every edit is a single byte range `[Start, End)`; the test harness verifies
`out[:Start] == src[:Start]` and `out[Start+len(New):] == src[End:]`, so the
untouched regions are byte-identical by construction, then re-parses the file
and folds it to confirm the edit took effect at the pointer.

| Edit | Layer | Span replaced | Untouched bytes | Result |
|---|---|---|---|---|
| `/master/persistence/size` 20Gi → 50Gi | eu-prod | 4 → 4 bytes | 471 | in place |
| `/auth/existingSecret` with trailing `# ref://…` comment | eu-prod | 13 → 16 | 462 | comment and its column kept |
| `/image/tag` `"8.2.1"` with `# @schema` marker | common | 7 → 7 | 1481 | double quotes and marker kept |
| `/replica/resources/limits/cpu` `'1'` | common | 3 → 3 | 1485 | single quotes kept |
| `/master/persistence/storageClass` `""` → `1000` | common | 2 → 6 | 1486 | re-quoted as `"1000"` so it stays a string |
| `/commonAnnotations/mode` `"NO"` | common | 4 → 5 | 1484 | quoting kept (YAML 1.1 trap avoided) |
| `/metrics/enabled` true → false, `/replica/replicaCount` 3 → 5 | eu-prod | 4 → 5, 1 → 1 | 471, 474 | typed scalars |
| `/replica/tolerations/0/value` (sequence index) | common | 5 → 5 | 1483 | in place |
| `/master/podLabels/app.kubernetes.io~1part-of` (escaped key) | common | 4 → 8 | 1484 | in place |
| `/replica/resources/limits/memory` (path missing below `/replica`) | eu-prod | insert 3 lines | all | nested block inserted after the last `replica` entry, before the blank line and `metrics:` |
| `/image/registry` (top-level key missing) | eu-prod | insert 2 lines | all | appended after the last entry |
| `/podSecurityContext/fsGroup` after a literal block scalar and before the file's trailing comment | common | insert 2 lines | all | lands between the scalar and the trailing comment |
| `/master/persistence/enabled: null` (delete a common key from eu-prod) | eu-prod | insert 1 line | all | folded stack drops the key and attributes the deletion to `env:eu-prod` |
| `/initScript` literal block scalar | common | 4 lines → 4 lines | all | whole `\|` block replaced, content indentation 2, trailing comment kept |
| `/commonAnnotations/description` folded `>-` scalar → one line | common | 3 lines → 1 | all | neighbour `mode:` line untouched |
| `/replica/tolerations` whole list | common | 4 lines → 3 | all | subtree re-encoded, `metrics:` section untouched |
| `Remove /replica/tolerations` (drop the null override) | eu-prod | 1 line removed | all | common toleration returns in the fold |
| write the current value | eu-prod | 4 → 4 identical | all | file unchanged (idempotent) |
| insert into an empty layer file | new | append | all | `master:\n  persistence:\n    size: 5Gi` |

Refusals (by design): edits whose pointer resolves through an alias
(`/master/resources/limits/cpu` via `*resources`) or a merge key
(`/replica/resources/requests/cpu` via `<<: *resources`) return `ErrAlias`
naming the anchor; edits to the renovate-owned layer return `ErrMachineOwned`;
extending an empty flow container (`extraFlags: []`) and removing the last key
of a mapping are refused with an explanation.

Fold: 29 effective leaves over four layers; `/replica/tolerations` reported
as deleted by `env:eu-prod`; `/image/tag` attributed to the machine layer;
alias- and merge-key-provided leaves are attributed to `common` with the
anchor's position (`10:10`, `7:10`), which is where a finding must point.

## The naive alternative, measured

Decoding to a node tree and encoding again with yaml.v3 (indentation detected
from the file) changes **10 of 65 lines** of `common/values.yaml` and **2 of
20** of `envs/eu-prod/values.yaml`. Every line with an aligned trailing comment
is re-spaced to a single space, and the anchored mapping is re-laid out. yaml.v3
also has no line-width setting, so long plain scalars fold at 80 columns. Whole
document re-encoding therefore cannot meet the criterion even on a tidy file;
the product splices bytes.

## Findings that change the design

1. **Confirms design 3.1 and 3.6**: `internal/values` on yaml.v3 nodes with
   positions is enough for `writeTo`; the write path is "one byte range per
   edit", never "re-encode the document". Multiple edits to one file are
   applied from the highest offset down or re-located after each apply.
2. **Alias rule for the form.** When the target layer holds the leaf through
   an alias or merge key, the UI must not edit in place; the affordance is
   "override with a literal in `<env layer>`" (which the insert path does),
   and the effective-values explorer points at the anchor. Design 3.6's
   "currently from / will write to" line gains a third state: "shared via
   anchor `*resources` in common/values.yaml".
3. **Flow-style containers are the residual.** `key: []` and `key: {}` cannot
   be extended in place without re-encoding that container; the product
   re-encodes only that container (bounded loss, one line) or asks the user
   to expand it. Inserted sequences follow yaml.v3's indented `- ` style
   regardless of the file's existing style.
4. **Positions are byte columns.** yaml.v3 reports byte-based columns; the
   fixture is ASCII. Files with multi-byte characters before a value on the
   same line need a rune-to-byte translation for the finding's `col`; the
   splice itself stays correct because it uses byte offsets.
5. **Open question for the company files**: Helmfile and some Argo setups
   template the values files themselves (`values.yaml.gotmpl`, `{{ .Values }}`
   inside YAML). Those are not YAML and cannot be edited this way; the layout
   reader must classify them as read-only (or render them first) and the
   form must write to a plain YAML layer above them. S6 cannot answer this on
   substitute inputs.

Design impact: sections 3.6 (alias state, flow-container residual) and 5.1
(gotmpl layers are read-only). No change to the domain model, the pipeline or
the roadmap.
