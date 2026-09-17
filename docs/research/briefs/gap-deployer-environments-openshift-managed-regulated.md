# gap-deployer-environments-openshift-managed-regulated

Research date 2026-09-13. All claims below come from live docs fetched this session unless tagged UNVERIFIED (memory-only or page not renderable). Red Hat docs pages return only navigation to the fetcher; OpenShift facts marked (RH-snippet) come from search-result excerpts of those pages, not full fetches.

## Cross-cutting finding

Every managed distribution has a two-layer constraint model: (a) **declarative objects a read-only snapshot can capture** (SCCs, PSA labels, Gatekeeper ConstraintTemplates/constraints, ResourceQuota/LimitRange, IDMS/ITMS, ClusterImagePolicy, CRD schemas, NodePools/ComputeClasses, StorageClasses) and (b) **opaque platform admission** (GKE Warden, AKS Deployment Safeguards mutators, EKS control-plane webhooks, rancher-webhook, OpenShift SCC mutation + namespace UID allocation). Layer (a) is emulatable with existing Go libraries; layer (b) must be re-implemented as versioned "distribution profiles" maintained from vendor docs. Offline evaluators that exist today and are importable Go: `k8s.io/pod-security-admission/policy` (v0.37.0, 2026-08-26; `EvaluatePod(levelVersion, meta, spec)` needs no API server) [24], `github.com/openshift/apiserver-library-go/pkg/securitycontextconstraints/sccmatching` (pseudo-version 2026-07-15) [2], Gatekeeper `gator test` (no referential data offline) [16], Kyverno CLI v1.19 (`apply`/`test`, VAP/MAP/ValidatingPolicy/MutatingPolicy, in-memory dynamic client from supplied manifests) [17], `kubectl-validate` (sig-cli, apiserver validation code for native+CRD schemas) [25], kubeconform.

## PART 1 — Distribution-specific constraints

### OpenShift 4.18–4.20

| Constraint | Object kind | Snapshot-able | Fails at | Offline evaluator | Emulation notes |
|---|---|---|---|---|---|
| SCC selection | `security.openshift.io/v1 SecurityContextConstraints` (cluster) + RBAC `use` verb on `securitycontextconstraints` | Yes (SCCs, Roles/RoleBindings/ClusterRoleBindings, SA) | Admission (mutate+validate); pod rejected with "unable to validate against any SCC" | `sccmatching.CreateProviderFromConstraint(ns, scc)` + `AssignSecurityContext(provider, pod)` are object-only; `ConstraintAppliesTo` needs an `authorizer.Authorizer` [2] | Order = priority desc → most restrictive → name [1]. Feed a snapshot-backed RBAC authorizer (build from Roles/Bindings) so `ConstraintAppliesTo` works offline. Default: all authenticated users get `restricted-v2` (RH-snippet [5]). 4.20 adds `restricted-v3` and `nested-container` SCCs for user namespaces GA (RH-snippet [5]). Online oracle: `oc adm policy scc-subject-review -f pod.yaml`, `scc-review` [1] — use to calibrate the emulator, not at validate time. |
| Namespace UID/GID/MCS pre-allocation | `Namespace` annotations `openshift.io/sa.scc.uid-range`, `sa.scc.supplemental-groups`, `sa.scc.mcs` | Yes for existing namespaces; **absent for namespaces the PR will create** | Admission mutation (runAsUser/fsGroup/seLinux filled from range; `MustRunAsRange`) [1] | `sccmatching` uses the Namespace object for pre-allocated values [2] | For new namespaces synthesise a placeholder range and emit a Warning: "image must not assume a fixed UID; chart hard-coding `runAsUser: 1000` will be rejected by restricted-v2". This is the #1 vendor-chart failure on OpenShift. |
| PSA ↔ SCC label sync | `Namespace` label `security.openshift.io/scc.podSecurityLabelSync`, PSA labels | Yes | Admission (PSA warn/audit; enforce only if labels set) | `pod-security-admission/policy` [24] | Evaluate PSS level from synced labels; OpenShift globally enforces `privileged`, warns/audits `restricted` (memory; UNVERIFIED for 4.20). |
| Routes vs Ingress vs Gateway API | `route.openshift.io/v1 Route`; `networking.k8s.io/v1 Ingress` (translated to Route by ingress-to-route controller); `gateway.networking.k8s.io` GA in 4.19 z-stream backed by Istio via OSSM 3; 4.20 creates only istiod until a `Gateway` exists [7][8] | Yes (Routes, IngressControllers, GatewayClass) | Route host collisions fail at **controller/status** (`Admitted=False`, host claimed by another namespace) not admission — UNVERIFIED wording | None | Snapshot all Routes cluster-wide to detect host claims; warn when a chart emits `Ingress` with annotations only nginx understands (OpenShift router ignores them). Gateway API requires OSSM operator presence → snapshot `GatewayClass`. |
| ClusterResourceQuota | `quota.openshift.io/v1 ClusterResourceQuota` (selects namespaces by label selector or annotation e.g. `openshift.io/requester`); per-ns view `AppliedClusterResourceQuota` | Yes | Admission (quota plugin) | Kubernetes quota evaluators (`k8s.io/kubernetes/pkg/quota/v1`) reusable offline — UNVERIFIED effort | Sum usage across selected namespaces from snapshot; enforcement across namespaces is eventually consistent (memory; page not fetchable — UNVERIFIED). |
| Project request template | `project.config.openshift.io/v1` `cluster.spec.projectRequestTemplate.name` → Template in `openshift-config`; generated via `oc adm create-bootstrap-project-template`; injects `NetworkPolicy`, `LimitRange`, `ResourceQuota` into **new** projects only [11] | Yes | Post-creation (objects exist before workloads) | None needed | When the PR creates a namespace, render the project template objects into the pseudo-cluster before evaluating quotas/limits/netpol. |
| Image registry policy | `image.config.openshift.io/v1 Image` (`allowedRegistriesForImport`, `registrySources.allowedRegistries/blockedRegistries/insecureRegistries`) [9]; `config.openshift.io/v1 ImageDigestMirrorSet` / `ImageTagMirrorSet` (mirrors, `mirrorSourcePolicy`) [9] | Yes | Import: API (ImageStream import); pull: **runtime** (CRI-O registries.conf via MCO) — ImagePullBackOff, not admission | None | Match every image ref in rendered manifests against allowed/blocked lists and rewrite through IDMS/ITMS to prove a mirror exists; digest refs required for IDMS. |
| Sigstore image policy | `config.openshift.io/v1 ClusterImagePolicy` / `ImagePolicy` GA in 4.20, incl. BYOPKI; MCO writes `/etc/containers/policy.json` [10] | Yes | Runtime (pull) | None | Check that images under policy scopes carry a verifiable signature in the deployer's mirror. |
| OLM v1 | `olm.operatorframework.io/v1 ClusterCatalog`, `ClusterExtension`; requires user-provided ServiceAccount with RBAC ("manifests without a ServiceAccount will be rejected"), registry+v1 bundles, AllNamespaces mode, no webhooks (June 2025 article, 4.18) [12]; single/own namespace is Tech Preview in 4.20 (TOC only) | Yes (catalog contents need catalogd unpack) | Admission (SA missing) + reconcile (RBAC preflight, CRD upgrade safety) | None | Validate `ClusterExtension` refs against snapshot of `ClusterCatalog` packages/channels; verify SA RBAC covers bundle contents (heavy; defer). Version-gate webhook-bearing operators. |
| Dry-run tooling | `oc apply --dry-run=server`, `oc adm policy scc-review` | n/a | n/a | Server-side only | Nothing official offline; the library route above is the only path. |

### GKE (Standard and Autopilot)

| Constraint | Object kind | Snapshot-able | Fails at | Offline evaluator | Emulation notes |
|---|---|---|---|---|---|
| Autopilot pod security constraints | Opaque: `warden-validating.common-webhooks.networking.gke.io` validating webhook plus proprietary `GKEAutopilot` authorization mode [15][31]; rules **not** exposed as API objects | Only the webhook configuration and exemptions (`WorkloadAllowlist`, `AllowlistSynchronizer` CRDs, GKE ≥1.35) [14][15] | Admission ("GKE Warden constraints violations") | None | Encode profile from docs [13]: no privileged (except allowlisted partner workloads), no host namespaces/hostNetwork, hostPath only read-only `/var/log/`, capabilities limited to SETPCAP, MKNOD, AUDIT_WRITE, CHOWN, DAC_OVERRIDE, FOWNER, FSETID, KILL, SETGID, SETUID, NET_BIND_SERVICE, SYS_CHROOT, SETFCAP, SYS_PTRACE (NET_RAW/NET_ADMIN dropped), no unsafe sysctls, volume types configMap/csi/downwardAPI/emptyDir/gcePersistentDisk/nfs/persistentVolumeClaim/projected/secret, `spec.externalIPs` blocked, `procMount: Unmasked` rejected ≥1.33, seccomp RuntimeDefault applied. Autopilot rewrites user webhooks to exclude system namespaces and rejects `*/*` wildcard rules [31]. |
| Autopilot resource requests | Opaque mutation/validation; compute classes are `ComputeClass` CRDs (memory, UNVERIFIED group) | Partially (ComputeClass, node selectors) | Admission mutation (below min → raised; missing → 0.5 vCPU/2 GiB, 1 GiB ephemeral; DaemonSet 50m/100Mi) and **rejection** (above max, bad ratio) [32] | None | Encode ratios: general-purpose 1:1–1:6.5, Balanced 1:1–1:8, Scale-Out exactly 1:4; round CPU to 0.25 vCPU (non-bursting); ephemeral 10 MiB–10 GiB; DaemonSet minima 10m/10Mi (1m/2Mi bursting). Emit Info diff showing what GKE will mutate so the Git values already match (avoids drift alerts in Argo). |
| Policy Controller | Gatekeeper: `templates.gatekeeper.sh ConstraintTemplate`, `constraints.gatekeeper.sh/*`, bundles (CIS GKE 1.5, PSP, NIST, PCI) [18][19]; fleet feature | Yes | Admission (deny) or audit/dryrun | `gator test` (CEL+Rego; no referential `data.inventory`; `gator expand` handles Assign/ModifySet) [16] | Snapshot templates+constraints and run gator in-process (import `pkg/gator`). Referential constraints (e.g. unique ingress host) need the snapshot fed as fake inventory — gator does not do this; would need a custom driver. |
| Binary Authorization | GCP-level policy (not a Kubernetes object); enforced by GKE admission | Via GCP API only (UNVERIFIED shape) | Admission (image attestation) | None | Snapshot policy via `gcloud`/API, check attestations/digest pinning on rendered images. |
| Custom org policies for GKE | GCP Organization Policy constraints on GKE resources [13-search] | GCP API | Cluster/resource creation, not pod admission | None | Out of scope for pod validation; relevant only if the tool provisions clusters. |

### AKS

| Constraint | Object kind | Snapshot-able | Fails at | Offline evaluator | Emulation notes |
|---|---|---|---|---|---|
| Azure Policy add-on | Gatekeeper v3; assignments synced every 15 min into `ConstraintTemplate`s prefixed `k8sazure*`, constraints prefixed `azurepolicy-*`, mutation templates (`Assign`, `AssignMetadata`, `ModifySet`); effects audit/deny/mutate; auto-excludes kube-system, gatekeeper-system; CEL/VAP behind `AKS-AzurePolicyK8sNativeValidation`; expansion (workload→what-if pod) opt-in; fail-open [21] | Yes (all are readable via kubectl) | Admission (deny/mutate) or audit | `gator test` / `gator expand` [16] | Same as GKE. Note fail-open: a "passed" validation is not proof the cluster would deny; snapshot is *more* strict than reality. Add-on collects diagnostic telemetry [21] — relevant for regulated customers. |
| Deployment Safeguards | Azure Policy initiative `c047ea8e-…`; `Warn` or `Enforce`; AKS Automatic: Enforce + PSS Baseline by default, cannot switch to Warn; AKS Standard: optional, PSS default Privileged; all-or-nothing (docs updated 2026-06-02) [20] | Yes (surfaced as Gatekeeper objects) | Admission; violations show as `admission webhook "validation.gatekeeper.sh" denied` | gator (validation side); mutators must be re-implemented | Mutators to emulate: resource requests (none → 500m/2048Mi req+limit; min 100m/100Mi; request capped to limit); anti-affinity + topologySpread (preferred weight 100, maxSkew 1, `ScheduleAnyway`) added when neither exists and label `kubernetes.azure.com/managedby=aks` absent. Validators: probes required, no `latest`/untagged images, CSI StorageClass only, unique Service selectors, no `kubernetes.azure.com` labels, `CriticalAddonsOnly` taint reserved, allowed images. PSS Baseline/Restricted error catalogue is in [20]. |
| Pod Security defaults | PSA via safeguards `--pss-level`; Automatic = Baseline [20] | Yes (namespace labels / safeguard config) | Admission | `pod-security-admission/policy` [24] | — |
| Image cleaner / ACR | Node-level eraser (runtime GC), ACR policies are registry-side | No (Azure API) | Runtime / pull | None | Out of scope beyond "image pullable from ACR" — UNVERIFIED details. |

### EKS (incl. Auto Mode)

| Constraint | Object kind | Snapshot-able | Fails at | Offline evaluator | Emulation notes |
|---|---|---|---|---|---|
| Pod Security Standards | PSA enabled with `privileged` for enforce/audit/warn cluster-wide (Kubernetes ≥1.23); namespace labels opt in [22] | Yes | Admission | [24] | Default is permissive; only labelled namespaces matter. |
| Pod Identity | Control-plane mutation injecting `AWS_CONTAINER_CREDENTIALS_FULL_URI` / `AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`; agent DaemonSet on 169.254.170.23 ports 80/2703 (built-in on Auto Mode); Linux EC2 only, no Fargate/Windows [23][30] | Association is an EKS API object, not Kubernetes | Runtime (no creds) | None | Validate ServiceAccount names in chart match `PodIdentityAssociation`s from AWS API snapshot; warn on `hostNetwork` (IMDS exposure). |
| Managed admission webhooks | `pod-identity-webhook` (IRSA) and `vpc-resource-*-webhook` (Security Groups for Pods, Windows) are `*WebhookConfiguration` objects pointing at control-plane-hosted services (memory; UNVERIFIED names) | Yes (configs), rules opaque | Admission mutation | None | Treat as benign mutators; only SGP annotations/`SecurityGroupPolicy` CRD need checks. |
| Auto Mode | Karpenter `NodePool`/`NodeClass`; Bottlerocket immutable AMI, SELinux enforcing, read-only rootfs, no SSH/SSM, 21-day max node lifetime; built-in EBS CSI, LB controller (Ingress/Service), Pod Identity Agent; DaemonSets allowed [29][30] | Yes (NodePool, NodeClass, StorageClass, IngressClass) | Scheduling (no NodePool matches selectors/taints) and runtime (hostPath writes on read-only root) | None (Karpenter has no offline "fits" library) | Emulate node selection: match nodeSelector/affinity/tolerations against `NodePool.requirements` and taints; flag hostPath writes and privileged (works but violates AWS guidance). Auto Mode explicitly recommends Kyverno/Gatekeeper for pod policy [29] — so snapshot those too. |
| ECR pull-through cache / scanning | Registry-side; scanning has **no** admission gate — needs Kyverno/Gatekeeper [22] | AWS API | Pull / none | None | Verify image refs resolve through configured pull-through prefixes. |
| EKS add-on policies | Add-on config schemas via EKS API | AWS API | Add-on reconcile | None | Out of scope for workload validation. |

### Rancher / RKE2 / K3s

| Constraint | Object kind | Snapshot-able | Fails at | Offline evaluator | Emulation notes |
|---|---|---|---|---|---|
| PSACT | `management.cattle.io/v3 PodSecurityAdmissionConfigurationTemplate` in the **Rancher management cluster**; built-ins `rancher-privileged`, `rancher-restricted` (46 exempt namespaces incl. cattle-*, kube-system, cert-manager, longhorn-system) [27][28]; applied to RKE2/K3s via `defaultPodSecurityAdmissionConfigurationTemplateName` → kube-apiserver `AdmissionConfiguration` file; imported/hosted clusters via namespace labels | Yes from Rancher API; **not** readable from the downstream cluster when applied as apiserver config (UNVERIFIED: v2.14 page text suggests visibility; latest page says apiserver file) | Admission | [24] with the template's defaults+exemptions | Snapshot importer must talk to Rancher (`provisioning.cattle.io/v1 Cluster.spec.defaultPodSecurityAdmissionConfigurationTemplateName`) in addition to the downstream kube API. |
| Project quotas & defaults | Rancher Project quota → native `ResourceQuota` and `LimitRange` per namespace; namespace over project budget gets quota 0; default limits only propagate to new namespaces [26] | Yes (native objects downstream; project definition in Rancher) | Admission (quota) + creation | Kubernetes quota/limitrange logic | rancher-webhook blocks user edits of Rancher-managed ResourceQuota/LimitRange [26-gh]. |
| rancher-webhook | Validates/mutates `Namespace` (project annotation `field.cattle.io/projectId`, PSA enforcement labels, resource-limit consistency), `Project` (quota consistency, container default resource limits), RBAC bindings (escalation), Secrets (creator annotations), provisioning `Cluster` [26-gh] | Config yes; logic opaque (open source, readable) | Admission | None | Emulate only namespace rules: PSA labels on namespaces must not weaken PSACT; new namespaces need project annotation if charts create them. |
| CIS profile | RKE2 `profile: cis` → restricted PSA + default NetworkPolicies (memory; UNVERIFIED) | Partially | Admission / networking | — | Include in "rancher-hardened" profile. |

## PART 2 — Regulated and air-gapped customers

### What auditors expect from a config/validation tool (evidence-shaped)

1. **Signed validation report as attestation.** Emit a DSSE envelope containing an in-toto Statement whose subject is the rendered-manifest digest (and the Git commit), with predicate `https://slsa.dev/verification_summary/v1` (`verificationResult PASSED|FAILED`, `verifier`, `timeVerified`, `resourceUri`, `policy` = snapshot digest + profile version, `inputAttestations`) [33][34]; optionally the in-toto Test Result predicate for per-check detail [34]. This maps directly to "who validated what against which snapshot".
2. **Audit trail.** Append-only log linking (user identity, value path, old/new, snapshot digest, profile version, report digest, PR URL). Store as attestations in the same OCI repo as the snapshot so Rekor-less environments still get tamper evidence via digest chains.
3. **FIPS 140-3.** Build with `GOFIPS140=v1.0.0` (Go Cryptographic Module v1.0.0, CMVP #5247, CAVP A6650, Go 1.24+); v1.26.0 is "Pending Review" on the In-Process list as of 2026-04-28; `GOFIPS140=certified` alias arrives in Go 1.27 / 1.26.3 / 1.25.10; runtime `GODEBUG=fips140=on` (`only` is not for production); crypto/tls drops non-approved suites; not supported on OpenBSD/Wasm/AIX/32-bit Windows; Go+BoringCrypto is slated for removal [3]. Consequence: do not depend on ed25519-only or non-approved algorithms in the plugin handshake/signing path when FIPS mode is on; ship a separate `-fips` binary and document the module version in the SBOM.
4. **SBOM.** BSI TR-03183-2 v2.1.0 (CRA guidance) requires CycloneDX ≥1.6 or SPDX ≥3.0.1, JSON/XML, SHA-512 hashes, SPDX licence identifiers, completeness indicator, **no vulnerability data inside the SBOM**; CISA updated minimum elements in 2025 [35][36]. Publish per-platform SBOMs and attach as attestations (Zarf does exactly this: per-platform SBOMs + keyless Sigstore signing, v0.85.0 2026-09-03 [37]).
5. **Provenance.** SLSA Build L3 provenance is the bar cited for federal procurement [36]; cosign v3.1.3 (2026-08-06) makes the protobuf bundle the default and patched a legacy-bundle verification bypass (GHSA-fx35-mq7g-6g98) — ship v3 bundles only [38]. FedRAMP 20x moved to machine-readable KSIs (56 Low / 61 Moderate) and RFC-0024 Rev5 machine-readable packages; OMB M-26-05 (Jan 2026) made vendor attestations optional per a secondary source — UNVERIFIED [39].
6. **No telemetry, offline updates.** Default off, documented egress list (ideally none), signed release manifests verifiable with a bundled trusted root. Note for contrast: Azure Policy add-on itself phones home [21].
7. **Offline Sigstore.** Keyless *signing* needs Fulcio + an OIDC issuer + (Rekor or TSA) — impossible fully offline unless the customer runs a private Sigstore stack. Keyless *verification* works offline given: a pinned `trusted_root.json` (Fulcio CA chain, Rekor key, CT log keys, TSA certs) from `cosign initialize` or `cosign trusted-root create`, a bundle containing signature+cert+tlog entry or RFC 3161 timestamp, and `cosign verify --offline --trusted-root … --certificate-identity … --certificate-oidc-issuer …` [40][41]. TUF refresh fails offline → pin and rotate roots manually. Nov 2025 report: v3.0.2 needed `--new-bundle-format=false` for offline protobuf bundles; whether 3.1.x fixed this is UNVERIFIED [41]. Flux 2.9 (2026-06-30) added `OCIRepository.spec.verify.trustedRootSecretRef` (`trusted_root.json`) for exactly this case, plus SOPS Age post-quantum and SSH commit-signature verification [42][43]. OpenShift 4.20 ClusterImagePolicy supports BYOPKI [10].
8. **Mirrors for schemas/policies/snapshots/plugins.** Package everything as OCI artifacts so it moves through Zarf (active, v0.85.0) [37], Hauler (last GitHub release v2.1.0, 2024-08-31 — treat as low-activity; UNVERIFIED whether releases moved) [44], Harbor/Nexus/Artifactory, Replicated air-gap bundles (`.airgap` with `airgap.yaml`, `app.tar.gz`, images dir; Embedded Cluster variant bundles k0s) [45], OpenShift `oc-mirror` v2 with IDMS/ITMS output [9].

### Procurability checklist (concrete features)

- [ ] Single static binary, `CGO_ENABLED=0`, FIPS variant via `GOFIPS140`, reproducible build, SLSA L3 provenance, cosign v3 bundle, CycloneDX 1.6 + SPDX 3.0.1 SBOMs.
- [ ] Zero network egress by default; explicit allow-list config; no crash reporting/telemetry; documented in a "network requirements" page.
- [ ] Snapshot, profiles, schemas, plugins all as OCI artifacts (signed, digest-pinned) importable from a local registry or tarball; `zhi mirror export/import`.
- [ ] Verification with private trusted roots (`--trusted-root`), BYO PKI (cosign key/cert, notation), and a documented "no Rekor" mode with RFC 3161 timestamps.
- [ ] Validation report = DSSE + SLSA VSA v1, attachable to the OCI snapshot and to the PR (as check-run output).
- [ ] Immutable audit log (append-only, hash-chained) with identity from OIDC/SSO or Git author, exportable as JSON for auditors.
- [ ] Deterministic evaluation: identical inputs → identical report digest (required for evidence reuse).
- [ ] Role separation: developer-authored schema/validators signed separately from deployer-filled values.
- [ ] Plugin binaries: signature + SBOM verified before launch; plugin allow-list per environment; ability to run with plugins disabled.
- [ ] Version-pinned distribution profiles (e.g. `openshift-4.20`, `aks-automatic-2026-06`) with changelog, so an auditor can see which rule set produced a report.

## PART 3 — Non-Kubernetes targets (scope verdict)

**Ansible / VMs.** Primitives: `ansible-playbook --syntax-check`, ansible-lint, `--check --diff` (needs reachable hosts; "check mode is not a sandbox"), Molecule (converge against real/virtual instances; built-in lint removed in v5) [46]. There is no "validate against an imported model of the target" primitive — the target is inventory + facts, not a schema. User base is large (Ansible 35% in the provisioning category of Docker's 2025 report [47]) but its config surface is host state, not manifests. **Verdict: out of scope for validation; optionally keep an "apply" hook.**

**Nomad.** 2.0.x (IBM V.M.F versioning since April 2026; IBM closed HashiCorp acquisition 2025-02-27) [48][49]. `nomad job validate` (syntax/semantic; requires `read-job` under ACLs) and `nomad job plan` (scheduler dry-run with placement failures, diff, Sentinel soft-mandatory warnings, `-policy-override`) both need a live server [50][51]; Sentinel policies can be unit-tested offline with the Sentinel CLI simulator and mocks [52]. No public share data; not in CNCF survey. **Verdict: out of scope; server-side plan already gives "validate against target" for Nomad users.**

**Podman Quadlet / kube play.** Offline primitives exist and are cheap: `/usr/lib/systemd/system-generators/podman-system-generator --dryrun`, `systemd-analyze --generators=true verify unit.service`, Podman 5.6 (July 2025) `podman quadlet install/list/print/rm` [53][54]; `podman kube play --validate=ignore|warn|strict` per current docs (default `ignore` silently drops unsupported fields; a 2026 blog claims the flag is absent in 5.8.2 — UNVERIFIED which version introduced it) [55]; supported kinds Pod, Deployment, PVC, ConfigMap, Secret, DaemonSet, Job. Podman at 19% vs Docker 71% (Stack Overflow 2025) [56]. **Verdict: in scope only as a "profile" on the Kubernetes path** — the same rendered YAML validated with a "podman-kube subset" ruleset (unsupported kinds/fields = Blocking), plus quadlet syntax check when a `.kube` unit is emitted.

**Baseline for comparison.** Kubernetes at 82% production among container users (CNCF 2025, 2026-01-20) [57]; Argo CD on ~60% of clusters, Flux ~11% of GitOps users [58]. Docker Compose remains the second target by developer count (Docker 71%). Compose + Kubernetes stay primary; Quadlet is a low-cost adjacency; Ansible and Nomad are out.

## Sources

1. https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/authentication_and_authorization/managing-pod-security-policies
2. https://pkg.go.dev/github.com/openshift/apiserver-library-go/pkg/securitycontextconstraints/sccmatching
3. https://go.dev/doc/security/fips140
4. https://docs.okd.io/4.19/post_installation_configuration/day_2_core_cnf_clusters/security/security-sec-context-constraints.html
5. https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/release_notes/ocp-4-20-release-notes (search excerpt only)
6. https://andreaskaris.github.io/blog/openshift/scc/
7. https://docs.okd.io/4.20/networking/ingress_load_balancing/configuring_ingress_cluster_traffic/ingress-gateway-api.html
8. https://www.redhat.com/en/blog/red-hat-openshift-419-accelerates-virtualization-and-enterprise-ai-innovation
9. https://docs.okd.io/latest/rest_api/config_apis/imagedigestmirrorset-config-openshift-io-v1.html ; https://docs.redhat.com/en/documentation/openshift_container_platform/4.17/html/config_apis/image-config-openshift-io-v1
10. https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/nodes/nodes-sigstore-using
11. https://docs.okd.io/4.20/applications/projects/configuring-project-creation.html
12. https://developers.redhat.com/articles/2025/06/02/manage-operators-clusterextensions-olm-v1
13. https://docs.cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security
14. https://docs.cloud.google.com/kubernetes-engine/docs/concepts/about-autopilot-privileged-workloads
15. https://docs.cloud.google.com/kubernetes-engine/docs/troubleshooting/autopilot-privileged-workloads
16. https://open-policy-agent.github.io/gatekeeper/website/docs/gator/
17. https://kyverno.io/docs/subprojects/kyverno-cli/
18. https://docs.cloud.google.com/kubernetes-engine/policy-controller/docs/overview
19. https://docs.cloud.google.com/kubernetes-engine/policy-controller/docs/latest/reference/constraint-template-library
20. https://learn.microsoft.com/en-us/azure/aks/deployment-safeguards
21. https://learn.microsoft.com/en-us/azure/governance/policy/concepts/policy-for-kubernetes
22. https://repost.aws/knowledge-center/eks-pod-pss-psa ; https://dev.to/alpeshkumbhare/eks-production-hardening-guide-security-karpenter-cost-and-upgrades-for-real-world-kubernetes-1c5e
23. https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html
24. https://pkg.go.dev/k8s.io/pod-security-admission/policy
25. https://github.com/kubernetes-sigs/kubectl-validate
26. https://ranchermanager.docs.rancher.com/how-to-guides/advanced-user-guides/manage-projects/manage-project-resource-quotas/about-project-resource-quotas ; https://github.com/rancher/webhook/blob/main/docs.md
27. https://documentation.suse.com/cloudnative/rancher-manager/v2.14/en/security/psact.html
28. https://documentation.suse.com/cloudnative/rancher-manager/latest/en/security/psact.html
29. https://docs.aws.amazon.com/whitepapers/latest/security-overview-amazon-eks-auto-mode/workloads.html
30. https://docs.aws.amazon.com/eks/latest/userguide/automode.html
31. https://unit42.paloaltonetworks.com/gke-autopilot-vulnerabilities/ ; https://nirmata.com/2025/03/18/effortless-policy-enforcement-on-gke-autopilot-a-kyverno-and-nirmata-control-hub-guide/
32. https://docs.cloud.google.com/kubernetes-engine/docs/concepts/autopilot-resource-requests
33. https://slsa.dev/spec/v0.1/verification_summary ; https://docs.develocity.ai/provenance-governor/1.9/attestation-verification-summary/
34. https://github.com/in-toto/attestation/tree/main/spec/predicates
35. https://sbomify.com/compliance/bsi-tr-03183/ ; https://sbomify.com/2026/08/20/bsi-tr-03183-four-parts-cra-update/ ; https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03183/TR-03183_node.html
36. https://www.minimus.io/post/software-supply-chain-security-tools ; https://www.netrise.io/xiot-security-blog/what-eo-14028-eu-cra-and-nist-csf-2.0-mean-for-software-supply-chain-transparency
37. https://github.com/zarf-dev/zarf/releases
38. https://github.com/sigstore/cosign/releases
39. https://knoxsystems.com/resources/fedramp-20x ; https://www.fedramp.gov/rfcs/0024
40. https://edu.chainguard.dev/open-source/sigstore/cosign/verifying-in-air-gapped-environments/
41. https://some-natalie.dev/blog/cosign-disconnected/
42. https://fluxcd.io/blog/2026/06/flux-v2.9.0/
43. https://fluxcd.io/flux/components/source/ocirepositories/
44. https://github.com/hauler-dev/hauler/releases
45. https://docs.replicated.com/embedded-cluster/v2/installing-embedded-air-gap
46. https://oneuptime.com/blog/post/2026-07-24-testing-ansible-roles/view ; https://github.com/ansible-community/molecule/issues/2344
47. https://www.docker.com/blog/2025-docker-state-of-app-dev/
48. https://developer.hashicorp.com/nomad/docs/release-notes/v2-0-x
49. https://linuxiac.com/ibm-completes-6-4-billion-acquisition-of-hashicorp/
50. https://developer.hashicorp.com/nomad/docs/commands/job/validate
51. https://developer.hashicorp.com/nomad/docs/commands/job/plan
52. https://docs.hashicorp.com/sentinel/configuration ; https://developer.hashicorp.com/nomad/docs/reference/sentinel-policy
53. https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html
54. https://github.com/containers/podman/releases/tag/v5.6.0
55. https://docs.podman.io/en/latest/markdown/podman-kube-play.1.html ; https://github.com/containers/podman/issues/15903
56. https://survey.stackoverflow.co/2025/technology
57. https://www.cncf.io/announcements/2026/01/20/kubernetes-established-as-the-de-facto-operating-system-for-ai-as-production-use-hits-82-in-2025-cncf-annual-cloud-native-survey/
58. https://www.cncf.io/announcements/2025/07/24/cncf-end-user-survey-finds-argo-cd-as-majority-adopted-gitops-solution-for-kubernetes/ ; https://thenewstack.io/survey-argocd-leaves-flux-and-other-gitops-platforms-behind/