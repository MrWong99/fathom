#!/usr/bin/env bash
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
#
# Render the S7 40-object app into rendered.yaml with the Helm CLI. The chain's
# --pre-rendered mode reads that file; its normal mode renders the same three
# charts in-process (S5 render package) from the values files next to this
# script. Rendered with helm v4.3.0; --kube-version/--api-versions mirror the
# oracle snapshot so Capabilities-gated templates (ServiceMonitor,
# PrometheusRule, PodDisruptionBudget) take the same branch as in-process.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
NS=s7
common=(--namespace "$NS" --kube-version 1.37.0
        --api-versions monitoring.coreos.com/v1 --api-versions policy/v1
        --api-versions networking.k8s.io/v1)
{
  helm template redis      charts/redis      -f values-redis.yaml      "${common[@]}"
  helm template postgresql charts/postgresql -f values-postgresql.yaml "${common[@]}"
  helm template rabbitmq   charts/rabbitmq   -f values-rabbitmq.yaml   "${common[@]}"
} > rendered.yaml
python3 - <<'PY'
import collections, yaml
docs = [d for d in yaml.safe_load_all(open("rendered.yaml")) if d]
kinds = collections.Counter(d["kind"] for d in docs)
print("objects:", len(docs))
for k, n in sorted(kinds.items()):
    print(f"  {n:3d} {k}")
PY
