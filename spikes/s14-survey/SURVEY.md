# fathom practitioner survey (S14)

*Draft 1, 2026-09-17. Everything from "Part 1" to the end of "Part 4" is the
questionnaire to send. "Part 5" is the protocol for the hands-on session with
three consultants. The "Sender annex" at the end is for the owner only: it holds
the decision rules that are fixed before the first answer arrives. Delete it
before sending.*

---

## Intro text (send as is)

We are deciding what a new pre-merge check for Kubernetes and Compose
deployments should catch first. The tool will render a Helm, Kustomize or
Compose change and validate it against a read-only snapshot of the target
cluster before the pull request opens, so that quota, policy, schema and
reference errors are caught on your laptop instead of at sync time.

This survey takes 10 to 15 minutes. It asks what actually breaks for you today,
which clusters and repositories your customers run, and how you edit values.
Answers are anonymous; the customer rows in Part 3 ask for categories only, not
names. The free-text answers at the end are the most valuable part.

Question IDs (A1, B1.3, C2.1, ...) are for tallying; ignore them.

---

## Part 1: About you (3 questions)

**A1. Which role describes most of your work?** (one)
- Developer: I write the charts, Compose files or application manifests
- Consultant: I deploy our software into customer clusters
- Supporter: I operate or troubleshoot installations that are already deployed
- Platform: I run clusters, policies and GitOps controllers
- Mixed: two or more of the above in roughly equal parts

**A2. How long have you worked with Kubernetes?** (one)
- Less than 1 year
- 1 to 3 years
- 3 to 6 years
- More than 6 years

**A3. How many distinct customer clusters have you deployed to or supported in the last 12 months?** (one)
- 0
- 1 to 2
- 3 to 5
- 6 to 10
- More than 10

---

## Part 2: What breaks (2 grids and 3 questions)

Think about the last three months of deployment changes you or your team made:
values changes, chart upgrades, new environments, Compose changes.

**B1. For each kind of failure below, how often did it hit you in the last three
months?** (grid; one answer per row)

Columns: Never | Once or twice | About monthly | About weekly | Daily or more

| ID | Failure kind (with the message you would recognise) |
|---|---|
| B1.1 | YAML syntax or typing surprise: indentation, `NO`/`ON` read as a boolean, `1e3` read as a number, a key silently ignored |
| B1.2 | Wrong value type or unit in values: `1000M` instead of `1000m`, a string where a number was expected, a misspelled key nobody noticed |
| B1.3 | Rendered manifest rejected by the API schema: `unknown field`, wrong type, wrong `apiVersion` |
| B1.4 | API version removed or deprecated on the target cluster version (`no matches for kind ... in version ...`) |
| B1.5 | A custom resource rejected by its CRD validation rule (Gateway API, Crossplane, Flux, operator CRs with cross-field rules) |
| B1.6 | Admission policy denied it: Kyverno, Gatekeeper/OPA, ValidatingAdmissionPolicy, Kubewarden (registry not allowed, label missing, privileged container) |
| B1.7 | Pod Security Admission: the Deployment was accepted but no Pod appeared (`violates PodSecurity "restricted:latest"`) |
| B1.8 | ResourceQuota exceeded (`exceeded quota: ... requested: 2000m, available: 1000m`) |
| B1.9 | LimitRange rejected the Pod (min/max/ratio) or applied defaults you did not expect |
| B1.10 | A referenced object was missing: Secret or ConfigMap key, StorageClass, IngressClass, PriorityClass, ServiceAccount (`CreateContainerConfigError`, PVC stuck Pending) |
| B1.11 | Image not pullable: tag does not exist, registry blocked or not mirrored, image unsigned (`ImagePullBackOff`) |
| B1.12 | The GitOps controller lacked permissions (`forbidden: User system:serviceaccount:... cannot create resource`) |
| B1.13 | Immutable field or ownership conflict: selector, `clusterIP`, PVC storage class, field-manager fights between Helm, Argo CD, Flux or a mutating policy |
| B1.14 | A webhook you do not control denied or changed the object with an unhelpful message (cert-manager, vendor mutators, cloud-provider webhooks) |
| B1.15 | Nothing could schedule it: taints, node selectors, node pools, Autopilot resource ratios; Pods stuck Pending |
| B1.16 | The effective value after merging layers was not what you expected (base, environment, cluster overrides), or it worked in dev and not in prod |
| B1.17 | Docker Compose: an unset variable became empty, a port was already allocated, a bind path was missing, `deploy:` was ignored, `container_name` collided |
| B1.18 | OpenShift specific: SCC or UID range rejection (`runAsUser ... out of range`, `restricted-v2` denies), Route host conflict, project template quota you did not know about |

**B2. For the same kinds, where was the failure usually discovered?** (grid; one
answer per row; leave a row blank if it never happened)

Columns: Before pushing, on my machine | In CI, before merge | At GitOps sync or `helm upgrade`, after merge | Pods Pending, crashing or at runtime | The customer noticed first

Rows: B2.1 to B2.18, same kinds as B1.

**B3. Which three kinds cost you the most working hours in the last three months?**
(pick up to three by number, most expensive first)

B3.first: ____ B3.second: ____ B3.third: ____

**B4. For the most expensive one, roughly how many working hours did it cost in
the last three months, across your team?** (one)
- Under 2 hours
- 2 to 8 hours
- 1 to 3 working days
- More than 3 working days

**B5. Is there a failure kind missing from the list?** (free text, optional)

---

## Part 3: The clusters you deploy to (per customer, up to five)

Fill one column per customer cluster you have deployed to or supported in the
last 12 months, most recent first. If there are more than five, take the five
you spent the most time on. Categories only; no customer names. Developers who
do not deploy: skip to Part 4.

| ID | Question | Customer 1 | Customer 2 | Customer 3 | Customer 4 | Customer 5 |
|---|---|---|---|---|---|---|
| C1 | Distribution: OpenShift / AKS / EKS / GKE Standard / GKE Autopilot / Rancher RKE2 or K3s / Tanzu / kubeadm or other vanilla / other | | | | | |
| C2 | Kubernetes minor version of production: 1.30 or older / 1.31 / 1.32 / 1.33 / 1.34 / 1.35 / 1.36 / 1.37 / do not know | | | | | |
| C3 | How Helm runs there: Argo CD / Flux / Helmfile / CI pipeline runs `helm upgrade` / a person runs `helm upgrade` from a laptop / Rancher Fleet / OLM operator / Kustomize `helmCharts` / no Helm, plain manifests or Kustomize | | | | | |
| C4 | Where the configuration repository lives: GitHub.com / GitHub Enterprise / GitLab.com / GitLab self-managed / Azure DevOps / Bitbucket / Gitea or Forgejo / other / there is no Git repository, values are handed over by mail or ticket | | | | | |
| C5 | Policy engines enforced (list all): Kyverno / Gatekeeper / ValidatingAdmissionPolicy or MutatingAdmissionPolicy / Kubewarden / Azure Policy / none / do not know | | | | | |
| C6 | Your own identity on that cluster: cluster-admin / cluster-wide read plus namespace admin / namespace-scoped only / no direct access, the customer applies changes / do not know | | | | | |
| C7 | Network: air-gapped / egress restricted with a mirror registry / open | | | | | |
| C8 | Secrets: External Secrets Operator / Sealed Secrets / SOPS / Vault injector / manually created / do not know | | | | | |
| C9 | Number of environments for our software at this customer (dev, staging, prod, ...): 1 / 2 / 3 / 4 or more | | | | | |
| C10 | Repository layout: DRY, the controller renders values and overlays / hydrated, rendered manifests are committed / both / do not know | | | | | |
| C11 | Regulated context (BSI, ISO 27001, KRITIS, FIPS or similar audit requirements apply to the deployment): yes / no / do not know | | | | | |
| C12 | Docker Compose deployments of our software at this customer: yes, in production / yes, dev or test only / no | | | | | |
| C13 | Who opens the change to the configuration repository: I do / the customer does / I push directly without review / there is no repository | | | | | |

---

## Part 4: How you work today (9 questions)

**D1. Before you open a pull request or merge request with a values change, what
do you run on your machine?** (all that apply)
- `helm template` or `helm lint`
- kubeconform or kubeval
- `kubectl apply --dry-run=server` against the target cluster
- `kyverno apply`, `gator test` or `conftest`
- `argocd app diff` or `flux diff`
- A script or Makefile the team maintains
- Nothing; CI or the controller tells me
- Other: ____

**D2. Where do you edit environment values?** (all that apply)
- Editor with a YAML schema attached (VS Code YAML extension, IntelliJ): which one: ____
- Editor without a schema
- Argo CD UI parameters tab
- A web form (Rancher, Kubeapps, a vendor console)
- Directly in the Git hosting web editor
- Other: ____

**D3. Do the charts you deploy ship a `values.schema.json`?** (one)
- Always
- Sometimes
- Rarely
- Never
- Do not know

**D4. If you author charts: does your chart ship a `values.schema.json`?** (one)
- Yes, maintained by hand
- Yes, generated from comments or a tool
- Partially
- No, but I would if it were generated from the existing values
- No
- I do not author charts

**D5. Typically, how long after pushing a change do you learn that it failed at
the customer?** (one)
- Under 1 minute
- 1 to 10 minutes
- 10 to 60 minutes
- Several hours
- The next day or later

**D6. Would a read-only snapshot of the customer cluster (CRDs, policies, quotas,
namespace labels, Secret names but never Secret values) be acceptable ...**
(one per row)

Columns: Yes | Only after a security review | No | Do not know

- D6.laptop: ... stored on your laptop
- D6.repo: ... committed to the configuration repository
- D6.registry: ... pushed to a container registry as a signed artifact

**D7. What is the one thing you most want caught before the pull request opens?**
(free text, one or two sentences)

**D8. What is the one thing you fear such a tool would get wrong or make worse?**
(free text, one or two sentences)

**D9. May we contact you for a 45-minute hands-on session where you make two
values changes with an editor and with a prototype form?** (one)
- Yes: name or handle: ____
- No

Thank you. Results go back to everyone who answered.

---

## Part 5: Hands-on session, form versus editor (protocol)

This is the task-based arm from design section 3.6. It is not a questionnaire;
it is a moderated 45-minute session with three consultants (recruited via D9),
run in weeks 5 to 6 of the sprint once the S9 form prototype exists.

### Setup

- Chart: Bitnami redis 28.2.1 with the synthetic layered values fixture from
  S6, replaced by the company's chart and real layered files as soon as they
  are available (then mark the result "company inputs").
- Cluster fixture: the kind cluster from the calibration spikes, with a
  `restricted` namespace, a ResourceQuota, a LimitRange and two StorageClasses,
  one of them RWX-capable.
- Editor arm: VS Code with the YAML extension, the per-environment schema
  attached by modeline (the `.fathom/schema.<env>.json` family from design
  section 3.8), and a `make validate` target that renders the change and runs
  `kubectl apply --dry-run=server` against the kind cluster. During the sprint
  this stands in for `fathom validate`; the moderator notes every place where
  the stand-in differs.
- Form arm: the S9 prototype form served locally against the same fixture,
  including the "currently from / will write to" affordance.
- Both arms end with the participant opening a pull request in a throwaway
  repository.

### Tasks

Two tasks of equal size; each participant does one task per arm so nobody
repeats a task.

- Task A: for environment `eu-prod`, move persistence to the RWX-capable
  storage class, raise the volume to 20 Gi, and keep every other environment
  unchanged. The fixture contains a trap: the base layer sets the storage class
  by anchor and the environment layer inherits it by alias.
- Task B: for environment `eu-prod`, enable the metrics exporter with a
  ServiceMonitor, set CPU and memory requests that fit the namespace quota, and
  point the image registry at the customer's mirror. The fixture contains a
  trap: the quota leaves room for one replica set at the default requests but
  not two.

Counterbalanced order (fixed before recruiting):

| Participant | First arm | Second arm |
|---|---|---|
| P1 | editor, task A | form, task B |
| P2 | form, task A | editor, task B |
| P3 | editor, task B | form, task A |

### Measures (recorded per arm, per participant)

| ID | Measure | How |
|---|---|---|
| E1 | Time to a pull request that passes validation | Stopwatch from task card handed over to PR opened |
| E2 | Validation failures before the first green run | Count from the tool log |
| E3 | Defects in the opened PR | Moderator compares the PR against the answer key: wrong file, wrong environment touched, trap not handled, value in the wrong layer |
| E4 | Confidence (1 to 7) | Asked right after the arm: "How sure are you that this PR is correct?" |
| E5 | Preference | Asked after both arms: editor / form / depends, with one sentence why |
| E6 | Think-aloud notes | Moderator notes every hesitation, every wrong turn and every request for information the tool did not show |

### Recording sheet

```
participant: P_   date: ____   moderator: ____
arm 1: ______ task: _   E1: __:__   E2: __   E3 defects: ______________   E4: _
arm 2: ______ task: _   E1: __:__   E2: __   E3 defects: ______________   E4: _
E5 preference: ______   why: ________________________________________________
E6 notes: ______________________________________________________________________
```

---

## Sender annex (delete before sending)

### What each part decides

| Survey part | Decision it feeds | Design reference |
|---|---|---|
| B1, B2, B3 | Order of the check backlog inside M1 and months 5 to 6 | design 10.2; landscape report 3.1 |
| C1, C2 | OpenShift-first profile; which managed distribution gets the first phase-2 profile; whether 1.34/1.35 MAP v1beta1 fallback matters | design 0 item 10, section 7, S8 |
| C3 | Priority of the `plain` layout reader versus Argo/Flux/Helmfile readers in M1 | design 5.1, 12 item 4 |
| C4, C13 | GitLab commit-status and MR-note adapter in months 5 to 6 or later | design 3.8, 12 item 3 |
| C5 | Kyverno-first confirmation; whether Gatekeeper Rego (S12) and Kubewarden move up | design 3.3, 10.3 |
| C6, D6 | The namespace-scoped first-hour path; snapshot sharing tiers | design 1.3, 3.5 |
| C7, C11 | Bundle export in M1 stays; regulated pack timing | design 7, 12 item 6 |
| C8 | ESO references only, or SOPS editing too | design 12 item 7 |
| C10 | Hydrated versus DRY primacy | design 12 item 1 |
| C12 | Compose host snapshot timing | design 6, 12 item 9 |
| D1, D5 | The "beat CI from below" copy and the two cadences | design 3.6 |
| D2, D3, D4 | Whether the contract-emit and modeline path lands before the form; the no-contract demo | design 1.2, 3.8 |
| Part 5 | Form residual: Go-native form, RJSF island, or editor only | design 3.6 |

### Pre-registered decision rules

These are written down now so the result cannot be bent later. `tally` in this
directory computes them from a CSV export of the answers.

1. **Backlog order.** For each failure kind k, score(k) is the sum over
   respondents of frequency(B1.k) x lateness(B2.k), with frequency weights
   Never 0, Once or twice 1, Monthly 2, Weekly 4, Daily 8, and lateness weights
   pre-push 0.5, CI 1, sync 2, runtime 3, customer 4. Kinds are ordered by score;
   B3 ranks are the tie-break and a sanity check (a kind in the top five by
   score but never named in B3 is reported as a discrepancy). Kinds the design
   cannot detect offline (B1.14) order the webhook-prediction copy, not the
   check backlog.
2. **OpenShift-first confirmed** if OpenShift is the plurality of C1 rows, or
   at least 40 percent of rows. If another distribution is the plurality, the
   MVP data-tier profile moves to that distribution and OpenShift becomes the
   first phase-2 profile.
3. **GitLab adapter in months 5 to 6** if GitLab.com plus GitLab self-managed
   exceed 30 percent of C4 rows that name a Git host (rows answering "no Git
   repository" are excluded from the denominator and counted separately as the
   "handover" share, which feeds the bot-mode copy).
4. **Plain reader first** if "CI pipeline runs helm upgrade" plus "a person runs
   helm upgrade from a laptop" exceed Argo CD plus Flux in C3; otherwise Argo
   and Flux readers land first and Helmfile follows the larger of Helmfile and
   plain.
5. **Compose host snapshot** stays in phase 2 unless "yes, in production"
   exceeds 40 percent of C12 rows; then open decision 9 goes to the owner with
   the number.
6. **Form residual (Part 5).** The form is worth a Go-native renderer if, in at
   least two of three participants, the form arm has fewer E3 defects or an E1
   time at least 30 percent shorter, and at least two of three prefer it in E5.
   If neither holds, the form becomes the RJSF island from design 3.6. If the
   editor arm wins both E3 and E5 for all three, the form leaves the MVP.

### Sample and timing

- Send to every consultant, supporter, developer and platform person at the
  company. A result needs at least 8 responses and at least 15 customer rows;
  RESULT.md reports n either way and marks a smaller sample "indicative".
- Close after two weeks (end of week 3). Tally with `go run ./cmd/tally
  responses.csv`, paste the output into RESULT.md, update the tracker.
- Part 5 runs in weeks 5 to 6 after S9 delivers the prototype; its result is
  appended to RESULT.md.

### Export format for the tally

One CSV row per respondent, one column per question ID, header row with the
IDs. Grid answers use the exact column labels above. Multi-select answers are
separated by `;`. Customer columns are `C1.1` to `C13.5` (question.customer).
B3 uses `B3.first`, `B3.second`, `B3.third` with the failure-kind number. The
free-text columns are copied verbatim. `testdata/responses.csv` in this
directory is a synthetic example with the right shape.
