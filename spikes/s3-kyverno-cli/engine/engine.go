// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package engine is the S3 exec adapter around the Kyverno CLI.
//
// It never imports Kyverno: the CLI is a digest-pinned subprocess that is
// given explicit files and arguments and a minimal explicit environment
// (HOME under the work directory and PATH=/usr/bin:/bin; nothing from the
// caller's process environment). The adapter runs two passes in the order
// the apiserver would (design.md 3.3):
//
//  1. mutate: `kyverno apply <mutate policies> --resource <objects> -o <dir>`
//     and take the patched objects from <dir>;
//  2. validate: `kyverno apply <validate policies, VAP+binding, ValidatingPolicy>
//     --resource <mutated objects> --values-file --context-file --userinfo
//     --parameter-resource --exception --policy-report --output-format json`
//     and parse the report.
//
// Every side file the CLI needs is generated from snapshot data (ConfigMaps,
// Namespaces, RoleBindings, a SelfSubjectReview identity) into the work
// directory. The exit code fathom reports is computed from the parsed report
// and the policies' own enforcement settings. The CLI's exit status is never
// used as a verdict: it is only checked for consistency (anything but 0/1,
// or a 0/1 that contradicts the report summary, is a tool error). The
// CLI's --audit-warn is deliberately not used: in v1.19.1 it also rewrites
// VAP Deny and ValidatingPolicy Deny failures to `warn` in the report while
// still exiting 1, so enforcement is derived from the policy files instead.
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// ExitCode is fathom's own exit code for the Kyverno stage.
type ExitCode int

const (
	// ExitClean means no Blocking finding and no evaluation error.
	ExitClean ExitCode = 0
	// ExitBlocking means at least one fail for an enforced policy
	// (ClusterPolicy Enforce, VAP binding Deny, ValidatingPolicy Deny).
	ExitBlocking ExitCode = 1
	// ExitToolError means the verdict could not be established: missing or
	// wrong binary, timeout, an unexpected CLI exit code, an unparsable
	// report, or a policy the CLI could not evaluate (result error). A
	// validate pass that matched nothing is not a tool error (see
	// Result.NoMatch).
	ExitToolError ExitCode = 3
)

// Severity mirrors the fathom finding severity (design.md 2.4).
type Severity string

const (
	SeverityInfo     Severity = "Info"
	SeverityWarning  Severity = "Warning"
	SeverityBlocking Severity = "Blocking"
)

// Options pin the binary and bound the run.
type Options struct {
	// Binary is the explicit path of the Kyverno CLI. PATH is never searched.
	Binary string
	// SHA256 is the expected hex digest of Binary; the adapter refuses to run
	// a binary whose digest differs.
	SHA256 string
	// Timeout bounds each CLI pass; zero means 60 s.
	Timeout time.Duration
	// WorkDir receives the generated side files, the mutated objects and the
	// CLI's HOME. It must exist and should be fresh per run.
	WorkDir string
}

// Identity is the admission requester the validate pass runs as.
type Identity struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups,omitempty"`
}

// Input is one admission simulation.
type Input struct {
	// PolicyFiles may hold, in any order and mixed per file: kyverno.io/v1
	// ClusterPolicy/Policy (mutate and validate rules), admissionregistration
	// ValidatingAdmissionPolicy + Binding (v1), MutatingAdmissionPolicy +
	// Binding (v1alpha1 only: CLI 1.19.1 silently drops v1/v1beta1 MAP
	// documents, so other versions are refused), policies.kyverno.io/v1
	// ValidatingPolicy and kyverno.io/v2 PolicyException.
	PolicyFiles []string
	// ObjectFiles are the manifests to admit (one or more documents each).
	ObjectFiles []string
	// SnapshotFiles carry the cluster side data: ConfigMaps, Namespaces,
	// RoleBindings/ClusterRoleBindings and optionally a SelfSubjectReview.
	SnapshotFiles []string
	// Identity overrides the SelfSubjectReview found in SnapshotFiles.
	Identity *Identity
}

// Resource identifies the admitted object a finding is about.
type Resource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// Finding is the spike's local finding record (design.md 2.4 has the full one).
type Finding struct {
	Policy   string   `json:"policy"`
	Rule     string   `json:"rule,omitempty"`
	Result   string   `json:"result"` // pass | fail | warn | error | skip
	Message  string   `json:"message"`
	Resource Resource `json:"resource"`
	Source   string   `json:"source"` // kyverno | ValidatingAdmissionPolicy | KyvernoValidatingPolicy
	Binding  string   `json:"binding,omitempty"`
	Enforced bool     `json:"enforced"`
	Severity Severity `json:"severity"`
}

// Pass records one CLI invocation.
type Pass struct {
	Args            []string      `json:"args"`
	CLIExit         int           `json:"cliExit"`
	ExpectedCLIExit int           `json:"expectedCliExit"`
	Stdout          string        `json:"-"`
	Stderr          string        `json:"-"`
	Duration        time.Duration `json:"duration"`
}

// Object is one admitted object after the mutate pass.
type Object struct {
	Resource Resource
	// Original is the input document as given.
	Original []byte
	// Mutated is the document after the mutate pass: the last document of
	// <name>-mutated.yaml. A ClusterPolicy mutate pass writes that file for
	// every object it was given, matched or not (reformatted, 4-space
	// indent), so Mutated may differ from Original only in formatting.
	Mutated []byte
	// Changed reports whether the mutate pass changed the object: the
	// canonical leaves (RFC 6901 pointer -> scalar) of Mutated differ from
	// those of Original. It is false for an object no mutate rule touched
	// even though the CLI wrote a file for it.
	Changed bool
	// MutationSteps is the number of YAML documents the CLI wrote for the
	// object: one per mutating policy that matched (cumulative), zero when
	// no file was written.
	MutationSteps int
	// File is the path handed to the validate pass.
	File string
}

// Result is the adapter's verdict.
type Result struct {
	Mutate   *Pass
	Validate *Pass
	Objects  []Object
	Findings []Finding
	Summary  map[string]int
	// SideFiles maps a role (values, context, userinfo, parameter:<ns>/<name>)
	// to the generated file path.
	SideFiles map[string]string
	ExitCode  ExitCode
	// Reason explains ExitToolError.
	Reason string
	// NoMatch is true when the validate pass matched no admitted object. The
	// CLI then prints no report at all (only the deprecation warnings) and
	// exits 0, so there is nothing to parse: the verdict is clean with no
	// findings. It is indistinguishable, from the CLI output alone, from a
	// side-file mistake that makes every selector miss (for instance a
	// namespaceSelector without the namespace's labels in values.yaml); the
	// product must surface it as a finding, never silently as clean.
	NoMatch bool
}

// Run executes both passes. A non-nil error always comes with a Result
// whose ExitCode is ExitToolError and whose Reason repeats the error.
func Run(ctx context.Context, opt Options, in Input) (*Result, error) {
	res := &Result{SideFiles: map[string]string{}}
	fail := func(err error) (*Result, error) {
		res.ExitCode = ExitToolError
		res.Reason = err.Error()
		return res, err
	}
	if err := VerifyBinary(opt.Binary, opt.SHA256); err != nil {
		return fail(err)
	}
	if opt.WorkDir == "" {
		return fail(errors.New("engine: WorkDir is required"))
	}
	if opt.Timeout == 0 {
		opt.Timeout = 60 * time.Second
	}
	for _, d := range []string{"home", "side", "mutated", "objects"} {
		if err := os.MkdirAll(filepath.Join(opt.WorkDir, d), 0o700); err != nil {
			return fail(err)
		}
	}

	pol, err := classifyPolicies(in.PolicyFiles)
	if err != nil {
		return fail(err)
	}
	snap, err := loadSnapshot(in.SnapshotFiles)
	if err != nil {
		return fail(err)
	}
	if in.Identity != nil {
		snap.identity = in.Identity
	}
	objs, err := loadObjects(in.ObjectFiles)
	if err != nil {
		return fail(err)
	}
	res.Objects = objs

	side, err := writeSideFiles(filepath.Join(opt.WorkDir, "side"), pol, snap)
	if err != nil {
		return fail(err)
	}
	res.SideFiles = side.roles

	// Pass 1: mutate.
	if len(pol.mutateFiles) > 0 {
		if err := checkUniqueNames(res.Objects); err != nil {
			return fail(err)
		}
		outDir := filepath.Join(opt.WorkDir, "mutated")
		args := []string{"apply"}
		args = append(args, pol.mutateFiles...)
		for _, f := range in.ObjectFiles {
			args = append(args, "--resource", f)
		}
		for _, e := range pol.exceptionFiles {
			args = append(args, "--exception", e)
		}
		args = append(args, side.args...)
		args = append(args, "-o", outDir)
		p, err := runCLI(ctx, opt, args)
		res.Mutate = p
		if err != nil {
			return fail(err)
		}
		p.ExpectedCLIExit = 0
		sum, ok := parseSummaryLine(p.Stdout)
		if !ok {
			return fail(fmt.Errorf("engine: mutate pass printed no summary line (exit %d):\n%s%s", p.CLIExit, p.Stdout, p.Stderr))
		}
		if sum["error"] > 0 || sum["fail"] > 0 {
			p.ExpectedCLIExit = 1
		}
		if p.CLIExit != p.ExpectedCLIExit {
			return fail(fmt.Errorf("engine: mutate pass exit %d, expected %d from summary %v", p.CLIExit, p.ExpectedCLIExit, sum))
		}
		if sum["error"] > 0 {
			return fail(fmt.Errorf("engine: mutate pass reported %d evaluation error(s)", sum["error"]))
		}
		if err := collectMutated(outDir, res.Objects); err != nil {
			return fail(err)
		}
	} else {
		for i := range res.Objects {
			res.Objects[i].Mutated = res.Objects[i].Original
		}
	}
	for i := range res.Objects {
		o := &res.Objects[i]
		o.File = filepath.Join(opt.WorkDir, "objects", fmt.Sprintf("%02d-%s.yaml", i, safeName(o.Resource)))
		if err := os.WriteFile(o.File, o.Mutated, 0o600); err != nil {
			return fail(err)
		}
	}

	// Pass 2: validate.
	if len(pol.validateFiles) == 0 {
		res.ExitCode = ExitClean
		return res, nil
	}
	args := []string{"apply"}
	args = append(args, pol.validateFiles...)
	for _, o := range res.Objects {
		args = append(args, "--resource", o.File)
	}
	for _, e := range pol.exceptionFiles {
		args = append(args, "--exception", e)
	}
	args = append(args, side.args...)
	args = append(args, "--policy-report", "--output-format", "json")
	p, err := runCLI(ctx, opt, args)
	res.Validate = p
	if err != nil {
		return fail(err)
	}
	rep, err := ParseReport([]byte(p.Stdout))
	if err != nil {
		if p.CLIExit == 0 && p.Stderr == "" && isNoMatchOutput(p.Stdout) {
			// No validate rule matched any admitted object: the CLI prints
			// no report and exits 0 (observed with 1.19.1). Clean, but
			// flagged so the caller can tell it from an evaluated pass.
			res.NoMatch = true
			res.Summary = map[string]int{}
			res.ExitCode = ExitClean
			return res, nil
		}
		return fail(fmt.Errorf("engine: validate pass (exit %d): %w\nstdout:\n%s\nstderr:\n%s", p.CLIExit, err, p.Stdout, p.Stderr))
	}
	res.Summary = rep.Summary
	p.ExpectedCLIExit = 0
	if rep.Summary["fail"] > 0 || rep.Summary["error"] > 0 {
		p.ExpectedCLIExit = 1
	}
	if p.CLIExit != p.ExpectedCLIExit {
		return fail(fmt.Errorf("engine: validate pass exit %d, expected %d from report summary %v", p.CLIExit, p.ExpectedCLIExit, rep.Summary))
	}
	res.Findings = toFindings(rep, pol)
	res.ExitCode, res.Reason = exitCode(res.Findings)
	if res.ExitCode == ExitToolError {
		return res, errors.New(res.Reason)
	}
	return res, nil
}

// VerifyBinary checks that path exists, is a regular file and has the
// expected sha256 hex digest.
func VerifyBinary(path, wantHex string) error {
	if path == "" {
		return errors.New("engine: no Kyverno binary path given")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("engine: binary path %q must be absolute", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("engine: kyverno binary: %w", err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("engine: kyverno binary %q is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("engine: kyverno binary %s sha256 %s, pinned %s", path, got, wantHex)
	}
	return nil
}

// runCLI executes the binary with an explicit, minimal environment: nothing
// from the caller's process environment (KUBECONFIG, HOME, proxies) reaches
// the CLI. Only exit codes 0 and 1 are the CLI's own verdict codes; anything
// else, a timeout or a start failure is a tool error.
func runCLI(ctx context.Context, opt Options, args []string) (*Pass, error) {
	p := &Pass{Args: append([]string(nil), args...)}
	cctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, opt.Binary, args...)
	cmd.Env = []string{
		"HOME=" + filepath.Join(opt.WorkDir, "home"),
		"PATH=/usr/bin:/bin",
	}
	cmd.Dir = opt.WorkDir
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	start := time.Now()
	err := cmd.Run()
	p.Duration = time.Since(start)
	p.Stdout, p.Stderr = so.String(), se.String()
	if cctx.Err() != nil {
		p.CLIExit = -1
		return p, fmt.Errorf("engine: kyverno %s timed out after %s", args[0], opt.Timeout)
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		p.CLIExit = 0
	case errors.As(err, &ee):
		p.CLIExit = ee.ExitCode()
	default:
		p.CLIExit = -1
		return p, fmt.Errorf("engine: kyverno could not be started: %w", err)
	}
	if p.CLIExit != 0 && p.CLIExit != 1 {
		return p, fmt.Errorf("engine: kyverno exited %d (not a verdict code):\n%s%s", p.CLIExit, p.Stdout, p.Stderr)
	}
	return p, nil
}

var summaryRE = regexp.MustCompile(`(?m)^pass: (\d+), fail: (\d+), warn: (\d+), error: (\d+), skip: (\d+)\s*$`)

// parseSummaryLine reads the CLI's plain-output summary line.
func parseSummaryLine(stdout string) (map[string]int, bool) {
	m := summaryRE.FindStringSubmatch(stdout)
	if m == nil {
		return nil, false
	}
	out := map[string]int{}
	for i, k := range []string{"pass", "fail", "warn", "error", "skip"} {
		n, _ := strconv.Atoi(m[i+1])
		out[k] = n
	}
	return out, true
}

// checkUniqueNames refuses two admitted objects with the same metadata.name
// before a mutate pass runs: `kyverno apply -o` names its output files by
// metadata.name only (kind and namespace are lost), so the second object's
// file would silently overwrite the first one's.
func checkUniqueNames(objs []Object) error {
	seen := map[string]int{}
	for _, o := range objs {
		seen[o.Resource.Name]++
	}
	for _, o := range objs {
		if seen[o.Resource.Name] > 1 {
			return fmt.Errorf("engine: %d admitted objects share metadata.name %q; kyverno apply -o names output files by name only", seen[o.Resource.Name], o.Resource.Name)
		}
	}
	return nil
}

// collectMutated reads <dir>/<name>-mutated.yaml for each object. A
// ClusterPolicy mutate pass writes that file for every object it was given,
// whether or not a rule matched it (the unmatched one comes back
// reformatted but with the same leaves); a MutatingAdmissionPolicy-only
// pass writes it only for matched objects. When more than one mutating
// policy matched, the file holds one YAML document per applied policy,
// cumulative in application order, so the last document is the final
// object. Changed is decided by comparing canonical leaves, never by the
// file's presence; a missing file keeps the original bytes.
func collectMutated(dir string, objs []Object) error {
	for i := range objs {
		o := &objs[i]
		b, err := os.ReadFile(filepath.Join(dir, o.Resource.Name+"-mutated.yaml"))
		switch {
		case err == nil:
			docs := splitYAML(b)
			if len(docs) == 0 {
				return fmt.Errorf("engine: %s-mutated.yaml is empty", o.Resource.Name)
			}
			o.Mutated = docs[len(docs)-1]
			o.MutationSteps = len(docs)
		case errors.Is(err, os.ErrNotExist):
			o.Mutated = o.Original
			continue
		default:
			return err
		}
		before, err := Leaves(o.Original)
		if err != nil {
			return fmt.Errorf("engine: %s: %w", o.Resource.Name, err)
		}
		after, err := Leaves(o.Mutated)
		if err != nil {
			return fmt.Errorf("engine: %s-mutated.yaml: %w", o.Resource.Name, err)
		}
		o.Changed = len(Delta(before, after)) > 0 || len(Delta(after, before)) > 0
	}
	return nil
}

// isNoMatchOutput reports whether stdout is what the CLI prints when no
// policy matched any object under --policy-report --output-format json:
// only deprecation warnings (or nothing at all).
func isNoMatchOutput(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Warning: ") {
			continue
		}
		return false
	}
	return true
}

func safeName(r Resource) string {
	s := strings.ToLower(r.Kind + "-" + r.Namespace + "-" + r.Name)
	return regexp.MustCompile(`[^a-z0-9.-]+`).ReplaceAllString(s, "-")
}

// Report is the parsed openreports.io/v1alpha1 ClusterReport the CLI emits
// with --policy-report --output-format json.
type Report struct {
	Kind       string         `json:"kind"`
	APIVersion string         `json:"apiVersion"`
	Summary    map[string]int `json:"summary"`
	Results    []ReportResult `json:"results"`
}

// ReportResult is one report entry.
type ReportResult struct {
	Source     string            `json:"source"`
	Policy     string            `json:"policy"`
	Rule       string            `json:"rule"`
	Result     string            `json:"result"`
	Message    string            `json:"message"`
	Resources  []Resource        `json:"resources"`
	Properties map[string]string `json:"properties"`
}

// ParseReport finds the JSON report in the CLI's stdout. The CLI prints
// deprecation warnings ("Warning: <file>: kyverno.io/v1 ClusterPolicy is
// deprecated ...") to stdout before the report, so the parser takes the
// first line that starts with '{'.
func ParseReport(stdout []byte) (*Report, error) {
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("{")) {
			continue
		}
		var r Report
		if err := json.Unmarshal(bytes.TrimSpace(line), &r); err != nil {
			return nil, fmt.Errorf("policy report is not JSON: %w", err)
		}
		if r.Kind != "ClusterReport" && r.Kind != "Report" && r.Kind != "ClusterPolicyReport" && r.Kind != "PolicyReport" {
			return nil, fmt.Errorf("policy report has kind %q", r.Kind)
		}
		if r.Summary == nil {
			return nil, errors.New("policy report has no summary")
		}
		return &r, nil
	}
	return nil, errors.New("no policy report found in CLI output")
}

func toFindings(rep *Report, pol *policySet) []Finding {
	var out []Finding
	for _, r := range rep.Results {
		enforced := pol.enforced(r.Policy, r.Rule)
		f := Finding{
			Policy:   r.Policy,
			Rule:     r.Rule,
			Result:   r.Result,
			Message:  r.Message,
			Source:   r.Source,
			Binding:  r.Properties["binding"],
			Enforced: enforced,
		}
		if len(r.Resources) > 0 {
			f.Resource = r.Resources[0]
		}
		switch r.Result {
		case "fail":
			if enforced {
				f.Severity = SeverityBlocking
			} else {
				f.Severity = SeverityWarning
			}
		case "warn", "error":
			f.Severity = SeverityWarning
		default:
			f.Severity = SeverityInfo
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Resource != b.Resource {
			return a.Resource.Name < b.Resource.Name
		}
		if a.Policy != b.Policy {
			return a.Policy < b.Policy
		}
		return a.Rule < b.Rule
	})
	return out
}

// exitCode maps findings to fathom's exit code. An evaluation error (a
// policy the CLI could not run, typically a missing context entry) is not a
// verdict: the stage cannot claim "clean", so it is a tool error.
func exitCode(fs []Finding) (ExitCode, string) {
	var blocking []string
	for _, f := range fs {
		if f.Result == "error" {
			return ExitToolError, fmt.Sprintf("policy %s could not be evaluated: %s", f.Policy, f.Message)
		}
		if f.Severity == SeverityBlocking {
			blocking = append(blocking, f.Policy)
		}
	}
	if len(blocking) > 0 {
		return ExitBlocking, ""
	}
	return ExitClean, ""
}

// yamlDocs splits a multi-document YAML file into JSON-decoded maps.
func yamlDocs(path string) ([]map[string]any, [][]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var docs []map[string]any
	var raws [][]byte
	for _, part := range splitYAML(b) {
		var m map[string]any
		if err := yaml.Unmarshal(part, &m); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(m) == 0 {
			continue
		}
		docs = append(docs, m)
		raws = append(raws, part)
	}
	return docs, raws, nil
}

var docSep = regexp.MustCompile(`(?m)^---\s*$`)

func splitYAML(b []byte) [][]byte {
	var out [][]byte
	for _, p := range docSep.Split(string(b), -1) {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, []byte(p))
	}
	return out
}

func str(m map[string]any, path ...string) string {
	v := get(m, path...)
	s, _ := v.(string)
	return s
}

func get(m map[string]any, path ...string) any {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[k]
		if !ok {
			return nil
		}
	}
	return cur
}

func list(m map[string]any, path ...string) []map[string]any {
	v, _ := get(m, path...).([]any)
	var out []map[string]any
	for _, e := range v {
		if em, ok := e.(map[string]any); ok {
			out = append(out, em)
		}
	}
	return out
}

func strList(v any) []string {
	l, _ := v.([]any)
	var out []string
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
