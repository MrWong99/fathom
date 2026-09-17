// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package validate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const testdata = "../testdata"

// expectIdentical pins what the calibration measured on 2026-09-18 (server
// v1.37.0 vs kubectl-validate v0.0.5-0.20260105161640-a97ccfaca20b under MVS
// at k8s.io v0.37.0). A change in either direction is a finding: update
// RESULT.md, then this map.
var expectIdentical = map[string]bool{
	"cel-fail":            true,
	"cel-pass":            true,
	"structural":          false, // decode-level wrapper and item text differ
	"structural-ignore":   true,
	"structural-type":     true,
	"ratcheting-replicas": false, // kubectl-validate has no old object: rejects what the server ratchets
	"ratcheting-name":     true,
	"budget":              true,
}

// expectPortIdentical pins the ~100-line strategy port (rest.BeforeCreate /
// rest.BeforeUpdate on customresource.NewStrategy) against the same goldens.
var expectPortIdentical = map[string]bool{
	"cel-fail":            true,
	"cel-pass":            true,
	"structural":          false, // the port has no decoder; unknown field is invisible, type error reported
	"structural-ignore":   true,
	"structural-type":     true,
	"ratcheting-replicas": true, // ratchets and warns like the server
	"ratcheting-name":     true,
	"budget":              true,
}

func loadManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := LoadManifest(filepath.Join(testdata, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Scenarios) == 0 {
		t.Fatal("manifest has no scenarios")
	}
	return m
}

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdata, rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func logComparison(t *testing.T, label, server, offline string) bool {
	t.Helper()
	same := Identical(server, offline)
	verdict := "NOT identical"
	if same {
		verdict = "identical"
	}
	t.Logf("%s: %s\n--- server ---\n%s--- %s ---\n%s--- normalised server ---\n%q\n--- normalised %s ---\n%q",
		label, verdict, server, label, offline, Normalise(server), label, Normalise(offline))
	return same
}

// TestGoldensMatchManifest guards the fixture: serverError in the manifest is
// the golden file, byte for byte.
func TestGoldensMatchManifest(t *testing.T) {
	t.Parallel()
	m := loadManifest(t)
	for _, sc := range m.Scenarios {
		golden := readFixture(t, sc.GoldenFile)
		if string(golden) != sc.ServerError {
			t.Errorf("%s: manifest serverError differs from %s", sc.Name, sc.GoldenFile)
		}
	}
}

// TestOfflineVsServer runs kubectl-validate offline on every scenario and
// compares the normalised field-error items with the server golden. The
// expected outcome per scenario is pinned in expectIdentical.
func TestOfflineVsServer(t *testing.T) {
	t.Parallel()
	m := loadManifest(t)
	off, err := NewOffline(os.DirFS(filepath.Join(testdata, "crds")))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, sc := range m.Scenarios {
		sc := sc
		seen[sc.Name] = true
		t.Run(sc.Name, func(t *testing.T) {
			doc := readFixture(t, sc.ObjectFile)
			var res Result
			switch {
			case sc.Name == "structural-ignore":
				res = off.CreateIgnoreUnknown(doc)
			case sc.Operation == "update":
				res = off.Update(doc)
			default:
				res = off.Create(doc)
			}
			t.Logf("notes: %s", sc.Notes)
			same := logComparison(t, "offline kubectl-validate", sc.ServerError, res.Text)
			want, ok := expectIdentical[sc.Name]
			if !ok {
				t.Fatalf("scenario %s is not pinned in expectIdentical", sc.Name)
			}
			if same != want {
				t.Errorf("identical=%v, pinned %v; recalibrate RESULT.md", same, want)
			}
			if res.Accepted != sc.ServerAccepted && sc.Name != "ratcheting-replicas" {
				t.Errorf("accepted=%v, server accepted=%v", res.Accepted, sc.ServerAccepted)
			}
		})
	}
	for name := range expectIdentical {
		if !seen[name] {
			t.Errorf("pinned scenario %s is not in the manifest", name)
		}
	}
}

// TestStrategyPort runs the ~100-line port (customresource.NewStrategy +
// rest.BeforeCreate / rest.BeforeUpdate) on every scenario, so the update
// scenarios get a real old object and ratcheting is exercised.
func TestStrategyPort(t *testing.T) {
	t.Parallel()
	m := loadManifest(t)
	t.Logf("CRDValidationRatcheting enabled in DefaultFeatureGate: %v", RatchetingEnabled())
	for _, sc := range m.Scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			strat, err := NewStrategy(readFixture(t, sc.CRDFile), "v1")
			if err != nil {
				t.Fatal(err)
			}
			doc := readFixture(t, sc.ObjectFile)
			var res Result
			if sc.Operation == "update" {
				res = strat.ValidateUpdate(doc, readFixture(t, sc.OldObjectFile))
			} else {
				res = strat.ValidateCreate(doc)
			}
			same := logComparison(t, "strategy port", sc.ServerError, res.Text)
			want, ok := expectPortIdentical[sc.Name]
			if !ok {
				t.Fatalf("scenario %s is not pinned in expectPortIdentical", sc.Name)
			}
			if same != want {
				t.Errorf("identical=%v, pinned %v; recalibrate RESULT.md", same, want)
			}
			if res.Accepted != sc.ServerAccepted && sc.Name != "structural" {
				t.Errorf("accepted=%v, server accepted=%v", res.Accepted, sc.ServerAccepted)
			}
		})
	}
}

// TestIgnoreUnknownIsNotParse pins the two facts behind the structural-ignore
// row: kubectl-validate's Parse is strict-only (it rejects the unknown field,
// so it cannot stand in for --validate=ignore), and the spike's plain-YAML
// decode that CreateIgnoreUnknown uses instead keeps the unknown field on the
// object, where the server would have pruned it. The text comparison in
// TestOfflineVsServer cannot see this difference.
func TestIgnoreUnknownIsNotParse(t *testing.T) {
	t.Parallel()
	off, err := NewOffline(os.DirFS(filepath.Join(testdata, "crds")))
	if err != nil {
		t.Fatal(err)
	}
	doc := readFixture(t, "objects/structural.yaml")
	if _, _, err := off.v.Parse(doc); err == nil {
		t.Fatal("Parse accepted an unknown field; it is documented as strict-only")
	} else if !strings.Contains(err.Error(), "spec.colour") {
		t.Fatalf("Parse error does not name the unknown field: %v", err)
	}
	obj, err := decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "colour"); !found {
		t.Fatal("plain decode pruned spec.colour; the structural-ignore row assumes it does not")
	}
	res := off.CreateIgnoreUnknown(doc)
	if res.Accepted {
		t.Fatal("CreateIgnoreUnknown accepted structural.yaml; the server rejects it on spec.replicas")
	}
	if strings.Contains(res.Text, "colour") {
		t.Fatalf("the unknown field leaked into the error text: %s", res.Text)
	}
}

func TestRatchetingGateDefault(t *testing.T) {
	t.Parallel()
	if !RatchetingEnabled() {
		t.Fatal("CRDValidationRatcheting is off by default; k8s.io/apiextensions-apiserver v0.37.0 locks it on since 1.33")
	}
}

func TestNormalise(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"kubectl single", "The Gadget \"ratchet\" is invalid: spec.name: Invalid value: \"OTHER\": name must be lowercase\n",
			[]string{"spec.name: Invalid value: \"OTHER\": name must be lowercase"}},
		{"kubectl multi", "The Widget \"cel-fail\" is invalid: \n* spec: Invalid value: replicas must not exceed maxReplicas\n* spec.name: Invalid value: \"UPPER\": name must be lowercase, got UPPER\n",
			[]string{"spec.name: Invalid value: \"UPPER\": name must be lowercase, got UPPER", "spec: Invalid value: replicas must not exceed maxReplicas"}},
		{"strategy wrapper", "Widget.spike.fathom.dev \"cel-fail\" is invalid: spec: Invalid value: replicas must not exceed maxReplicas",
			[]string{"spec: Invalid value: replicas must not exceed maxReplicas"}},
		{"server decode wrapper", "Error from server (BadRequest): error when creating \"objects/structural.yaml\": Widget in version \"v1\" cannot be handled as a Widget: strict decoding error: unknown field \"spec.colour\"\n",
			[]string{"Widget in version \"v1\" cannot be handled as a Widget: strict decoding error: unknown field \"spec.colour\""}},
		{"warning", "Warning: spec.name: Invalid value: \"UPPER\": name must be lowercase\n",
			[]string{"Warning: spec.name: Invalid value: \"UPPER\": name must be lowercase"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Normalise(tc.in)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// extraFlags are the kubectl apply flags commands.sh adds per scenario.
var extraFlags = map[string][]string{
	"structural-ignore": {"--validate=ignore"},
}

// TestOracleReplay re-runs every --dry-run=server against the kind oracle and
// checks stderr is still byte-identical to the golden. Skips when the cluster
// is unreachable. It relies on the state commands.sh left on the cluster (the
// CRDs and the ratchet Gadget); it does not re-run commands.sh.
func TestOracleReplay(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kctx := os.Getenv("FATHOM_ORACLE_CONTEXT")
	if kctx == "" {
		kctx = "kind-fathom-oracle"
	}
	if out, err := exec.CommandContext(ctx, "kubectl", "--context", kctx, "--request-timeout=5s", "get", "--raw", "/version").CombinedOutput(); err != nil {
		t.Skipf("kind oracle %s unreachable: %v: %s", kctx, err, out)
	}
	m := loadManifest(t)
	td, err := filepath.Abs(testdata)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range m.Scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			args := []string{"--context", kctx, "--request-timeout=30s", "apply", "--dry-run=server", "-f", sc.ObjectFile}
			args = append(args, extraFlags[sc.Name]...)
			cmd := exec.Command("kubectl", args...)
			cmd.Dir = td // the golden embeds the relative object path
			var stderr bytes.Buffer
			cmd.Stdout = nil
			cmd.Stderr = &stderr
			err := cmd.Run()
			if (err == nil) != sc.ServerAccepted {
				t.Errorf("exit ok=%v, manifest serverAccepted=%v; stderr: %s", err == nil, sc.ServerAccepted, stderr.String())
			}
			if stderr.String() != sc.ServerError {
				t.Errorf("stderr drifted from golden %s:\n--- live ---\n%s--- golden ---\n%s", sc.GoldenFile, stderr.String(), sc.ServerError)
			}
		})
	}
}
