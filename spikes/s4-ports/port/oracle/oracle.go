// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package oracle is the test tooling shared by the port tests: the
// testdata/manifest.json types, the kubectl wrapper stripper, the object
// flattener used to compare mutations, and a kubectl runner for the live
// kind oracle (skipped when unreachable).
package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

// Manifest mirrors testdata/manifest.json (the fields the tests read).
type Manifest struct {
	Oracle struct {
		Context       string `json:"context"`
		ServerVersion string `json:"serverVersion"`
		Namespace     string `json:"namespace"`
	} `json:"oracle"`
	Scenarios  []Scenario  `json:"scenarios"`
	RBACChecks []RBACCheck `json:"rbacChecks"`
}

// Scenario is one kubectl --dry-run=server call.
type Scenario struct {
	Name            string `json:"name"`
	Area            string `json:"area"`
	Kind            string `json:"kind"`
	ObjectFile      string `json:"objectFile"`
	GoldenFile      string `json:"goldenFile"`
	ServerAllowed   bool   `json:"serverAllowed"`
	ServerMessage   string `json:"serverMessage"`
	MutatedFile     string `json:"mutatedFile"`
	QuotaStatusFile string `json:"quotaStatusFile"`
	LimitRangeFile  string `json:"limitRangeFile"`
	Notes           string `json:"notes"`
}

// RBACCheck is one kubectl auth can-i call.
type RBACCheck struct {
	Subject        string `json:"subject"`
	Verb           string `json:"verb"`
	Resource       string `json:"resource"`
	Subresource    string `json:"subresource"`
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Allowed        bool   `json:"allowed"`
	KubectlVerdict string `json:"kubectlVerdict"`
	Notes          string `json:"notes"`
}

// Testdata returns the absolute path of spikes/s4-ports/testdata from any package in the module.
func Testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata")
}

// LoadManifest reads testdata/manifest.json.
func LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(Testdata(), "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// GroupFor is the discovery step kubectl does for a bare resource name: the
// API group of the resources the fixture uses. Everything else is core.
func GroupFor(resource string) string {
	switch resource {
	case "deployments", "replicasets", "statefulsets", "daemonsets":
		return "apps"
	}
	return ""
}

// kubectlWrapper is the only text stripped from kubectl's stderr before a
// denial is compared: kubectl's own prefix around the apiserver's Status
// message. `Error from server (<Reason>): ` is printed by kubectl for a
// StatusError and `error when creating "<file>": ` by the apply command;
// everything after it is StatusError.Error() verbatim, which is what the
// offline chain returns.
var kubectlWrapper = regexp.MustCompile(`^Error from server \([A-Za-z]+\): error when (?:creating|applying|patching|updating) "[^"]*": `)

// StripWrapper removes the kubectl wrapper (and a trailing newline) from a
// captured stderr; it returns the input unchanged when the wrapper is absent.
func StripWrapper(stderr string) string {
	s := strings.TrimRight(stderr, "\n")
	return kubectlWrapper.ReplaceAllString(s, "")
}

// serverManaged are the leaves the apiserver or kubectl own on every object
// and that no admission plugin sets: dropped before mutation comparison.
var serverManaged = []string{
	"/metadata/creationTimestamp",
	"/metadata/uid",
	"/metadata/generation",
	"/metadata/resourceVersion",
	"/metadata/managedFields",
	"/metadata/annotations/kubectl.kubernetes.io~1last-applied-configuration",
	"/status",
}

// Leaves is a flattened object: RFC 6901 pointer -> JSON-encoded scalar.
type Leaves map[string]string

// Flatten converts a typed object to its leaves, dropping the server-managed
// paths. Quantities render canonically ("1500m", "1", "256Mi") because the
// conversion goes through the typed object's JSON marshalling.
func Flatten(obj k8sruntime.Object) (Leaves, error) {
	u, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	out := Leaves{}
	walk("", u, out)
	for p := range out {
		for _, m := range serverManaged {
			if p == m || strings.HasPrefix(p, m+"/") {
				delete(out, p)
			}
		}
	}
	return out, nil
}

func walk(prefix string, v interface{}, out Leaves) {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 0 {
			return
		}
		for k, val := range t {
			walk(prefix+"/"+escape(k), val, out)
		}
	case []interface{}:
		for i, val := range t {
			walk(fmt.Sprintf("%s/%d", prefix, i), val, out)
		}
	case nil:
		return
	default:
		b, _ := json.Marshal(t)
		out[prefix] = string(b)
	}
}

func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// Added returns the leaves of after that are absent from, or differ in, before.
func Added(before, after Leaves) Leaves {
	out := Leaves{}
	for p, v := range after {
		if bv, ok := before[p]; !ok || bv != v {
			out[p] = v
		}
	}
	return out
}

// Owned filters leaves to the paths LimitRanger writes: container and init
// container resources and its annotation.
func Owned(l Leaves) Leaves {
	out := Leaves{}
	for p, v := range l {
		if strings.HasPrefix(p, "/spec/containers/") && strings.Contains(p, "/resources/") ||
			strings.HasPrefix(p, "/spec/initContainers/") && strings.Contains(p, "/resources/") ||
			p == "/metadata/annotations/kubernetes.io~1limit-ranger" {
			out[p] = v
		}
	}
	return out
}

// Equal reports whether two leaf sets are identical.
func Equal(a, b Leaves) bool {
	if len(a) != len(b) {
		return false
	}
	for p, v := range a {
		if bv, ok := b[p]; !ok || bv != v {
			return false
		}
	}
	return true
}

// Paths lists the paths of l sorted, for logs.
func Paths(l Leaves) []string {
	out := make([]string, 0, len(l))
	for p := range l {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Kubectl runs kubectl against the oracle context.
type Kubectl struct {
	Context string
}

// NewKubectl picks the context from FATHOM_ORACLE_CONTEXT or the manifest.
func NewKubectl(m *Manifest) *Kubectl {
	ctx := os.Getenv("FATHOM_ORACLE_CONTEXT")
	if ctx == "" {
		ctx = m.Oracle.Context
	}
	return &Kubectl{Context: ctx}
}

// Reachable is true when the context answers /readyz within five seconds.
func (k *Kubectl) Reachable() bool {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return false
	}
	_, _, err := k.Run(context.Background(), "", "get", "--raw", "/readyz")
	return err == nil
}

// Run executes kubectl with the context, in dir when non-empty.
func (k *Kubectl) Run(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--context", k.Context}, args...)...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// DryRun is `kubectl apply --dry-run=server -f <objects/name.yaml> -o yaml`
// run from testdata, so the wrapper quotes the same path as the goldens.
// It returns the mutated object YAML (stdout), stderr and whether kubectl
// exited 0.
func (k *Kubectl) DryRun(ctx context.Context, objectFile string) (mutated, stderr string, admitted bool, err error) {
	out, errs, runErr := k.Run(ctx, Testdata(), "apply", "--dry-run=server", "-f", objectFile, "-o", "yaml")
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return "", errs, false, runErr
	}
	return out, errs, runErr == nil, nil
}

// CanI is `kubectl auth can-i --as=<subject> <verb> <resource>[/<name>] [--subresource=X] [-n ns]`.
func (k *Kubectl) CanI(ctx context.Context, c RBACCheck) (verdict string, err error) {
	args := []string{"auth", "can-i", "--as=" + c.Subject, c.Verb, c.Resource}
	if c.Name != "" {
		args[len(args)-1] = c.Resource + "/" + c.Name
	}
	if c.Subresource != "" {
		args = append(args, "--subresource="+c.Subresource)
	}
	if c.Namespace != "" {
		args = append(args, "-n", c.Namespace)
	}
	out, errs, runErr := k.Run(ctx, "", args...)
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return "", fmt.Errorf("%v: %s", runErr, errs)
	}
	return strings.TrimSpace(out), nil
}
