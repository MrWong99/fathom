#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Copy the S7 Kyverno policy layer (50 ClusterPolicies) from
# github.com/kyverno/policies (Apache-2.0) at the pinned commit into
# snapshot/engines/kyverno/, forcing every policy to Enforce with background
# off. The two PolicyExceptions next to them are authored, not copied.
set -euo pipefail
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMIT=2716f4a26a3c27590a1d6d960dee4ce043e4fa4a
SRC="${S7_POLICIES_SRC:-}"
OUT="$T/snapshot/engines/kyverno"
if [ -z "$SRC" ]; then
  SRC="$(mktemp -d "${TMPDIR:-/tmp}/s7-policies.XXXXXX")/policies"
  git clone -q https://github.com/kyverno/policies.git "$SRC"
  git -C "$SRC" checkout -q "$COMMIT"
fi
[ "$(git -C "$SRC" rev-parse HEAD)" = "$COMMIT" ] || { echo "source is not at $COMMIT" >&2; exit 1; }
mkdir -p "$OUT"

POLICIES=(
  # pod-security baseline (validate)
  pod-security/baseline/disallow-capabilities
  pod-security/baseline/disallow-host-namespaces
  pod-security/baseline/disallow-host-path
  pod-security/baseline/disallow-host-ports
  pod-security/baseline/disallow-host-ports-range
  pod-security/baseline/disallow-host-process
  pod-security/baseline/disallow-privileged-containers
  pod-security/baseline/disallow-proc-mount
  pod-security/baseline/disallow-selinux
  pod-security/baseline/restrict-apparmor-profiles
  pod-security/baseline/restrict-seccomp
  pod-security/baseline/restrict-sysctls
  # pod-security restricted (validate)
  pod-security/restricted/disallow-capabilities-strict
  pod-security/restricted/disallow-privilege-escalation
  pod-security/restricted/require-run-as-nonroot
  pod-security/restricted/require-run-as-non-root-user
  pod-security/restricted/restrict-seccomp-strict
  pod-security/restricted/restrict-volume-types
  # best-practices (validate)
  best-practices/disallow-cri-sock-mount
  best-practices/disallow-default-namespace
  best-practices/disallow-helm-tiller
  best-practices/disallow-latest-tag
  best-practices/require-drop-all
  best-practices/require-drop-cap-net-raw
  best-practices/require-labels
  best-practices/require-pod-requests-limits
  best-practices/require-probes
  best-practices/require-ro-rootfs
  best-practices/restrict-image-registries
  best-practices/restrict-node-port
  best-practices/restrict-service-external-ips
  # other (validate)
  other/disallow-secrets-from-env-vars
  other/limit-containers-per-pod
  other/memory-requests-equal-limits
  other/require-container-port-names
  other/require-non-root-groups
  other/restrict-automount-sa-token
  other/restrict-service-port-range
  other/disallow-localhost-services
  other/restrict-loadbalancer
  # mutate
  other/add-default-securitycontext
  other/add-labels
  other/add-ndots
  best-practices/add-safe-to-evict
  other/add-tolerations
  other/add-imagepullsecrets
  other/always-pull-images
  other/add-emptydir-sizelimit
  other/mutate-large-termination-gps
  other/add-nodeSelector
)

n=0
for p in "${POLICIES[@]}"; do
  base="$(basename "$p")"
  src="$SRC/$p/$base.yaml"
  dest="$OUT/$base.yaml"
  python3 - "$src" "$dest" "$p" "$COMMIT" <<'PY'
import re, sys
src, dest, rel, commit = sys.argv[1:]
text = open(src, encoding="utf-8").read()
lines = text.splitlines()
out = []
seen_vfa = seen_bg = False
for l in lines:
    if re.match(r"^  validationFailureAction:", l):
        l = "  validationFailureAction: Enforce"; seen_vfa = True
    elif re.match(r"^  background:", l):
        l = "  background: false"; seen_bg = True
    out.append(l)
is_validate = any(re.match(r"^\s+validate:", l) for l in lines)
ins = []
if is_validate and not seen_vfa:
    ins.append("  validationFailureAction: Enforce")
if not seen_bg:
    ins.append("  background: false")
if ins:
    for i, l in enumerate(out):
        if re.match(r"^spec:\s*$", l):
            out[i+1:i+1] = ins
            break
    else:
        raise SystemExit(f"{src}: no spec: line")
header = f"# source: github.com/kyverno/policies/{rel}/{rel.split('/')[-1]}.yaml @ {commit} (Apache-2.0); Enforce + background=false forced for S7\n"
open(dest, "w", encoding="utf-8").write(header + "\n".join(out).rstrip("\n") + "\n")
PY
  n=$((n+1))
done
echo "copied $n policies to $OUT"
python3 - "$OUT" <<'PY'
import glob, os, sys, yaml
mut = val = 0
for f in sorted(glob.glob(os.path.join(sys.argv[1], "*.yaml"))):
    d = yaml.safe_load(open(f))
    if d["kind"] != "ClusterPolicy":
        continue
    s = d["spec"]
    assert s.get("background") is False, f
    rules = s["rules"]
    if any("mutate" in r for r in rules):
        mut += 1
    if any("validate" in r for r in rules):
        assert s.get("validationFailureAction") == "Enforce", f
        val += 1
print(f"validate={val} mutate={mut}")
PY
