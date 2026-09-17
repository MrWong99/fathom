// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"k8s.io/apiserver/pkg/authorization/authorizer"
)

const testdata = "../testdata"

// requestVerb is the kubectl sub-command commands.sh used per scenario;
// everything else was captured with `kubectl apply`.
var requestVerb = map[string]string{
	"map-jsonpatch-create": "create",
}

var (
	sharedOnce  sync.Once
	sharedEval  *Evaluator
	sharedAuthz *SnapshotAuthorizer
	sharedMan   *Manifest
	sharedErr   error
)

func shared(t *testing.T) (*Evaluator, *SnapshotAuthorizer, *Manifest) {
	t.Helper()
	sharedOnce.Do(func() {
		snap, err := LoadSnapshot(testdata)
		if err != nil {
			sharedErr = err
			return
		}
		sharedMan, err = LoadManifest(testdata)
		if err != nil {
			sharedErr = err
			return
		}
		sharedAuthz = NewSnapshotAuthorizer()
		sharedEval, sharedErr = New(snap, os.DirFS(filepath.Join(testdata, "openapi")), Options{Authorizer: sharedAuthz})
	})
	if sharedErr != nil {
		t.Fatalf("setup: %v", sharedErr)
	}
	return sharedEval, sharedAuthz, sharedMan
}

func loadRequest(t *testing.T, sc Scenario) *RequestObject {
	t.Helper()
	verb := requestVerb[sc.Name]
	if verb == "" {
		verb = "apply"
	}
	req, err := LoadRequestObject(testdata, sc.ObjectFile, verb)
	if err != nil {
		t.Fatal(err)
	}
	ApplyNativeDefaults(req.Object)
	return req
}

// TestScenarios compares every manifest scenario with the oracle's golden
// files: kubectl output byte for byte, exit code, warnings as a set, and for
// mutations the canonicalised object.
func TestScenarios(t *testing.T) {
	t.Parallel()
	eval, _, man := shared(t)
	for _, sc := range man.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			req := loadRequest(t, sc)
			res := eval.Admit(context.Background(), req.Object)

			wantOut, err := os.ReadFile(filepath.Join(testdata, sc.GoldenFile))
			if err != nil {
				t.Fatal(err)
			}
			wantExitRaw, err := os.ReadFile(filepath.Join(testdata, "golden", sc.Name+".exit"))
			if err != nil {
				t.Fatal(err)
			}
			wantExit, _ := strconv.Atoi(strings.TrimSpace(string(wantExitRaw)))

			gotOut, gotExit := RenderKubectl(req, res)
			if res.Allowed() != sc.ServerAllowed {
				t.Errorf("allowed: offline %v, server %v", res.Allowed(), sc.ServerAllowed)
			}
			if gotExit != wantExit {
				t.Errorf("exit code: offline %d, server %d", gotExit, wantExit)
			}
			if gotOut != string(wantOut) {
				t.Errorf("kubectl output differs\n--- offline\n%s--- server\n%s", gotOut, wantOut)
			}
			gotWarn := append([]string{}, res.Warnings...)
			wantWarn := make([]string, 0, len(sc.ServerWarnings))
			for _, w := range sc.ServerWarnings {
				wantWarn = append(wantWarn, strings.TrimPrefix(w, "Warning: "))
			}
			sort.Strings(gotWarn)
			sort.Strings(wantWarn)
			if !reflect.DeepEqual(gotWarn, wantWarn) {
				t.Errorf("warnings (as sets)\n offline %q\n server  %q", gotWarn, wantWarn)
			}

			if sc.MutatedFile == "" {
				return
			}
			wantYAML, err := os.ReadFile(filepath.Join(testdata, sc.MutatedFile))
			if err != nil {
				t.Fatal(err)
			}
			want, err := CanonicalYAML(wantYAML)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Canonical(res.Object)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("mutated object differs\n--- offline\n%s\n--- server\n%s", CanonicalJSON(got), CanonicalJSON(want))
			}
		})
	}
}

// TestAuthorizerQuestion records what the vap-deny policy asks the
// authorizer and checks the snapshot authorizer answers it the way kind's
// RBAC does for kubernetes-admin (kubeadm:cluster-admins -> cluster-admin).
func TestAuthorizerQuestion(t *testing.T) {
	// Not parallel: it inspects the shared authorizer's log.
	eval, authz, man := shared(t)
	var sc Scenario
	for _, s := range man.Scenarios {
		if s.Name == "vap-deny-pass" {
			sc = s
		}
	}
	req := loadRequest(t, sc)
	authz.Reset()
	res := eval.Admit(context.Background(), req.Object)
	if !res.Allowed() {
		t.Fatalf("vap-deny-pass denied: %v", res.Err)
	}
	checks := authz.Checks()
	if len(checks) != 1 {
		t.Fatalf("expected exactly one authorizer question, got %d: %v", len(checks), checks)
	}
	c := checks[0]
	if c.User != "kubernetes-admin" || c.Verb != "get" || c.APIGroup != "" || c.Resource != "configmaps" || c.Namespace != "s2" || c.Decision != authorizer.DecisionAllow {
		t.Errorf("unexpected question/answer: %s", c)
	}
	t.Logf("authorizer question: %s", c)
}

// TestDefaultCompatibilityVersion records what the apiserver helper returns
// in a non-kubernetes binary, which is why the evaluator pins 1.36 itself.
func TestDefaultCompatibilityVersion(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Logf("environment.DefaultCompatibilityVersion() panicked in this process: %v", r)
		}
	}()
	t.Logf("environment.DefaultCompatibilityVersion() in this process = %s", defaultCompatibilityVersionString())
}
