// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestRenderKubectlBranches pins the RenderKubectl branches the oracle
// goldens do not reach (multiple causes, duplicate causes, nil Details,
// empty Kind/Name, non-Invalid reason, non-Status error). These are
// transcribed from kubectl v1.36 checkErr/statusCausesToAggrError/
// MultilineError and are self-consistency checks only: no server output was
// captured for them.
func TestRenderKubectlBranches(t *testing.T) {
	t.Parallel()
	req := &RequestObject{Verb: "apply", Source: "objects/x.yaml", Object: &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "s2"},
	}}
	gk := schema.GroupKind{Group: "apps", Kind: "deployments"}
	invalid := func(causes ...metav1.StatusCause) error {
		st := &apierrors.StatusError{ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    422,
			Reason:  metav1.StatusReasonInvalid,
			Message: `error when creating "objects/x.yaml": Deployment.apps "x" is invalid`,
			Details: &metav1.StatusDetails{Group: gk.Group, Kind: gk.Kind, Name: "x", Causes: causes},
		}}
		return st
	}
	cases := []struct {
		name     string
		res      Result
		wantOut  string
		wantExit int
	}{
		{
			name:     "admitted with warnings",
			res:      Result{Warnings: []string{"w1", "w2"}},
			wantOut:  "Warning: w1\nWarning: w2\ndeployment.apps/x created (server dry run)\n",
			wantExit: 0,
		},
		{
			name:     "invalid one cause",
			res:      Result{Err: invalid(metav1.StatusCause{Field: "", Message: "denied"})},
			wantOut:  "The deployments \"x\" is invalid: : denied\n",
			wantExit: 1,
		},
		{
			name: "invalid several causes with a duplicate",
			res: Result{Err: invalid(
				metav1.StatusCause{Field: "spec.a", Message: "one"},
				metav1.StatusCause{Field: "spec.b", Message: "two"},
				metav1.StatusCause{Field: "spec.a", Message: "one"},
			)},
			wantOut:  "The deployments \"x\" is invalid: \n* spec.a: one\n* spec.b: two\n",
			wantExit: 1,
		},
		{
			name: "invalid duplicates collapse to one line",
			res: Result{Err: invalid(
				metav1.StatusCause{Field: "spec.a", Message: "one"},
				metav1.StatusCause{Field: "spec.a", Message: "one"},
			)},
			wantOut:  "The deployments \"x\" is invalid: spec.a: one\n",
			wantExit: 1,
		},
		{
			name:     "invalid no causes",
			res:      Result{Err: invalid()},
			wantOut:  "The deployments \"x\" is invalid\n",
			wantExit: 1,
		},
		{
			name: "invalid nil details uses message",
			res: Result{Err: &apierrors.StatusError{ErrStatus: metav1.Status{
				Status: metav1.StatusFailure, Code: 422, Reason: metav1.StatusReasonInvalid, Message: "bad body",
			}}},
			wantOut:  "The request is invalid: bad body\n",
			wantExit: 1,
		},
		{
			name: "invalid details without kind or name",
			res: Result{Err: &apierrors.StatusError{ErrStatus: metav1.Status{
				Status: metav1.StatusFailure, Code: 422, Reason: metav1.StatusReasonInvalid, Message: "ignored",
				Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Field: "f", Message: "m"}}},
			}}},
			wantOut:  "The request is invalid: f: m\n",
			wantExit: 1,
		},
		{
			name: "forbidden",
			res: Result{Err: apierrors.NewForbidden(
				schema.GroupResource{Group: "apps", Resource: "deployments"}, "x", errors.New("policy said no"))},
			wantOut:  "Error from server (Forbidden): error when creating \"objects/x.yaml\": deployments.apps \"x\" is forbidden: policy said no\n",
			wantExit: 1,
		},
		{
			name:     "plain error",
			res:      Result{Err: errors.New("boom")},
			wantOut:  "error: boom\n",
			wantExit: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, exit := RenderKubectl(req, tc.res)
			if out != tc.wantOut || exit != tc.wantExit {
				t.Errorf("got (%q, %d), want (%q, %d)", out, exit, tc.wantOut, tc.wantExit)
			}
		})
	}
}
