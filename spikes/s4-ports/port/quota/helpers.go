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

// fathom: the helpers the quota evaluators need that live only in
// k8s.io/kubernetes, copied verbatim at tag v1.37.0:
//   - IsExtendedResourceName, IsPrefixedNativeResource, IsNativeResource and
//     ScopedResourceSelectorRequirementsAsSelector from
//     pkg/apis/core/v1/helper/helpers.go (Copyright 2014 The Kubernetes Authors,
//     the header above);
//   - IsDRAExtendedResourceName from pkg/scheduler/util/utils.go
//     (Copyright 2017 The Kubernetes Authors, same license).

package quota

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation"
)

// IsExtendedResourceName returns true if:
// 1. the resource name is not in the default namespace;
// 2. resource name does not have "requests." prefix,
// to avoid confusion with the convention in quota
// 3. it satisfies the rules in IsQualifiedName() after converted into quota resource name
func IsExtendedResourceName(name v1.ResourceName) bool {
	if IsNativeResource(name) || strings.HasPrefix(string(name), v1.DefaultResourceRequestsPrefix) {
		return false
	}
	// Ensure it satisfies the rules in IsQualifiedName() after converted into quota resource name
	nameForQuota := fmt.Sprintf("%s%s", v1.DefaultResourceRequestsPrefix, string(name))
	if errs := validation.IsQualifiedName(nameForQuota); len(errs) != 0 {
		return false
	}
	return true
}

// IsPrefixedNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace.
func IsPrefixedNativeResource(name v1.ResourceName) bool {
	return strings.Contains(string(name), v1.ResourceDefaultNamespacePrefix)
}

// IsNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace. Partially-qualified (unprefixed) names are
// implicitly in the kubernetes.io/ namespace.
func IsNativeResource(name v1.ResourceName) bool {
	return !strings.Contains(string(name), "/") ||
		IsPrefixedNativeResource(name)
}

// ScopedResourceSelectorRequirementsAsSelector converts the ScopedResourceSelectorRequirement api type into a struct that implements
// labels.Selector.
func ScopedResourceSelectorRequirementsAsSelector(ssr v1.ScopedResourceSelectorRequirement) (labels.Selector, error) {
	selector := labels.NewSelector()
	var op selection.Operator
	switch ssr.Operator {
	case v1.ScopeSelectorOpIn:
		op = selection.In
	case v1.ScopeSelectorOpNotIn:
		op = selection.NotIn
	case v1.ScopeSelectorOpExists:
		op = selection.Exists
	case v1.ScopeSelectorOpDoesNotExist:
		op = selection.DoesNotExist
	default:
		return nil, fmt.Errorf("%q is not a valid scope selector operator", ssr.Operator)
	}
	r, err := labels.NewRequirement(string(ssr.ScopeName), op, ssr.Values)
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*r)
	return selector, nil
}

// IsDRAExtendedResourceName returns true when name is an extended resource name, or an implicit extended resource name
// derived from device class name with the format of deviceclass.resource.kubernetes.io/<device class name>
func IsDRAExtendedResourceName(name v1.ResourceName) bool {
	return IsExtendedResourceName(name) || strings.HasPrefix(string(name), resourceapi.ResourceDeviceClassPrefix)
}
