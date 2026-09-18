# S13 fixture: snapshot-as-bundle for Troubleshoot analyzers

`snapshot/` is a partial fathom snapshot in the design §3.5 layout, exported
read-only from the kind oracle `kind-fathom-oracle` (server v1.37.0) with
kubectl v1.36.4 on 2026-09-17 23:44 UTC. Only the four layers the S13 analyzers read are
populated. Objects are stored as `kubectl get -o yaml` prints them (one object
per file, `managedFields` already dropped by kubectl); nothing else was edited,
so `resourceVersion`, `uid`, `creationTimestamp` and node heartbeat times are
the oracle's real values.

| Snapshot path | Layer (§3.5) | Content | Troubleshoot bundle path the adapter must synthesise |
|---|---|---|---|
| `manifest.json` | — | §3.5 manifest; `serverVersion` is the gitVersion string; `layers[].digest` is sha256 over the layer's files concatenated in `LC_ALL=C sort` order | — |
| `discovery/version.json` | discovery [schema] | raw `GET /version` body (`k8s.io/apimachinery/pkg/version.Info`) | `cluster-info/cluster_version.json` = `{"info": <version.json>, "string": info.gitVersion}` |
| `catalogs/storageclasses/<name>.yaml` | catalogs [policy] | one `storage.k8s.io/v1` StorageClass per file | `cluster-resources/storage-classes.json` = `StorageClassList{items: all files}` |
| `crds/<name>.yaml` | crds [schema] | one `apiextensions.k8s.io/v1` CustomResourceDefinition per file, `x-kubernetes-validations` intact | `cluster-resources/custom-resource-definitions.json` = `CustomResourceDefinitionList{items: all files}` |
| `nodes/<name>.yaml` | nodes [full] | one `v1` Node per file (status.allocatable/capacity, labels, taints, conditions) | `cluster-resources/nodes.json` = `NodeList{items: all files}` |

Files present:

- `catalogs/storageclasses/standard.yaml` — the kind default class
  (`storageclass.kubernetes.io/is-default-class: "true"`, provisioner
  `rancher.io/local-path`).
- `crds/blobs.spike.fathom.dev.yaml`, `crds/gadgets.spike.fathom.dev.yaml`,
  `crds/widgets.spike.fathom.dev.yaml` — the S1/S2 spike CRDs. The kyverno.io
  and wgpolicyk8s.io CRDs installed by S3 are deliberately not exported
  (recorded as `degraded` on the `crds` layer in `manifest.json`).
- `nodes/fathom-oracle-control-plane.yaml` — the single kind node:
  allocatable cpu 32, memory 65507964Ki, pods 110, arch amd64, taint
  `node-role.kubernetes.io/control-plane:NoSchedule`.

`analyzers.yaml` holds two `troubleshoot.sh/v1beta2` `Analyzer` documents:
`s13-pass` (every analyzer must yield `IsPass`) and `s13-fail` (every analyzer
must yield `IsFail`). Both are evaluated by `analyze.Analyze` with `getFile`
served from the table above; no files are written to disk.

Regenerate with (read-only against the oracle):

```sh
k() { kubectl --context kind-fathom-oracle "$@"; }
k get --raw /version > snapshot/discovery/version.json
k get storageclass standard -o yaml > snapshot/catalogs/storageclasses/standard.yaml
for c in blobs gadgets widgets; do k get crd $c.spike.fathom.dev -o yaml > snapshot/crds/$c.spike.fathom.dev.yaml; done
k get node fathom-oracle-control-plane -o yaml > snapshot/nodes/fathom-oracle-control-plane.yaml
```
