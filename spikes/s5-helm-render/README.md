# S5: Helm 4.3 SDK render with snapshot Capabilities and lookup

## Goal

Show that the product can render a Helm chart with the real Helm v4.3.0 SDK
against a cluster **snapshot** instead of a live cluster, that the render is
byte-identical across runs once the entropy- and clock-dependent template
functions are replaced through `CustomTemplateFuncs`, and that a 2020-12
`values.schema.json` with `x-fathom-*` keywords and a standard `$schema` is
accepted by `helm lint`, `helm template` and Argo CD's Helm without warnings and
without network access. Record how Helm 4 validates a parent schema against
subchart values.

## Pass criterion (tracker)

`helm lint`/`helm template` accept the file with no warning and no network;
Argo renders it; two renders byte-identical

## Method

- `render/render.go`: `Snapshot{KubeVersion, APIVersions, Objects}` builds
  Helm `Capabilities` and answers `lookup`; `Render` coalesces and
  schema-validates values exactly as `helm template` does
  (`ToRenderValuesWithSchemaValidation`), then runs `engine.Engine` with the
  snapshot-backed `lookup` and the deterministic replacements installed
  through `CustomTemplateFuncs`. `RenderDesignPath` is the literal design path
  (`RenderWithClientProvider` over client-go's fake dynamic client) kept for
  comparison.
- `render/determinism.go`: seeded SHA-256 counter stream behind `randAlphaNum`,
  `randAlpha`, `randNumeric`, `randAscii`, `randBytes`, `randInt`, `shuffle`,
  `uuidv4`; fixed `now`, `ago`, `date`, `htmlDate`; Ed25519 certificates for
  `genCA*`, `genSelfSignedCert*`, `genSignedCert*`, `genPrivateKey`;
  shape-preserving `bcrypt`/`htpasswd`.
- Fixtures: `testdata/charts/parent` (+ `charts/child`), a synthetic chart that
  prints every Capabilities/lookup/entropy function into a ConfigMap and ships
  a 2020-12 schema using `$ref`/`$defs`, `unevaluatedProperties`,
  `dependentRequired`, `if`/`then` and `x-fathom-*`; the subchart ships its own
  schema with `additionalProperties: false`. `testdata/charts/redis` is Bitnami
  redis 28.2.1 (Apache-2.0, `oci://registry-1.docker.io/bitnamicharts/redis`,
  digest `sha256:94293bfd25955a765ee6857c717e0d39628d8273c02af50974ad180bd2cbdf78`),
  vendored unchanged as the **substitute input** until company charts arrive;
  `testdata/redis.values.schema.json` is the contract the tests add to a copy
  of it.
- Oracles: `helm` v4.3.0 in PATH and the Helm binary Argo CD v3.5.3 bundles
  (v4.2.1, fetched and checksum-verified against Argo's own checksum file by
  `hack/fetch-argo-helm.sh`), both run under `unshare -rn` (no network) with
  Argo's exact `helm template` argument list from `util/helm/cmd.go`.

Run: `go test -race -count=1 -v ./...`. The Argo parity test needs
`hack/fetch-argo-helm.sh v3.5.3 ~/.cache/fathom-spikes/argo-v3.5.3` once (or
`S5_ARGO_HELM=/path/to/helm`); it skips otherwise.
