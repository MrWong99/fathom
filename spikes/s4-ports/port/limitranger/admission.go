/*
Copyright 2014 The Kubernetes Authors.

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

// fathom port of k8s.io/kubernetes/plugin/pkg/admission/limitranger/admission.go
// at tag v1.37.0. Changes against upstream, each marked "fathom:" below:
//   - k8s.io/kubernetes/pkg/apis/core (api.*) types are k8s.io/api/core/v1 (corev1.*);
//   - the informer/lister/client, live-lookup LRU and singleflight plumbing is
//     replaced by a RangeSource fed from the snapshot; Register, SetExternalKube*,
//     ValidateInitialization and GetLimitRanges are dropped;
//   - podRequests/podLimits keep upstream's signature, pod-level override loop
//     and supportedPodLevelResources set (cpu and memory only, upstream lines
//     607-617, 661-671, 677-689); their container aggregation (upstream's
//     addResourceList/maxResourceList sidecar formula, lines 561-606 and
//     640-660) is k8s.io/component-helpers/resource.PodRequests/PodLimits with
//     ExcludeOverhead (upstream's local copy ignores pod overhead),
//     UseStatusResources=false (upstream NOTE) and SkipPodLevelResources=true.
//     The override is not delegated because component-helpers'
//     IsSupportedPodLevelResource also accepts hugepages-*, which upstream's
//     local isSupportedPodLevelResource does not (S4 RESULT, drift found);
//   - feature.DefaultFeatureGate.Enabled(features.PodLevelResources) is the
//     constant gates.PodLevelResources.
// Everything else, including every error string, is upstream text in upstream order.

package limitranger

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/gates"
)

const (
	limitRangerAnnotation = "kubernetes.io/limit-ranger"
	// PluginName indicates name of admission plugin.
	PluginName = "LimitRanger"
)

// fathom: RangeSource replaces the informer lister plus live lookup. It returns
// the LimitRanges of a namespace as recorded in the snapshot.
type RangeSource func(namespace string) ([]*corev1.LimitRange, error)

// LimitRanger enforces usage limits on a per resource basis in the namespace
type LimitRanger struct {
	*admission.Handler
	actions LimitRangerActions
	ranges  RangeSource // fathom: was lister corev1listers.LimitRangeLister + client + liveLookupCache
}

var _ admission.MutationInterface = &LimitRanger{}
var _ admission.ValidationInterface = &LimitRanger{}

// Admit admits resources into cluster that do not violate any defined LimitRange in the namespace
func (l *LimitRanger) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) (err error) {
	return l.runLimitFunc(a, l.actions.MutateLimit)
}

// Validate admits resources into cluster that do not violate any defined LimitRange in the namespace
func (l *LimitRanger) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) (err error) {
	return l.runLimitFunc(a, l.actions.ValidateLimit)
}

func (l *LimitRanger) runLimitFunc(a admission.Attributes, limitFn func(limitRange *corev1.LimitRange, kind string, obj runtime.Object) error) (err error) {
	if !l.actions.SupportsAttributes(a) {
		return nil
	}

	// ignore all objects marked for deletion
	oldObj := a.GetOldObject()
	if oldObj != nil {
		oldAccessor, err := meta.Accessor(oldObj)
		if err != nil {
			return admission.NewForbidden(a, err)
		}
		if oldAccessor.GetDeletionTimestamp() != nil {
			return nil
		}
	}

	items, err := l.GetLimitRanges(a)
	if err != nil {
		return err
	}

	// ensure it meets each prescribed min/max
	for i := range items {
		limitRange := items[i]

		if !l.actions.SupportsLimit(limitRange) {
			continue
		}

		err = limitFn(limitRange, a.GetResource().Resource, a.GetObject())
		if err != nil {
			return admission.NewForbidden(a, err)
		}
	}
	return nil
}

// GetLimitRanges returns a LimitRange object with the items held in
// the indexer if available, or do alive lookup of the value.
// fathom: the indexer and the live lookup are the snapshot RangeSource.
func (l *LimitRanger) GetLimitRanges(a admission.Attributes) ([]*corev1.LimitRange, error) {
	items, err := l.ranges(a.GetNamespace())
	if err != nil {
		return nil, admission.NewForbidden(a, fmt.Errorf("unable to %s %v at this time because there was an error enforcing limit ranges", a.GetOperation(), a.GetResource()))
	}
	return items, nil
}

// NewLimitRanger returns an object that enforces limits based on the supplied limit function
// fathom: takes the RangeSource instead of being wired by the admission initializer.
func NewLimitRanger(actions LimitRangerActions, ranges RangeSource) (*LimitRanger, error) {
	if actions == nil {
		actions = &DefaultLimitRangerActions{}
	}

	return &LimitRanger{
		Handler: admission.NewHandler(admission.Create, admission.Update),
		actions: actions,
		ranges:  ranges,
	}, nil
}

// defaultContainerResourceRequirements returns the default requirements for a container
// the requirement.Limits are taken from the LimitRange defaults (if specified)
// the requirement.Requests are taken from the LimitRange default request (if specified)
func defaultContainerResourceRequirements(limitRange *corev1.LimitRange) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{}
	requirements.Requests = corev1.ResourceList{}
	requirements.Limits = corev1.ResourceList{}

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		if limit.Type == corev1.LimitTypeContainer {
			for k, v := range limit.DefaultRequest {
				requirements.Requests[corev1.ResourceName(k)] = v.DeepCopy()
			}
			for k, v := range limit.Default {
				requirements.Limits[corev1.ResourceName(k)] = v.DeepCopy()
			}
		}
	}
	return requirements
}

// mergeContainerResources handles defaulting all of the resources on a container.
func mergeContainerResources(container *corev1.Container, defaultRequirements *corev1.ResourceRequirements, annotationPrefix string, annotations []string) []string {
	setRequests := []string{}
	setLimits := []string{}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	for k, v := range defaultRequirements.Limits {
		_, found := container.Resources.Limits[k]
		if !found {
			container.Resources.Limits[k] = v.DeepCopy()
			setLimits = append(setLimits, string(k))
		}
	}
	for k, v := range defaultRequirements.Requests {
		_, found := container.Resources.Requests[k]
		if !found {
			container.Resources.Requests[k] = v.DeepCopy()
			setRequests = append(setRequests, string(k))
		}
	}
	if len(setRequests) > 0 {
		sort.Strings(setRequests)
		a := strings.Join(setRequests, ", ") + fmt.Sprintf(" request for %s %s", annotationPrefix, container.Name)
		annotations = append(annotations, a)
	}
	if len(setLimits) > 0 {
		sort.Strings(setLimits)
		a := strings.Join(setLimits, ", ") + fmt.Sprintf(" limit for %s %s", annotationPrefix, container.Name)
		annotations = append(annotations, a)
	}
	return annotations
}

// mergePodResourceRequirements merges enumerated requirements with default requirements
// it annotates the pod with information about what requirements were modified
func mergePodResourceRequirements(pod *corev1.Pod, defaultRequirements *corev1.ResourceRequirements) {
	annotations := []string{}

	for i := range pod.Spec.Containers {
		annotations = mergeContainerResources(&pod.Spec.Containers[i], defaultRequirements, "container", annotations)
	}

	for i := range pod.Spec.InitContainers {
		annotations = mergeContainerResources(&pod.Spec.InitContainers[i], defaultRequirements, "init container", annotations)
	}

	if len(annotations) > 0 {
		if pod.ObjectMeta.Annotations == nil {
			pod.ObjectMeta.Annotations = make(map[string]string)
		}
		val := "LimitRanger plugin set: " + strings.Join(annotations, "; ")
		pod.ObjectMeta.Annotations[limitRangerAnnotation] = val
	}
}

// requestLimitEnforcedValues returns the specified values at a common precision to support comparability
func requestLimitEnforcedValues(requestQuantity, limitQuantity, enforcedQuantity resource.Quantity) (request, limit, enforced int64) {
	request = requestQuantity.Value()
	limit = limitQuantity.Value()
	enforced = enforcedQuantity.Value()
	// do a more precise comparison if possible (if the value won't overflow)
	if request <= resource.MaxMilliValue && limit <= resource.MaxMilliValue && enforced <= resource.MaxMilliValue {
		request = requestQuantity.MilliValue()
		limit = limitQuantity.MilliValue()
		enforced = enforcedQuantity.MilliValue()
	}
	return
}

// minConstraint enforces the min constraint over the specified resource
func minConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if !reqExists {
		return fmt.Errorf("minimum %s usage per %s is %s.  No request is specified", resourceName, limitType, enforced.String())
	}
	if observedReqValue < enforcedValue {
		return fmt.Errorf("minimum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	if limExists && (observedLimValue < enforcedValue) {
		return fmt.Errorf("minimum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	return nil
}

// maxRequestConstraint enforces the max constraint over the specified resource
// use when specify LimitType resource doesn't recognize limit values
func maxRequestConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	observedReqValue, _, enforcedValue := requestLimitEnforcedValues(req, resource.Quantity{}, enforced)

	if !reqExists {
		return fmt.Errorf("maximum %s usage per %s is %s.  No request is specified", resourceName, limitType, enforced.String())
	}
	if observedReqValue > enforcedValue {
		return fmt.Errorf("maximum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	return nil
}

// maxConstraint enforces the max constraint over the specified resource
func maxConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if !limExists {
		return fmt.Errorf("maximum %s usage per %s is %s.  No limit is specified", resourceName, limitType, enforced.String())
	}
	if observedLimValue > enforcedValue {
		return fmt.Errorf("maximum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	if reqExists && (observedReqValue > enforcedValue) {
		return fmt.Errorf("maximum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	return nil
}

// limitRequestRatioConstraint enforces the limit to request ratio over the specified resource
func limitRequestRatioConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, _ := requestLimitEnforcedValues(req, lim, enforced)

	if !reqExists || (observedReqValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no request is specified or request is 0", resourceName, limitType, enforced.String())
	}
	if !limExists || (observedLimValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no limit is specified or limit is 0", resourceName, limitType, enforced.String())
	}

	observedRatio := float64(observedLimValue) / float64(observedReqValue)
	displayObservedRatio := observedRatio
	maxLimitRequestRatio := float64(enforced.Value())
	if enforced.Value() <= resource.MaxMilliValue {
		observedRatio = observedRatio * 1000
		maxLimitRequestRatio = float64(enforced.MilliValue())
	}

	if observedRatio > maxLimitRequestRatio {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but provided ratio is %f", resourceName, limitType, enforced.String(), displayObservedRatio)
	}

	return nil
}

// DefaultLimitRangerActions is the default implementation of LimitRangerActions.
type DefaultLimitRangerActions struct{}

// ensure DefaultLimitRangerActions implements the LimitRangerActions interface.
var _ LimitRangerActions = &DefaultLimitRangerActions{}

// MutateLimit enforces resource requirements of incoming resources
// against enumerated constraints on the LimitRange.  It may modify
// the incoming object to apply default resource requirements if not
// specified, and enumerated on the LimitRange
func (d *DefaultLimitRangerActions) MutateLimit(limitRange *corev1.LimitRange, resourceName string, obj runtime.Object) error {
	switch resourceName {
	case "pods":
		return PodMutateLimitFunc(limitRange, obj.(*corev1.Pod))
	}
	return nil
}

// ValidateLimit verifies the resource requirements of incoming
// resources against enumerated constraints on the LimitRange are
// valid
func (d *DefaultLimitRangerActions) ValidateLimit(limitRange *corev1.LimitRange, resourceName string, obj runtime.Object) error {
	switch resourceName {
	case "pods":
		return PodValidateLimitFunc(limitRange, obj.(*corev1.Pod))
	case "persistentvolumeclaims":
		return PersistentVolumeClaimValidateLimitFunc(limitRange, obj.(*corev1.PersistentVolumeClaim))
	}
	return nil
}

// fathom: api.Kind("Pod") / api.Kind("PersistentVolumeClaim") on external types.
var (
	podKind = corev1.SchemeGroupVersion.WithKind("Pod").GroupKind()
	pvcKind = corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim").GroupKind()
)

// SupportsAttributes ignores all calls that do not deal with pod resources or storage requests (PVCs).
// Also ignores any call that has a subresource defined.
func (d *DefaultLimitRangerActions) SupportsAttributes(a admission.Attributes) bool {
	// Handle in-place vertical scaling of pods, where users modify container
	// resources using the resize subresource.
	if a.GetSubresource() == "resize" && a.GetKind().GroupKind() == podKind && a.GetOperation() == admission.Update {
		return true
	}

	// No other subresources are supported
	if a.GetSubresource() != "" {
		return false
	}

	// Since containers and initContainers cannot currently be added, removed, or updated, it is unnecessary
	// to mutate and validate limitrange on pod updates. Trying to mutate containers or initContainers on a pod
	// update request will always fail pod validation because those fields are immutable once the object is created.
	if a.GetKind().GroupKind() == podKind && a.GetOperation() == admission.Update {
		return false
	}

	return a.GetKind().GroupKind() == podKind || a.GetKind().GroupKind() == pvcKind
}

// SupportsLimit always returns true.
func (d *DefaultLimitRangerActions) SupportsLimit(limitRange *corev1.LimitRange) bool {
	return true
}

// PersistentVolumeClaimValidateLimitFunc enforces storage limits for PVCs.
// Users request storage via pvc.Spec.Resources.Requests.  Min/Max is enforced by an admin with LimitRange.
// Claims will not be modified with default values because storage is a required part of pvc.Spec.
// All storage enforced values *only* apply to pvc.Spec.Resources.Requests.
func PersistentVolumeClaimValidateLimitFunc(limitRange *corev1.LimitRange, pvc *corev1.PersistentVolumeClaim) error {
	var errs []error
	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		limitType := limit.Type
		if limitType == corev1.LimitTypePersistentVolumeClaim {
			for k, v := range limit.Min {
				// normal usage of minConstraint. pvc.Spec.Resources.Limits is not recognized as user input
				if err := minConstraint(string(limitType), string(k), v, pvc.Spec.Resources.Requests, corev1.ResourceList{}); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.Max {
				// We want to enforce the max of the LimitRange against what
				// the user requested.
				if err := maxRequestConstraint(string(limitType), string(k), v, pvc.Spec.Resources.Requests); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

// PodMutateLimitFunc sets resource requirements enumerated by the pod against
// the specified LimitRange.  The pod may be modified to apply default resource
// requirements if not specified, and enumerated on the LimitRange
func PodMutateLimitFunc(limitRange *corev1.LimitRange, pod *corev1.Pod) error {
	defaultResources := defaultContainerResourceRequirements(limitRange)
	mergePodResourceRequirements(pod, &defaultResources)
	return nil
}

// PodValidateLimitFunc enforces resource requirements enumerated by the pod against
// the specified LimitRange.
func PodValidateLimitFunc(limitRange *corev1.LimitRange, pod *corev1.Pod) error {
	var errs []error

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		limitType := limit.Type
		// enforce container limits
		if limitType == corev1.LimitTypeContainer {
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
			for j := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}

		// enforce pod limits on init containers
		if limitType == corev1.LimitTypePod {
			opts := podResourcesOptions{
				// fathom: was feature.DefaultFeatureGate.Enabled(features.PodLevelResources)
				PodLevelResourcesEnabled: gates.PodLevelResources,
			}
			podRequests := podRequests(pod, opts)
			podLimits := podLimits(pod, opts)
			for k, v := range limit.Min {
				if err := minConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.Max {
				if err := maxConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.MaxLimitRequestRatio {
				if err := limitRequestRatioConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

type podResourcesOptions struct {
	// PodLevelResourcesEnabled indicates that the PodLevelResources feature gate is
	// enabled.
	PodLevelResourcesEnabled bool
}

// fathom: aggregateOptions makes component-helpers compute what upstream's local
// podRequests/podLimits aggregation computes: spec only (UseStatusResources=false,
// upstream NOTE), no pod overhead (ExcludeOverhead), no pod-level override
// (SkipPodLevelResources; the override is applied below with upstream's set).
var aggregateOptions = resourcehelper.PodResourcesOptions{
	ExcludeOverhead:       true,
	UseStatusResources:    false,
	SkipPodLevelResources: true,
}

// podRequests is a simplified version of pkg/api/v1/resource/PodRequests that operates against the core version of
// pod. Any changes to that calculation should be reflected here.
// NOTE: We do not want to check status resources here, only the spec. This is equivalent to setting
// UseStatusResources=false in the common helper.
// TODO: Maybe we can consider doing a partial conversion of the pod to a v1
// type and then using the pkg/api/v1/resource/PodRequests.
// TODO(ndixita): PodRequests method exists in
// staging/src/k8s.io/component-helpers/resource/helpers.go. Refactor the code to
// avoid duplicating podRequests method.
func podRequests(pod *corev1.Pod, opts podResourcesOptions) corev1.ResourceList {
	// fathom: upstream's container loop, init-container loop and maxResourceList
	// (lines 571-605) are the same sidecar formula as component-helpers'
	// AggregateContainerRequests, which PodRequests runs with these options.
	reqs := resourcehelper.PodRequests(pod, aggregateOptions)

	// If PodLevelResources feature is enabled and resources are set at pod-level,
	// override aggregated container requests of resources supported by pod-level
	// resources with quantities specified at pod-level.
	if opts.PodLevelResourcesEnabled && pod.Spec.Resources != nil {
		for resourceName, quantity := range pod.Spec.Resources.Requests {
			if isSupportedPodLevelResource(resourceName) {
				// override with pod-level resource requests
				reqs[resourceName] = quantity
			}
		}
	}

	return reqs
}

// podLimits is a simplified version of pkg/api/v1/resource/PodLimits that operates against the core version of
// pod. Any changes to that calculation should be reflected here.
// NOTE: We do not want to check status resources here, only the spec. This is equivalent to setting
// UseStatusResources=false in the common helper.
// TODO: Maybe we can consider doing a partial conversion of the pod to a v1
// type and then using the pkg/api/v1/resource/PodLimits.
// TODO(ndixita): PodLimits method exists in
// staging/src/k8s.io/component-helpers/resource/helpers.go. Refactor the code to
// avoid duplicating podLimits method.
func podLimits(pod *corev1.Pod, opts podResourcesOptions) corev1.ResourceList {
	// fathom: upstream lines 631-660 are component-helpers' AggregateContainerLimits.
	limits := resourcehelper.PodLimits(pod, aggregateOptions)

	// If PodLevelResources feature is enabled and resources are set at pod-level,
	// override aggregated container limits of resources supported by pod-level
	// resources with quantities specified at pod-level.
	if opts.PodLevelResourcesEnabled && pod.Spec.Resources != nil {
		for resourceName, quantity := range pod.Spec.Resources.Limits {
			if isSupportedPodLevelResource(resourceName) {
				// override with pod-level resource limits
				limits[resourceName] = quantity
			}
		}
	}

	return limits
}

var supportedPodLevelResources = sets.New(corev1.ResourceCPU, corev1.ResourceMemory)

// isSupportedPodLevelResources checks if a given resource is supported by pod-level
// resource management through the PodLevelResources feature. Returns true if
// the resource is supported.
// isSupportedPodLevelResource method exists in
// staging/src/k8s.io/component-helpers/resource/helpers.go.
// isSupportedPodLevelResource is added here to avoid conversion of v1.
// Pod to api.Pod.
// TODO(ndixita): Find alternatives to avoid duplicating the code.
func isSupportedPodLevelResource(name corev1.ResourceName) bool {
	return supportedPodLevelResources.Has(name)
}
