/*
Copyright 2019 The Kubernetes Authors.

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

// Fathom edit: upstream is pkg/admission/reinvocation.go, reinvoker.Admit
// (lines 35-51; its doc comment, lines 31-34, is carried too). The wrapped admission chain is passed as a function instead
// of an admission.MutationInterface; the two-pass logic is unchanged.

package port

import (
	"context"

	"k8s.io/apiserver/pkg/admission"
)

// AdmitFunc is one pass of a mutating admission chain.
type AdmitFunc func(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error

// AdmitWithReinvocation performs an admission control check using the wrapped admission chain, reinvoking the
// admission chain if needed according to the reinvocation policy.  Plugins are expected to check
// the admission attributes' reinvocation context against their reinvocation policy to decide if
// they should re-run, and to update the reinvocation context if they perform any mutations.
func AdmitWithReinvocation(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces, mutator AdmitFunc) error {
	err := mutator(ctx, a, o)
	if err != nil {
		return err
	}
	s := a.GetReinvocationContext()
	if s.ShouldReinvoke() {
		s.SetIsReinvoke()
		// Calling admit a second time will reinvoke all in-tree plugins
		// as well as any webhook plugins that need to be reinvoked based on the
		// reinvocation policy.
		return mutator(ctx, a, o)
	}
	return nil
}
