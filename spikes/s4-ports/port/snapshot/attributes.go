// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package snapshot

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/kubernetes/scheme"
)

// CreateAttributes builds the admission.Attributes the apiserver would hand
// the admission chain for `kubectl apply --dry-run=server` of a new object:
// operation Create, no old object, no subresource, dryRun true. The resource
// is guessed from the kind the way the REST mapper does for built-in types.
func CreateAttributes(obj runtime.Object, userInfo user.Info) (admission.Attributes, error) {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return nil, fmt.Errorf("object kind: %w", err)
	}
	gvk := gvks[0]
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	return admission.NewAttributesRecord(obj, nil, gvk, accessor.GetNamespace(), accessor.GetName(), gvr, "", admission.Create, nil, true, userInfo), nil
}
