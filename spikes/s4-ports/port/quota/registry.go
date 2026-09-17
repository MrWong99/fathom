// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package quota

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/utils/clock"
)

// legacyObjectCountAliases are what we used to do simple object counting quota with mapped to alias
// (the map in upstream pkg/quota/v1/evaluator/core/registry.go at tag v1.37.0).
var legacyObjectCountAliases = map[schema.GroupVersionResource]corev1.ResourceName{
	corev1.SchemeGroupVersion.WithResource("configmaps"):             corev1.ResourceConfigMaps,
	corev1.SchemeGroupVersion.WithResource("resourcequotas"):         corev1.ResourceQuotas,
	corev1.SchemeGroupVersion.WithResource("replicationcontrollers"): corev1.ResourceReplicationControllers,
	corev1.SchemeGroupVersion.WithResource("secrets"):                corev1.ResourceSecrets,
}

// NewEvaluators is fathom's replacement for upstream NewEvaluators: the three
// ported evaluators plus the legacy object-count aliases. The ResourceClaim
// evaluator, its DRA feature gates and the device-class informer cache are
// left out (no DRA in the MVP); count/<resource>.<group> evaluators for
// other kinds are added by the caller with generic.NewObjectCountEvaluator.
func NewEvaluators(f quota.ListerForResourceFunc, clock clock.Clock) []quota.Evaluator {
	result := []quota.Evaluator{
		NewPodEvaluator(f, clock),
		NewServiceEvaluator(f),
		NewPersistentVolumeClaimEvaluator(f),
	}
	for gvr, alias := range legacyObjectCountAliases {
		result = append(result,
			generic.NewObjectCountEvaluator(gvr.GroupResource(), generic.ListResourceUsingListerFunc(f, gvr), alias))
	}
	return result
}

// NewRegistry wraps NewEvaluators in the staging registry.
func NewRegistry(f quota.ListerForResourceFunc, clock clock.Clock) quota.Registry {
	return generic.NewRegistry(NewEvaluators(f, clock))
}
