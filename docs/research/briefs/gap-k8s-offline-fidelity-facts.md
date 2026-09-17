# gap-k8s-offline-fidelity-facts

Method: live web (WebSearch x14, WebFetch/curl on kubernetes.io, k8s CHANGELOGs, KEP READMEs + kep.yaml, Argo CD docs/repo, Gatekeeper docs, KWOK docs+source, Kyverno docs+release notes, GitHub API for release dates). Where a fetched summary contradicted a primary source (e.g. the KEP-5073 README phase table vs. the feature-gates table; several release years), I trusted the primary source and note it. Date: 2026-09-13.

## Summary table

| # | Item | Verdict | Conf. |
|---|------|---------|-------|
| 1 | Declarative validation | KEP-5073 (KEP-4153 superseded). `DeclarativeValidation` GA/locked 1.36; `DeclarativeValidationBeta` beta-on 1.36; `Takeover` deprecated 1.36, locked 1.37. Rules NOT published to /openapi/v3 (explicit non-goal). Native-type offline validation stays best-effort. | high |
| 2 | Manifest-based admission (KEP-5793) | Gate `ManifestBasedAdmissionControlConfig`: alpha 1.36 (off), beta 1.37 (on). `.static.k8s.io` objects are by design invisible to the API; no exposure planned. | high |
| 3 | MutatingAdmissionPolicy | alpha 1.32 (v1alpha1), beta 1.34 (v1beta1, off), GA 1.36 `admissionregistration.k8s.io/v1` on by default; 1.37 stores as v1. | high |
| 4 | CRD CEL rules | stable 1.29; per-expression static limit 10,000,000, per-CRD 100,000,000; runtime per-call 1,000,000, per-object budget 10,000,000; ratcheting stable 1.33. | high |
| 5 | Argo CD SSD | Stable since 3.1.0; still opt-in (`controller.diff.server.side: "false"`), auto when ServerSideApply sync option set. No SSD for new resources; mutating webhooks excluded unless `IncludeMutationWebhook=true`. `timeout.reconciliation` 120s + jitter 60s; 3.4/3.5 changed no diff defaults. | high |
| 6 | Gatekeeper VAP gen | beta + default-on since v3.20; requires k8s ≥1.30. gator CEL engine: no referential data; `namespaceObject` supported; `request.userInfo` for CEL undocumented. | high/med |
| 7 | KWOK/envtest | `kwokctl snapshot export` default filter excludes CRDs, VAP/MAP, webhooks, ResourceQuota, classes. Admission on by default. envtest v0.25 can pass any apiserver flag via `APIServer.Configure()`. No published startup numbers. | high / UNVERIFIED for timings |
| 8 | PSA config | No API/endpoint; kube-apiserver has no `/configz`. EKS/GKE: defaults privileged, no exemptions, not viewable. OpenShift: defaults shipped in operator bindata (readable). | high |
| 9 | Kyverno CLI 1.19.1 | apply: ClusterPolicy, VAP+VAPB(+params), MAP+MAPB, ValidatingPolicy/MutatingPolicy/…; mocks via Context file + `apiCallResponses`/`globalContextEntries`; admission op simulation added in 1.19. Playground: mock vars only. | high |
| 10 | Housekeeping | Helm 3 bugfix end 2026-09-09, security end 2027-02-10; Timoni v0.34.0 2026-08-30; kubeconform v0.8.0 2026-06-04; Weave GitOps last stable v0.38.0 2023-12-06; Datree archived 2024-06-06; k8s 1.36.0 2026-04-22, 1.37.0 2026-08-26. | high |
| 11 | KYAML | KEP-5295 stable 1.37; `kubectl get -o kyaml` stable. | high |

---

## 1. Declarative validation of native types

**Verdict.** The active KEP is **KEP-5073** ("Declarative Validation of Kubernetes Native Types With validation-gen"). **KEP-4153** is listed as "ALPHA Superseded", latest milestone v1.29 [S3]. kep.yaml for 5073: `stage: stable`, `latest-milestone: v1.36`, milestones alpha v1.33, beta v1.33, stable v1.36 [S1b].

Feature-gate table (kubernetes.io, v1.37 build) [S4]:
- `DeclarativeValidation`: true/Beta 1.33–1.35; **true/Stable 1.36–** (docs page: "Default: true, LockToDefault: true").
- `DeclarativeValidationTakeover`: false/Beta 1.33–1.35; **Deprecated 1.36–**. CHANGELOG-1.37: "Locked the deprecated `DeclarativeValidationTakeover` feature gate to its default value; it can no longer be set (#139212)" [S6].
- `DeclarativeValidationBeta`: **true/Beta 1.36–** — "Global Safety Switch for Beta-stage validation rules (`+k8s:beta`)".

Contradiction note: the KEP-5073 README phase table still describes v1.36 as "Phase 1 – Introduction" with `DeclarativeValidation` "Beta, default:true" and "v1.39+: Gate Removal & GA" [S1]. The feature-gates table, kep.yaml, CHANGELOG and the 1.36 blog ("Declarative Validation Graduates to GA") disagree; trust those. What is GA is the *framework/gate*; per-rule enforcement is staged: docs (1.37 default): "both validation systems run for Alpha and shadowed rules. Beta rules are enforced. The results of the hand-written validation are used for Alpha rules" [S2]. Mismatches increment `declarative_validation_mismatch_total`.

**OpenAPI publication — settled: NOT published.** KEP-5073 Non-Goals: "It is not a goal of this KEP directly to publish validation rules to OpenAPI" and "It is not a goal of this KEP to expose unenforced validation markers in CR schemas in CR openapi" [S1]. GA blog: "unlocks the **future** ability to publish validation rules via OpenAPI… This paves the way for tools like kubectl, client libraries, and IDEs to perform rich client-side validation" [S5]. kubectl-validate README: "For Native Types, the OpenAPI definitions are a best-effort replication of the handwritten validation rules conducted by the apiserver" [S7].

**Coverage.** KEP goals: "Eliminate 90% of net-new hand-written validation within 5 kube releases (target start: v1.33)", "Convert 50% of existing hand-written validation within 5 kube releases". Analysis: of "~1181 validation rules… ~15% forbidden… ~10% object name… ~10% cross-field… The remaining 65% can be represented using JSON Schema value validations" [S1]. 1.37 contributor blog: "Roughly 75% of new validations were written in DV" in 1.37; 118 API-review PRs [S8]. No primary source quantifies the share of *existing* native validation migrated to date — UNVERIFIED.

**Implication.** For core types (Deployment, Service, Pod…) an offline validator using `/openapi/v3` gets types/required/enum/format/listType and nothing from `+k8s:` markers, immutability, unions, or cross-field rules. Exact parity requires executing an apiserver (envtest/kwok) or vendoring k8s validation packages. Fidelity tier for native types: "schema + heuristics", not "as the apiserver would". Only CRDs (x-kubernetes-validations in the published CRD schema) reach near-exact offline fidelity.

## 2. Manifest-based admission control (KEP-5793)

**Verdict.** Gate `ManifestBasedAdmissionControlConfig`: false/Alpha 1.36; **true/Beta 1.37** [S4][S6: "Graduated the ManifestBasedAdmissionControlConfig feature gate to Beta and enabled it by default (#140559)"]. kep.yaml: alpha v1.36, beta v1.37, stable unset [S9b]. The 1.36 blog is titled "Admission Policies That Can't Be Deleted" [S10]; a fetched summary calling the gate "StaticAdmissionPolicies" is wrong (no such gate in the table).

API visibility — by design none. KEP: "Manifest-based admission control objects are not visible through the Kubernetes API. These objects cannot be controlled through the API by design, may not be synchronized between API servers, and exposing them (similar to mirror pods) has proven error-prone in practice"; "Isolated universe… no paramKind"; risk table: "Manifest-based configurations are not visible via the API. → Dedicated metrics expose loaded configuration counts and health. Audit annotations indicate manifest-based sources. API server logs show loaded configurations at startup" [S9]. Docs: names must end `.static.k8s.io`; supported plugins ValidatingAdmissionWebhook/MutatingAdmissionWebhook/ValidatingAdmissionPolicy/MutatingAdmissionPolicy; only `admissionregistration.k8s.io/v1`; webhooks must use `clientConfig.url`; `spec.paramKind`/`paramRef` not allowed [S11]. No plan to expose them (confidence high — KEP explicitly rejects mirror-pod-style exposure).

**Implication.** Snapshot import from the API cannot capture static policies. Add a "static admission bundle" import path (operator hands over the directory; policies are plain v1 YAML so the same evaluator works), and surface "unknown static policies may exist (1.37+)" as a fidelity caveat. Audit-log annotations are the only in-cluster evidence.

## 3. MutatingAdmissionPolicy

**Verdict.** KEP-3962 kep.yaml: alpha v1.32, beta v1.34, stable v1.36 [S12]. Gate table: `MutatingAdmissionPolicy` false/Alpha 1.30–1.33, false/Beta 1.34–1.35, **true/Stable 1.36–** [S4]. CHANGELOG-1.36: "Promoted the MutatingAdmissionPolicy to GA (v1)… enabled by default (#136039)" [S13]. CHANGELOG-1.37: "MutatingAdmissionPolicy and MutatingAdmissionPolicyBinding are now stored in etcd as admissionregistration.k8s.io/v1 (#137375)" [S6]. Docs examples use `admissionregistration.k8s.io/v1` [S14]. Pre-1.36: v1alpha1 (1.32–1.33), v1beta1 (1.34–1.35), gate + runtime-config required.

**Implication.** Snapshot must read v1 on ≥1.36 and fall back to v1beta1/v1alpha1 on 1.34/1.35 clusters (Kyverno 1.19 also added "support MutatingAdmissionPolicy v1 on Kubernetes 1.36" #16703 [S25]). Evaluating MAP offline needs an ApplyConfiguration/JSONPatch mutator; Argo's SSD ignores mutations anyway (item 5).

## 4. CRD validation rules and ratcheting

**Verdict.** Docs: "Validation rules — FEATURE STATE: stable v1.29" [S15]. Ratcheting gate `CRDValidationRatcheting`: Alpha 1.28–1.29, Beta 1.30–1.32, **Stable 1.33–** [S4]. The docs page describes the cost system qualitatively only; numbers come from source (kubernetes master) [S16][S17]:
- `StaticEstimatedCostLimit = 10000000` (per-expression static estimate), `StaticEstimatedCRDCostLimit = 100000000` ("largest-allowed total cost for the x-kubernetes-validations rules of a CRD").
- `PerCallLimit = 1000000` ("roughly 0.1 second for each expression validation call"), `RuntimeCELCostBudget = 10000000` ("overall cost budget for runtime CEL validation cost per ValidatingAdmissionPolicyBinding or CustomResource… roughly 1 second"), `RuntimeCELCostBudgetMatchConditions = 2500000`, `MaxEvaluatedMessageExpressionSizeBytes = 5*1024`.

**Implication.** Reuse `k8s.io/apiserver/pkg/cel` + apiextensions schema/cel packages with these constants so offline results (including budget-exceeded errors and ratcheting of unchanged fields when `oldObject` is provided) match the server.

## 5. Argo CD Server-Side Diff

**Verdict.** docs/user-guide/diff-strategies.md (master): "Legacy: This is the main diff strategy used by default"; "Server-Side Diff — Current Status: Stable (Since v3.1.0)… used automatically for Applications that enable the Server-Side Apply sync option… can be overridden by… `ServerSideDiff=false`" [S18]. `argocd-cmd-params-cm`: `controller.diff.server.side: "false"` [S19]. New resources: "Server-Side Diff will not be performed during the creation of new resources… validation webhooks won't be executed when calculating diffs if the resource is not applied in the cluster yet" [S18]. Mutating webhooks: "Server-Side Diff does not include changes made by mutation webhooks by default… `argocd.argoproj.io/compare-options: IncludeMutationWebhook=true`… only effective when Server-Side Diff is enabled" [S18].

3.3→3.4 and 3.4→3.5 upgrade guides contain no diff-default changes (3.4: `Missing` health only when ALL resources missing, cluster version format `vMajor.Minor.Patch`; 3.5: Helm 4.2.1, React 19, EventList gRPC type, mTLS opt-in, Source Integrity replaces GnuPG) [S20][S21]; 3.0→3.3 guides have no SSD mentions either. 3.5.x SSD-related fixes only: "apply HideSecretData to server-side diff results for Secrets (#27598)", "reuse server-side diff result when masking Secret data (#27858)" [S22].

`argocd-cm.yaml`: `timeout.reconciliation: 120s` ("Two minutes by default with additional jitter"), `timeout.reconciliation.jitter: 60s` ("defaults to 1 minute") [S23]. 3.5.0 added `webhook.refresh.jitter` (default 0, "Add Configurable Jitter for Webhook-Triggered application Refreshes #25433") — it did not change the 120s/60s defaults. Releases: v3.5.0 2026-08-04, v3.5.1 2026-08-12, v3.5.2 2026-08-27 [S22].

**Implication.** Argo's pre-sync feedback is weaker than assumed: new resources get client-side diff only (no admission), mutations invisible by default, SSD off unless SSA or explicit opt-in. A local pre-push validator that runs admission for *new* objects is additive, not redundant.

## 6. Gatekeeper VAP generation and gator

**Verdict.** Docs: "VAP management through Gatekeeper — Feature State: Gatekeeper version v3.20 (beta)… enabled by default"; "By default both flags [`--default-create-vap-for-templates`, `--default-create-vap-binding-for-constraints`] are set to `true` now that the feature is in beta"; "Requires minimum Kubernetes v1.30" [S24]. v3.20.0 notes: "VAP integration is beta and enabled by default, hence VAP/VAPB resources will be generated by default for CT/C with `K8sNativeValidation` engine with `CEL` code" [S24b]; v3.20.0 date not verified (latest: v3.23.1 2026-08-27, v3.24.0-beta.0 2026-07-13). `--sync-vap-enforcement-scope` deprecated, removal v3.24.

gator: "Flag `enable-k8s-native-validation`… By default… `true`"; new `default-k8s-native-validation-failure-policy` (Fail|Ignore); "The CEL engine does not support referential constraints"; "Gator cannot determine if a type is Namespace-scoped… Always specify `metadata.namespace`" [S26]. AdmissionReview inputs are supported in `gator verify` with `userInfo`, but the documented access path is Rego (`input.review.userInfo.username`). Documented CEL variables: `variables.params`, `variables.anyObject`, `namespaceObject` ("aligns with the Kubernetes Validating Admission Policy `namespaceObject` variable"), plus object/oldObject [S27]. `request.userInfo`/`authorizer` for the CEL engine: not documented → UNVERIFIED (medium: likely unsupported).

**Implication.** Prefer consuming the *generated* VAP/VAPB from the cluster (same evaluator as native VAP) over re-implementing the Gatekeeper engine; Rego templates need OPA embedding and referential data (inventory) from the snapshot.

## 7. KWOK v0.8 snapshot / envtest

**Verdict.** kwok v0.8.0 released 2026-06-23 [S28]. `kwokctl snapshot export` (marked experimental) default `--filter`: `namespace,node,serviceaccount,configmap,secret,limitrange,runtimeclass.node.k8s.io,priorityclass.scheduling.k8s.io,clusterrolebindings…,clusterroles…,rolebindings…,roles…,daemonset.apps,deployment.apps,replicaset.apps,statefulset.apps,cronjob.batch,job.batch,persistentvolumeclaim,persistentvolume,pod,service,endpoints` [S29]. So **by default no CRDs, no CustomResources, no VAP/MAP/bindings, no webhook configurations, no ResourceQuota, no StorageClass/IngressClass/NetworkPolicy** (LimitRange yes). Extra kinds can be added via `--filter`. Restore (`snapshot/load.go`): re-links `ownerReferences`; if the object carries `status`, it calls `UpdateStatus(... FieldValidation: "Ignore")` → status (e.g. ResourceQuota.status) is restored when exported. No webhook-specific handling in load.go: webhook configurations would be created verbatim and, with their Services absent, admission calls fail per `failurePolicy` — inference, UNVERIFIED by docs. Admission: `kwokctl create cluster --kube-admission` "(default true)"; when enabled kwok passes no `--admission-control`, i.e. upstream default plugin set applies (PodSecurity, LimitRanger, ResourceQuota, VAP, MAP…); `--kube-feature-gates`, `--kube-runtime-config` accepted [S30][S31].

envtest: controller-runtime v0.25.0 (2026-09-03) [S32]; `APIServer.Configure() *process.Arguments` "may be used to customize the flags used to launch the API server"; `DefaultKubeAPIServerFlags` deprecated [S33]. Passing `--admission-control-config-file` + `--feature-gates=ManifestBasedAdmissionControlConfig=true` is therefore mechanically possible (1.36 binaries), but no doc or test confirms it — UNVERIFIED. Startup times: no published numbers for kwokctl binary runtime or envtest (KWOK only claims "in seconds"; envtest `ControlPlaneStartTimeout` default 20s) — UNVERIFIED.

**Implication.** Do not rely on `kwokctl snapshot export` as the snapshot format; build our own exporter (typed list of GVRs incl. CRDs, admission policies, quotas w/ status, classes, PSA labels) and use kwok/envtest only as the optional "execute admission for real" tier.

## 8. PodSecurity AdmissionConfiguration observability

**Verdict.** There is no API. kube-apiserver source (`cmd/kube-apiserver/app/server.go`, `pkg/controlplane/apiserver/*`) contains no `configz`; only the kubelet installs `configz.InstallHandler` [S34]. The docs page gives the `PodSecurityConfiguration` defaults/exemptions file and says it "needs to be specified via the `--admission-control-config-file`" with no read-back path [S35]. EKS blog: "No PSA exemptions are configured at Kubernetes API server startup. The Privileged PSS profile is configured by default for all PSA modes, and set to latest versions"; EKS does not allow customizing the static config [S36]. GKE: no access to API server flags; namespace labels only [S37]. OpenShift: defaults live in cluster-kube-apiserver-operator bindata (`PodSecurity… exemptions: usernames: system:serviceaccount:openshift-infra:build-controller`, enforce privileged / audit+warn restricted per Red Hat docs) [S38]; the merged config is readable in-cluster (openshift-kube-apiserver `config` ConfigMap) — medium. kubeadm/self-managed: the static-pod manifest reveals the flag but the file lives on the node.

**Implication.** Model PSA cluster defaults/exemptions as declared assumptions with vendor presets (EKS/GKE/AKS: privileged/latest, no exemptions; OpenShift preset), overridable by the deployer; evaluate namespace labels from the snapshot exactly (PSA library `k8s.io/pod-security-admission` is importable).

## 9. Kyverno CLI 1.19.1 / Playground

**Verdict.** v1.19.0 2026-08-20, v1.19.1 2026-09-10 [S25]. Blog: "Official deprecation of ClusterPolicy and Policy, with removal planned for v1.20"; "the Kyverno Playground supports all the new policy types" [S39]. CLI docs: `kyverno apply` supports ClusterPolicy, VAP+VAPB, MAP+MAPB, ValidatingPolicy, PolicyException (+ `--parameter-resource` "Path to resource files that act as ValidatingAdmissionPolicy/MutatingAdmissionPolicy parameters", `--context-file` "File containing context data for CEL policies", Context kind `cli.kyverno.io/v1alpha1` with `spec.resources`); without `--cluster` "the CLI automatically builds an in-memory dynamic client from the supplied manifests and enables `apiCall` against that snapshot" [S40][S41]. Test files: `apiCallResponses` ("inline mock responses for context.apiCall and CEL http.Get()/http.Post() calls") and `globalContextEntries` [S40]. 1.19 additions: http.Post mocks (#16297), k8sresource-backed GCE mocks (#16123), "support admission operation simulation for policy testing" (#16787), MAP v1 on 1.36 (#16703) [S25]. Limitation: "does not embed the Kubernetes control plane components and therefore is not able to perform the types of initial mutations subjected to a resource as part of an in-cluster creation flow"; external HTTP "out of scope for offline evaluation" [S40]. Playground: "It is currently not possible to add variables from external resources or do actual API calls. It is only possible to mock variables"; supports `username, groups, roles, cluster roles` [S42].

**Implication.** Kyverno's own engine (Go module) plus a snapshot-backed fake dynamic client gives near-exact offline Kyverno results; supply userInfo/operation and namespace objects from the snapshot.

## 10. Release housekeeping

- Helm 3 EOL: "Final feature release: September 9th, 2026"; "Bug fixes up to… September 9th, 2026"; "Security fixes end February 10th, 2027 (extended from November 2026)"; after that no updates [S43]. Helm 4 GA 2025-11-12.
- Timoni: v0.34.0 2026-08-30 (v0.29–0.34 all Aug 2026; `timoni mod vet` "validates the rendered custom resources against their CRD schemas and CEL rules") [S44].
- kubeconform: v0.8.0 2026-06-04 (prev v0.7.0 2025-05-12) [S45].
- Weave GitOps: last stable v0.38.0 2023-12-06; v0.39.1-rc.1 2026-01-25; repo not archived [S46].
- Datree: archived 2024-06-06; company closed July 2023; last release 1.9.19 2023-07-23 [S47].
- Kubernetes 1.36.0 GA 2026-04-22 (secondary sources; releases page: 1.36.4 2026-08-11, EOL 2027-06-28); 1.37.0 2026-08-26 (EOL 2027-10-28); 1.34 EOL 2026-10-27 [S48].

## 11. KYAML

**Verdict.** KEP-5295 kep.yaml: alpha v1.34, beta v1.35, **stable v1.37** [S49]. CHANGELOG-1.37: "Promoted support for `kubectl get -o kyaml` to Stable (#140076)" [S6]. Alpha was gated by env `KUBECTL_KYAML=true`. 
**Implication.** Emit KYAML for generated manifests/diffs only as an option; Git-stored values stay plain YAML for Helm/Kustomize compatibility.

---

## Sources

1. https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/5073-declarative-validation-with-validation-gen/README.md — 1b. …/kep.yaml
2. https://kubernetes.io/docs/reference/using-api/declarative-validation/
3. https://www.kubernetes.dev/resources/keps/4153/
4. https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/
5. https://kubernetes.io/blog/2026/05/05/kubernetes-v1-36-declarative-validation-ga/
6. https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-1.37.md
7. https://github.com/kubernetes-sigs/kubectl-validate (README)
8. https://www.kubernetes.dev/blog/2026/08/27/kubernetes-v1-37-declarative-validation/
9. https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/5793-manifest-based-admission-control-config/README.md — 9b. …/kep.yaml
10. https://kubernetes.io/blog/2026/05/04/kubernetes-v1-36-manifest-based-admission-control/
11. https://kubernetes.io/docs/reference/access-authn-authz/manifest-admission-control/
12. https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/3962-mutating-admission-policies/kep.yaml
13. https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-1.36.md
14. https://kubernetes.io/docs/reference/access-authn-authz/mutating-admission-policy/
15. https://github.com/kubernetes/website/blob/main/content/en/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions.md
16. https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/apis/cel/config.go
17. https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation/validation.go
18. https://github.com/argoproj/argo-cd/blob/master/docs/user-guide/diff-strategies.md
19. https://github.com/argoproj/argo-cd/blob/master/docs/operator-manual/argocd-cmd-params-cm.yaml
20. https://argo-cd.readthedocs.io/en/stable/operator-manual/upgrading/3.3-3.4/
21. https://argo-cd.readthedocs.io/en/stable/operator-manual/upgrading/3.4-3.5/
22. https://github.com/argoproj/argo-cd/releases (v3.5.0/3.5.1/3.5.2 via GitHub API)
23. https://github.com/argoproj/argo-cd/blob/master/docs/operator-manual/argocd-cm.yaml
24. https://open-policy-agent.github.io/gatekeeper/website/docs/validating-admission-policy/ — 24b. https://github.com/open-policy-agent/gatekeeper/releases/tag/v3.20.0
25. https://github.com/kyverno/kyverno/releases (v1.19.0, v1.19.1 bodies via API)
26. https://open-policy-agent.github.io/gatekeeper/website/docs/gator/
27. https://open-policy-agent.github.io/gatekeeper/website/docs/constrainttemplates/
28. https://github.com/kubernetes-sigs/kwok/releases
29. https://kwok.sigs.k8s.io/docs/generated/kwokctl_snapshot_export/
30. https://kwok.sigs.k8s.io/docs/generated/kwokctl_create_cluster/
31. https://github.com/kubernetes-sigs/kwok/blob/main/pkg/kwokctl/components/kube_apiserver.go ; …/pkg/kwokctl/snapshot/load.go ; https://kwok.sigs.k8s.io/docs/user/kwokctl-snapshot/
32. https://github.com/kubernetes-sigs/controller-runtime/releases
33. https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/internal/testing/controlplane/apiserver.go ; …/pkg/envtest/server.go
34. https://github.com/kubernetes/kubernetes/blob/master/cmd/kube-apiserver/app/server.go ; …/pkg/kubelet/server/server.go
35. https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-admission-controller/
36. https://aws.amazon.com/blogs/containers/implementing-pod-security-standards-in-amazon-eks/
37. https://docs.cloud.google.com/kubernetes-engine/docs/how-to/podsecurityadmission
38. https://github.com/openshift/cluster-kube-apiserver-operator/blob/master/bindata/assets/config/defaultconfig.yaml ; https://www.redhat.com/en/blog/pod-security-admission-in-openshift-4.11
39. https://kyverno.io/blog/2026/08/20/announcing-kyverno-release-1.19/
40. https://kyverno.io/docs/subprojects/kyverno-cli/
41. https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
42. https://github.com/kyverno/playground/blob/main/README.md
43. https://helm.sh/blog/helm-v3-end-of-life/
44. https://github.com/stefanprodan/timoni/releases/tag/v0.34.0
45. https://github.com/yannh/kubeconform/releases
46. https://github.com/weaveworks/weave-gitops/releases
47. https://github.com/datreeio/datree
48. https://kubernetes.io/releases/ ; https://docs.k3s.io/blog/2026/05/27/K3s-1.36-release
49. https://github.com/kubernetes/enhancements/blob/master/keps/sig-cli/5295-kyaml/kep.yaml