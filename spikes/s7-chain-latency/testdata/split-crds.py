#!/usr/bin/env python3
# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT
"""Split multi-document manifests into one file per CustomResourceDefinition.

usage: split-crds.py <out-dir> <source-label> <file>...

Only apiextensions.k8s.io/v1 CRDs are kept; every other document is dropped.
The document text is written verbatim (no YAML re-serialisation) so the file is
the CRD as the project ships it. A CRD name seen before (another project bundling
the same CRD, or standard+experimental gateway-api) is skipped and reported.
Prints one JSON line per source: {"source", "written", "skipped", "cel"}.
"""
import json
import os
import re
import sys

import yaml

try:
    Loader = yaml.CSafeLoader
except AttributeError:  # pragma: no cover
    Loader = yaml.SafeLoader

SEP = re.compile(r"^---\s*$", re.M)

# prometheus-operator's bundle carries a scalar `=` key (the YAML 1.1 "value"
# tag); PyYAML has no constructor for it, so read it as the plain string.
Loader.add_constructor("tag:yaml.org,2002:value", lambda loader, node: loader.construct_scalar(node))


def has_cel(node):
    if isinstance(node, dict):
        if "x-kubernetes-validations" in node:
            return True
        return any(has_cel(v) for v in node.values())
    if isinstance(node, list):
        return any(has_cel(v) for v in node)
    return False


def main():
    out, label, files = sys.argv[1], sys.argv[2], sys.argv[3:]
    os.makedirs(out, exist_ok=True)
    written, skipped, cel = [], [], 0
    for path in files:
        text = open(path, encoding="utf-8").read()
        for chunk in SEP.split(text):
            body = chunk.strip("\n")
            if not body.strip() or all(l.startswith("#") for l in body.splitlines() if l.strip()):
                continue
            try:
                doc = yaml.load(body, Loader=Loader)
            except yaml.YAMLError as e:  # helm-templated bundles carry non-YAML docs
                print(f"warn: {path}: unparsable document skipped: {e}", file=sys.stderr)
                continue
            if not isinstance(doc, dict) or doc.get("kind") != "CustomResourceDefinition":
                continue
            if doc.get("apiVersion") != "apiextensions.k8s.io/v1":
                print(f"warn: {path}: {doc.get('apiVersion')} CRD skipped", file=sys.stderr)
                continue
            name = doc["metadata"]["name"]
            dest = os.path.join(out, name + ".yaml")
            if os.path.exists(dest):
                skipped.append(name)
                continue
            with open(dest, "w", encoding="utf-8") as f:
                f.write("# source: " + label + "\n" + body + "\n")
            written.append(name)
            if has_cel(doc):
                cel += 1
    print(json.dumps({"source": label, "written": len(written), "skipped": skipped, "cel": cel, "names": written}))


if __name__ == "__main__":
    main()
