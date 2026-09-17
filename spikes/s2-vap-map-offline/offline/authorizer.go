// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// AuthzCheck is one question the policies asked the authorizer and the answer
// it got, in SubjectAccessReview terms.
type AuthzCheck struct {
	User      string
	Groups    []string
	Verb      string
	APIGroup  string
	Resource  string
	Namespace string
	Name      string
	Decision  authorizer.Decision
}

func (c AuthzCheck) String() string {
	return fmt.Sprintf("%s %v: %s %s/%s ns=%q name=%q -> %s", c.User, c.Groups, c.Verb, c.APIGroup, c.Resource, c.Namespace, c.Name, decisionString(c.Decision))
}

func decisionString(d authorizer.Decision) string {
	switch d {
	case authorizer.DecisionAllow:
		return "Allow"
	case authorizer.DecisionDeny:
		return "Deny"
	default:
		return "NoOpinion"
	}
}

// SnapshotAuthorizer answers the `authorizer` CEL variable from the snapshot.
//
// The snapshot of this spike carries no RBAC objects, only the request
// identity (whoami.yaml: kubernetes-admin in group kubeadm:cluster-admins).
// kind (kubeadm) binds that group to the cluster-admin ClusterRole through the
// ClusterRoleBinding kubeadm:cluster-admins, and system:masters is allowed
// unconditionally by the apiserver, so members of either group get Allow for
// everything; every other subject gets NoOpinion (the RBAC port that reads
// Role/ClusterRole/Binding objects from a snapshot is planned under
// internal/admit/port and is not part of S2). Every check is recorded.
type SnapshotAuthorizer struct {
	ClusterAdminGroups []string

	mu     sync.Mutex
	checks []AuthzCheck
}

var _ authorizer.UnconditionalAuthorizer = &SnapshotAuthorizer{}

// NewSnapshotAuthorizer returns the authorizer with kind's admin groups.
func NewSnapshotAuthorizer() *SnapshotAuthorizer {
	return &SnapshotAuthorizer{ClusterAdminGroups: []string{"kubeadm:cluster-admins", "system:masters"}}
}

// Authorize implements authorizer.UnconditionalAuthorizer.
func (s *SnapshotAuthorizer) Authorize(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
	decision := authorizer.DecisionNoOpinion
	reason := "no RBAC objects in snapshot"
	var groups []string
	if u := a.GetUser(); u != nil {
		groups = u.GetGroups()
		for _, g := range groups {
			if slices.Contains(s.ClusterAdminGroups, g) {
				decision = authorizer.DecisionAllow
				reason = "group " + g + " is bound to cluster-admin"
				break
			}
		}
	}
	c := AuthzCheck{
		Verb: a.GetVerb(), APIGroup: a.GetAPIGroup(), Resource: a.GetResource(),
		Namespace: a.GetNamespace(), Name: a.GetName(), Decision: decision, Groups: groups,
	}
	if u := a.GetUser(); u != nil {
		c.User = u.GetName()
	}
	s.mu.Lock()
	s.checks = append(s.checks, c)
	s.mu.Unlock()
	return decision, reason, nil
}

// Checks returns a copy of every recorded authorization question.
func (s *SnapshotAuthorizer) Checks() []AuthzCheck {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.checks)
}

// Reset clears the recorded questions.
func (s *SnapshotAuthorizer) Reset() {
	s.mu.Lock()
	s.checks = nil
	s.mu.Unlock()
}
