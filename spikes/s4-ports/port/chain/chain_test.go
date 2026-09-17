// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/oracle"
	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

func loadOne(t *testing.T, path string) runtime.Object {
	t.Helper()
	objs, err := snapshot.LoadObjects(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if len(objs) != 1 {
		t.Fatalf("%s: want one object, got %d", path, len(objs))
	}
	return objs[0]
}

// compareDenial checks the offline error text against the server's stderr
// after StripWrapper. Every scenario is pinned in both directions: an
// unexpected admit or an unexpected denial fails.
func compareDenial(t *testing.T, name string, res Result, serverStderr string, serverAllowed bool) bool {
	t.Helper()
	want := oracle.StripWrapper(serverStderr)
	if serverAllowed {
		if res.Err != nil {
			t.Errorf("%s: server admitted, offline denied: %v", name, res.Err)
			return false
		}
		return true
	}
	if res.Err == nil {
		t.Errorf("%s: server denied %q, offline admitted", name, want)
		return false
	}
	got := res.Err.Error()
	t.Logf("%s\n  server : %s\n  offline: %s", name, want, got)
	if got != want {
		// several quotas exceeded: the server reports one in informer order,
		// offline reports the first by name and lists all of them
		for _, qe := range res.QuotaErrs {
			if qe.Error() == want {
				t.Logf("%s: order-dependent match, server picked one of %d exceeded quotas; offline lists all: %v", name, len(res.QuotaErrs), res.QuotaErrs)
				return true
			}
		}
		t.Errorf("%s: denial text differs\n  server : %s\n  offline: %s", name, want, got)
		return false
	}
	return true
}

// compareMutation checks the offline object against the server-mutated one.
// Both are diffed against the input; the offline diff must be a subset of the
// server diff (offline never changes what the server did not), and on the
// leaves LimitRanger owns (container resources, its annotation) the two
// diffs must be identical. The leaves only the server changed (defaulting,
// ServiceAccount, Priority, DefaultTolerationSeconds admission) are logged.
func compareMutation(t *testing.T, name string, input, offline, server runtime.Object) (identical bool, serverOnly []string) {
	t.Helper()
	in, err := oracle.Flatten(input)
	if err != nil {
		t.Fatal(err)
	}
	off, err := oracle.Flatten(offline)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oracle.Flatten(server)
	if err != nil {
		t.Fatal(err)
	}
	offAdded := oracle.Added(in, off)
	srvAdded := oracle.Added(in, srv)
	identical = true
	for p, v := range offAdded {
		if sv, ok := srvAdded[p]; !ok || sv != v {
			t.Errorf("%s: offline set %s=%s, server set %q", name, p, v, sv)
			identical = false
		}
	}
	if !oracle.Equal(oracle.Owned(offAdded), oracle.Owned(srvAdded)) {
		t.Errorf("%s: LimitRanger-owned leaves differ\n  offline: %v\n  server : %v", name, oracle.Owned(offAdded), oracle.Owned(srvAdded))
		identical = false
	}
	for _, p := range oracle.Paths(srvAdded) {
		if _, ok := offAdded[p]; !ok {
			serverOnly = append(serverOnly, p)
		}
	}
	t.Logf("%s: offline set %d leaves %v; server additionally set %d leaves outside LimitRanger: %v",
		name, len(offAdded), oracle.Paths(offAdded), len(serverOnly), serverOnly)
	return identical, serverOnly
}

// TestGoldens runs every scenario of testdata/manifest.json offline through
// LimitRanger mutate, LimitRanger validate and ResourceQuota, against the
// snapshot LimitRange and the quota status the scenario was captured with,
// and compares with the goldens.
func TestGoldens(t *testing.T) {
	t.Parallel()
	m, err := oracle.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	td := oracle.Testdata()
	for _, sc := range m.Scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			statusFile := "" // -> quota-status.yaml (phase A)
			if sc.QuotaStatusFile != "" {
				statusFile = filepath.Base(sc.QuotaStatusFile)
			}
			lrFile := "" // -> limitrange.yaml
			if sc.LimitRangeFile != "" {
				lrFile = filepath.Base(sc.LimitRangeFile)
			}
			c, err := NewFiles(filepath.Join(td, "snapshot"), lrFile, statusFile)
			if err != nil {
				t.Fatal(err)
			}
			input := loadOne(t, filepath.Join(td, sc.ObjectFile))
			obj := loadOne(t, filepath.Join(td, sc.ObjectFile))
			res := c.Admit(context.Background(), obj, nil)

			golden, err := os.ReadFile(filepath.Join(td, sc.GoldenFile))
			if err != nil {
				t.Fatal(err)
			}
			if oracle.StripWrapper(string(golden)) != oracle.StripWrapper(sc.ServerMessage) {
				t.Fatalf("manifest serverMessage and golden disagree for %s", sc.Name)
			}
			if !compareDenial(t, sc.Name, res, string(golden), sc.ServerAllowed) {
				return
			}
			if sc.ServerAllowed {
				server := loadOne(t, filepath.Join(td, sc.MutatedFile))
				compareMutation(t, sc.Name, input, res.Object, server)
			}
		})
	}
}

// TestOracleLive re-runs every scenario against the kind oracle now (skipped
// when the context is unreachable) and compares the live --dry-run=server
// result with the offline chain fed the *live* LimitRange and quota status,
// so the comparison holds whatever phase the s4 namespace is in. Nothing is
// created or deleted on the cluster.
func TestOracleLive(t *testing.T) {
	t.Parallel()
	m, err := oracle.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	k := oracle.NewKubectl(m)
	if !k.Reachable() {
		t.Skipf("kind oracle context %q not reachable; skipping live replay", k.Context)
	}
	ctx := context.Background()
	dir := t.TempDir()
	lr, _, err := k.Run(ctx, "", "get", "limitrange", "-n", m.Oracle.Namespace, "-o", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	qs, _, err := k.Run(ctx, "", "get", "resourcequota", "-n", m.Oracle.Namespace, "-o", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "limitrange.yaml"), []byte(lr), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "quota-status.yaml"), []byte(qs), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	td := oracle.Testdata()
	for _, sc := range m.Scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			mutated, stderr, admitted, err := k.DryRun(ctx, sc.ObjectFile)
			if err != nil {
				t.Fatal(err)
			}
			input := loadOne(t, filepath.Join(td, sc.ObjectFile))
			obj := loadOne(t, filepath.Join(td, sc.ObjectFile))
			res := c.Admit(ctx, obj, nil)
			if !compareDenial(t, sc.Name, res, stderr, admitted) {
				return
			}
			if admitted {
				objs, err := snapshot.DecodeObjects([]byte(mutated))
				if err != nil || len(objs) != 1 {
					t.Fatalf("decode live mutated object: %v (%d objects)", err, len(objs))
				}
				compareMutation(t, sc.Name, input, res.Object, objs[0])
			}
		})
	}
}

// TestNoLimitRangeNoQuotaIsNoop pins that a namespace without either object
// passes everything through untouched.
func TestNoLimitRangeNoQuotaIsNoop(t *testing.T) {
	t.Parallel()
	td := oracle.Testdata()
	c, err := New(filepath.Join(td, "snapshot"), "")
	if err != nil {
		t.Fatal(err)
	}
	obj := loadOne(t, filepath.Join(td, "objects", "lr-over-max.yaml"))
	acc := obj.(interface{ SetNamespace(string) })
	acc.SetNamespace("other")
	res := c.Admit(context.Background(), obj, nil)
	if res.Err != nil {
		t.Fatalf("namespace without LimitRange/quota must admit: %v", res.Err)
	}
	if strings.Contains(obj.(interface{ GetNamespace() string }).GetNamespace(), "s4") {
		t.Fatal("namespace not moved")
	}
}
