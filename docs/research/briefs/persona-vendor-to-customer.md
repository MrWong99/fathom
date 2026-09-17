# persona-vendor-to-customer

## Summary

The vendor-to-customer ("ISV ships software, someone else installs it in a cluster they own") problem has a mature reference implementation: **Replicated** (KOTS Config custom resource + Troubleshoot.sh preflights + support bundles + Compatibility Matrix + Embedded Cluster). It is the closest analogue to zhi's target and the only tool found that cleanly separates (a) "which values exist, types, defaults, conditions, regex validation" (Config), (b) "environment requirements evaluated against the *target* cluster before install" (Preflight collectors/analyzers with strict/blocking outcomes), and (c) a customer-facing admin UI. Its weaknesses: proprietary SaaS control plane ($2k–3k/mo base + $50–200/license/mo), heavy install-time runtime (admin console, MinIO/rqlite), preflights run *at install time in the customer cluster*, not offline against an imported snapshot, and the Config schema is a flat, string-typed form DSL, not JSON Schema.

Everything else splits into: **form-from-schema UIs** (Rancher questions.yaml, OLM CSV x-descriptors, Kubeapps `form:true` JSON-Schema extensions, Glasskube valueDefinitions, Palette schema.yaml, Massdriver JSON-Schema params) that stop at per-field validation and have no environment checks; **BYOC control planes** (Nuon, Distr, Omnistrate, Tensor9) that model tenant/install and inputs but treat the customer cluster as a black box beyond "prerequisites docs"; **GitOps/CD** (Argo CD, Codefresh—now Octopus—, Harness, Octopus) that surface Helm parameters and post-deploy object status but do no pre-push environment validation; and **spec languages** (Score, OAM, Crossplane XRD, Timoni/CUE) that give strong typed contracts but no cluster-snapshot validation.

Nobody found combines: developer-declared schema + cross-value rules + environment requirements → offline evaluation against an imported *pseudo cluster* → GitOps push. The offline primitives exist separately (`kyverno apply`, `gator test`, kubeconform, Troubleshoot analyzers run on a collected bundle) and can be composed. That gap is zhi's opportunity.

Where memory and web disagreed, web wins: Glasskube's package manager repo is **archived (2026-06-17)**, company pivoted to **Distr**; Kubeapps **archived 2025-08-25** (SAP maintenance fork); Codefresh **acquired by Octopus (Feb 2024)**; Helm **4.0.0 released 2025-11-12**; KOTS latest **v1.130.2 (2026-05-01)**; Timoni is active (**v0.29.0, 2026-08-03**), not dormant.

## Tools table

| Tool | URL | Status (verified) | What it does | Values schema format | Pre-install env checks | Deployer UI | Extensibility | Pricing/OSS | Relevance |
|---|---|---|---|---|---|---|---|---|---|
| Replicated KOTS + Embedded Cluster | docs.replicated.com | Active; KOTS v1.130.2 (2026-05); EC v2.18.1 (k8s 1.35); EC v3 **beta** (Helm-templated release files, `troubleshoot.sh/v1beta3`) | Vendor portal + customer admin console; Helm-native (HelmChart v2) or EC (k0s) installs; licensing, air-gap bundles, support bundles | `kots.io/v1beta1 Config`: groups→items; types bool/dropdown/file/heading/label/password/radio/text/textarea; `when`, `required`, `hidden`, `readonly`, `repeatable`, `validation.regex` (RE2); templating `repl{{ ConfigOption "x" }}`; mapped to Helm via `HelmChart.spec.values` / `optionalValues[].when` / `exclude` | **Yes**: Troubleshoot Preflight (collectors+analyzers, `strict: true` blocks deploy); also runs on upgrades (June 2026); `helm template \| kubectl preflight` for Helm-only installs | Admin console config screen (auto-generated from Config), preflight results page, Enterprise Portal | Custom collectors/analyzers (Enterprise plan), `runPod` collector = arbitrary container | Proprietary SaaS. Builders $2,000/mo + $50/license/mo; Business $3,000/mo + $200/license/mo; Enterprise custom; CMX $500/mo + cluster cost. Troubleshoot & KOTS are Apache-2.0 | **copy** (Config+Preflight split, outcomes model, support bundle), **integrate** (Troubleshoot analyzers run offline on a bundle) |
| Troubleshoot.sh | github.com/replicatedhq/troubleshoot | Active, Apache-2.0, ~587★, 2,045 commits | `kubectl preflight` / `kubectl support-bundle`; collectors→redactors→analyzers | n/a | 25+ analyzers: clusterVersion, storageClass, customResourceDefinition, ingress, secret, configMap, imagePullSecret, nodeResources, distribution, containerRuntime, registryImages, http, certificates, sysctl, jsonCompare/yamlCompare/textAnalyze, clusterPodStatuses… | CLI/TUI results, JSON output | Go lib; `runPod`/`exec`/`http`/`data` collectors | Apache-2.0 | **integrate**: embed as Go library; analyzers are the de-facto "environment requirement" vocabulary |
| Replicated Compatibility Matrix | docs.replicated.com/vendor/testing-about | Active | Ephemeral clusters (kind, k3s, EKS/GKE/AKS/OKE, OpenShift, VMs) for CI; per-minute billing | n/a | Tests real installs on N distros; **cannot** configure CNI/CSI/Ingress plugins | Vendor portal/CLI/GitHub Action | — | $500/mo + usage | compete-adjacent; zhi's "pseudo cluster" is the cheap offline counterpart |
| Kubeapps | github.com/SAP/kubeapps | **Archived** by VMware 2025-08-25; SAP fork = security/compat fixes only, "new features not guaranteed"; Chainguard fork 2025-11 | Web UI to browse Helm/Carvel repos and install with a form | values.schema.json + non-standard extensions: `"form": true`, `"render": "slider"`, `sliderMin/Max/Unit`, `hidden: {value, condition}` | None | Basic form + YAML editor | Plugins (Helm, Flux, Carvel) | Apache-2.0 | copy the `form`/`hidden` JSON-Schema-extension idea only; do not depend on |
| Rancher Apps & Marketplace | ranchermanager.docs.rancher.com | Active (SUSE) | Chart catalog UI with generated forms | `questions.yaml`: `variable,label,description,type(string/multiline/boolean/int/enum/password/storageclass/hostname/pvc/secret/cloudcredential),required,default,group,min/max,min_length/max_length,options,valid_chars,invalid_chars,show_if,subquestions,show_subquestion_if`; Jexl evaluation (differs from legacy Ember) | Only indirectly: `storageclass`/`pvc`/`secret` types read *live* cluster resources to populate dropdowns | Rancher UI | Chart-level only | Apache-2.0 | **copy** the cluster-aware field types (storageclass, secret, pvc pickers) |
| OpenShift OLM / OperatorHub | github.com/openshift/console …/descriptors | OLM v0 CSVs still consumed; OLM v1 GA since OCP 4.18 uses `ClusterExtension`, registry+v1 bundles still the packaging | Operator catalog; console auto-generates CR creation form | CRD openAPIV3Schema + CSV `specDescriptors[].x-descriptors` URNs: `podCount, resourceRequirements, booleanSwitch, checkbox, text, number, password, select:*, fieldGroup:*, fieldDependency:<path>:<value>, hidden, advanced, io.kubernetes:Secret/ConfigMap/…`; `statusDescriptors` (`podStatuses, conditions, phase, link`) | CSV `spec.minKubeVersion`, `installModes`, `customresourcedefinitions.required[]`, `nativeAPIs` — evaluated by OLM at install | OpenShift console form | Operator = code | Apache-2.0 | **copy** URN-style UI hints + `required` CRD/API declaration model |
| Glasskube (package manager) | github.com/glasskube/glasskube | **Archived 2026-06-17** (Apache-2.0, 3.5k★); company pivoted to **Distr** | K8s package manager w/ GUI, dependency mgmt | `PackageManifest.valueDefinitions`: type boolean/text/number/options; `constraints {required,minLength,maxLength,min,max,pattern}`; `metadata {label,description,hints}`; `targets[]` = JSON-patch or `valueTemplate` into Helm values/manifests; `dependencies` semver | None beyond dependency version constraints | GUI (archived) | Package repo | Apache-2.0 | copy `targets` (schema-field → chart-value patch mapping); status = dead |
| Distr (Glasskube) | github.com/glasskube/distr | Active, Apache-2.0, ~1.2k★ | OSS control plane for self-hosted/BYOC: hub (Postgres, Loki, OCI registry) + Docker/Helm agents in customer env; customer portal, licenses per customer, GitHub Action, MCP server; "compatibility reports" attachable to versions | Compose file or Helm chart + values template per version; per-deployment-target values | Not found (compatibility *reports* are vendor-published, not evaluated on target) | White-label customer portal | REST API, JS SDK | OSS self-host; hosted plans | **compete/integrate**: closest OSS to Replicated; lacks validation layer zhi would add |
| Palette (Spectro Cloud) | docs.spectrocloud.com/registries-and-packs/pack-constraints | Active | Cluster profiles composed of packs | `schema.yaml` per pack: `key.path: schema: '{{ required \| format "${string:/regex/}" }}'`, `${number:[5000-5005]}`, `${list:[A,B]}`, `password`, `ipv4`, `version`, `readonly`; `pack.json constraints.resources` (min CPU m / memory Mi / disk GB × replicaCountParamRef, scheduleType) | Resource constraints checked at profile/cluster submission; upgrade-path validation | Palette UI | Custom packs/registries | Proprietary | copy resource-constraint declaration form |
| Rafay | docs.rafay.co | Active | Fleet mgmt; cluster blueprints; custom app catalogs; self-service workflows | Blueprint/add-on values | Blueprint drift/policy at cluster level | Rafay console | Templates | Proprietary | low |
| Nirmata | nirmata.com | Active; "AI-powered enterprise platform for Kyverno" | Multi-cluster Kyverno policy mgmt/reporting | n/a | Policy evaluation is the product; `kyverno apply` for offline | Control Hub | Kyverno policies | OSS Kyverno + commercial | **integrate**: Kyverno CLI as pseudo-cluster policy engine |
| Octopus Deploy | octopus.com/docs/tenants | Active; owns Codefresh (Feb 2024) | Release/environment/tenant deployment; Kubernetes Live Object Status (2025.3+, orphan deletion 2026.3) | Variable set templates: string, sensitive, select, checkbox, certificate, account; tenant variables not snapshotted; scoping by env/tenant tag | None pre-deploy (Live Object Status is explicitly post-deploy) | Rich web UI | Steps, MCP server | Proprietary | **copy** tenant×environment×variable-template model |
| Codefresh GitOps | codefresh.io/docs | Active under Octopus | Argo-based GitOps with Form/YAML app editor | Argo `helm.parameters/values/valuesObject/valueFiles` | None | Form mode mirrors Argo fields, not chart schema | Argo | Proprietary + OSS Argo | low |
| Harness GitOps | developer.harness.io | Active; multi-source apps GA Feb 2026 | Argo CD wrapper | Argo helm source | None | Harness UI | — | Proprietary | low |
| Argo CD | argo-cd.readthedocs.io | Active | GitOps controller | `spec.source.helm.{parameters,valuesObject,values,valueFiles,ignoreMissingValueFiles,skipSchemaValidation}`; precedence `parameters > valuesObject > values > valueFiles > chart values.yaml` | Helm runs values.schema.json + `kubeVersion` at render; no cluster-requirement checks | "Parameters" tab (`--set` overrides only; known bug: does not show merged values; v3.4.1 regression #27773) | Config Mgmt Plugins | Apache-2.0 | **integrate** (zhi writes what Argo reads) |
| Helm values.schema.json ecosystem | helm.sh; losisin; dadav; helm-docs | Helm 4.0.0 (2025-11-12); Helm 3 bugfix EOL 2026-07-08, security EOL 2026-11-11; losisin v2.6.0; dadav active; helm-docs GPL-3.0 | Draft-07 schema validated on install/upgrade/lint/template (`--skip-schema-validation`); `Chart.yaml kubeVersion` semver | Annotation conventions: losisin `# @schema type: integer; minimum: 1; enum: [..]; pattern; required; nullable; $ref; hidden…`; dadav block `# @schema … # @schema`; helm-docs `# -- desc`, `# @default --`, `# @ignored`; Bitnami `## @param`; magicschema unifies all four | `kubeVersion` only | None (IDE completion) | — | OSS (MIT/Apache/GPL) | **integrate**: emit/consume values.schema.json as lingua franca |
| Score | github.com/score-spec/spec | Active, CNCF Sandbox, spec v1b1, Apache-2.0 | Workload spec → score-k8s / score-compose | `containers.*.variables`, `service.ports`, `resources.<name>.{type,class,params}`, `${resources.db.host}` placeholders | None by design ("workload-level properties" only) | none | Provisioners per platform | OSS | copy resource-dependency declaration (`type: postgres`) as *requirement* vocabulary |
| Crossplane XRD | docs.crossplane.io (v2.4) | Active | Platform APIs as CRDs | `openAPIV3Schema` (enum, default, required, CEL `x-kubernetes-validations`); v2 namespaced XRs by default | CRD validation only | none (kubectl/Backstage) | Composition functions | Apache-2.0 | copy CEL-on-schema for cross-value rules |
| Timoni | github.com/stefanprodan/timoni | Active: v0.29.0 (2026-08-03), Apache-2.0, 2k★ | CUE modules + bundles (per-env values.cue), OCI distribution | CUE `#Config` schema with constraints/defaults; `timoni mod vet` validates rendered resources against CRD-derived CUE schemas | Validates against **CRD schemas** (importable from cluster) — nearest "pseudo cluster" for schemas, not policies/quotas | CLI only | CUE | OSS | **copy/integrate**: CRD→schema import; bundle-per-env |
| Nuon | github.com/nuonco/nuon | Active, **AGPL-3.0**, 8k+ commits; Nuon Cloud / BYOC / self-hosted | BYOC control plane + in-customer runner; components: terraform/helm/container_image/job; sandboxes | TOML: `nuon.toml`, `inputs.toml` `[[input]] name, display_name, description, default, sensitive, type(string/number/bool/json/yaml/hcl), group`; referenced as `{{.nuon.inputs.x}}` in Helm values/TF vars; LSP + VS Code ext | Sandbox provisions infra; yaml/hcl input syntax validated early; no cluster-policy checks found; "policies" feature exists (blog) | Vendor + install UI | Components, actions, workflows | AGPL + commercial | **copy** install/input/sandbox model; AGPL blocks embedding |
| Omnistrate | docs.omnistrate.com | Active | Private-label control plane: SaaS/BYOC/air-gap; Compose `x-omnistrate-*`, Helm, Kustomize, operators; service plans/environments | API parameters per plan (e.g. `cloud_provider_native_network_id`) | Documented prerequisites (DNS, NAT, subnet tags, PrivateLink ports 8443–8506); no found pre-check tool | Customer + fleet UI | — | Proprietary; free tier; enterprise contact | compete (heavier) |
| Tensor9 | tensor9.com | Active, seed-funded (May 2025) | Compiles Terraform/CFN/Helm/Compose into customer-side "appliance"; "digital twin" mirrors deployed state for vendor observability | Vendor IaC | n/a | Vendor dashboard | — | Proprietary | copy the *twin* metaphor (zhi's pseudo cluster = twin of constraints, not state) |
| Massdriver | docs.massdriver.cloud/bundles | Active | Bundles: `massdriver.yaml` with JSON-Schema draft-07 `params`, `connections` (artifact defs), `ui` hints (immutability, ordering); TF/OpenTofu/Helm/Bicep steps | JSON Schema + UI annotations | Connection typing = dependency contract | Generated forms | OSS artifact-definitions, CLI | Proprietary SaaS | copy `connections` as typed env-dependency contract |
| Plural.sh | plural.sh | Active (OSS console/deployment-operator on GitHub) | Fleet GitOps with PR automation (self-service via generated PRs) | Service templates | None pre-PR | Console | PR automation pipelines | OSS + commercial | **copy** "self-service = tool opens PR" pattern |
| Porter / Qovery / Northflank BYOC / Coherence | vendor sites | Active PaaS-style BYOC (Porter $6/GB-RAM + $13/vCPU mgmt fee; Qovery sells no compute; Northflank BYOC into AWS/GCP/Azure/OCI/on-prem) | Deploy *your* app into *your* cloud; not ISV→customer | Own abstractions; Qovery exposes Helm/TF | Provider bootstraps cluster it controls | Yes | Limited | Proprietary | low (wrong persona: same org deploys) |
| Humanitec | developer.humanitec.com | Active; Orchestrator v2 (2026); no public pricing (~$2–5k/mo reported) | Platform Orchestrator: Score → resource graph → env-specific config | Score + resource definitions | Plan-only deployments show resource graph (March 2026) | Portal | Resource definitions/drivers | Proprietary | copy "plan-only" preview concept |

## Contract patterns (developer → deployer) with snippets

### 1. Values contract (what exists, types, defaults, secrets, conditions)

**KOTS Config** (flat, form-first; strings everywhere; conditions via Go templates):
```yaml
apiVersion: kots.io/v1beta1
kind: Config
spec:
  groups:
  - name: database
    title: Database
    items:
    - name: db_type
      type: radio
      default: embedded
      items: [{name: embedded, title: Embedded}, {name: external, title: External}]
    - name: db_host
      type: text
      required: true
      when: 'repl{{ ConfigOptionEquals "db_type" "external" }}'
    - name: db_password
      type: password
      validation:
        regex:
          pattern: '^.{12,64}$'
          message: "12–64 chars"
```
Mapped to Helm:
```yaml
apiVersion: kots.io/v1beta2
kind: HelmChart
spec:
  values:
    postgresql: { enabled: repl{{ ConfigOptionEquals "db_type" "embedded" }} }
  optionalValues:
  - when: 'repl{{ ConfigOptionEquals "db_type" "external" }}'
    recursiveMerge: true
    values: { externalPostgresql: { host: 'repl{{ ConfigOption "db_host" }}' } }
```
Limits: only text/textarea/password/file validated; repeatable items and hidden/`when:false` items skipped; no cross-value rules beyond `when`; no numeric ranges; no JSON Schema.

**Helm values.schema.json via losisin annotations** (schema-first; deployer sees nothing unless a UI exists):
```yaml
replicaCount: 1 # @schema type: integer; minimum: 1; maximum: 20
service:
  type: ClusterIP # @schema enum: [ClusterIP, LoadBalancer, NodePort]
ingress:
  className: "" # @schema pattern: ^[a-z0-9-]*$
db:
  password: "" # @schema required; minLength: 12; description: Set via secret ref
```
Cross-value rules: only via JSON-Schema `if/then`, `dependentRequired`, `oneOf` — Helm 4 enforces these strictly ("errors that previous versions silently ignored").

**Rancher questions.yaml** (form-first with cluster-aware pickers):
```yaml
questions:
- variable: persistence.storageClass
  type: storageclass      # dropdown populated from live cluster
  label: Storage Class
  group: Storage
- variable: service.type
  type: enum
  options: [ClusterIP, NodePort]
- variable: service.nodePort
  type: int
  min: 30000
  max: 32767
  show_if: "service.type=NodePort"
```

**OLM CSV descriptors** (UI hints layered on CRD schema):
```yaml
spec:
  customresourcedefinitions:
    owned:
    - name: myapps.example.com
      specDescriptors:
      - path: replicas
        x-descriptors: ['urn:alm:descriptor:com.tectonic.ui:podCount']
      - path: db.secretName
        x-descriptors: ['urn:alm:descriptor:io.kubernetes:Secret']
      - path: tls.cert
        x-descriptors: ['urn:alm:descriptor:com.tectonic.ui:fieldDependency:tls.enabled:true']
```

**Glasskube valueDefinitions** (schema + explicit target mapping — the cleanest "schema field → chart path" binding):
```yaml
valueDefinitions:
  dbName:
    type: text
    constraints: {required: true, minLength: 5, pattern: '^[a-z]+$'}
    metadata: {label: Database name, hints: [...]}
    targets:
    - chartName: example
      patch: {op: add, path: /config/database/dbName}
```

**Nuon inputs.toml**: `[[input]] name="vpc_id" display_name="VPC ID" type="string" sensitive=false default="" group="network"`; consumed as `{{.nuon.inputs.vpc_id}}`. Secrets = `sensitive=true`, excluded from synced config files.

**Massdriver**: `params:` is raw JSON Schema draft-07; `connections:` is JSON Schema of typed upstream artifacts (e.g. `massdriver/kubernetes-cluster`) = declared dependency on environment.

**Timoni/CUE** (types + constraints + defaults in one language, cross-field logic native):
```cue
#Config: {
  replicas: *1 | int & >=1 & <=20
  service: type: *"ClusterIP" | "LoadBalancer"
  ingress: { enabled: *false | bool, className?: string & =~"^[a-z0-9-]+$" }
  if ingress.enabled { ingress.className: string }   // cross-value rule
}
```

**Crossplane XRD** (CRD schema + CEL): `x-kubernetes-validations: [{rule: "!self.ha || self.size != 'small'", message: "HA requires ≥medium"}]`.

### 2. Environment requirements contract (checked against the target BEFORE deploy)

**Troubleshoot Preflight** — the only widely-deployed format with a rich cluster-requirement vocabulary and outcome semantics (fail/warn/pass, `strict`, `exclude`):
```yaml
apiVersion: troubleshoot.sh/v1beta2
kind: Preflight
spec:
  collectors: []                        # clusterInfo+clusterResources auto-included
  analyzers:
  - clusterVersion:
      strict: true
      outcomes:
      - fail: {when: "< 1.29.0", message: Requires Kubernetes ≥1.29}
      - pass: {message: OK}
  - storageClass:
      checkName: RWX storage
      storageClassName: "{{ .Values.persistence.storageClass }}"
      outcomes: [{fail: {message: StorageClass missing}}, {pass: {message: found}}]
  - customResourceDefinition:
      customResourceDefinitionName: certificates.cert-manager.io
      outcomes: [{fail: {message: cert-manager required}}, {pass: {message: ok}}]
  - nodeResources:
      checkName: Total CPU
      outcomes: [{fail: {when: "sum(cpuCapacity) < 8", message: Need 8 cores}}, {pass: {message: ok}}]
  - ingress: {namespace: default, ingressName: myapp, outcomes: [...]}
  - imagePullSecret: {registryName: ghcr.io, outcomes: [...]}
  - distribution: {outcomes: [{fail: {when: "== docker-desktop"}}]}
```
Delivered as a `Secret` labeled `troubleshoot.sh/kind: preflight` in the chart's templates; run by `helm template … | kubectl preflight -`. Notable: *values feed the requirement* (`storageClassName: {{ .Values.… }}`) — the requirement is value-dependent, which is exactly the "deployer picks a StorageClass, tool checks it exists and is RWX-capable" flow. Gaps: no analyzer for RWX capability (storageClass only checks presence), no quota, no PSA/Kyverno/Gatekeeper policy simulation, no NetworkPolicy/egress check without `runPod`/`http` collectors; results are evaluated *in* the cluster at install, though `preflight` can also run on a saved bundle.

**Helm `Chart.yaml kubeVersion: ">= 1.29.0-0"`** — only checked by Helm at install; Argo CD honors it at render.

**OLM CSV**: `spec.minKubeVersion`, `installModes`, `customresourcedefinitions.required`, `nativeAPIs` → OLM refuses install if unmet.

**Palette pack.json**: `constraints.resources: [{type: cpu, minLimit: 2000, resourceRequestParamRef: "resources.requests.cpu", replicaCountParamRef: "replicas", scheduleType: worker}]` — declared resource minimums derived from values.

**Score `resources:`** (`db: {type: postgres, class: ha}`) declares dependency *types*; the platform decides fulfilment — nearest thing to a portable "I need X" vocabulary but no verification semantics.

### 3. Offline evaluation primitives usable for a "pseudo cluster"
- `kyverno apply policies/ --resource rendered.yaml --values-file vars.yaml --context-file ctx.yaml --policy-report` (supports ClusterPolicy (deprecated), ValidatingPolicy, ValidatingAdmissionPolicy; CEL contexts via `--context-file`).
- `gator test --filename=templates/ --filename=constraints/ --filename=rendered/ --output=json` (Gatekeeper, offline).
- `kubeconform` with CRD schemas; `kube-linter`.
- Troubleshoot: `preflight --interactive=false bundle.tar.gz` style analysis on an already-collected support bundle (collectors = snapshot; analyzers = rules). This is the natural split for zhi: *import* = run collectors once against live cluster; *validate* = run analyzers + policy engines offline.
- Timoni: CRD OpenAPI → CUE schema import for offline resource validation.

### 4. Environment/tenant modelling
- **Octopus**: Project × Environment × Tenant (tags for grouping); variable templates per project (string/sensitive/select/checkbox/certificate/account); tenant variables not snapshotted → new tenant deploys existing release; scoping by env/tenant tag. Most complete matrix model.
- **Nuon**: App → Installs (one per customer account), each with input values + sandbox outputs; `installs/*.toml` synced; sensitive inputs never persisted.
- **Distr**: Application → Versions → Deployment Targets (per customer, Docker or Kubernetes agent) with per-target values; per-customer licenses gate versions.
- **Omnistrate**: Service → Service Plan → Environment (dev/prod) → Instance; customers see own instances, provider sees fleet.
- **Replicated**: Customer → License (channels, entitlements, expiry) → Instance; Config values live in the customer's admin console (not Git).
- **GitOps-native**: Argo/Flux `valueFiles: [values.yaml, values-<env>.yaml]` + `ignoreMissingValueFiles`; Timoni bundle per env; Kustomize overlays. Secrets: ExternalSecrets/SOPS/SealedSecrets references, never values.

### 5. Transition (handoff vs self-service with same artifact)
- **Replicated**: same release installs via admin console (ops handoff) or `helm install` with Enterprise Portal/preflight plugin (self-service devs); Config CR drives both.
- **Plural**: self-service = PR automation opening the Git change; ops = same pipeline merges.
- **Octopus**: tenant-aware lifecycles let a team self-promote while ops owns environments.
- **Humanitec**: plan-only deployments show resource graph before apply.
- **Distr**: vendor portal vs white-label customer portal on same version artifact.

## Pain-point evidence (with sources)
- Replicated State of Self-Hosted 2025: 82% of vendors support self-hosted; 67% use Helm as primary install; most teams spend <30 h/month testing across environments; ~half ship to self-hosted less often than SaaS. Replicated's own guidance: "codify common issues with preflight checks… use support bundles for asynchronous troubleshooting."
- Reviews of KOTS (Capterra/SoftwareFinder 2026): "many things require command line work", "gaps in usability and logging in the UI", "bugs and occasional regressions", "time and effort to install via KOTS still higher than desired". Replicated recommends plain Helm for customer-managed clusters.
- RWX/StorageClass: PrivateBin chart #56 (RWX unsupported on GKE default class), azuredisk-csi #2122 (`MULTI_NODE_MULTI_WRITER` error), headlamp #2579 (storageClassName not applied → PVC Pending), trusca PR #470 ("warn at install time when RWX persistence has no storageClassName"); Bitnami troubleshooting page. Summary in search: "install silently hangs" — time-to-discover is measured in Pending PVCs.
- Policy denials: Gatekeeper #3337 / ratify #258 `admission webhook "validation.gatekeeper.sh" denied the request`; Helm #11176 upgrade fails due to admission webhook; Kyverno #5476 "policy passes with kyverno apply but fails in cluster" (offline≠online drift risk). PSA: baseline/restricted enforced per namespace labels since 1.25 — charts written for privileged dev clusters fail on restricted namespaces.
- Quota: webhooks/ResourceQuota reject charts requesting more than namespace allows; only visible after `helm install` (adhdecode 2026 debugging guides).
- Argo CD UI: parameters tab "does not represent the complete set of values" (docs), regression #27773 (v3.4.1, 2026) — deployers cannot trust the GitOps UI as a values editor.
- CMX limitation: cloud test clusters cannot set CNI/CSI/Ingress plugins → vendor CI cannot reproduce customer storage/ingress specifics; strengthens case for importing *actual* customer constraints.
- Rancher #4706: conditional logic semantics diverged between two UI generations (Ember vs Jexl) — form DSL expression languages are a maintenance liability.

## Design implications for zhi (10–15)
1. **Two artifacts, one authored by developers**: a *values contract* (JSON Schema draft 2020-12 with UI-hint extensions `x-zhi-*`, compiled to/from `values.schema.json`) and an *environment contract* (Troubleshoot-compatible analyzer list). Keep them separate as KOTS does; merge nothing into Helm templates.
2. **Adopt Troubleshoot analyzers as the requirement vocabulary** and embed the Go library; extend with missing analyzers (RWX-capable StorageClass via CSI driver capabilities, ResourceQuota headroom, PSA namespace level, IngressClass, egress reachability, CRD *version* not just presence).
3. **Pseudo cluster = collector snapshot + policy bundle**: `zhi env import` runs clusterInfo/clusterResources collectors plus exports Kyverno/Gatekeeper/VAP policies, ResourceQuotas, LimitRanges, PSA labels, StorageClasses (+CSIDriver), IngressClasses, CRDs (OpenAPI), node capacity; store as versioned, redactable bundle in Git. Validate = render (Helm/Kustomize/Compose) → kubeconform against snapshot CRDs → `kyverno apply`/`gator test` with exported policies → analyzers. Cite Kyverno #5476: mark offline results as "simulated" with a confidence flag.
4. **Value-dependent requirements**: let requirements reference values (`storageClassName: {{ .values.persistence.storageClass }}`) as Troubleshoot allows; the deployer's choice is checked, not just the chart's default.
5. **Cluster-aware field types** (Rancher `storageclass`/`secret`/`pvc`, OLM `io.kubernetes:Secret`): populate dropdowns from the imported snapshot, not a live connection — works air-gapped and pre-push.
6. **Cross-value rules in CEL** (Crossplane/K8s `x-kubernetes-validations` style) rather than a bespoke `when` DSL; keeps parity with what the cluster itself enforces and avoids Rancher's Ember/Jexl divergence. Severities Info/Warning/Blocking map 1:1 to Troubleshoot pass/warn/fail + `strict`.
7. **Output is Git, not a runtime**: write `values-<env>.yaml` / overlays / Compose `.env`, open a PR (Plural pattern), and emit a machine-readable validation report as PR check. No admin console in the cluster (KOTS's biggest criticism).
8. **Environment model = Octopus tenant×environment** with per-environment snapshot reference and secret *references* (ESO/SOPS keys) typed as `secretRef` — never secret values (Nuon `sensitive`, Octopus `sensitive`).
9. **Same contract, two surfaces**: developer CLI/CI (`zhi validate` in pipeline) and deployer UI (form generated from schema + hints). Transition to DevOps = developers pick up the deployer surface for their own env; no artifact change.
10. **Plugin depth**: evidence says extensibility that mattered was (a) custom collectors/analyzers (Replicated Enterprise upsell), (b) `runPod`-style arbitrary checks, (c) config-management plugins (Argo CMP), (d) field renderers. Recommend: in-process Go interfaces for renderers/schema-extensions; out-of-process (gRPC or WASM, as Helm 4 moved to) only for *collectors/analyzers* and *importers*; drop the store/transform types unless a concrete need appears.
11. **Emit standard artifacts**: `values.schema.json` (Helm 4 strict validation is your free enforcement in every pipeline), `Chart.yaml kubeVersion`, and a Troubleshoot `Preflight` Secret so Replicated/`kubectl preflight` users get value without zhi at runtime.
12. **Docker Compose parity**: environment snapshot for Compose = Docker engine info, available networks/volumes, host ports, resource limits; validate `.env` against same schema.
13. **Air-gap**: snapshot import/export as OCI artifact (mirrors Distr/Replicated registry patterns); include imagePullSecret/registryImages analyzers to detect unmirrored images before push.
14. **Report time-to-discover**: track "would have failed at" stage (admission / scheduling / runtime) per finding; this is the persona's core metric.
15. **License**: avoid AGPL dependencies (Nuon) and GPL (helm-docs) if embedding; Troubleshoot/Kyverno/Gatekeeper/Timoni are Apache-2.0.

## Uncertainties
- Replicated pricing figures from pricing page (2026-09 fetch); may be region/negotiated. Enterprise-only "custom collectors/analyzers" gating per pricing page, not verified in docs.
- KOTS Config → Helm mapping page (`helm-native-v2-using`) did not itself show `values` mapping; snippet taken from `helm-optional-value-keys` page.
- Troubleshoot `storageClass` analyzer confirmed presence-only; an RWX-capability analyzer was *not* found (absence, not proof).
- Distr per-target values mechanism and "compatibility reports" semantics not verified beyond README/search excerpts.
- Nuon inputs validation (regex/enum) not found in docs; only type-check + early YAML/HCL syntax validation confirmed. Nuon "policies" feature seen only in a blog title.
- Omnistrate plan prices not extractable (page is JS-rendered); free tier button observed.
- OLM v1 (ClusterExtension) still consumes registry+v1 bundles with CSVs per Red Hat docs; whether console still renders `x-descriptors` forms for v1-installed extensions not verified.
- Kubeapps `form:true` docs flagged as "outdated" (issue #5982); the format shown is from a 2019-era doc revision.
- Helm 4 "richer validation rules / stricter schema" claims come from secondary blogs, not helm.sh; helm.sh charts page itself is marked "not yet updated for Helm 4."
- Score CNCF level ("Sandbox") from secondary sources; GitHub README says "hosted in CNCF Slack."
- Palette resource-constraint field names from a single docs fetch; exact JSON keys may differ.

## Sources
- https://docs.replicated.com/reference/custom-resource-config
- https://docs.replicated.com/reference/custom-resource-preflight
- https://docs.replicated.com/vendor/preflight-defining
- https://docs.replicated.com/vendor/helm-optional-value-keys
- https://docs.replicated.com/vendor/helm-native-v2-using
- https://docs.replicated.com/vendor/testing-about
- https://docs.replicated.com/embedded-cluster/v3/embedded-config
- https://docs.replicated.com/release-notes/rn-embedded-cluster
- https://www.replicated.com/pricing
- https://www.replicated.com/blog/replicated-monthly-release-highlights---june-2026
- https://www.replicated.com/blog/introducing-the-state-of-self-hosted-survey-2025
- https://www.replicated.com/blog/announcing-regex-config-input-validation
- https://github.com/replicatedhq/troubleshoot
- https://troubleshoot.sh/docs/analyze/
- https://github.com/replicatedhq/kots
- https://github.com/replicatedhq/kots/releases
- https://www.capterra.com/p/216052/Replicated/
- https://softwarefinder.com/project-management-software/replicated/reviews
- https://github.com/vmware-tanzu/kubeapps
- https://github.com/SAP/kubeapps
- https://github.com/vmware-tanzu/kubeapps/issues/5982
- https://github.com/kubeapps/kubeapps/blob/c84798e8f6211ce7be5efa51233077c29432b657/docs/developer/basic-form-support.md
- https://ranchermanager.docs.rancher.com/how-to-guides/new-user-guides/helm-charts-in-rancher/create-apps
- https://github.com/rancher/dashboard/issues/4706
- https://github.com/openshift/console/blob/main/frontend/packages/operator-lifecycle-manager/src/components/descriptors/README.md
- https://github.com/openshift/console/blob/main/frontend/packages/operator-lifecycle-manager/src/components/descriptors/reference/reference.md
- https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/operators/olm-v1
- https://glasskube.dev/products/package-manager/docs/reference/package-manifest/
- https://github.com/glasskube/glasskube
- https://github.com/glasskube/distr
- https://distr.sh/docs/platform/kubernetes-compatibility-matrix/
- https://docs.spectrocloud.com/registries-and-packs/pack-constraints/
- https://docs.rafay.co/blueprints/overview/
- https://nirmata.com/
- https://octopus.com/docs/tenants
- https://octopus.com/docs/kubernetes/live-object-status
- https://octopus.com/news/octopus-acquires-codefresh
- https://codefresh.io/docs/docs/ci-cd-guides/gitops-deployments/
- https://developer.harness.io/docs/continuous-delivery/gitops/connect-and-manage/manage-argo-configs/
- https://argo-cd.readthedocs.io/en/stable/user-guide/helm/
- https://github.com/argoproj/argo-cd/issues/27773
- https://helm.sh/docs/topics/charts/
- https://helm.sh/blog/helm-4-released/
- https://helm.sh/blog/helm-v3-end-of-life/
- https://github.com/losisin/helm-values-schema-json
- https://github.com/losisin/helm-values-schema-json/blob/main/docs/README.md
- https://github.com/dadav/helm-schema
- https://github.com/norwoodj/helm-docs
- https://pkg.go.dev/go.jacobcolvin.com/x/magicschema
- https://github.com/score-spec/spec
- https://docs.crossplane.io/latest/composition/composite-resource-definitions/
- https://github.com/stefanprodan/timoni
- https://github.com/stefanprodan/timoni/releases
- https://timoni.sh/bundle/
- https://github.com/nuonco/nuon
- https://docs.nuon.co/configuration-files
- https://github.com/nuonco/awesome-byoc
- https://docs.omnistrate.com/usecases/byoc/
- https://omnistrate.com/pricing
- https://www.tensor9.com/product/
- https://techcrunch.com/2025/05/14/tensor9-helps-vendors-deploy-their-software-into-any-environment-using-digital-twins/
- https://docs.massdriver.cloud/bundles
- https://www.plural.sh/products/self-service-automations
- https://docs.plural.sh/how-to/deploy/pr-automation
- https://www.qovery.com/blog/northflank-alternatives-worth-considering
- https://northflank.com/blog/best-options-for-byoc-in-cloud-computing
- https://developer.humanitec.com/platform-orchestrator/docs/updates/change-log-v2/
- https://humanitec.com/pricing
- https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
- https://github.com/kyverno/kyverno/issues/5476
- https://open-policy-agent.github.io/gatekeeper/website/docs/gator/
- https://github.com/open-policy-agent/gatekeeper/issues/3337
- https://github.com/helm/helm/issues/11176
- https://github.com/PrivateBin/helm-chart/issues/56
- https://github.com/kubernetes-sigs/azuredisk-csi-driver/issues/2122
- https://github.com/kubernetes-sigs/headlamp/issues/2579
- https://github.com/trustedoss/trusca/pull/470
- https://docs.bitnami.com/kubernetes/faq/troubleshooting/troubleshooting-persistence-volumes/
- https://adhdecode.com/debugging/helm/error-webhook-denied-the-request/
- https://github.com/yannh/kubeconform