// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

// Pinned Kyverno CLI v1.19.1 (linux/amd64): the sha256 of the binary
// extracted from kyverno-cli_v1.19.1_linux_x86_64.tar.gz, whose own digest
// is the one listed in the release checksums.txt (the checksums file lists
// the tarball, not the binary).
const (
	pinnedSHA256  = "dfa1ffe747e43d0d5a34cbc676ff96ecafdf7ef979eee1c6d9d6606e3613c138"
	defaultBinary = "/home/luk/go/bin/kyverno"
)

var testdata = func() string {
	p, err := filepath.Abs(filepath.Join("..", "testdata"))
	if err != nil {
		panic(err)
	}
	return p
}()

func td(parts ...string) string {
	return filepath.Join(append([]string{testdata}, parts...)...)
}

// etd resolves a spike-owned fixture under engine/testdata.
func etd(parts ...string) string {
	p, err := filepath.Abs(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		panic(err)
	}
	return p
}

func binary() string {
	if b := os.Getenv("FATHOM_KYVERNO_BIN"); b != "" {
		return b
	}
	return defaultBinary
}

// opts returns pinned options with a fresh work directory, skipping the test
// when the pinned binary is not installed.
func opts(t *testing.T) Options {
	t.Helper()
	if _, err := os.Stat(binary()); err != nil {
		t.Skipf("kyverno CLI not found at %s: %v", binary(), err)
	}
	return Options{Binary: binary(), SHA256: pinnedSHA256, Timeout: 60 * time.Second, WorkDir: t.TempDir()}
}

type manifest struct {
	Scenarios []scenario `json:"scenarios"`
	ColdStart []int      `json:"coldStartMs"`
}

type scenario struct {
	Name          string   `json:"name"`
	PolicyFiles   []string `json:"policyFiles"`
	ObjectFile    string   `json:"objectFile"`
	SideFiles     []string `json:"sideFiles"`
	ServerAllowed bool     `json:"serverAllowed"`
	GoldenFile    string   `json:"goldenFile"`
	MutatedFile   string   `json:"mutatedFile"`
	CLICase       string   `json:"cliCase"`
	CLIExit       int      `json:"cliExit"`
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	b, err := os.ReadFile(td("manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// The oracle recorded vpol-violating-team as two goldens (webhook race)
	// rather than a manifest entry; add it so the union is asserted.
	m.Scenarios = append(m.Scenarios, scenario{
		Name:        "vpol-violating-team",
		PolicyFiles: []string{"policies/a-require-team-label.yaml", "policies/f-vpol-require-team-label.yaml"},
		ObjectFile:  "objects/violating-team.yaml",
		SideFiles:   []string{"snapshot/cm-allowed-teams.yaml"},
		GoldenFile:  "golden/vpol-violating-team.server.txt",
	})
	return m
}

// input builds the adapter input from a manifest scenario: only files under
// snapshot/ are side data; the oracle's hand-written cli/*.yaml and
// userinfo.yaml are never passed, the adapter generates its own.
func (s scenario) input() Input {
	in := Input{ObjectFiles: []string{td(s.ObjectFile)}}
	for _, p := range s.PolicyFiles {
		in.PolicyFiles = append(in.PolicyFiles, td(p))
	}
	for _, f := range s.SideFiles {
		if strings.HasPrefix(f, "snapshot/") {
			in.SnapshotFiles = append(in.SnapshotFiles, td(f))
		}
	}
	return in
}

func (s scenario) serverVerdict(t *testing.T) (Verdict, []string) {
	t.Helper()
	return unionVerdict(t, td(s.GoldenFile))
}

// unionVerdict parses a golden and, when <name>.alt.server.txt exists
// beside it (a webhook race recorded both texts), the union of both.
func unionVerdict(t *testing.T, golden string) (Verdict, []string) {
	t.Helper()
	files := []string{golden}
	alt := strings.TrimSuffix(golden, ".server.txt") + ".alt.server.txt"
	if _, err := os.Stat(alt); err == nil {
		files = append(files, alt)
	}
	var v Verdict
	var texts []string
	for i, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, string(b))
		pv, err := ParseServerVerdict(string(b))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if i == 0 {
			v = pv
			continue
		}
		v.Allowed = v.Allowed && pv.Allowed
		v.Denials = append(v.Denials, pv.Denials...)
	}
	sortDenials(v.Denials)
	return v, texts
}

func TestBinaryDigest(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "kyverno")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path, sha string
		wantErr         string
	}{
		{"pinned", binary(), pinnedSHA256, ""},
		{"wrong digest", binary(), strings.Repeat("0", 64), "sha256"},
		{"other file", fake, pinnedSHA256, "sha256"},
		{"missing", filepath.Join(tmp, "nope"), pinnedSHA256, "no such file"},
		{"relative", "kyverno", pinnedSHA256, "must be absolute"},
		{"empty", "", pinnedSHA256, "no Kyverno binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.path == binary() {
				if _, err := os.Stat(binary()); err != nil {
					t.Skipf("kyverno CLI not found at %s", binary())
				}
			}
			err := VerifyBinary(c.path, c.sha)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestScenariosVsServer runs every oracle scenario through the two-pass
// adapter and compares the normalised offline verdict with the normalised
// server verdict: allowed/denied and the set of {policy, rule, message}.
func TestScenariosVsServer(t *testing.T) {
	m := loadManifest(t)
	for _, s := range m.Scenarios {
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			o := opts(t)
			res, err := Run(context.Background(), o, s.input())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			server, texts := s.serverVerdict(t)
			offline := OfflineVerdict(res)
			for _, tx := range texts {
				t.Logf("server:\n%s", strings.TrimRight(tx, "\n"))
			}
			for _, f := range res.Findings {
				t.Logf("offline: %-8s %-8s %s/%s %s: %s", f.Severity, f.Result, f.Policy, f.Rule, f.Resource.Name, f.Message)
			}
			cliExit := -1
			if res.Validate != nil {
				cliExit = res.Validate.CLIExit
			}
			t.Logf("exit: fathom=%d cli=%d (oracle case %s recorded cli exit %d) side files=%v", res.ExitCode, cliExit, s.CLICase, s.CLIExit, keys(res.SideFiles))
			if !reflect.DeepEqual(server, offline) {
				t.Errorf("verdict differs\nserver:  %s\noffline: %s", mustJSON(server), mustJSON(offline))
			}
			want := ExitBlocking
			if server.Allowed {
				want = ExitClean
			}
			if res.ExitCode != want {
				t.Errorf("fathom exit %d, want %d", res.ExitCode, want)
			}
			if s.ServerAllowed != server.Allowed && s.CLICase != "" {
				t.Errorf("manifest serverAllowed=%v but golden parses as allowed=%v", s.ServerAllowed, server.Allowed)
			}
		})
	}
}

// Fields the apiserver owns on a stored Deployment: metadata bookkeeping,
// status and the defaults it fills in on CREATE. They are removed from the
// server-side delta before it is compared with the CLI's mutation delta.
var serverOwned = []*regexp.Regexp{
	regexp.MustCompile(`^/metadata/(annotations|creationTimestamp|generation|resourceVersion|uid|managedFields)(/|$)`),
	regexp.MustCompile(`^/status(/|$)`),
	regexp.MustCompile(`^/spec/(progressDeadlineSeconds|revisionHistoryLimit|strategy)(/|$)`),
	regexp.MustCompile(`^/spec/template/spec/(dnsPolicy|restartPolicy|schedulerName|terminationGracePeriodSeconds)$`),
	regexp.MustCompile(`^/spec/template/spec/containers/\d+/(imagePullPolicy|terminationMessagePath|terminationMessagePolicy)$`),
}

// TestMutatedObjectCanonical compares the pass-1 output with the object the
// cluster stored. Canonical form: RFC 6901 pointer -> JSON scalar leaves.
// Two assertions: every leaf of the CLI's mutated object is present with the
// same value in the stored object, and the set of leaves the mutation added
// or changed is exactly the set the cluster added beyond apiserver defaults.
func TestMutatedObjectCanonical(t *testing.T) {
	t.Parallel()
	m := loadManifest(t)
	var s scenario
	for _, c := range m.Scenarios {
		if c.MutatedFile != "" {
			s = c
		}
	}
	if s.Name == "" {
		t.Fatal("no scenario with mutatedFile")
	}
	o := opts(t)
	res, err := Run(context.Background(), o, s.input())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertMutationMatchesServer(t, res, td(s.MutatedFile))
}

// assertMutationMatchesServer compares the single pass-1 output in res with
// the object the server stored (or returned from a server dry run) at
// storedPath and returns the CLI's mutation delta.
func assertMutationMatchesServer(t *testing.T, res *Result, storedPath string) map[string]string {
	t.Helper()
	if res.Mutate == nil || len(res.Objects) != 1 || !res.Objects[0].Changed {
		t.Fatalf("expected one mutated object, got %+v", res.Objects)
	}
	t.Logf("pass 1 args: %s", strings.Join(res.Mutate.Args, " "))
	t.Logf("pass 1 output:\n%s", res.Objects[0].Mutated)
	original, err := Leaves(res.Objects[0].Original)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := Leaves(res.Objects[0].Mutated)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	server, err := Leaves(stored)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range cli {
		if sv, ok := server[k]; !ok || sv != v {
			t.Errorf("leaf %s: cli %s, stored %q", k, v, sv)
		}
	}
	cliDelta := Delta(original, cli)
	serverDelta := Filter(Delta(original, server), serverOwned)
	t.Logf("cli mutation delta:    %s", mustJSON(cliDelta))
	t.Logf("server mutation delta: %s", mustJSON(serverDelta))
	if !reflect.DeepEqual(cliDelta, serverDelta) {
		t.Errorf("mutation delta differs\ncli:    %s\nserver: %s", mustJSON(cliDelta), mustJSON(serverDelta))
	}
	if len(cliDelta) == 0 {
		t.Error("mutation changed nothing")
	}
	return cliDelta
}

// TestMAPParamRef exercises a MutatingAdmissionPolicy with a ConfigMap
// paramRef (engine/testdata/g-map-max-replicas-label.yaml) through pass 1,
// against the object the oracle returned from a server dry run with the
// same policy applied at admissionregistration.k8s.io/v1
// (engine/testdata/probes.sh). The CLI parses MAP only at v1alpha1; other
// versions are refused rather than silently dropped, and a paramRef the
// snapshot lacks is refused rather than silently applying nothing.
func TestMAPParamRef(t *testing.T) {
	t.Parallel()
	limits, ns := td("snapshot/cm-replica-limits.yaml"), td("snapshot/namespace.yaml")
	t.Run("mutation delta equals the server's", func(t *testing.T) {
		t.Parallel()
		o := opts(t)
		res, err := Run(context.Background(), o, Input{
			PolicyFiles:   []string{td("policies/b-add-default-securitycontext.yaml"), etd("g-map-max-replicas-label.yaml")},
			ObjectFiles:   []string{etd("map-object.yaml")},
			SnapshotFiles: []string{limits, ns},
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if _, ok := res.SideFiles["parameter:s3/replica-limits"]; !ok {
			t.Errorf("no --parameter-resource generated for the MAP paramRef: %v", keys(res.SideFiles))
		}
		if res.Objects[0].MutationSteps != 2 {
			t.Errorf("expected one document per matching mutating policy (b, MAP) in the -o file, got %d", res.Objects[0].MutationSteps)
		}
		delta := assertMutationMatchesServer(t, res, etd("map-paramref.dryrun.yaml"))
		if got := delta["/metadata/labels/fathom.dev~1max-replicas"]; got != `"3"` {
			t.Errorf("MAP paramRef label: %q, want \"3\" from ConfigMap s3/replica-limits", got)
		}
		if res.Validate != nil || res.ExitCode != ExitClean {
			t.Errorf("mutate-only run: validate=%v exit=%d", res.Validate, res.ExitCode)
		}
	})
	t.Run("v1 document refused, not silently dropped", func(t *testing.T) {
		t.Parallel()
		o := opts(t)
		b, err := os.ReadFile(etd("g-map-max-replicas-label.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		v1 := filepath.Join(o.WorkDir, "g-map-v1.yaml")
		if err := os.WriteFile(v1, []byte(strings.ReplaceAll(string(b), "admissionregistration.k8s.io/v1alpha1", "admissionregistration.k8s.io/v1")), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Run(context.Background(), o, Input{PolicyFiles: []string{v1}, ObjectFiles: []string{etd("map-object.yaml")}, SnapshotFiles: []string{limits, ns}})
		if err == nil || res.ExitCode != ExitToolError || !strings.Contains(res.Reason, "only at admissionregistration.k8s.io/v1alpha1") || res.Mutate != nil {
			t.Errorf("exit=%d mutate=%v reason=%q", res.ExitCode, res.Mutate, res.Reason)
		}
	})
	t.Run("paramRef missing from the snapshot refused", func(t *testing.T) {
		t.Parallel()
		o := opts(t)
		res, err := Run(context.Background(), o, Input{PolicyFiles: []string{etd("g-map-max-replicas-label.yaml")}, ObjectFiles: []string{etd("map-object.yaml")}, SnapshotFiles: []string{ns}})
		if err == nil || res.ExitCode != ExitToolError || !strings.Contains(res.Reason, "not in the snapshot") || res.Mutate != nil {
			t.Errorf("exit=%d mutate=%v reason=%q", res.ExitCode, res.Mutate, res.Reason)
		}
	})
}

// TestTwoPassValidateSeesMutation asserts that the pass-2 verdict depends on
// the pass-1 output: the probe policy (cli/probe-require-nonroot.yaml)
// requires the field (b) adds. Alone it blocks; after (b) it passes because
// pass 2 is fed the mutated file, not the input.
func TestTwoPassValidateSeesMutation(t *testing.T) {
	t.Parallel()
	obj := td("objects/passing-mutated.yaml")
	probe := td("cli/probe-require-nonroot.yaml")
	t.Run("probe alone blocks", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), opts(t), Input{PolicyFiles: []string{probe}, ObjectFiles: []string{obj}})
		if err != nil || res.ExitCode != ExitBlocking || res.Mutate != nil {
			t.Fatalf("exit=%d mutate=%v err=%v findings=%s", res.ExitCode, res.Mutate, err, mustJSON(res.Findings))
		}
		if v := OfflineVerdict(res); len(v.Denials) != 1 || v.Denials[0].Policy != "probe-require-nonroot" {
			t.Errorf("denials: %s", mustJSON(v))
		}
	})
	t.Run("after (b) it passes on the mutated file", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), opts(t), Input{PolicyFiles: []string{td("policies/b-add-default-securitycontext.yaml"), probe}, ObjectFiles: []string{obj}})
		if err != nil || res.ExitCode != ExitClean || res.Mutate == nil || res.Validate == nil {
			t.Fatalf("exit=%d err=%v findings=%s", res.ExitCode, err, mustJSON(res.Findings))
		}
		ob := res.Objects[0]
		if !ob.Changed || !strings.Contains(string(ob.Mutated), "runAsNonRoot: true") {
			t.Errorf("pass-1 output lacks the mutation:\n%s", ob.Mutated)
		}
		if !slices.Contains(res.Validate.Args, ob.File) || slices.Contains(res.Validate.Args, obj) {
			t.Errorf("pass 2 must be given the mutated file %s, not the input %s: %v", ob.File, obj, res.Validate.Args)
		}
		if res.Summary["pass"] != 1 || res.Summary["fail"] != 0 {
			t.Errorf("summary %v", res.Summary)
		}
	})
}

// TestUnmatchedObjectUnchanged: the CLI writes <name>-mutated.yaml for every
// object of a ClusterPolicy mutate pass, matched or not; Changed must come
// from the canonical comparison, and pass 2 must report only the matched
// object.
func TestUnmatchedObjectUnchanged(t *testing.T) {
	t.Parallel()
	res, err := Run(context.Background(), opts(t), Input{
		PolicyFiles:   []string{td("policies/b-add-default-securitycontext.yaml"), td("policies/a-require-team-label.yaml")},
		ObjectFiles:   []string{td("objects/passing-mutated.yaml"), etd("other-namespace.yaml")},
		SnapshotFiles: []string{td("snapshot/cm-allowed-teams.yaml")},
	})
	if err != nil || res.ExitCode != ExitClean || res.NoMatch {
		t.Fatalf("exit=%d nomatch=%v err=%v", res.ExitCode, res.NoMatch, err)
	}
	if len(res.Objects) != 2 || !res.Objects[0].Changed || res.Objects[1].Changed {
		t.Fatalf("changed flags: %v %v", res.Objects[0].Changed, res.Objects[1].Changed)
	}
	other := res.Objects[1]
	before, _ := Leaves(other.Original)
	after, _ := Leaves(other.Mutated)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("unmatched object changed:\n%s", mustJSON(Delta(before, after)))
	}
	t.Logf("unmatched object file written by the CLI: %v (bytes identical: %v)", string(other.Mutated) != "", string(other.Mutated) == string(other.Original))
	if len(res.Findings) != 1 || res.Findings[0].Result != "pass" || res.Findings[0].Resource.Name != "web-passing" {
		t.Errorf("findings: %s", mustJSON(res.Findings))
	}
}

// TestNoMatchIsClean: when no validate rule matches any admitted object the
// CLI prints no report at all and exits 0; that is a clean verdict flagged
// NoMatch, not a tool error.
func TestNoMatchIsClean(t *testing.T) {
	t.Parallel()
	t.Run("validate pass matched nothing", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), opts(t), Input{
			PolicyFiles:   []string{td("policies/a-require-team-label.yaml")},
			ObjectFiles:   []string{etd("other-namespace.yaml")},
			SnapshotFiles: []string{td("snapshot/cm-allowed-teams.yaml")},
		})
		if err != nil || res.ExitCode != ExitClean || !res.NoMatch || len(res.Findings) != 0 || res.Validate == nil || res.Validate.CLIExit != 0 {
			t.Fatalf("exit=%d nomatch=%v cli=%v err=%v findings=%s", res.ExitCode, res.NoMatch, res.Validate, err, mustJSON(res.Findings))
		}
		if _, perr := ParseReport([]byte(res.Validate.Stdout)); perr == nil {
			t.Errorf("expected no report on stdout, got one:\n%s", res.Validate.Stdout)
		}
	})
	t.Run("mutate-only pass, nothing to validate", func(t *testing.T) {
		t.Parallel()
		res, err := Run(context.Background(), opts(t), Input{PolicyFiles: []string{td("policies/b-add-default-securitycontext.yaml")}, ObjectFiles: []string{etd("other-namespace.yaml")}})
		if err != nil || res.ExitCode != ExitClean || res.NoMatch || res.Validate != nil || res.Objects[0].Changed {
			t.Fatalf("exit=%d nomatch=%v validate=%v changed=%v err=%v", res.ExitCode, res.NoMatch, res.Validate, res.Objects[0].Changed, err)
		}
	})
}

// TestDuplicateNamesRefused: kyverno apply -o names its files by
// metadata.name only, so two admitted objects sharing a name are refused
// before the mutate pass runs.
func TestDuplicateNamesRefused(t *testing.T) {
	t.Parallel()
	in := Input{
		PolicyFiles: []string{td("policies/b-add-default-securitycontext.yaml")},
		ObjectFiles: []string{td("objects/passing-mutated.yaml"), etd("same-name-other-namespace.yaml")},
	}
	res, err := Run(context.Background(), opts(t), in)
	if err == nil || res.ExitCode != ExitToolError || !strings.Contains(res.Reason, `share metadata.name "web-passing"`) || res.Mutate != nil {
		t.Errorf("exit=%d mutate=%v reason=%q", res.ExitCode, res.Mutate, res.Reason)
	}
	// Without a mutate pass the same two objects are fine.
	in.PolicyFiles = []string{td("policies/a-require-team-label.yaml")}
	in.SnapshotFiles = []string{td("snapshot/cm-allowed-teams.yaml")}
	res, err = Run(context.Background(), opts(t), in)
	if err != nil || res.ExitCode != ExitClean || len(res.Findings) != 1 {
		t.Errorf("validate-only: exit=%d err=%v findings=%s", res.ExitCode, err, mustJSON(res.Findings))
	}
}

// TestValidatingPolicyWithoutValidationActions: the served CRD has no
// default for spec.validationActions; the oracle denies under such a policy
// (engine/testdata/vpol-no-validation-actions.server.txt, recorded by
// engine/testdata/probes.sh with (a) also on the cluster, hence the union
// with the .alt golden), so the adapter treats absent as Deny.
func TestValidatingPolicyWithoutValidationActions(t *testing.T) {
	t.Parallel()
	pol, err := classifyPolicies([]string{etd("f-vpol-no-validation-actions.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if !pol.enforced("vpol-default-action-probe", "") {
		t.Errorf("enforcement: %s", pol.EnforcementTable())
	}
	res, err := Run(context.Background(), opts(t), Input{
		PolicyFiles:   []string{td("policies/a-require-team-label.yaml"), etd("f-vpol-no-validation-actions.yaml")},
		ObjectFiles:   []string{td("objects/violating-team.yaml")},
		SnapshotFiles: []string{td("snapshot/cm-allowed-teams.yaml")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	server, texts := unionVerdict(t, etd("vpol-no-validation-actions.server.txt"))
	for _, tx := range texts {
		t.Logf("server:\n%s", strings.TrimRight(tx, "\n"))
	}
	offline := OfflineVerdict(res)
	if !reflect.DeepEqual(server, offline) || res.ExitCode != ExitBlocking {
		t.Errorf("exit=%d\nserver:  %s\noffline: %s", res.ExitCode, mustJSON(server), mustJSON(offline))
	}
}

// fakeBinary writes a shell script standing in for the CLI and returns
// options pinned to its digest, so the adapter's handling of CLI contract
// violations can be exercised without a broken real binary.
func fakeBinary(t *testing.T, script string) Options {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "kyverno")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("#!/bin/sh\n" + script))
	return Options{Binary: bin, SHA256: hex.EncodeToString(h[:]), Timeout: 10 * time.Second, WorkDir: dir}
}

// TestCLIContractViolations pins the tool-error rows of the exit-code table
// that the real binary cannot produce: an exit code that is not a verdict,
// a 0/1 that contradicts the report, and no or unparsable report output.
func TestCLIContractViolations(t *testing.T) {
	t.Parallel()
	report := func(summary string) string {
		return `printf '%s\n' '{"apiVersion":"openreports.io/v1alpha1","kind":"ClusterReport","summary":{` + summary + `},"results":[]}'`
	}
	warn := `printf 'Warning: x: kyverno.io/v1 ClusterPolicy is deprecated\n'`
	validate := Input{PolicyFiles: []string{td("policies/a-require-team-label.yaml")}, ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{td("snapshot/cm-allowed-teams.yaml")}}
	mutate := Input{PolicyFiles: []string{td("policies/b-add-default-securitycontext.yaml")}, ObjectFiles: []string{td("objects/passing-mutated.yaml")}}
	cases := []struct {
		name, script string
		in           Input
		wantCLI      int
		reason       string
	}{
		{"exit 2 is not a verdict code", "exit 2", validate, 2, "exited 2 (not a verdict code)"},
		{"exit 0 contradicts fail in the report", report(`"fail":1,"pass":0`) + "; exit 0", validate, 0, "exit 0, expected 1 from report summary"},
		{"exit 1 contradicts a clean report", report(`"fail":0,"pass":1`) + "; exit 1", validate, 1, "exit 1, expected 0 from report summary"},
		{"unparsable report", `printf '{"kind":"ClusterReport",\n'; exit 0`, validate, 0, "policy report is not JSON"},
		{"report of another kind", `printf '{"kind":"Other","summary":{}}\n'; exit 0`, validate, 0, `policy report has kind "Other"`},
		{"report without summary", `printf '{"kind":"ClusterReport"}\n'; exit 0`, validate, 0, "policy report has no summary"},
		{"no report and exit 1", warn + "; exit 1", validate, 1, "no policy report found"},
		{"no report and unexpected stdout", warn + `; printf 'Applying 1 policy rule(s) to 1 resource(s)...\n'; exit 0`, validate, 0, "no policy report found"},
		{"no report and stderr", warn + `; printf 'boom\n' >&2; exit 0`, validate, 0, "no policy report found"},
		{"mutate pass without summary line", warn + "; exit 0", mutate, 0, "mutate pass printed no summary line"},
		{"mutate pass exit contradicts summary", `printf 'pass: 0, fail: 1, warn: 0, error: 0, skip: 0 \n'; exit 0`, mutate, 0, "mutate pass exit 0, expected 1"},
		{"mutate pass evaluation error", `printf 'pass: 0, fail: 0, warn: 0, error: 1, skip: 0 \n'; exit 1`, mutate, 1, "mutate pass reported 1 evaluation error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			res, err := Run(context.Background(), fakeBinary(t, c.script), c.in)
			if err == nil || res.ExitCode != ExitToolError || !strings.Contains(res.Reason, c.reason) {
				t.Errorf("exit=%d err=%v reason=%q, want reason containing %q", res.ExitCode, err, res.Reason, c.reason)
			}
			p := res.Validate
			if p == nil {
				p = res.Mutate
			}
			if p == nil || p.CLIExit != c.wantCLI {
				t.Errorf("recorded CLI exit %+v, want %d", p, c.wantCLI)
			}
		})
	}
	// A benign fake that mimics the no-match output must be clean, not an error.
	res, err := Run(context.Background(), fakeBinary(t, warn+"; exit 0"), validate)
	if err != nil || res.ExitCode != ExitClean || !res.NoMatch {
		t.Errorf("no-match mimic: exit=%d nomatch=%v err=%v", res.ExitCode, res.NoMatch, err)
	}
}

// TestExitCodeMapping pins fathom's exit code against the CLI's for every
// case class: clean, Blocking, Audit fail, VAP Warn binding, PolicyException,
// evaluation error, digest mismatch, missing binary and timeout.
func TestExitCodeMapping(t *testing.T) {
	t.Parallel()
	pol := func(ps ...string) []string {
		var out []string
		for _, p := range ps {
			if strings.HasPrefix(p, "engine/testdata/") {
				out = append(out, etd(strings.TrimPrefix(p, "engine/testdata/")))
			} else {
				out = append(out, td(p))
			}
		}
		return out
	}
	cm := td("snapshot/cm-allowed-teams.yaml")
	ns := td("snapshot/namespace.yaml")
	limits := td("snapshot/cm-replica-limits.yaml")
	cases := []struct {
		name     string
		in       Input
		mutate   func(*Options)
		wantExit ExitCode
		wantCLI  int // -1: CLI never ran
		wantSev  Severity
		reason   string
	}{
		{
			name:     "clean: pass",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{cm}},
			wantExit: ExitClean, wantCLI: 0, wantSev: SeverityInfo,
		},
		{
			name:     "clean: no validate rule matched any object (CLI prints no report)",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{etd("other-namespace.yaml")}, SnapshotFiles: []string{cm}},
			wantExit: ExitClean, wantCLI: 0,
		},
		{
			name:     "clean: rule skipped by PolicyException",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml", "policies/e-polex-legacy-app.yaml"), ObjectFiles: []string{td("objects/excused.yaml")}, SnapshotFiles: []string{cm}},
			wantExit: ExitClean, wantCLI: 0, wantSev: SeverityInfo,
		},
		{
			name:     "blocking: Enforce ClusterPolicy fail",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/violating-team.yaml")}, SnapshotFiles: []string{cm}},
			wantExit: ExitBlocking, wantCLI: 1, wantSev: SeverityBlocking,
		},
		{
			name:     "warning: Audit ClusterPolicy fail (CLI still exits 1)",
			in:       Input{PolicyFiles: pol("engine/testdata/a-require-team-label-audit.yaml"), ObjectFiles: []string{td("objects/violating-team.yaml")}, SnapshotFiles: []string{cm}},
			wantExit: ExitClean, wantCLI: 1, wantSev: SeverityWarning,
		},
		{
			name:     "warning: VAP binding validationActions [Warn] (CLI still exits 1)",
			in:       Input{PolicyFiles: pol("engine/testdata/d-vap-replica-limit-warn.yaml"), ObjectFiles: []string{td("objects/vap-violating.yaml")}, SnapshotFiles: []string{limits, ns}},
			wantExit: ExitClean, wantCLI: 1, wantSev: SeverityWarning,
		},
		{
			name:     "blocking: VAP binding Deny",
			in:       Input{PolicyFiles: pol("policies/d-vap-replica-limit.yaml"), ObjectFiles: []string{td("objects/vap-violating.yaml")}, SnapshotFiles: []string{limits, ns}},
			wantExit: ExitBlocking, wantCLI: 1, wantSev: SeverityBlocking,
		},
		{
			name:     "tool error: ConfigMap context missing from snapshot (result error)",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/violating-team.yaml")}},
			wantExit: ExitToolError, wantCLI: 1, wantSev: SeverityWarning, reason: "could not be evaluated",
		},
		{
			name:     "tool error: VAP parameter missing from snapshot (result error)",
			in:       Input{PolicyFiles: pol("policies/d-vap-replica-limit.yaml"), ObjectFiles: []string{td("objects/vap-violating.yaml")}, SnapshotFiles: []string{ns}},
			wantExit: ExitToolError, wantCLI: 1, wantSev: SeverityWarning, reason: "could not be evaluated",
		},
		{
			name:     "tool error: digest mismatch",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{cm}},
			mutate:   func(o *Options) { o.SHA256 = strings.Repeat("f", 64) },
			wantExit: ExitToolError, wantCLI: -1, reason: "sha256",
		},
		{
			name:     "tool error: missing binary",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{cm}},
			mutate:   func(o *Options) { o.Binary = filepath.Join(o.WorkDir, "absent-kyverno") },
			wantExit: ExitToolError, wantCLI: -1, reason: "no such file",
		},
		{
			name:     "tool error: timeout",
			in:       Input{PolicyFiles: pol("policies/a-require-team-label.yaml"), ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{cm}},
			mutate:   func(o *Options) { o.Timeout = time.Nanosecond },
			wantExit: ExitToolError, wantCLI: -1, reason: "timed out",
		},
		{
			name:     "tool error: unsupported policy kind",
			in:       Input{PolicyFiles: []string{cm}, ObjectFiles: []string{td("objects/passing-mutated.yaml")}},
			wantExit: ExitToolError, wantCLI: -1, reason: "unsupported policy document",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			o := opts(t)
			if c.mutate != nil {
				c.mutate(&o)
			}
			res, err := Run(context.Background(), o, c.in)
			if res == nil {
				t.Fatal("nil result")
			}
			gotCLI := -1
			if res.Validate != nil {
				gotCLI = res.Validate.CLIExit
			}
			t.Logf("fathom exit=%d cli exit=%d err=%v", res.ExitCode, gotCLI, err)
			if res.ExitCode != c.wantExit {
				t.Errorf("fathom exit %d, want %d (reason %q)", res.ExitCode, c.wantExit, res.Reason)
			}
			if (err != nil) != (c.wantExit == ExitToolError) {
				t.Errorf("err=%v for exit %d", err, res.ExitCode)
			}
			if gotCLI != c.wantCLI {
				t.Errorf("cli exit %d, want %d", gotCLI, c.wantCLI)
			}
			if c.reason != "" && !strings.Contains(res.Reason, c.reason) {
				t.Errorf("reason %q does not contain %q", res.Reason, c.reason)
			}
			if c.wantSev != "" {
				found := false
				for _, f := range res.Findings {
					if f.Severity == c.wantSev {
						found = true
					}
				}
				if !found {
					t.Errorf("no %s finding in %s", c.wantSev, mustJSON(res.Findings))
				}
			}
		})
	}
}

// TestFullChain admits all five objects under every policy at once, as the
// admin identity: every server denial must be present as a Blocking finding
// (the webhook race means the server shows one per object; fathom reports
// all of them).
func TestFullChain(t *testing.T) {
	t.Parallel()
	o := opts(t)
	in := Input{
		PolicyFiles: []string{
			td("policies/a-require-team-label.yaml"), td("policies/b-add-default-securitycontext.yaml"),
			td("policies/c-restrict-protected-deployer.yaml"), td("policies/d-vap-replica-limit.yaml"),
			td("policies/e-polex-legacy-app.yaml"), td("policies/f-vpol-require-team-label.yaml"),
		},
		ObjectFiles: []string{
			td("objects/violating-team.yaml"), td("objects/passing-mutated.yaml"), td("objects/excused.yaml"),
			td("objects/protected.yaml"), td("objects/vap-violating.yaml"),
		},
		SnapshotFiles: []string{
			td("snapshot/cm-allowed-teams.yaml"), td("snapshot/cm-replica-limits.yaml"), td("snapshot/namespace.yaml"),
			td("snapshot/rbac-argocd.yaml"), td("snapshot/identity-admin.yaml"),
		},
	}
	res, err := Run(context.Background(), o, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("pass 1: %s (exit %d, %s)", strings.Join(res.Mutate.Args, " "), res.Mutate.CLIExit, res.Mutate.Duration)
	t.Logf("pass 2: %s (exit %d, %s)", strings.Join(res.Validate.Args, " "), res.Validate.CLIExit, res.Validate.Duration)
	t.Logf("summary: %v", res.Summary)
	for _, ob := range res.Objects {
		t.Logf("object %s/%s mutated=%v", ob.Resource.Namespace, ob.Resource.Name, ob.Changed)
		if !ob.Changed { // every fixture object is in s3, which (b) matches
			t.Errorf("object %s not mutated by (b)", ob.Resource.Name)
		}
	}
	offline := OfflineVerdict(res)
	want := Verdict{Denials: []Denial{
		{Policy: "require-team-label", Rule: "team-label-allowed", Message: "label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)"},
		{Policy: "restrict-protected-deployer", Rule: "argocd-only", Message: `protected Deployments may only be applied by the argocd service account (group system:serviceaccounts:argocd); requester kubernetes-admin has groups ["kubeadm:cluster-admins","system:authenticated"]`},
		{Policy: "s3-replica-limit", Rule: "s3-replica-limit-binding", Message: "replicas 5 exceeds maxReplicas 3 from ConfigMap replica-limits"},
		{Policy: "vpol-require-team-label", Message: "label team=MISSING is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)"},
		{Policy: "vpol-require-team-label", Message: "label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)"},
	}}
	sortDenials(want.Denials)
	if !reflect.DeepEqual(offline, want) {
		t.Errorf("denials differ\ngot:  %s\nwant: %s", mustJSON(offline), mustJSON(want))
	}
	if res.ExitCode != ExitBlocking {
		t.Errorf("exit %d, want 1", res.ExitCode)
	}
}

// TestGeneratedSideFilesMatchOracle checks the generated side files against
// the ones the oracle agent wrote by hand and verified with the CLI.
func TestGeneratedSideFilesMatchOracle(t *testing.T) {
	t.Parallel()
	pol, err := classifyPolicies([]string{td("policies/a-require-team-label.yaml"), td("policies/d-vap-replica-limit.yaml"), td("policies/f-vpol-require-team-label.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if !pol.enforced("require-team-label", "team-label-allowed") || !pol.enforced("s3-replica-limit", "") || !pol.enforced("vpol-require-team-label", "") {
		t.Errorf("enforcement: %s", pol.EnforcementTable())
	}
	snap, err := loadSnapshot([]string{td("snapshot/cm-allowed-teams.yaml"), td("snapshot/cm-replica-limits.yaml"), td("snapshot/namespace.yaml"), td("snapshot/rbac-argocd.yaml"), td("snapshot/identity-argocd.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sf, err := writeSideFiles(dir, pol, snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("args: %s", strings.Join(sf.args, " "))
	for role, p := range sf.roles {
		b, _ := os.ReadFile(p)
		t.Logf("%s:\n%s", role, b)
	}

	gen := readYAML(t, sf.roles["values"])
	oracle := readYAML(t, td("cli/values.yaml"))
	gotCM := get(gen, "policies").([]any)[0].(map[string]any)["rules"].([]any)[0].(map[string]any)["values"].(map[string]any)["allowedTeams"].(map[string]any)["data"]
	wantCM := get(oracle, "policies").([]any)[0].(map[string]any)["rules"].([]any)[0].(map[string]any)["values"].(map[string]any)["allowedTeams"].(map[string]any)["data"]
	if !reflect.DeepEqual(gotCM, wantCM) {
		t.Errorf("values allowedTeams.data: %v, oracle %v", gotCM, wantCM)
	}
	if !reflect.DeepEqual(get(gen, "namespaceSelector"), get(oracle, "namespaceSelector")) {
		t.Errorf("namespaceSelector: %v, oracle %v", get(gen, "namespaceSelector"), get(oracle, "namespaceSelector"))
	}

	genCtx := readYAML(t, sf.roles["context"])
	oracleCtx := readYAML(t, td("cli/context.yaml"))
	for _, want := range list(oracleCtx, "spec", "resources") {
		found := false
		for _, got := range list(genCtx, "spec", "resources") {
			if reflect.DeepEqual(got, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("context.yaml lacks oracle resource %s", mustJSON(want))
		}
	}

	genUI := readYAML(t, sf.roles["userinfo"])
	oracleUI := readYAML(t, td("userinfo.yaml"))
	if !reflect.DeepEqual(get(genUI, "userInfo"), get(oracleUI, "userInfo")) {
		t.Errorf("userInfo: %v, oracle %v", get(genUI, "userInfo"), get(oracleUI, "userInfo"))
	}
	if got := strList(get(genUI, "clusterRoles")); !reflect.DeepEqual(got, []string{"edit"}) {
		t.Errorf("clusterRoles from RoleBinding argocd-edit: %v, want [edit]", got)
	}
	if _, ok := sf.roles["parameter:s3/replica-limits"]; !ok {
		t.Errorf("no parameter resource generated: %v", keys(sf.roles))
	}
}

func TestParseServerVerdict(t *testing.T) {
	t.Parallel()
	cases := map[string]Verdict{
		"golden/excused.server.txt":                {Allowed: true},
		"golden/passing-mutated-dryrun.server.txt": {Allowed: true},
		"golden/passing-mutated.server.txt":        {Allowed: true},
		"golden/protected-argocd.server.txt":       {Allowed: true},
		"golden/vpol-passing.server.txt":           {Allowed: true},
		"golden/violating-team.server.txt": {Denials: []Denial{{
			Policy: "require-team-label", Rule: "team-label-allowed",
			Message: "label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)",
		}}},
		"golden/protected-admin.server.txt": {Denials: []Denial{{
			Policy: "restrict-protected-deployer", Rule: "argocd-only",
			Message: `protected Deployments may only be applied by the argocd service account (group system:serviceaccounts:argocd); requester kubernetes-admin has groups ["kubeadm:cluster-admins","system:authenticated"]`,
		}}},
		"golden/vap-violating.server.txt": {Denials: []Denial{{
			Policy: "s3-replica-limit", Rule: "s3-replica-limit-binding",
			Message: "replicas 5 exceeds maxReplicas 3 from ConfigMap replica-limits",
		}}},
		"golden/vpol-excused.server.txt": {Denials: []Denial{{
			Policy:  "vpol-require-team-label",
			Message: "label team=MISSING is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)",
		}}},
		"golden/vpol-violating-team.server.txt": {Denials: []Denial{{
			Policy:  "vpol-require-team-label",
			Message: "label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)",
		}}},
		"golden/vpol-violating-team.alt.server.txt": {Denials: []Denial{{
			Policy: "require-team-label", Rule: "team-label-allowed",
			Message: "label team=pirates is not one of the allowed teams [platform,payments,search] (ConfigMap s3/allowed-teams)",
		}}},
	}
	for f, want := range cases {
		b, err := os.ReadFile(td(f))
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseServerVerdict(string(b))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\ngot  %s\nwant %s", f, mustJSON(got), mustJSON(want))
		}
	}
	if _, err := ParseServerVerdict("something else entirely\n"); err == nil {
		t.Error("unknown text must not parse")
	}
}

func TestParseReport(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(td("cli/out/iii-fail-report-json.txt"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseReport(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary["error"] != 1 || len(r.Results) != 1 || r.Results[0].Result != "error" {
		t.Errorf("unexpected report %+v", r)
	}
	if _, err := ParseReport([]byte("Warning: x\npass: 0, fail: 0, warn: 0, error: 0, skip: 0 \n")); err == nil {
		t.Error("plain output must not parse as a report")
	}
	if _, err := ParseReport([]byte(`{"kind":"Other"}`)); err == nil {
		t.Error("wrong kind must not parse")
	}
}

// TestColdStart times five validate-only runs of the smallest scenario:
// pass 2 alone (CLI process start to exit) and the whole adapter call.
func TestColdStart(t *testing.T) {
	o := opts(t)
	in := Input{PolicyFiles: []string{td("policies/a-require-team-label.yaml")}, ObjectFiles: []string{td("objects/passing-mutated.yaml")}, SnapshotFiles: []string{td("snapshot/cm-allowed-teams.yaml")}}
	var cli, total []time.Duration
	for i := 0; i < 5; i++ {
		o.WorkDir = t.TempDir()
		start := time.Now()
		res, err := Run(context.Background(), o, in)
		total = append(total, time.Since(start))
		if err != nil || res.ExitCode != ExitClean {
			t.Fatalf("run %d: exit %d err %v", i, res.ExitCode, err)
		}
		cli = append(cli, res.Validate.Duration)
	}
	mn, md := stats(cli)
	tmn, tmd := stats(total)
	t.Logf("cold start, kyverno apply (pass 2 only): runs=%v min=%s median=%s", cli, mn, md)
	t.Logf("cold start, adapter Run incl. side files: runs=%v min=%s median=%s", total, tmn, tmd)
	if md > 5*time.Second {
		t.Errorf("median cold start %s is far beyond any interactive budget", md)
	}
}

func stats(ds []time.Duration) (min, median time.Duration) {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[0], s[len(s)/2]
}

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
