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

// fathom port of k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac/rbac.go at
// tag v1.37.0: RulesAllow, RuleAllows, the authorizingVisitor and Authorize.
// Changes, each marked "fathom:": rbacv1helpers.* are the copies in
// evaluation_helpers.go; the klog V(5) denial log, the RulesFor adapter and
// the client-go lister adapters
// (RoleGetter, RoleBindingLister, ...) are dropped, the snapshot StaticRoles in
// rule.go is the only backend.

// Package rbac implements the authorizer.Authorizer interface using roles base access control.
package rbac

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

type RequestToRuleMapper interface {
	// RulesFor returns all known PolicyRules and any errors that happened while locating those rules.
	// Any rule returned is still valid, since rules are deny by default.  If you can pass with the rules
	// supplied, you do not have to fail the request.  If you cannot, you should indicate the error along
	// with your denial.
	RulesFor(ctx context.Context, subject user.Info, namespace string) ([]rbacv1.PolicyRule, error)

	// VisitRulesFor invokes visitor() with each rule that applies to a given user in a given namespace,
	// and each error encountered resolving those rules. Rule may be nil if err is non-nil.
	// If visitor() returns false, visiting is short-circuited.
	VisitRulesFor(ctx context.Context, user user.Info, namespace string, visitor func(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool)
}

var _ = authorizer.Authorizer(&RBACAuthorizer{})

type RBACAuthorizer struct {
	authorizationRuleResolver RequestToRuleMapper
}

// authorizingVisitor short-circuits once allowed, and collects any resolution errors encountered
type authorizingVisitor struct {
	requestAttributes authorizer.Attributes

	allowed bool
	reason  string
	errors  []error
}

func (v *authorizingVisitor) visit(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool {
	if rule != nil && RuleAllows(v.requestAttributes, rule) {
		v.allowed = true
		v.reason = fmt.Sprintf("RBAC: allowed by %s", source.String())
		return false
	}
	if err != nil {
		v.errors = append(v.errors, err)
	}
	return true
}

func (r *RBACAuthorizer) Authorize(ctx context.Context, requestAttributes authorizer.Attributes) (authorizer.Decision, string, error) {
	ruleCheckingVisitor := &authorizingVisitor{requestAttributes: requestAttributes}

	r.authorizationRuleResolver.VisitRulesFor(ctx, requestAttributes.GetUser(), requestAttributes.GetNamespace(), ruleCheckingVisitor.visit)
	if ruleCheckingVisitor.allowed {
		return authorizer.DecisionAllow, ruleCheckingVisitor.reason, nil
	}

	// fathom: the klog.V(5) denial log block is dropped.

	reason := ""
	if len(ruleCheckingVisitor.errors) > 0 {
		reason = fmt.Sprintf("RBAC: %v", utilerrors.NewAggregate(ruleCheckingVisitor.errors))
	}
	return authorizer.DecisionNoOpinion, reason, nil
}

// ConditionsAwareAuthorize is not conditions-aware, converts the Authorize decision.
func (r *RBACAuthorizer) ConditionsAwareAuthorize(ctx context.Context, requestAttributes authorizer.Attributes) authorizer.ConditionsAwareDecision {
	return authorizer.ConditionsAwareDecisionFromParts(r.Authorize(ctx, requestAttributes))
}

// EvaluateConditions is not supported by this authorizer.
func (*RBACAuthorizer) EvaluateConditions(_ context.Context, _ authorizer.ConditionsAwareDecision, _ authorizer.ConditionsData) (authorizer.Decision, string, error) {
	return authorizer.DecisionDeny, "", authorizer.ErrorConditionEvaluationNotSupported
}

// fathom: New takes the four snapshot-backed interfaces of rule.go directly.
func New(roles RoleGetter, roleBindings RoleBindingLister, clusterRoles ClusterRoleGetter, clusterRoleBindings ClusterRoleBindingLister) *RBACAuthorizer {
	authorizer := &RBACAuthorizer{
		authorizationRuleResolver: NewDefaultRuleResolver(
			roles, roleBindings, clusterRoles, clusterRoleBindings,
		),
	}
	return authorizer
}

func RulesAllow(requestAttributes authorizer.Attributes, rules ...rbacv1.PolicyRule) bool {
	for i := range rules {
		if RuleAllows(requestAttributes, &rules[i]) {
			return true
		}
	}

	return false
}

func RuleAllows(requestAttributes authorizer.Attributes, rule *rbacv1.PolicyRule) bool {
	if requestAttributes.IsResourceRequest() {
		combinedResource := requestAttributes.GetResource()
		if len(requestAttributes.GetSubresource()) > 0 {
			combinedResource = requestAttributes.GetResource() + "/" + requestAttributes.GetSubresource()
		}

		return VerbMatches(rule, requestAttributes.GetVerb()) &&
			APIGroupMatches(rule, requestAttributes.GetAPIGroup()) &&
			ResourceMatches(rule, combinedResource, requestAttributes.GetSubresource()) &&
			ResourceNameMatches(rule, requestAttributes.GetName())
	}

	return VerbMatches(rule, requestAttributes.GetVerb()) &&
		NonResourceURLMatches(rule, requestAttributes.GetPath())
}
