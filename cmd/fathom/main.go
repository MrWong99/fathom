// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Command fathom is the pre-push validator for GitOps deployments: it renders
// Helm, Kustomize and Compose values with the real engines, runs the target
// cluster's admission chain offline against an imported snapshot, and opens
// the pull request. This is the pre-alpha entry point; the CLI surface is
// specified in docs/design/design.md section 3.8 (where it still carries the
// working name "zhi").
package main

import (
	"fmt"
	"os"
)

// Injected via -ldflags at build time (see Makefile).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("fathom %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	fmt.Fprintln(os.Stderr, "fathom: pre-alpha. The spike sprint comes first; see spikes/README.md.")
	fmt.Fprintln(os.Stderr, "usage: fathom version")
	os.Exit(2)
}
