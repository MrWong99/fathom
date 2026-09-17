# S16: Compose two-pass load, variables to schema, three findings

## Goal

Load a Compose project the way design section 6 describes (compose-go, first
pass with interpolation, validation, consistency check and environment
resolution skipped; second pass full), turn `template.ExtractVariables` into a
2020-12 `values.schema.json` over the interpolation variables, and produce the
three findings named in the design (`${VAR:?}` unset, unknown `x-` key,
`deploy.*` outside Swarm) in the `pkg/report` shape of design section 2.4 with
`{file, service}` identity. Pass criterion: the finding record needs no new
field.

## Pass criterion (tracker)

No domain-model field changes

## Method

- `report/finding.go`: design section 2.4 transcribed as a Go struct; a test
  pins the field list by reflection, so any addition fails the criterion.
- `compose/compose.go`: `Load` runs pass 1 via `cli.ProjectOptions.LoadModel`
  with `SkipInterpolation`, `SkipValidation`, `SkipConsistencyCheck` and
  `SkipResolveEnvironment`, extracts the variables and scans the raw files
  (following `include:`) for `${VAR` positions and owning services; pass 2 is
  `LoadProject` with the deployer's mapping (`.env` plus explicit values, never
  the process environment) and profiles. `VariableSchema` emits the contract;
  `ValidateEnv` validates the mapping with jsonschema v6 and turns the 2020-12
  output units into findings; `Findings` adds unset-without-default and
  defaulted variables, unknown extension keys and Swarm-only `deploy` keys.
- Fixture `testdata/shop/`: `compose.yaml` (include, `x-` keys, `:?`, `:-` and
  bare variables, `deploy` with honoured and Swarm-only keys, a profile),
  `compose.override.yaml` (`!override`), `compose.monitoring.yaml` (included),
  `.env`. **Substitute input** until a company project is available.
- `testdata/findings.golden.json`: the twelve findings, regenerated with
  `go test ./... -args -update`.
