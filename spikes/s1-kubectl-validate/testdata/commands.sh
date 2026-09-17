#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Spike S1 oracle: replays every --dry-run=server scenario against the kind
# cluster and rewrites golden/<scenario>.server.txt with kubectl's stderr,
# verbatim. Run from anywhere; paths are resolved relative to this file.
#
# Oracle used to record the goldens: kind v0.33.0, kindest/node:v1.37.0
# (sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5),
# kubectl client v1.36.4, server v1.37.0. kubectl apply defaults to
# --validate=strict, i.e. server-side fieldValidation=Strict.
set -u
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CTX="${FATHOM_ORACLE_CONTEXT:-kind-fathom-oracle}"
k() { kubectl --context "$CTX" "$@"; }
dry() { # dry <golden name> [kubectl apply flags...]; stdout dropped, stderr kept verbatim
  local name="$1"; shift
  k apply --dry-run=server "$@" >/dev/null 2>"$T/golden/$name.server.txt"
  echo "$name: exit=$?"
}
cd "$T"

k create namespace s1 --dry-run=client -o yaml | k apply -f - >/dev/null

# --- cel-fail / cel-pass / structural: Widget -------------------------------
k apply -f crds/widget.yaml
k wait --for=condition=Established crd/widgets.spike.fathom.dev --timeout=60s
dry cel-fail          -f objects/cel-fail.yaml
dry cel-pass          -f objects/cel-pass.yaml
dry structural        -f objects/structural.yaml                   # strict (default): unknown field rejected first
dry structural-ignore -f objects/structural.yaml --validate=ignore # server prunes spec.colour, reports the type error
dry structural-type   -f objects/structural-type.yaml              # wrong type only

# --- ratcheting: Gadget ----------------------------------------------------
# Version A has no rule; the object is created FOR REAL with spec.name UPPER.
# Version B adds `self == self.lowerAscii()` on spec.name (same served version).
k delete gadget ratchet -n s1 --ignore-not-found >/dev/null
k apply -f crds/gadget-a.yaml
k wait --for=condition=Established crd/gadgets.spike.fathom.dev --timeout=60s
k apply -f objects/ratcheting-initial.yaml
k apply -f crds/gadget-b.yaml
k wait --for=condition=Established crd/gadgets.spike.fathom.dev --timeout=60s
sleep 3 # let the CRD handler pick up the new schema before the dry-runs
k get gadget ratchet -n s1 -o yaml > objects/ratcheting-old.yaml
dry ratcheting-replicas -f objects/ratcheting-replicas.yaml # accepted (ratcheted), warning on stderr
dry ratcheting-name     -f objects/ratcheting-name.yaml     # rejected: spec.name changed to OTHER

# --- budget: Blob ------------------------------------------------------------
k apply -f crds/budget.yaml
k wait --for=condition=Established crd/blobs.spike.fathom.dev --timeout=60s
sleep 2
dry budget -f objects/budget.yaml

# The CRDs and the ratchet Gadget are intentionally left on the cluster.
