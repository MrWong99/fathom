#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Spike-owned oracle probes for engine/testdata (the goldens under
# testdata/golden stay owned by testdata/commands.sh). Assumes commands.sh has
# run: ClusterPolicies (a)-(c), the VAP (d), the s3 side data and Deployment
# s3/web-passing are on the cluster. Everything this script creates it deletes.
#
#  1. vpol-no-validation-actions.server.txt: (f) without spec.validationActions
#     applied as vpol-default-action-probe; kubectl --dry-run=server of
#     objects/violating-team.yaml, verbatim. The ClusterPolicy webhook (a) is
#     also on the cluster, so the text alternates (webhook race): the
#     ClusterPolicy text was seen once in three runs on 2026-09-18 and is kept
#     as .alt.server.txt (copied by hand from a run that produced it); the
#     test asserts the union of both.
#  2. map-paramref.dryrun.yaml: MutatingAdmissionPolicy (g) with a ConfigMap
#     paramRef applied at admissionregistration.k8s.io/v1 (the version the
#     server serves; the fixture is written at v1alpha1 for the CLI); the
#     object the server would store for map-object.yaml (dry-run CREATE,
#     -o yaml), which carries both the Kyverno (b) and the MAP mutation.
set -u
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
D="$T/../../testdata"
CTX="${FATHOM_ORACLE_CONTEXT:-kind-fathom-oracle}"
k() { kubectl --context "$CTX" "$@"; }
cd "$T"

# --- 1. ValidatingPolicy without validationActions ----------------------------
k apply -f f-vpol-no-validation-actions.yaml
k wait --for=jsonpath='{.status.conditionStatus.ready}'=true vpol/vpol-default-action-probe --timeout=120s
sleep 3
(cd "$D" && k apply --dry-run=server -f objects/violating-team.yaml) >vpol-no-validation-actions.server.txt 2>&1
echo "vpol-no-validation-actions: exit=$?"
k delete -f f-vpol-no-validation-actions.yaml

# --- 2. MutatingAdmissionPolicy with a ConfigMap paramRef ---------------------
sed 's#admissionregistration.k8s.io/v1alpha1#admissionregistration.k8s.io/v1#' g-map-max-replicas-label.yaml | k apply -f -
sleep 3
k apply --dry-run=server -o yaml -f map-object.yaml >map-paramref.dryrun.yaml 2>&1
echo "map-paramref: exit=$?"
k -n s3 get deploy web-map --ignore-not-found   # must print nothing: dry run only
sed 's#admissionregistration.k8s.io/v1alpha1#admissionregistration.k8s.io/v1#' g-map-max-replicas-label.yaml | k delete -f -
