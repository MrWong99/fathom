// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"slices"
	"strings"
)

// policySet is what the adapter learned from the policy files: which file
// goes to which pass, which policies are enforced, which ConfigMap context
// entries and VAP parameters the snapshot must supply.
type policySet struct {
	mutateFiles    []string
	validateFiles  []string
	exceptionFiles []string

	// enforce is keyed "policy" and "policy/rule"; a rule-level entry wins.
	enforce map[string]bool

	// configMapContexts lists ClusterPolicy/Policy context entries of the
	// form {policy, rule, contextName, cm namespace, cm name}.
	configMapContexts []cmContext

	// params lists VAP and MAP binding paramRefs {policy, kind, namespace,
	// name}.
	params []paramRef
}

type cmContext struct {
	policy, rule, name, namespace, cmName string
}

type paramRef struct {
	policy, kind, namespace, name string
	// mutating marks a MutatingAdmissionPolicyBinding paramRef. The CLI
	// does not report a missing MAP parameter (no error result, no file,
	// "pass: 0", exit 0: the binding's parameterNotFoundAction is ignored),
	// so the adapter refuses to run when the snapshot lacks it; a missing
	// VAP parameter is left to the CLI, which yields result error.
	mutating bool
}

// mapCLIVersion is the only MutatingAdmissionPolicy API version Kyverno CLI
// 1.19.1 parses; the kind 1.37 oracle serves the same kind at v1.
const mapCLIVersion = "admissionregistration.k8s.io/v1alpha1"

func (p *policySet) enforced(policy, rule string) bool {
	if v, ok := p.enforce[policy+"/"+rule]; ok && rule != "" {
		return v
	}
	if v, ok := p.enforce[policy]; ok {
		return v
	}
	return false
}

// classifyPolicies reads every policy file once and routes it by kind:
//
//   - ClusterPolicy/Policy rules with `mutate` -> pass 1; rules with
//     `validate` -> pass 2 (a file holding both goes to both passes);
//   - ValidatingAdmissionPolicy + ValidatingAdmissionPolicyBinding and
//     policies.kyverno.io ValidatingPolicy -> pass 2;
//   - MutatingAdmissionPolicy + MutatingAdmissionPolicyBinding (only at
//     admissionregistration.k8s.io/v1alpha1: CLI 1.19.1 drops v1 and
//     v1beta1 documents with a non-fatal parse error and "Applying 0 policy
//     rule(s)", a silent non-evaluation, so those versions are refused
//     here) and MutatingPolicy -> pass 1;
//   - PolicyException (kyverno.io/v2 or policies.kyverno.io) -> --exception
//     on both passes.
//
// Enforcement: ClusterPolicy `rule.validate.failureAction` or
// `spec.validationFailureAction` equal to Enforce (Kyverno's default is
// Audit); a VAP binding whose validationActions contains Deny; a
// ValidatingPolicy whose spec.validationActions contains Deny or is absent.
// The served ValidatingPolicy CRD has no default for validationActions; the
// kind oracle denies a violating object under a ValidatingPolicy without
// the field (engine/testdata/vpol-no-validation-actions.server.txt), so
// absent is treated as Deny.
func classifyPolicies(files []string) (*policySet, error) {
	ps := &policySet{enforce: map[string]bool{}}
	vapKinds := map[string]string{} // VAP/MAP name -> paramKind kind
	for _, f := range files {
		docs, _, err := yamlDocs(f)
		if err != nil {
			return nil, err
		}
		var toMutate, toValidate, toException bool
		for _, d := range docs {
			kind := str(d, "kind")
			apiVersion := str(d, "apiVersion")
			name := str(d, "metadata", "name")
			switch kind {
			case "ClusterPolicy", "Policy":
				specAction := str(d, "spec", "validationFailureAction")
				for _, r := range list(d, "spec", "rules") {
					rname := str(r, "name")
					if get(r, "mutate") != nil {
						toMutate = true
					}
					if v, ok := get(r, "validate").(map[string]any); ok {
						toValidate = true
						action := str(v, "failureAction")
						if action == "" {
							action = specAction
						}
						ps.enforce[name+"/"+rname] = action == "Enforce"
						ps.enforce[name] = ps.enforce[name] || action == "Enforce"
					}
					for _, c := range list(r, "context") {
						if cm, ok := get(c, "configMap").(map[string]any); ok {
							ps.configMapContexts = append(ps.configMapContexts, cmContext{
								policy: name, rule: rname, name: str(c, "name"),
								namespace: str(cm, "namespace"), cmName: str(cm, "name"),
							})
						}
					}
				}
			case "ValidatingAdmissionPolicy":
				toValidate = true
				pk := str(d, "spec", "paramKind", "kind")
				vapKinds[name] = pk
				if _, ok := ps.enforce[name]; !ok {
					ps.enforce[name] = false
				}
			case "ValidatingAdmissionPolicyBinding":
				toValidate = true
				policy := str(d, "spec", "policyName")
				actions := strList(get(d, "spec", "validationActions"))
				ps.enforce[policy] = ps.enforce[policy] || slices.Contains(actions, "Deny")
				if pr, ok := get(d, "spec", "paramRef").(map[string]any); ok {
					ps.params = append(ps.params, paramRef{
						policy: policy, kind: vapKinds[policy],
						namespace: str(pr, "namespace"), name: str(pr, "name"),
					})
				}
			case "ValidatingPolicy", "ImageValidatingPolicy":
				toValidate = true
				actions := strList(get(d, "spec", "validationActions"))
				ps.enforce[name] = len(actions) == 0 || slices.Contains(actions, "Deny")
			case "MutatingAdmissionPolicy":
				if apiVersion != mapCLIVersion {
					return nil, fmt.Errorf("%s: %s %s is at %s; kyverno CLI 1.19.1 evaluates MutatingAdmissionPolicy only at %s and silently drops other versions", f, kind, name, apiVersion, mapCLIVersion)
				}
				toMutate = true
				vapKinds[name] = str(d, "spec", "paramKind", "kind")
			case "MutatingAdmissionPolicyBinding":
				if apiVersion != mapCLIVersion {
					return nil, fmt.Errorf("%s: %s %s is at %s; kyverno CLI 1.19.1 evaluates MutatingAdmissionPolicyBinding only at %s and silently drops other versions", f, kind, name, apiVersion, mapCLIVersion)
				}
				toMutate = true
				policy := str(d, "spec", "policyName")
				if pr, ok := get(d, "spec", "paramRef").(map[string]any); ok {
					ps.params = append(ps.params, paramRef{
						policy: policy, kind: vapKinds[policy], mutating: true,
						namespace: str(pr, "namespace"), name: str(pr, "name"),
					})
				}
			case "MutatingPolicy":
				toMutate = true
			case "PolicyException":
				toException = true
			default:
				return nil, fmt.Errorf("%s: unsupported policy document %s %s", f, apiVersion, kind)
			}
		}
		if toMutate {
			ps.mutateFiles = append(ps.mutateFiles, f)
		}
		if toValidate {
			ps.validateFiles = append(ps.validateFiles, f)
		}
		if toException {
			ps.exceptionFiles = append(ps.exceptionFiles, f)
		}
	}
	// A binding may name a VAP declared in a later file: fill paramKind now.
	for i := range ps.params {
		if ps.params[i].kind == "" {
			ps.params[i].kind = vapKinds[ps.params[i].policy]
		}
		if ps.params[i].kind == "" {
			ps.params[i].kind = "ConfigMap"
		}
	}
	return ps, nil
}

// EnforcementTable renders the derived enforcement for RESULT.md and logs.
func (p *policySet) EnforcementTable() string {
	keys := make([]string, 0, len(p.enforce))
	for k := range p.enforce {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v ", k, p.enforce[k])
	}
	return b.String()
}
