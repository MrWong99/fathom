// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package rbac

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apiserver/pkg/authorization/authorizer"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/oracle"
)

func label(c oracle.RBACCheck) string {
	s := c.Verb + " " + c.Resource
	if c.Subresource != "" {
		s += " subresource=" + c.Subresource
	}
	if c.Name != "" {
		s += " name=" + c.Name
	}
	if c.Namespace != "" {
		s += " ns=" + c.Namespace
	}
	return s
}

func request(c oracle.RBACCheck) Request {
	return Request{Subject: c.Subject, Verb: c.Verb, Group: oracle.GroupFor(c.Resource), Resource: c.Resource,
		Subresource: c.Subresource, Name: c.Name, Namespace: c.Namespace}
}

// TestCanIGoldens answers every rbacChecks entry from the snapshot and
// compares with kubectl's recorded verdict.
func TestCanIGoldens(t *testing.T) {
	t.Parallel()
	m, err := oracle.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewFromSnapshot(filepath.Join(oracle.Testdata(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range m.RBACChecks {
		c := c
		t.Run(fmt.Sprintf("%02d %s", i, label(c)), func(t *testing.T) {
			t.Parallel()
			got, reason, err := a.CanI(context.Background(), request(c))
			if err != nil {
				t.Fatal(err)
			}
			want := c.KubectlVerdict == "yes"
			if want != c.Allowed {
				t.Fatalf("manifest inconsistent: verdict %q allowed %v", c.KubectlVerdict, c.Allowed)
			}
			t.Logf("kubectl=%s offline=%v %s", c.KubectlVerdict, got, reason)
			if got != want {
				t.Errorf("kubectl said %s, offline %v (%s)", c.KubectlVerdict, got, reason)
			}
		})
	}
}

// TestCanILive replays every check against the kind oracle (skipped when
// unreachable) and compares kubectl's live verdict with the offline one.
func TestCanILive(t *testing.T) {
	t.Parallel()
	m, err := oracle.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	k := oracle.NewKubectl(m)
	if !k.Reachable() {
		t.Skipf("kind oracle context %q not reachable; skipping live replay", k.Context)
	}
	a, err := NewFromSnapshot(filepath.Join(oracle.Testdata(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range m.RBACChecks {
		c := c
		t.Run(fmt.Sprintf("%02d %s", i, label(c)), func(t *testing.T) {
			t.Parallel()
			live, err := k.CanI(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := a.CanI(context.Background(), request(c))
			if err != nil {
				t.Fatal(err)
			}
			if (live == "yes") != got {
				t.Errorf("live kubectl %q, offline %v", live, got)
			}
		})
	}
}

func TestUserInfo(t *testing.T) {
	t.Parallel()
	u := UserInfo("system:serviceaccount:s4:deployer", nil)
	want := []string{"system:serviceaccounts", "system:serviceaccounts:s4", "system:authenticated"}
	if fmt.Sprint(u.GetGroups()) != fmt.Sprint(want) {
		t.Fatalf("groups %v, want %v", u.GetGroups(), want)
	}
	if g := UserInfo("alice", []string{"dev"}).GetGroups(); fmt.Sprint(g) != "[dev system:authenticated]" {
		t.Fatalf("user groups %v", g)
	}
}

// TestRuleAllows pins the matcher edge cases the fixture does not reach.
func TestRuleAllows(t *testing.T) {
	t.Parallel()
	attrs := func(verb, group, res, sub, name string) authorizer.Attributes {
		return authorizer.AttributesRecord{Verb: verb, APIGroup: group, Resource: res, Subresource: sub, Name: name, ResourceRequest: true}
	}
	cases := []struct {
		name string
		rule rbacv1.PolicyRule
		a    authorizer.Attributes
		want bool
	}{
		{"*/scale matches any scale", rbacv1.PolicyRule{Verbs: []string{"update"}, APIGroups: []string{"*"}, Resources: []string{"*/scale"}}, attrs("update", "apps", "deployments", "scale", ""), true},
		{"deployments does not match deployments/scale", rbacv1.PolicyRule{Verbs: []string{"update"}, APIGroups: []string{"apps"}, Resources: []string{"deployments"}}, attrs("update", "apps", "deployments", "scale", ""), false},
		{"resourceNames never match an unnamed request", rbacv1.PolicyRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{"a"}}, attrs("get", "", "configmaps", "", ""), false},
		{"group mismatch", rbacv1.PolicyRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"deployments"}}, attrs("get", "apps", "deployments", "", ""), false},
		{"non-resource URL prefix", rbacv1.PolicyRule{Verbs: []string{"get"}, NonResourceURLs: []string{"/healthz/*"}}, authorizer.AttributesRecord{Verb: "get", Path: "/healthz/ping"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RulesAllow(tc.a, tc.rule); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBindingWithoutRoleIsNotFatal(t *testing.T) {
	t.Parallel()
	crb := &rbacv1.ClusterRoleBinding{RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "missing"},
		Subjects: []rbacv1.Subject{{Kind: "Group", Name: "system:authenticated"}}}
	cr := &rbacv1.ClusterRole{Rules: []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"nodes"}}}}
	cr.Name = "view-nodes"
	crb2 := &rbacv1.ClusterRoleBinding{RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "view-nodes"},
		Subjects: []rbacv1.Subject{{Kind: "Group", Name: "system:authenticated"}}}
	res, _ := NewTestRuleResolver(nil, nil, []*rbacv1.ClusterRole{cr}, []*rbacv1.ClusterRoleBinding{crb, crb2})
	rules, err := res.RulesFor(context.Background(), UserInfo("alice", nil), "")
	if err == nil || len(rules) != 1 {
		t.Fatalf("want one rule and a resolution error, got %d rules, err %v", len(rules), err)
	}
}
