# S6: yaml.v3 round-trip over layered values files with `writeTo`

## Goal

Show that fathom can fold a stack of values layers (anchors, aliases, merge
keys, comments, markers, block scalars, `null` deletes, a machine-owned layer)
into effective values with the source layer of every leaf, then write one edit
into a chosen layer (`writeTo`) so that every byte outside the edited span is
untouched and the edit lands in the intended file.

## Pass criterion (tracker)

Byte-identical untouched regions; edits land in the intended file

## Method

- `values/values.go`: `Layer` (raw bytes + yaml.v3 node tree + owner),
  RFC 6901 pointers, `Effective` with Helm's coalesce semantics (maps merge,
  scalars and lists replace, `null` in a higher layer deletes; aliases and
  merge keys resolved by yaml.v3's decoder) recording layer and position per
  leaf, and `Reencode`, the naive decode-then-encode alternative that the
  tests measure.
- `values/write.go`: `Set(layer, pointer, value)` and `Remove` return an
  `Edit{Start, End, Old, New}` byte range computed from yaml.v3 node positions
  (`Line`, `Column`, `LineComment`, `Style`) and the raw source: single-line
  scalars are replaced inside their line with quoting style and trailing
  comment kept; block scalars, folded scalars and subtrees are replaced line
  range for line range; missing paths are inserted under the deepest existing
  mapping with the file's indentation; edits through an alias or merge key
  and edits to machine-owned layers are refused.
- Fixture `testdata/layers/`: `common/values.yaml` (modeline, marker comments,
  anchor + alias + merge key, quoted and plain scalars, empty flow containers,
  folded and literal block scalars, trailing comment), `variants/ha.yaml`,
  `envs/eu-prod/values.yaml` (overrides, a `null` delete with a comment),
  `envs/eu-prod/images.yaml` (renovate-owned). **Substitute input** until the
  company's layered files arrive.
- Every write test checks: prefix and suffix of the file are byte-identical,
  the file still parses, and the effective value at the pointer is the one
  written.

Run: `go test -race -count=1 -v ./...`.
