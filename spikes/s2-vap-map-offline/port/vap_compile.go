/*
Copyright 2024 The Kubernetes Authors.

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

// Package port holds copies of unexported symbols of k8s.io/apiserver v0.37.0
// that the offline ValidatingAdmissionPolicy / MutatingAdmissionPolicy
// evaluator needs. See NOTICE for the upstream line ranges.
//
// Fathom edit: the upstream file is
// pkg/admission/plugin/policy/validating/plugin.go. compilePolicy read the
// composition environment from a package-level sync.Once
// (getCompositionEnvTemplateWithStrictCost, plugin.go:52-57); here the caller
// passes the *environment.EnvSet so the compatibility version is explicit.
package port

import (
	v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	"k8s.io/apiserver/pkg/cel/environment"
)

// CompileValidatingPolicy is validating.compilePolicy (plugin.go:147-181).
func CompileValidatingPolicy(policy *validating.Policy, compositionEnvTemplate *environment.EnvSet) validating.Validator {
	hasParam := false
	if policy.Spec.ParamKind != nil {
		hasParam = true
	}
	optionalVars := cel.OptionalVariableDeclarations{HasParams: hasParam, HasAuthorizer: true}
	expressionOptionalVars := cel.OptionalVariableDeclarations{HasParams: hasParam, HasAuthorizer: false}
	failurePolicy := policy.Spec.FailurePolicy
	var matcher matchconditions.Matcher = nil
	matchConditions := policy.Spec.MatchConditions
	filterCompiler, err := cel.NewCompositedCompiler(compositionEnvTemplate)
	if err != nil {
		return validating.NewValidator(nil, nil, nil, nil, failurePolicy, err)
	}
	filterCompiler.CompileAndStoreVariables(convertv1beta1Variables(policy.Spec.Variables), optionalVars, environment.StoredExpressions)

	if len(matchConditions) > 0 {
		matchExpressionAccessors := make([]cel.ExpressionAccessor, len(matchConditions))
		for i := range matchConditions {
			matchExpressionAccessors[i] = (*matchconditions.MatchCondition)(&matchConditions[i])
		}
		matcher = matchconditions.NewMatcher(filterCompiler.CompileCondition(matchExpressionAccessors, optionalVars, environment.StoredExpressions), failurePolicy, "policy", "validate", policy.Name)
	}
	res := validating.NewValidator(
		filterCompiler.CompileCondition(convertv1Validations(policy.Spec.Validations), optionalVars, environment.StoredExpressions),
		matcher,
		filterCompiler.CompileCondition(convertv1AuditAnnotations(policy.Spec.AuditAnnotations), optionalVars, environment.StoredExpressions),
		filterCompiler.CompileCondition(convertv1MessageExpressions(policy.Spec.Validations), expressionOptionalVars, environment.StoredExpressions),
		failurePolicy,
		nil,
	)

	return res
}

func convertv1Validations(inputValidations []v1.Validation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		validation := validating.ValidationCondition{
			Expression: validation.Expression,
			Message:    validation.Message,
			Reason:     validation.Reason,
		}
		celExpressionAccessor[i] = &validation
	}
	return celExpressionAccessor
}

func convertv1MessageExpressions(inputValidations []v1.Validation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		if validation.MessageExpression != "" {
			condition := validating.MessageExpressionCondition{
				MessageExpression: validation.MessageExpression,
			}
			celExpressionAccessor[i] = &condition
		}
	}
	return celExpressionAccessor
}

func convertv1AuditAnnotations(inputValidations []v1.AuditAnnotation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		validation := validating.AuditAnnotationCondition{
			Key:             validation.Key,
			ValueExpression: validation.ValueExpression,
		}
		celExpressionAccessor[i] = &validation
	}
	return celExpressionAccessor
}

func convertv1beta1Variables(variables []v1.Variable) []cel.NamedExpressionAccessor {
	namedExpressions := make([]cel.NamedExpressionAccessor, len(variables))
	for i, variable := range variables {
		namedExpressions[i] = &validating.Variable{Name: variable.Name, Expression: variable.Expression}
	}
	return namedExpressions
}
