/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// fathom port of k8s.io/kubernetes/pkg/registry/rbac/validation/rule.go at tag
// v1.37.0: the AuthorizationRuleResolver interface, DefaultRuleResolver,
// VisitRulesFor, GetRoleReferenceRules, appliesTo and the StaticRoles resolver
// (upstream's NewTestRuleResolver) which is the production resolver here: it
// resolves from the lists of role objects the snapshot holds. Dropped, marked
// "fathom:": ConfirmNoEscalation and CompactRules (escalation checks on RBAC
// writes; fathom never writes RBAC to a cluster) with their klog and
// rbacv1helpers.CompactString imports. Everything kept is upstream text.

package rbac

import (
	"context"
	"errors"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
)

type AuthorizationRuleResolver interface {
	// GetRoleReferenceRules attempts to resolve the role reference of a RoleBinding or ClusterRoleBinding.  The passed namespace should be the namespace
	// of the role binding, the empty string if a cluster role binding.
	GetRoleReferenceRules(ctx context.Context, roleRef rbacv1.RoleRef, namespace string) ([]rbacv1.PolicyRule, error)

	// RulesFor returns the list of rules that apply to a given user in a given namespace and error.  If an error is returned, the slice of
	// PolicyRules may not be complete, but it contains all retrievable rules.  This is done because policy rules are purely additive and policy determinations
	// can be made on the basis of those rules that are found.
	RulesFor(ctx context.Context, user user.Info, namespace string) ([]rbacv1.PolicyRule, error)

	// VisitRulesFor invokes visitor() with each rule that applies to a given user in a given namespace, and each error encountered resolving those rules.
	// If visitor() returns false, visiting is short-circuited.
	VisitRulesFor(ctx context.Context, user user.Info, namespace string, visitor func(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool)
}

// fathom: ConfirmNoEscalation dropped.

type DefaultRuleResolver struct {
	roleGetter               RoleGetter
	roleBindingLister        RoleBindingLister
	clusterRoleGetter        ClusterRoleGetter
	clusterRoleBindingLister ClusterRoleBindingLister
}

func NewDefaultRuleResolver(roleGetter RoleGetter, roleBindingLister RoleBindingLister, clusterRoleGetter ClusterRoleGetter, clusterRoleBindingLister ClusterRoleBindingLister) *DefaultRuleResolver {
	return &DefaultRuleResolver{roleGetter, roleBindingLister, clusterRoleGetter, clusterRoleBindingLister}
}

type RoleGetter interface {
	GetRole(ctx context.Context, namespace, name string) (*rbacv1.Role, error)
}

type RoleBindingLister interface {
	ListRoleBindings(ctx context.Context, namespace string) ([]*rbacv1.RoleBinding, error)
}

type ClusterRoleGetter interface {
	GetClusterRole(ctx context.Context, name string) (*rbacv1.ClusterRole, error)
}

type ClusterRoleBindingLister interface {
	ListClusterRoleBindings(ctx context.Context) ([]*rbacv1.ClusterRoleBinding, error)
}

func (r *DefaultRuleResolver) RulesFor(ctx context.Context, user user.Info, namespace string) ([]rbacv1.PolicyRule, error) {
	visitor := &ruleAccumulator{}
	r.VisitRulesFor(ctx, user, namespace, visitor.visit)
	return visitor.rules, utilerrors.NewAggregate(visitor.errors)
}

type ruleAccumulator struct {
	rules  []rbacv1.PolicyRule
	errors []error
}

func (r *ruleAccumulator) visit(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool {
	if rule != nil {
		r.rules = append(r.rules, *rule)
	}
	if err != nil {
		r.errors = append(r.errors, err)
	}
	return true
}

func describeSubject(s *rbacv1.Subject, bindingNamespace string) string {
	switch s.Kind {
	case rbacv1.ServiceAccountKind:
		if len(s.Namespace) > 0 {
			return fmt.Sprintf("%s %q", s.Kind, s.Name+"/"+s.Namespace)
		}
		return fmt.Sprintf("%s %q", s.Kind, s.Name+"/"+bindingNamespace)
	default:
		return fmt.Sprintf("%s %q", s.Kind, s.Name)
	}
}

type clusterRoleBindingDescriber struct {
	binding *rbacv1.ClusterRoleBinding
	subject *rbacv1.Subject
}

func (d *clusterRoleBindingDescriber) String() string {
	return fmt.Sprintf("ClusterRoleBinding %q of %s %q to %s",
		d.binding.Name,
		d.binding.RoleRef.Kind,
		d.binding.RoleRef.Name,
		describeSubject(d.subject, ""),
	)
}

type roleBindingDescriber struct {
	binding *rbacv1.RoleBinding
	subject *rbacv1.Subject
}

func (d *roleBindingDescriber) String() string {
	return fmt.Sprintf("RoleBinding %q of %s %q to %s",
		d.binding.Name+"/"+d.binding.Namespace,
		d.binding.RoleRef.Kind,
		d.binding.RoleRef.Name,
		describeSubject(d.subject, d.binding.Namespace),
	)
}

func (r *DefaultRuleResolver) VisitRulesFor(ctx context.Context, user user.Info, namespace string, visitor func(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool) {
	if clusterRoleBindings, err := r.clusterRoleBindingLister.ListClusterRoleBindings(ctx); err != nil {
		if !visitor(nil, nil, err) {
			return
		}
	} else {
		sourceDescriber := &clusterRoleBindingDescriber{}
		for _, clusterRoleBinding := range clusterRoleBindings {
			subjectIndex, applies := appliesTo(user, clusterRoleBinding.Subjects, "")
			if !applies {
				continue
			}
			rules, err := r.GetRoleReferenceRules(ctx, clusterRoleBinding.RoleRef, "")
			if err != nil {
				if !visitor(nil, nil, err) {
					return
				}
				continue
			}
			sourceDescriber.binding = clusterRoleBinding
			sourceDescriber.subject = &clusterRoleBinding.Subjects[subjectIndex]
			for i := range rules {
				if !visitor(sourceDescriber, &rules[i], nil) {
					return
				}
			}
		}
	}

	if len(namespace) > 0 {
		if roleBindings, err := r.roleBindingLister.ListRoleBindings(ctx, namespace); err != nil {
			if !visitor(nil, nil, err) {
				return
			}
		} else {
			sourceDescriber := &roleBindingDescriber{}
			for _, roleBinding := range roleBindings {
				subjectIndex, applies := appliesTo(user, roleBinding.Subjects, namespace)
				if !applies {
					continue
				}
				rules, err := r.GetRoleReferenceRules(ctx, roleBinding.RoleRef, namespace)
				if err != nil {
					if !visitor(nil, nil, err) {
						return
					}
					continue
				}
				sourceDescriber.binding = roleBinding
				sourceDescriber.subject = &roleBinding.Subjects[subjectIndex]
				for i := range rules {
					if !visitor(sourceDescriber, &rules[i], nil) {
						return
					}
				}
			}
		}
	}
}

// GetRoleReferenceRules attempts to resolve the RoleBinding or ClusterRoleBinding.
func (r *DefaultRuleResolver) GetRoleReferenceRules(ctx context.Context, roleRef rbacv1.RoleRef, bindingNamespace string) ([]rbacv1.PolicyRule, error) {
	switch roleRef.Kind {
	case "Role":
		role, err := r.roleGetter.GetRole(ctx, bindingNamespace, roleRef.Name)
		if err != nil {
			return nil, err
		}
		return role.Rules, nil

	case "ClusterRole":
		clusterRole, err := r.clusterRoleGetter.GetClusterRole(ctx, roleRef.Name)
		if err != nil {
			return nil, err
		}
		return clusterRole.Rules, nil

	default:
		return nil, fmt.Errorf("unsupported role reference kind: %q", roleRef.Kind)
	}
}

// appliesTo returns whether any of the bindingSubjects applies to the specified subject,
// and if true, the index of the first subject that applies
func appliesTo(user user.Info, bindingSubjects []rbacv1.Subject, namespace string) (int, bool) {
	for i, bindingSubject := range bindingSubjects {
		if appliesToUser(user, bindingSubject, namespace) {
			return i, true
		}
	}
	return 0, false
}

func has(set []string, ele string) bool {
	for _, s := range set {
		if s == ele {
			return true
		}
	}
	return false
}

func appliesToUser(user user.Info, subject rbacv1.Subject, namespace string) bool {
	switch subject.Kind {
	case rbacv1.UserKind:
		return user.GetName() == subject.Name

	case rbacv1.GroupKind:
		return has(user.GetGroups(), subject.Name)

	case rbacv1.ServiceAccountKind:
		// default the namespace to namespace we're working in if its available.  This allows rolebindings that reference
		// SAs in th local namespace to avoid having to qualify them.
		saNamespace := namespace
		if len(subject.Namespace) > 0 {
			saNamespace = subject.Namespace
		}
		if len(saNamespace) == 0 {
			return false
		}
		// use a more efficient comparison for RBAC checking
		return serviceaccount.MatchesUsername(saNamespace, subject.Name, user.GetName())
	default:
		return false
	}
}

// NewTestRuleResolver returns a rule resolver from lists of role objects.
// fathom: this is the production resolver over the snapshot's role objects.
func NewTestRuleResolver(roles []*rbacv1.Role, roleBindings []*rbacv1.RoleBinding, clusterRoles []*rbacv1.ClusterRole, clusterRoleBindings []*rbacv1.ClusterRoleBinding) (AuthorizationRuleResolver, *StaticRoles) {
	r := StaticRoles{
		roles:               roles,
		roleBindings:        roleBindings,
		clusterRoles:        clusterRoles,
		clusterRoleBindings: clusterRoleBindings,
	}
	return newMockRuleResolver(&r), &r
}

func newMockRuleResolver(r *StaticRoles) AuthorizationRuleResolver {
	return NewDefaultRuleResolver(r, r, r, r)
}

// StaticRoles is a rule resolver that resolves from lists of role objects.
type StaticRoles struct {
	roles               []*rbacv1.Role
	roleBindings        []*rbacv1.RoleBinding
	clusterRoles        []*rbacv1.ClusterRole
	clusterRoleBindings []*rbacv1.ClusterRoleBinding
}

func (r *StaticRoles) GetRole(ctx context.Context, namespace, name string) (*rbacv1.Role, error) {
	if len(namespace) == 0 {
		return nil, errors.New("must provide namespace when getting role")
	}
	for _, role := range r.roles {
		if role.Namespace == namespace && role.Name == name {
			return role, nil
		}
	}
	return nil, errors.New("role not found")
}

func (r *StaticRoles) GetClusterRole(ctx context.Context, name string) (*rbacv1.ClusterRole, error) {
	for _, clusterRole := range r.clusterRoles {
		if clusterRole.Name == name {
			return clusterRole, nil
		}
	}
	return nil, errors.New("clusterrole not found")
}

func (r *StaticRoles) ListRoleBindings(ctx context.Context, namespace string) ([]*rbacv1.RoleBinding, error) {
	if len(namespace) == 0 {
		return nil, errors.New("must provide namespace when listing role bindings")
	}

	roleBindingList := []*rbacv1.RoleBinding{}
	for _, roleBinding := range r.roleBindings {
		if roleBinding.Namespace != namespace {
			continue
		}
		// TODO(ericchiang): need to implement label selectors?
		roleBindingList = append(roleBindingList, roleBinding)
	}
	return roleBindingList, nil
}

func (r *StaticRoles) ListClusterRoleBindings(ctx context.Context) ([]*rbacv1.ClusterRoleBinding, error) {
	return r.clusterRoleBindings, nil
}
