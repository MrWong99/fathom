// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package quota

import (
	"errors"
	"path/filepath"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/resourcequota"
	quota "k8s.io/apiserver/pkg/quota/v1"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

// Check is the admission-time ResourceQuota check over snapshot quotas: for
// every quota of the object's namespace it computes status.used + usage(obj)
// against status.hard, honouring scopes and scopeSelector through the ported
// evaluators' Matches/MatchingScopes, and returns the quotas with the usage
// the request would leave behind, or the server's Forbidden error.
//
// The arithmetic and the error text are not re-implemented: this calls the
// staging plugin's exported resourcequota.CheckRequest
// (k8s.io/apiserver/pkg/admission/plugin/resourcequota/controller.go), which
// is the function the apiserver runs per request, with no LimitedResources
// configuration. That is what phrases
// "exceeded quota: <name>, requested: <r>, used: <u>, limited: <l>" with the
// resource names sorted and rendered by Quantity.String; admission.NewForbidden
// then wraps it as `<resource> "<name>" is forbidden: ...`.
//
// The quotas are checked in order and the first exceeded one is reported,
// like the server. The server's order is the informer cache's map order (not
// deterministic: two samples of eight live dry-runs of q-scope-high in phase B
// split s4-quota to s4-quota-high 6:2 and then 7:1); fathom sorts by name so that
// two runs report the same quota (design: Validate is a pure function) and
// CheckAll reports every exceeded quota.
func Check(quotas []corev1.ResourceQuota, a admission.Attributes, registry quota.Registry) ([]corev1.ResourceQuota, error) {
	evaluator := registry.Get(a.GetResource().GroupResource())
	if evaluator == nil {
		return quotas, nil
	}
	// the server only considers quotas of the request's namespace
	var ns []corev1.ResourceQuota
	for _, q := range quotas {
		if q.Namespace == a.GetNamespace() {
			ns = append(ns, q)
		}
	}
	if len(ns) == 0 {
		return quotas, nil
	}
	sort.SliceStable(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
	return resourcequota.CheckRequest(ns, a, evaluator, nil)
}

// CheckAll evaluates every quota of the namespace on its own and returns one
// error per exceeded quota, sorted by quota name. The server stops at the
// first exceeded quota in its (non-deterministic) order, so its message is
// always one of these; fathom reports all of them (design 3.3 V: one finding
// per quota).
func CheckAll(quotas []corev1.ResourceQuota, a admission.Attributes, registry quota.Registry) []error {
	evaluator := registry.Get(a.GetResource().GroupResource())
	if evaluator == nil {
		return nil
	}
	var ns []corev1.ResourceQuota
	for _, q := range quotas {
		if q.Namespace == a.GetNamespace() {
			ns = append(ns, q)
		}
	}
	sort.SliceStable(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
	var errs []error
	for i := range ns {
		if _, err := resourcequota.CheckRequest(ns[i:i+1], a, evaluator, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// LoadQuotas reads the namespace's ResourceQuotas with their status from
// <dir>/<file> (default quota-status.yaml). The status is the quota
// controller's last write, which is what the server's admission compares
// against; a quota without status.used is rejected by CheckRequest as
// "status unknown", so a snapshot must carry the status subresource.
func LoadQuotas(dir, file string) ([]corev1.ResourceQuota, error) {
	if file == "" {
		file = "quota-status.yaml"
	}
	items, err := snapshot.Load[*corev1.ResourceQuota](filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, errors.New("no ResourceQuota in " + filepath.Join(dir, file))
	}
	out := make([]corev1.ResourceQuota, 0, len(items))
	for _, q := range items {
		out = append(out, *q)
	}
	return out, nil
}

// RecomputeUsage returns what the quota controller would write as status.used
// for q, from the namespace's objects in the snapshot's full slice (design
// 3.3: the old-revision subtraction needs this). It is quota.CalculateUsage
// over the ported evaluators; gvrs restricts the kinds counted so that
// resources with no objects in the snapshot (count/configmaps when configmaps
// were not exported) are left out. nil counts everything in the registry.
func RecomputeUsage(q *corev1.ResourceQuota, registry quota.Registry, gvrs []schema.GroupResource) (corev1.ResourceList, error) {
	hard := q.Status.Hard
	if len(hard) == 0 {
		hard = q.Spec.Hard
	}
	if gvrs != nil {
		filtered := quota.Registry(&filteredRegistry{inner: registry, keep: gvrs})
		return quota.CalculateUsage(q.Namespace, q.Spec.Scopes, hard, filtered, q.Spec.ScopeSelector)
	}
	return quota.CalculateUsage(q.Namespace, q.Spec.Scopes, hard, registry, q.Spec.ScopeSelector)
}

type filteredRegistry struct {
	inner quota.Registry
	keep  []schema.GroupResource
}

func (f *filteredRegistry) Add(e quota.Evaluator)    { f.inner.Add(e) }
func (f *filteredRegistry) Remove(e quota.Evaluator) { f.inner.Remove(e) }
func (f *filteredRegistry) Get(gr schema.GroupResource) quota.Evaluator {
	for _, k := range f.keep {
		if k == gr {
			return f.inner.Get(gr)
		}
	}
	return nil
}
func (f *filteredRegistry) List() []quota.Evaluator {
	var out []quota.Evaluator
	for _, e := range f.inner.List() {
		if f.Get(e.GroupResource()) != nil {
			out = append(out, e)
		}
	}
	return out
}
