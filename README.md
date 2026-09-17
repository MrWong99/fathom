# fathom

**fathom is the PR check that already knows your cluster.**

To fathom something is to understand it thoroughly; a fathom is the depth a
sailor sounds before entering the harbour. fathom does both for a deployment:
one static Go binary renders Helm, Kustomize and Docker Compose values with the
real engines, runs the target cluster's admission chain offline against an
imported snapshot of that cluster, tells you which field will fail at which
stage with what fidelity, and opens the pull request into the folder Argo CD or
Flux already watch. Git is the only write target. No in-cluster component, no
hosted control plane.

It is built for two people who rarely sit at the same desk: the developer who
writes the chart and declares what is configurable, and the consultant who
deploys it into a customer cluster and knows the environment. The same
artifacts serve a team where developers deploy themselves.

## Status

Pre-alpha. This is the ground-up successor to
[MrWong99/zhi](https://github.com/MrWong99/zhi); the research and design that
led here are in `docs/` (written under the working name zhi). Work starts with a
six-week spike sprint (`spikes/README.md`); no product code is written until the
calibration spikes and the practitioner survey have results.

## Documents

- `docs/brief.html`: the research and design brief (open in a browser).
- `docs/design/design.md`: Design v2, the current plan: the plugin-depth
  decision, the pipeline order, the snapshot format, the roadmap and the open
  decisions.
- `docs/research/landscape-report.md`: the 2026 landscape synthesis.
- `docs/research/current-codebase-audit.md`: what carries over from zhi.

## Build

```sh
make build        # static binary in bin/fathom
make check        # fmt + vet + lint + test
make spikes       # run every spike module
```

Requires Go 1.26+ and golangci-lint v2.

## License

MIT, see `LICENSE`.
