// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package rbac

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

// Request is one `kubectl auth can-i` question: the subject and the resource
// attributes of a SelfSubjectAccessReview. Namespace "" is a cluster-scoped
// request (kubectl itself sends the kubeconfig namespace for those; RBAC
// only reads it to pick RoleBindings, so the verdict is the same).
type Request struct {
	Subject     string // system:serviceaccount:<ns>:<name>, or any user name
	Groups      []string
	Verb        string
	Group       string
	Resource    string
	Subresource string
	Name        string
	Namespace   string
}

// Authorizer answers Requests from the RBAC objects of a snapshot directory.
type Authorizer struct {
	rbac *RBACAuthorizer
}

// NewFromSnapshot reads every Role, ClusterRole, RoleBinding and
// ClusterRoleBinding from the YAML files under <dir>/rbac (single objects,
// multi-document files or `kind: List`, as `kubectl get ... -o yaml` writes
// them). An aggregated ClusterRole is used as exported: the controller-manager
// has already materialised the aggregated rules into its .rules, so no
// aggregation runs offline.
func NewFromSnapshot(dir string) (*Authorizer, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "rbac"))
	if err != nil {
		return nil, err
	}
	var objs []runtime.Object
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && (filepath.Ext(e.Name()) == ".yaml" || filepath.Ext(e.Name()) == ".yml" || filepath.Ext(e.Name()) == ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		o, err := snapshot.LoadObjects(filepath.Join(dir, "rbac", n))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		objs = append(objs, o...)
	}
	return NewFromObjects(objs), nil
}

// NewFromObjects builds the authorizer from decoded objects; non-RBAC kinds are ignored.
func NewFromObjects(objs []runtime.Object) *Authorizer {
	var (
		roles []*rbacv1.Role
		rbs   []*rbacv1.RoleBinding
		crs   []*rbacv1.ClusterRole
		crbs  []*rbacv1.ClusterRoleBinding
	)
	for _, o := range objs {
		switch t := o.(type) {
		case *rbacv1.Role:
			roles = append(roles, t)
		case *rbacv1.RoleBinding:
			rbs = append(rbs, t)
		case *rbacv1.ClusterRole:
			crs = append(crs, t)
		case *rbacv1.ClusterRoleBinding:
			crbs = append(crbs, t)
		}
	}
	_, static := NewTestRuleResolver(roles, rbs, crs, crbs)
	return &Authorizer{rbac: New(static, static, static, static)}
}

// UserInfo builds the user.Info the apiserver's authenticator would attach:
// for a ServiceAccount subject the name plus the groups system:serviceaccounts,
// system:serviceaccounts:<ns> and system:authenticated; for any other name
// the given groups plus system:authenticated.
func UserInfo(subject string, groups []string) user.Info {
	all := append([]string{}, groups...)
	if ns, _, err := serviceaccount.SplitUsername(subject); err == nil {
		all = append(all, serviceaccount.MakeGroupNames(ns)...)
	}
	all = append(all, user.AllAuthenticated)
	return &user.DefaultInfo{Name: subject, Groups: all}
}

// CanI answers like `kubectl auth can-i`: true when some rule that binds to
// the subject allows the request; the reason names the binding that allowed it.
func (a *Authorizer) CanI(ctx context.Context, req Request) (bool, string, error) {
	attrs := authorizer.AttributesRecord{
		User:            UserInfo(req.Subject, req.Groups),
		Verb:            req.Verb,
		Namespace:       req.Namespace,
		APIGroup:        req.Group,
		Resource:        req.Resource,
		Subresource:     req.Subresource,
		Name:            req.Name,
		ResourceRequest: true,
	}
	decision, reason, err := a.rbac.Authorize(ctx, attrs)
	return decision == authorizer.DecisionAllow, reason, err
}
