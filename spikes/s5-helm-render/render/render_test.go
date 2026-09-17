// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigyaml "sigs.k8s.io/yaml"
)

var fixedNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

const (
	parentDir = "../testdata/charts/parent"
	redisDir  = "../testdata/charts/redis"
	redisSchm = "../testdata/redis.values.schema.json"
)

func secret(ns, name string, data map[string]string) *unstructured.Unstructured {
	d := map[string]any{}
	for k, v := range data {
		d[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": name, "namespace": ns},
		"type":     "Opaque",
		"data":     d,
	}}
}

// snapshot is a 1.37 cluster with the Prometheus operator installed, no
// OpenShift, and two Secrets in the demo namespace.
func snapshot() *Snapshot {
	return &Snapshot{
		KubeVersion: "v1.37.0",
		APIVersions: append(append([]string{}, common.DefaultCapabilities.APIVersions...), "monitoring.coreos.com/v1"),
		Objects: []*unstructured.Unstructured{
			secret("demo", "existing", map[string]string{"password": "s3cret"}),
			secret("demo", "redis", map[string]string{"redis-password": "from-cluster"}),
			secret("other", "existing", map[string]string{"password": "wrong-namespace"}),
		},
	}
}

func opts(deterministic bool, seed string) Options {
	return Options{ReleaseName: "redis", Namespace: "demo", Seed: seed, Now: fixedNow, Deterministic: deterministic}
}

func load(t *testing.T, dir string) *chart.Chart {
	t.Helper()
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return c
}

// redisWithSchema copies the vendored Bitnami chart into a temp dir, adds the
// fixture contract as values.schema.json and removes the .helmignore line that
// Bitnami ships ("# Schema validation (temporarily disabled)"), which would
// otherwise make the loader drop the schema. TestHelmignoreDropsSchema records
// that behaviour.
func redisWithSchema(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "redis")
	copyTree(t, redisDir, dst)
	schema, err := os.ReadFile(redisSchm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "values.schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	ign, err := os.ReadFile(filepath.Join(dst, ".helmignore"))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, line := range strings.Split(string(ign), "\n") {
		if strings.TrimSpace(line) != "values.schema.json" {
			keep = append(keep, line)
		}
	}
	if err := os.WriteFile(filepath.Join(dst, ".helmignore"), []byte(strings.Join(keep, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func docByKindName(t *testing.T, docs []Doc, kind, name string) Doc {
	t.Helper()
	for _, d := range docs {
		if d.Kind == kind && d.Name == name {
			return d
		}
	}
	t.Fatalf("no %s/%s in %d docs", kind, name, len(docs))
	return Doc{}
}

func dataOf(t *testing.T, d Doc) map[string]string {
	t.Helper()
	var m struct {
		Data map[string]string `json:"data"`
	}
	if err := sigyaml.Unmarshal([]byte(d.Canonical), &m); err != nil {
		t.Fatal(err)
	}
	return m.Data
}

func digest(docs []Doc) string {
	sum := sha256.Sum256([]byte(Join(docs)))
	return hex.EncodeToString(sum[:])
}

func TestParentCapabilitiesAndLookup(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	out, err := Render(c, nil, snapshot(), opts(true, "seed-1"))
	if err != nil {
		t.Fatal(err)
	}
	docs, err := Docs(out)
	if err != nil {
		t.Fatal(err)
	}
	data := dataOf(t, docByKindName(t, docs, "ConfigMap", "redis-parent"))
	want := map[string]string{
		"kubeVersion":       "v1.37.0",
		"kubeMinor":         "37",
		"hasRoute":          "false",
		"hasServiceMonitor": "true",
		"existingSecret":    base64.StdEncoding.EncodeToString([]byte("s3cret")),
		"secretCount":       "2",
		"renderedAt":        "2026-09-17T12:00:00Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("%s = %q, want %q", k, data[k], v)
		}
	}
	child := dataOf(t, docByKindName(t, docs, "ConfigMap", "redis-child"))
	if child["kubeVersion"] != "v1.37.0" {
		t.Errorf("subchart sees kubeVersion %q", child["kubeVersion"])
	}

	// An OpenShift snapshot flips the discovery-driven branch.
	os := snapshot()
	os.APIVersions = append(os.APIVersions, "route.openshift.io/v1")
	out, err = Render(c, nil, os, opts(true, "seed-1"))
	if err != nil {
		t.Fatal(err)
	}
	docs, _ = Docs(out)
	if got := dataOf(t, docByKindName(t, docs, "ConfigMap", "redis-parent"))["hasRoute"]; got != "true" {
		t.Errorf("hasRoute with OpenShift discovery = %q, want true", got)
	}
}

// TestDesignPathLookup shows the literal design path (RenderWithClientProvider
// over a fake dynamic client) answers lookup from the snapshot too.
func TestDesignPathLookup(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	out, err := RenderDesignPath(c, nil, snapshot(), opts(false, ""))
	if err != nil {
		t.Fatal(err)
	}
	docs, err := Docs(out)
	if err != nil {
		t.Fatal(err)
	}
	data := dataOf(t, docByKindName(t, docs, "ConfigMap", "redis-parent"))
	if data["existingSecret"] != base64.StdEncoding.EncodeToString([]byte("s3cret")) || data["secretCount"] != "2" {
		t.Errorf("design path lookup: existingSecret=%q secretCount=%q", data["existingSecret"], data["secretCount"])
	}
}

// TestStockEngineIsNotDeterministic is the negative control: the same inputs
// rendered twice by the stock engine differ in every entropy-derived key,
// while derivePassword (scrypt) already agrees.
func TestStockEngineIsNotDeterministic(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	a, err := Render(c, nil, snapshot(), opts(false, ""))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(c, nil, snapshot(), opts(false, ""))
	if err != nil {
		t.Fatal(err)
	}
	da, _ := Docs(a)
	db, _ := Docs(b)
	ma := dataOf(t, docByKindName(t, da, "ConfigMap", "redis-parent"))
	mb := dataOf(t, docByKindName(t, db, "ConfigMap", "redis-parent"))
	var differ []string
	for k := range ma {
		if ma[k] != mb[k] {
			differ = append(differ, k)
		}
	}
	t.Logf("stock engine, two renders: %d keys differ %v", len(differ), differ)
	for _, k := range []string{"generated", "generated2", "uuid", "caCertSha", "certSha", "signedSha", "bcrypt", "htpasswd", "randBytes"} {
		if ma[k] == mb[k] {
			t.Errorf("stock %s unexpectedly equal across renders", k)
		}
	}
	if ma["derived"] != mb["derived"] {
		t.Errorf("derivePassword is not deterministic in sprig: %q vs %q", ma["derived"], mb["derived"])
	}
	if digest(da) == digest(db) {
		t.Errorf("stock renders should not be byte-identical")
	}
}

func TestDeterministicRenderIsByteIdentical(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	a, err := Render(c, nil, snapshot(), opts(true, "seed-A"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(c, nil, snapshot(), opts(true, "seed-A"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("template counts differ: %d vs %d", len(a), len(b))
	}
	for k := range a {
		if a[k] != b[k] {
			t.Errorf("template %s differs between two deterministic renders", k)
		}
	}
	da, _ := Docs(a)
	db, _ := Docs(b)
	if digest(da) != digest(db) {
		t.Fatalf("digests differ: %s vs %s", digest(da), digest(db))
	}
	data := dataOf(t, docByKindName(t, da, "ConfigMap", "redis-parent"))
	if data["generated"] == data["generated2"] {
		t.Errorf("two randAlphaNum calls returned the same value %q; charts expect distinct secrets", data["generated"])
	}
	if !strings.HasPrefix(data["bcrypt"], "$2a$10$") || len(data["bcrypt"]) != 60 {
		t.Errorf("bcrypt shape %q", data["bcrypt"])
	}
	if len(data["uuid"]) != 36 || data["uuid"][14] != '4' {
		t.Errorf("uuidv4 shape %q", data["uuid"])
	}
	// A different seed changes the generated values, so the seed really is
	// an input to the report.
	other, err := Render(c, nil, snapshot(), opts(true, "seed-B"))
	if err != nil {
		t.Fatal(err)
	}
	do, _ := Docs(other)
	if dataOf(t, docByKindName(t, do, "ConfigMap", "redis-parent"))["generated"] == data["generated"] {
		t.Errorf("seed does not influence randAlphaNum")
	}
	t.Logf("deterministic digest %s", digest(da))
}

// TestRedisRender is the substitute-input measurement on Bitnami redis 28.2.1:
// lookup reuses the cluster password, TLS autogeneration goes through genCA and
// genSignedCert, two deterministic renders are byte-identical, two stock
// renders are not, and the render time is recorded.
func TestRedisRender(t *testing.T) {
	t.Parallel()
	dir := redisWithSchema(t)
	c := load(t, dir)
	if len(c.Schema) == 0 {
		t.Fatal("schema not loaded from the fixture copy")
	}
	vals := map[string]any{
		"architecture": "replication",
		"replica":      map[string]any{"replicaCount": 2},
		"master":       map[string]any{"persistence": map[string]any{"storageClass": "standard", "size": "20Gi"}},
		"tls":          map[string]any{"enabled": true, "autoGenerated": true},
		"metrics":      map[string]any{"enabled": true},
	}

	start := time.Now()
	a, err := Render(c, vals, snapshot(), opts(true, "seed-redis"))
	el := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(c, vals, snapshot(), opts(true, "seed-redis"))
	if err != nil {
		t.Fatal(err)
	}
	da, err := Docs(a)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := Docs(b)
	t.Logf("redis: %d documents, first render %s, digest %s", len(da), el, digest(da))
	if Join(da) != Join(db) {
		t.Errorf("two deterministic redis renders differ")
	}

	sec := dataOf(t, docByKindName(t, da, "Secret", "redis"))
	if got, want := sec["redis-password"], base64.StdEncoding.EncodeToString([]byte("from-cluster")); got != want {
		t.Errorf("lookup: redis-password = %q, want the cluster's %q", got, want)
	}
	var tlsSecret Doc
	for _, d := range da {
		if d.Kind == "Secret" && strings.Contains(d.Canonical, "tls.crt") {
			tlsSecret = d
		}
	}
	if tlsSecret.Name == "" {
		t.Fatalf("no TLS secret rendered; docs: %v", kinds(da))
	}
	crt, _ := base64.StdEncoding.DecodeString(dataOf(t, tlsSecret)["tls.crt"])
	if !bytes.Contains(crt, []byte("BEGIN CERTIFICATE")) {
		t.Errorf("tls.crt is not PEM")
	}

	// Without the cluster Secret the chart generates a password: stock
	// renders differ, deterministic renders do not.
	empty := &Snapshot{KubeVersion: "v1.37.0", APIVersions: common.DefaultCapabilities.APIVersions}
	s1, _ := Render(c, vals, empty, opts(false, ""))
	s2, _ := Render(c, vals, empty, opts(false, ""))
	ds1, _ := Docs(s1)
	ds2, _ := Docs(s2)
	var differing []string
	for i := range ds1 {
		if ds1[i].Canonical != ds2[i].Canonical {
			differing = append(differing, ds1[i].Kind+"/"+ds1[i].Name)
		}
	}
	t.Logf("redis, stock engine, no cluster secret: %d of %d documents differ between two renders: %v", len(differing), len(ds1), differing)
	if len(differing) == 0 {
		t.Errorf("expected the generated password and TLS material to differ across stock renders")
	}
	d1, _ := Render(c, vals, empty, opts(true, "seed-redis"))
	d2, _ := Render(c, vals, empty, opts(true, "seed-redis"))
	dd1, _ := Docs(d1)
	dd2, _ := Docs(d2)
	if Join(dd1) != Join(dd2) {
		t.Errorf("deterministic renders without cluster secret differ")
	}
	if dataOf(t, docByKindName(t, dd1, "Secret", "redis"))["redis-password"] == sec["redis-password"] {
		t.Errorf("generated password should differ from the cluster one")
	}

	// KubeVersion drives Bitnami's capability helpers: common.capabilities.psp.supported
	// is true below 1.25, so templates/master/psp.yaml renders on a 1.20
	// snapshot and not on 1.37 (the PDB helper is a constant in common 2.x).
	pspVals := map[string]any{"podSecurityPolicy": map[string]any{"create": true, "enabled": true}}
	countPSP := func(kv string) int {
		snap := &Snapshot{KubeVersion: kv, APIVersions: common.DefaultCapabilities.APIVersions}
		o, err := Render(c, pspVals, snap, opts(true, "seed-redis"))
		if err != nil {
			t.Fatal(err)
		}
		do, _ := Docs(o)
		n := 0
		for _, d := range do {
			if d.Kind == "PodSecurityPolicy" {
				n++
			}
		}
		return n
	}
	if n := countPSP("v1.37.0"); n != 0 {
		t.Errorf("1.37 snapshot rendered %d PodSecurityPolicy objects", n)
	}
	if n := countPSP("v1.20.0"); n != 1 {
		t.Errorf("1.20 snapshot rendered %d PodSecurityPolicy objects, want 1", n)
	}
}

func kinds(docs []Doc) []string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Kind+"/"+d.Name)
	}
	return out
}

// TestSchema2020Semantics proves Helm 4.3 evaluates 2020-12 keywords, not just
// tolerates the $schema URL: unevaluatedProperties, if/then, dependentRequired,
// $ref into $defs and x-fathom-* annotations.
func TestSchema2020Semantics(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	cases := []struct {
		name string
		vals map[string]any
		want string // substring of the error; "" means valid
	}{
		{"defaults are valid", nil, ""},
		{"pattern via $ref/$defs", map[string]any{"persistence": map[string]any{"size": "8G"}}, "does not match pattern"},
		{"unevaluatedProperties (2020-12 only)", map[string]any{"persistence": map[string]any{"foo": 1}}, "/persistence/foo"},
		{"if/then", map[string]any{"ingress": map[string]any{"enabled": true}}, "/ingress/host"},
		{"subchart schema", map[string]any{"child": map[string]any{"replicas": 0}}, "/child/replicas"},
		{"x-fathom keywords are ignored by validation", map[string]any{"password": "x"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Render(c, tc.vals, snapshot(), opts(true, "s"))
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.want != "" && err == nil:
				t.Errorf("expected error containing %q, got none", tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestSubchartMergeSemantics records what each schema sees after coalescing:
// the parent schema sees the whole merged map including the subchart section
// and global; the subchart schema sees its own section plus global (and only
// global: the child fixture has additionalProperties=false).
func TestSubchartMergeSemantics(t *testing.T) {
	t.Parallel()
	c := load(t, parentDir)
	strict := *c
	strict.Schema = []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
	  "properties":{"persistence":{},"ingress":{},"password":{}},"additionalProperties":false}`)
	_, err := Render(&strict, nil, snapshot(), opts(true, "s"))
	if err == nil {
		t.Fatal("parent schema with additionalProperties=false must see subchart and global keys")
	}
	// helm 4.3.0 / jsonschema v6 phrase it as
	// "at '': additional properties 'child', 'global' not allowed".
	for _, key := range []string{"'child'", "'global'"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should name %s as additional property:\n%s", key, err)
		}
	}
	t.Logf("parent schema over the merged map reports:\n%s", err)
	// The unmodified fixture passes: child's additionalProperties=false
	// tolerates exactly {global, enabled, replicas}.
	if _, err := Render(c, nil, snapshot(), opts(true, "s")); err != nil {
		t.Errorf("child schema rejected the coalesced values: %v", err)
	}
}

// TestHelmignoreDropsSchema records the Bitnami finding: the vendored chart's
// .helmignore lists values.schema.json, so a schema dropped into the chart
// directory is silently ignored by the loader.
func TestHelmignoreDropsSchema(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "redis")
	copyTree(t, redisDir, dst)
	schema, _ := os.ReadFile(redisSchm)
	if err := os.WriteFile(filepath.Join(dst, "values.schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	c := load(t, dst)
	if len(c.Schema) != 0 {
		t.Fatalf("expected the .helmignore to drop the schema; loaded %d bytes", len(c.Schema))
	}
	_, err := Render(c, map[string]any{"replica": map[string]any{"replicaCount": -1}}, snapshot(), opts(true, "s"))
	if err != nil {
		t.Errorf("with the schema ignored, an invalid value should render: %v", err)
	}
	t.Log("Bitnami redis 28.2.1 .helmignore contains 'values.schema.json' (comment: Schema validation (temporarily disabled)); Helm ignores any schema in the chart directory")
}

// --- CLI oracles: helm 4.3.0 in PATH and Argo CD's helm 4.2.1 ------------

func helmBinary(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not in PATH")
	}
	return p
}

// argoHelm returns the helm binary Argo CD v3.5.3 bundles, fetched by
// hack/fetch-argo-helm.sh, or skips.
func argoHelm(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("S5_ARGO_HELM"); p != "" {
		return p
	}
	cache, err := os.UserCacheDir()
	if err == nil {
		p := filepath.Join(cache, "fathom-spikes", "argo-v3.5.3", "linux-amd64", "helm")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("Argo CD's helm not found; run hack/fetch-argo-helm.sh v3.5.3 $XDG_CACHE_HOME/fathom-spikes/argo-v3.5.3 or set S5_ARGO_HELM")
	return ""
}

// offline runs a command in a fresh network namespace when unshare allows it.
func offline(t *testing.T, name string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var cmd *exec.Cmd
	if exec.Command("unshare", "-rn", "true").Run() == nil {
		cmd = exec.Command("unshare", append([]string{"-rn", name}, args...)...)
	} else {
		t.Log("unshare -rn unavailable: running with network")
		cmd = exec.Command(name, args...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

func assertNoWarnings(t *testing.T, label, stderr string) {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "warn") || strings.Contains(l, "error") {
			t.Errorf("%s: stderr has a warning or error: %s", label, line)
		}
	}
}

func TestCLIAcceptsSchemaOffline(t *testing.T) {
	t.Parallel()
	helm := helmBinary(t)
	ver, _ := exec.Command(helm, "version", "--template", "{{.Version}}").Output()
	t.Logf("helm in PATH: %s", ver)
	for _, dir := range []string{parentDir, redisWithSchema(t)} {
		_, stderr, err := offline(t, helm, "lint", "--strict", dir)
		if err != nil {
			t.Errorf("helm lint --strict %s: %v\n%s", dir, err, stderr)
		}
		assertNoWarnings(t, "lint "+dir, stderr)
		out, stderr, err := offline(t, helm, "template", "redis", dir, "--namespace", "demo", "--kube-version", "1.37.0")
		if err != nil {
			t.Errorf("helm template %s: %v\n%s", dir, err, stderr)
		}
		assertNoWarnings(t, "template "+dir, stderr)
		if stderr != "" {
			t.Logf("template %s stderr: %s", dir, stderr)
		}
		t.Logf("helm template %s: %d documents, stderr %d bytes", filepath.Base(dir), len(SplitDocs(out)), len(stderr))
	}
	// Negative control: the CLI enforces the contract on the redis chart once
	// the .helmignore line is gone.
	dir := redisWithSchema(t)
	_, stderr, err := offline(t, helm, "template", "redis", dir, "--namespace", "demo", "--set", "master.persistence.size=8G")
	if err == nil || !strings.Contains(stderr, "does not match pattern") {
		t.Errorf("expected a schema error from the CLI, got err=%v stderr=%s", err, stderr)
	}
	// Negative control: a remote $ref needs the network, so contracts must
	// not use one.
	remote := filepath.Join(t.TempDir(), "parent")
	copyTree(t, parentDir, remote)
	if err := os.WriteFile(filepath.Join(remote, "values.schema.json"), []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"password":{"$ref":"https://example.com/secret.schema.json"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = offline(t, helm, "template", "t", remote, "--namespace", "demo")
	if err == nil {
		t.Errorf("a remote $ref rendered offline; stderr=%s", stderr)
	}
	t.Logf("remote $ref offline: %s", strings.TrimSpace(strings.Split(stderr, "\n")[0]))
}

// TestArgoHelmParity renders the redis chart with the exact helm binary and
// argument list Argo CD v3.5.3 uses (util/helm/cmd.go: template <path>
// --name-template --namespace --kube-version --api-versions --include-crds)
// and compares every document with the SDK render.
func TestArgoHelmParity(t *testing.T) {
	t.Parallel()
	argo := argoHelm(t)
	ver, _ := exec.Command(argo, "version", "--template", "{{.Version}}").Output()
	t.Logf("Argo CD v3.5.3 bundles helm %s", ver)
	dir := redisWithSchema(t)
	extra := []string{"monitoring.coreos.com/v1", "route.openshift.io/v1"}
	args := []string{"template", dir, "--name-template", "redis", "--namespace", "demo", "--kube-version", "1.37.0",
		"--set", "auth.password=fixed-parity", "--set", "metrics.enabled=true", "--set", "architecture=replication"}
	for _, v := range extra {
		args = append(args, "--api-versions", v)
	}
	args = append(args, "--include-crds")
	out, stderr, err := offline(t, argo, args...)
	if err != nil {
		t.Fatalf("argo helm: %v\n%s", err, stderr)
	}
	assertNoWarnings(t, "argo helm template", stderr)

	c := load(t, dir)
	snap := &Snapshot{KubeVersion: "v1.37.0", APIVersions: append(append([]string{}, common.DefaultCapabilities.APIVersions...), extra...)}
	vals := map[string]any{"auth": map[string]any{"password": "fixed-parity"}, "metrics": map[string]any{"enabled": true}, "architecture": "replication"}
	sdk, err := Render(c, vals, snap, opts(true, "parity"))
	if err != nil {
		t.Fatal(err)
	}
	sdkDocs, err := Docs(sdk)
	if err != nil {
		t.Fatal(err)
	}
	cliDocs, err := Docs(map[string]string{"argo": out})
	if err != nil {
		t.Fatal(err)
	}
	byKey := func(docs []Doc) map[string]string {
		m := map[string]string{}
		for _, d := range docs {
			m[d.Kind+"/"+d.Name] = d.Canonical
		}
		return m
	}
	a, b := byKey(sdkDocs), byKey(cliDocs)
	identical, differing := 0, []string{}
	for k, v := range a {
		if b[k] == v {
			identical++
		} else {
			differing = append(differing, k)
		}
	}
	t.Logf("Argo helm %s vs SDK: %d documents each, %d identical, differing: %v", strings.TrimSpace(string(ver)), len(a), identical, differing)
	if len(a) != len(b) || identical != len(a) {
		t.Errorf("Argo's render and the SDK render are not document-identical: sdk=%d cli=%d identical=%d differing=%v", len(a), len(b), identical, differing)
	}
}

func TestDeterministicFuncsUnit(t *testing.T) {
	t.Parallel()
	f1 := DeterministicFuncs("x", fixedNow)
	f2 := DeterministicFuncs("x", fixedNow)
	if a, b := f1["randAlphaNum"].(func(int) string)(32), f2["randAlphaNum"].(func(int) string)(32); a != b {
		t.Errorf("randAlphaNum not reproducible: %s vs %s", a, b)
	}
	ca, err := f1["genCA"].(func(string, int) (Certificate, error))("ca", 10)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := f1["genSignedCert"].(func(string, []any, []any, int, Certificate) (Certificate, error))("leaf", []any{"10.0.0.1"}, []any{"a.svc"}, 10, ca)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(leaf.Cert, "-----BEGIN CERTIFICATE-----") || !strings.HasPrefix(leaf.Key, "-----BEGIN PRIVATE KEY-----") {
		t.Errorf("signed cert shape wrong")
	}
	ca2, _ := f2["genCA"].(func(string, int) (Certificate, error))("ca", 10)
	if ca.Cert != ca2.Cert {
		t.Errorf("genCA not reproducible")
	}
	if n := f1["now"].(func() time.Time)(); !n.Equal(fixedNow) {
		t.Errorf("now = %v", n)
	}
	if got := fmt.Sprint(f1["randInt"].(func(int, int) int)(5, 5)); got != "5" {
		t.Errorf("randInt with empty range = %s", got)
	}
}
