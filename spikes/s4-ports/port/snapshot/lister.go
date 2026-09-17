// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package snapshot

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

// ListerForObjects returns a quota.ListerForResourceFunc over a fixed set of
// objects (the snapshot's `full` slice), so the ported evaluators' UsageStats
// and quota.CalculateUsage can recompute a namespace's usage the way the
// quota controller does, without an informer.
func ListerForObjects(objs []runtime.Object) quota.ListerForResourceFunc {
	byGVR := map[schema.GroupVersionResource][]runtime.Object{}
	for _, o := range objs {
		gvks, _, err := scheme.Scheme.ObjectKinds(o)
		if err != nil || len(gvks) == 0 {
			continue
		}
		gvr, _ := meta.UnsafeGuessKindToResource(gvks[0])
		byGVR[gvr] = append(byGVR[gvr], o)
	}
	return func(gvr schema.GroupVersionResource) (cache.GenericLister, error) {
		return &sliceLister{gvr: gvr, items: byGVR[gvr]}, nil
	}
}

type sliceLister struct {
	gvr       schema.GroupVersionResource
	namespace string
	items     []runtime.Object
}

func (l *sliceLister) List(selector labels.Selector) ([]runtime.Object, error) {
	var out []runtime.Object
	for _, o := range l.items {
		a, err := meta.Accessor(o)
		if err != nil {
			return nil, err
		}
		if l.namespace != "" && a.GetNamespace() != l.namespace {
			continue
		}
		if selector != nil && !selector.Matches(labels.Set(a.GetLabels())) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (l *sliceLister) Get(name string) (runtime.Object, error) {
	for _, o := range l.items {
		a, err := meta.Accessor(o)
		if err != nil {
			return nil, err
		}
		if (l.namespace == "" || a.GetNamespace() == l.namespace) && a.GetName() == name {
			return o, nil
		}
	}
	return nil, errors.NewNotFound(l.gvr.GroupResource(), fmt.Sprintf("%s/%s", l.namespace, name))
}

func (l *sliceLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return &sliceLister{gvr: l.gvr, namespace: namespace, items: l.items}
}
