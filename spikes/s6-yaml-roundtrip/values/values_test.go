// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package values

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const layersDir = "../testdata/layers"

// stack loads the fixture in fold order: common < variant < env < machine.
func stack(t *testing.T) []*Layer {
	t.Helper()
	specs := []struct{ name, path, owner string }{
		{"common", "common/values.yaml", "human"},
		{"variant:ha", "variants/ha.yaml", "human"},
		{"env:eu-prod", "envs/eu-prod/values.yaml", "human"},
		{"env:eu-prod/images", "envs/eu-prod/images.yaml", "machine:renovate"},
	}
	var out []*Layer
	for _, s := range specs {
		l, err := LoadLayer(s.name, filepath.Join(layersDir, s.path), s.owner)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func layer(t *testing.T, name string) *Layer {
	t.Helper()
	for _, l := range stack(t) {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no layer %s", name)
	return nil
}

// check applies the edit, verifies the untouched regions byte for byte, that
// the result still parses, that the effective value at the pointer is what
// was written, and returns the new source.
func check(t *testing.T, l *Layer, e Edit, pointer string, want any) []byte {
	t.Helper()
	out := e.Apply(l.Src)
	if !bytes.Equal(out[:e.Start], l.Src[:e.Start]) {
		t.Fatalf("prefix changed")
	}
	if !bytes.Equal(out[e.Start+len(e.New):], l.Src[e.End:]) {
		t.Fatalf("suffix changed")
	}
	if string(l.Src[e.Start:e.End]) != e.Old {
		t.Fatalf("edit.Old %q does not match the source span %q", e.Old, l.Src[e.Start:e.End])
	}
	nl, err := ParseLayer(l.Name, l.Path, l.Owner, out)
	if err != nil {
		t.Fatalf("edited file does not parse: %v\n%s", err, out)
	}
	leaves, _, err := Effective([]*Layer{nl})
	if err != nil {
		t.Fatal(err)
	}
	if want == nil {
		if _, ok := leaves[pointer]; ok {
			t.Errorf("%s still present after writing null", pointer)
		}
		return out
	}
	got, ok := leaves[pointer]
	if !ok {
		t.Fatalf("%s missing after edit; leaves: %v\n%s", pointer, keys(leaves), out)
	}
	if got.Value != want {
		t.Errorf("%s = %#v, want %#v", pointer, got.Value, want)
	}
	return out
}

func keys(m map[string]Leaf) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPointer(t *testing.T) {
	t.Parallel()
	toks, err := Parse("/master/podLabels/app.kubernetes.io~1part-of/x~0y")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(toks, "|") != "master|podLabels|app.kubernetes.io/part-of|x~y" {
		t.Errorf("tokens %v", toks)
	}
	if Join(toks) != "/master/podLabels/app.kubernetes.io~1part-of/x~0y" {
		t.Errorf("join %s", Join(toks))
	}
	if _, err := Parse("master"); err == nil {
		t.Error("pointer without leading slash accepted")
	}
}

func TestEffectiveFold(t *testing.T) {
	t.Parallel()
	leaves, deleted, err := Effective(stack(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		pointer string
		value   any
		layer   string
	}{
		{"/replica/replicaCount", 3, "env:eu-prod"},
		{"/master/persistence/size", "20Gi", "env:eu-prod"},
		{"/master/persistence/enabled", true, "common"},
		{"/master/resources/limits/cpu", "500m", "common"},    // through the alias *resources
		{"/replica/resources/requests/cpu", "100m", "common"}, // through the merge key
		{"/replica/resources/limits/cpu", "1", "common"},      // overrides the merged limits
		{"/image/tag", "8.2.3", "env:eu-prod/images"},         // machine layer wins
		{"/commonAnnotations/mode", "NO", "common"},
		{"/metrics/serviceMonitor/enabled", true, "env:eu-prod"},
		{"/master/podLabels/app.kubernetes.io~1part-of", "shop", "common"},
	}
	for _, c := range cases {
		got, ok := leaves[c.pointer]
		if !ok {
			t.Errorf("%s missing", c.pointer)
			continue
		}
		if got.Value != c.value || got.Layer != c.layer {
			t.Errorf("%s = %#v from %s, want %#v from %s", c.pointer, got.Value, got.Layer, c.value, c.layer)
		}
	}
	if _, ok := leaves["/replica/tolerations/0/key"]; ok {
		t.Errorf("tolerations should be deleted by the env layer's null")
	}
	if deleted["/replica/tolerations"] != "env:eu-prod" {
		t.Errorf("deleted = %v", deleted)
	}
	if l := leaves["/master/persistence/size"]; l.Line != 10 || l.Column != 11 {
		t.Errorf("position of size in eu-prod = %d:%d, want 10:11", l.Line, l.Column)
	}
	t.Logf("alias-provided leaf position: %d:%d; merge-key leaf position: %d:%d",
		leaves["/master/resources/limits/cpu"].Line, leaves["/master/resources/limits/cpu"].Column,
		leaves["/replica/resources/requests/cpu"].Line, leaves["/replica/resources/requests/cpu"].Column)
	t.Logf("effective leaves: %d, deleted keys: %d", len(leaves), len(deleted))
}

func TestReplaceScalarInPlace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, layer, pointer string
		value                any
		wantOld, wantNew     string
		wantLine             string // full line after the edit
	}{
		{"env leaf", "env:eu-prod", "/master/persistence/size", "50Gi", "20Gi", "50Gi", "    size: 50Gi"},
		{"keeps trailing comment", "env:eu-prod", "/auth/existingSecret", "redis-eu-prod-v2", "redis-eu-prod", "redis-eu-prod-v2", "  existingSecret: redis-eu-prod-v2   # ref://vault/shop/eu-prod#redis"},
		{"keeps double quotes and comment", "common", "/image/tag", "8.2.4", `"8.2.1"`, `"8.2.4"`, `  tag: "8.2.4"          # @schema type:string`},
		{"keeps single quotes", "common", "/replica/resources/limits/cpu", "2", `'1'`, `'2'`, "      cpu: '2'"},
		{"quotes a numeric-looking string", "common", "/master/persistence/storageClass", "1000", `""`, `"1000"`, `    storageClass: "1000"    # empty = cluster default`},
		{"bool", "env:eu-prod", "/metrics/enabled", false, "true", "false", "  enabled: false"},
		{"int", "env:eu-prod", "/replica/replicaCount", 5, "3", "5", "  replicaCount: 5"},
		{"sequence index", "common", "/replica/tolerations/0/value", "redis", "cache", "redis", "      value: redis"},
		{"escaped key", "common", "/master/podLabels/app.kubernetes.io~1part-of", "checkout", "shop", "checkout", "    app.kubernetes.io/part-of: checkout"},
		{"quoted NO stays quoted", "common", "/commonAnnotations/mode", "YES", `"NO"`, `"YES"`, `  mode: "YES"            # quoted on purpose: YAML 1.1 would read NO as false`},
		{"scalar replaces null", "env:eu-prod", "/replica/tolerations", "none", "null", "none", "  tolerations: none       # prod nodes are not tainted: drop the common toleration"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			l := layer(t, c.layer)
			e, err := Set(l, c.pointer, c.value)
			if err != nil {
				t.Fatal(err)
			}
			if e.Kind != "replace" || e.Old != c.wantOld || e.New != c.wantNew {
				t.Errorf("edit = %+v, want replace %q -> %q", e, c.wantOld, c.wantNew)
			}
			out := check(t, l, e, c.pointer, c.value)
			if !strings.Contains(string(out), c.wantLine+"\n") {
				t.Errorf("edited file lacks line %q:\n%s", c.wantLine, out)
			}
			t.Logf("%s: %d bytes replaced by %d, %d bytes untouched", c.layer, len(e.Old), len(e.New), len(l.Src)-len(e.Old))
		})
	}
}

func TestIdempotentWrite(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod")
	e, err := Set(l, "/master/persistence/size", "20Gi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e.Apply(l.Src), l.Src) {
		t.Errorf("writing the current value changed the file")
	}
}

func TestInsertUnderExistingMapping(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod")
	e, err := Set(l, "/replica/resources/limits/memory", "2Gi")
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != "insert" {
		t.Fatalf("kind %s", e.Kind)
	}
	out := check(t, l, e, "/replica/resources/limits/memory", "2Gi")
	want := "  tolerations: null       # prod nodes are not tainted: drop the common toleration\n  resources:\n    limits:\n      memory: 2Gi\n\nmetrics:\n"
	if !strings.Contains(string(out), want) {
		t.Errorf("insertion landed wrong:\n%s", out)
	}
	leaves, _, _ := Effective([]*Layer{must(ParseLayer("e", "e", "human", out))})
	if leaves["/metrics/enabled"].Line != 20 {
		t.Errorf("metrics.enabled moved to line %d, want 20 (17 + 3 inserted lines)", leaves["/metrics/enabled"].Line)
	}
}

func TestInsertTopLevelKey(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod")
	e, err := Set(l, "/image/registry", "registry.eu-prod.example")
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/image/registry", "registry.eu-prod.example")
	if !bytes.HasSuffix(out, []byte("    enabled: true\nimage:\n  registry: registry.eu-prod.example\n")) {
		t.Errorf("top-level insert should append after the last entry:\n%s", out)
	}
}

func TestInsertAfterBlockScalarKeepsTrailingComment(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	e, err := Set(l, "/podSecurityContext/fsGroup", 1001)
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/podSecurityContext/fsGroup", 1001)
	want := "  echo \"starting\"\npodSecurityContext:\n  fsGroup: 1001\n\n# trailing comment at the end of the file\n"
	if !strings.Contains(string(out), want) {
		t.Errorf("insert after a block scalar landed wrong:\n%s", out)
	}
}

func TestNullDeleteInEnvLayer(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod")
	e, err := Set(l, "/master/persistence/enabled", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/master/persistence/enabled", nil)
	if !strings.Contains(string(out), "    size: 20Gi\n    enabled: null\n") {
		t.Errorf("null was not inserted under master.persistence:\n%s", out)
	}
	all := stack(t)
	all[2] = must(ParseLayer(l.Name, l.Path, l.Owner, out))
	leaves, deleted, err := Effective(all)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leaves["/master/persistence/enabled"]; ok || deleted["/master/persistence/enabled"] != "env:eu-prod" {
		t.Errorf("null delete not reflected: present=%v deleted=%v", ok, deleted)
	}
}

func TestReplaceBlockScalar(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	script := "#!/bin/sh\nset -e\necho \"starting v2\"\n"
	e, err := Set(l, "/initScript", script)
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/initScript", script)
	want := "initScript: |\n  #!/bin/sh\n  set -e\n  echo \"starting v2\"\n\n# trailing comment at the end of the file\n"
	if !strings.Contains(string(out), want) {
		t.Errorf("block scalar replacement wrong:\n%s", out)
	}
	if !strings.Contains(e.Old, "# keep this comment: it is part of the script") {
		t.Errorf("old span should cover the whole block scalar: %q", e.Old)
	}
}

func TestReplaceFoldedScalarKeepsNeighbours(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	e, err := Set(l, "/commonAnnotations/description", "Redis for the shop.")
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/commonAnnotations/description", "Redis for the shop.")
	if !strings.Contains(string(out), "  description: Redis for the shop.\n  mode: \"NO\"") {
		t.Errorf("folded scalar replacement wrong:\n%s", out)
	}
}

func TestReplaceSubtree(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	tol := []any{map[string]any{"key": "cache", "operator": "Exists", "effect": "NoSchedule"}}
	e, err := Set(l, "/replica/tolerations", tol)
	if err != nil {
		t.Fatal(err)
	}
	out := e.Apply(l.Src)
	nl := must(ParseLayer("c", "c", "human", out))
	leaves, _, _ := Effective([]*Layer{nl})
	if leaves["/replica/tolerations/0/operator"].Value != "Exists" || leaves["/replica/tolerations/0/key"].Value != "cache" {
		t.Errorf("subtree not replaced: %v\n%s", keys(leaves), out)
	}
	if _, ok := leaves["/replica/tolerations/0/value"]; ok {
		t.Errorf("old list element survived")
	}
	if !strings.Contains(string(out), "\nmetrics:\n  enabled: false\n") {
		t.Errorf("neighbour section damaged:\n%s", out)
	}
	t.Logf("subtree replacement:\n%s", e.New)
}

func TestAliasEditRefused(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	for _, p := range []string{"/master/resources/limits/cpu", "/replica/resources/requests/cpu"} {
		_, err := Set(l, p, "1")
		if !errors.Is(err, ErrAlias) {
			t.Errorf("%s: expected ErrAlias, got %v", p, err)
		} else {
			t.Logf("%s: %v", p, err)
		}
	}
	if _, err := Set(l, "/replica/resources/limits/cpu", "2"); err != nil {
		t.Errorf("literal sibling of a merge key should be editable: %v", err)
	}
}

func TestMachineOwnedRefused(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod/images")
	if _, err := Set(l, "/image/tag", "9.0.0"); !errors.Is(err, ErrMachineOwned) {
		t.Errorf("expected ErrMachineOwned, got %v", err)
	}
	if _, err := Remove(l, "/image/tag"); !errors.Is(err, ErrMachineOwned) {
		t.Errorf("expected ErrMachineOwned, got %v", err)
	}
}

func TestRemoveKey(t *testing.T) {
	t.Parallel()
	l := layer(t, "env:eu-prod")
	e, err := Remove(l, "/replica/tolerations")
	if err != nil {
		t.Fatal(err)
	}
	if e.Old != "  tolerations: null       # prod nodes are not tainted: drop the common toleration\n" {
		t.Errorf("removed span %q", e.Old)
	}
	out := e.Apply(l.Src)
	all := stack(t)
	all[2] = must(ParseLayer(l.Name, l.Path, l.Owner, out))
	leaves, deleted, _ := Effective(all)
	if leaves["/replica/tolerations/0/key"].Value != "dedicated" || len(deleted) != 0 {
		t.Errorf("after removing the null override the common toleration should return: %v %v", leaves["/replica/tolerations/0/key"], deleted)
	}
	if _, err := Remove(l, "/auth/existingSecret"); err == nil {
		t.Errorf("removing the last key of a mapping must be refused")
	}
}

func TestFlowContainersRefused(t *testing.T) {
	t.Parallel()
	l := layer(t, "common")
	if _, err := Set(l, "/master/extraFlags/0", "--maxmemory 1gb"); err == nil {
		t.Errorf("appending to an empty flow sequence must be refused (out of scope), got nil")
	} else {
		t.Logf("flow sequence: %v", err)
	}
}

func TestEmptyLayerInsert(t *testing.T) {
	t.Parallel()
	l := must(ParseLayer("new", "envs/new/values.yaml", "human", []byte("# new environment\n")))
	e, err := Set(l, "/master/persistence/size", "5Gi")
	if err != nil {
		t.Fatal(err)
	}
	out := check(t, l, e, "/master/persistence/size", "5Gi")
	if string(out) != "# new environment\nmaster:\n  persistence:\n    size: 5Gi\n" {
		t.Errorf("empty layer insert:\n%s", out)
	}
}

// TestReencodeChangesLines measures the naive alternative: decode to nodes
// and encode again. Every changed line is a reason the product splices.
func TestReencodeChangesLines(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"common/values.yaml", "envs/eu-prod/values.yaml"} {
		src, err := os.ReadFile(filepath.Join(layersDir, f))
		if err != nil {
			t.Fatal(err)
		}
		out, err := Reencode(src)
		if err != nil {
			t.Fatal(err)
		}
		a, b := strings.Split(string(src), "\n"), strings.Split(string(out), "\n")
		same := map[string]bool{}
		for _, ln := range b {
			same[ln] = true
		}
		changed := 0
		var examples []string
		for _, ln := range a {
			if !same[ln] {
				changed++
				if len(examples) < 6 {
					examples = append(examples, ln)
				}
			}
		}
		t.Logf("%s: %d of %d original lines are not present verbatim after yaml.v3 re-encode (byte-identical: %v); examples: %q", f, changed, len(a), bytes.Equal(src, out), examples)
		if bytes.Equal(src, out) {
			t.Errorf("%s: expected the naive re-encode to change the file", f)
		}
	}
}

func must(l *Layer, err error) *Layer {
	if err != nil {
		panic(err)
	}
	return l
}
