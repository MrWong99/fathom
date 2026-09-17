// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

// Denial is the normalised unit both sides are compared on: the policy, the
// rule (the binding name for a ValidatingAdmissionPolicy, empty for a
// ValidatingPolicy, which has no rule) and the rule's own message text.
type Denial struct {
	Policy  string `json:"policy"`
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message"`
}

// Verdict is the normalised server or offline outcome.
type Verdict struct {
	Allowed bool     `json:"allowed"`
	Denials []Denial `json:"denials,omitempty"`
}

var (
	allowedRE = regexp.MustCompile(`(?m)^[a-z0-9.\-/]+ (created|configured|unchanged)( \(server dry run\))?$`)
	cpolHdrRE = regexp.MustCompile(`admission webhook "validate\.kyverno\.svc-fail" denied the request: `)
	vpolRE    = regexp.MustCompile(`admission webhook "vpol\.validate\.kyverno\.svc-fail" denied the request: Policy (\S+) failed: (.*)$`)
	vapRE     = regexp.MustCompile(`ValidatingAdmissionPolicy '([^']+)' with binding '([^']+)' denied request: (.*)$`)
	cpolRule  = regexp.MustCompile(`^  (\S+): (.*)$`)
)

// ParseServerVerdict applies the minimal normalisation to kubectl's verbatim
// stdout+stderr from `kubectl apply --dry-run=server`:
//
//   - "<resource> created|configured|unchanged [(server dry run)]" -> allowed;
//   - the ClusterPolicy webhook wrapper `Error from server: error when
//     creating "<file>": admission webhook "validate.kyverno.svc-fail" denied
//     the request: \n\nresource <Kind>/<ns>/<name> was blocked due to the
//     following policies \n\n<policy>:\n  <rule>: <message>` -> one Denial per
//     "  <rule>: <message>" line under its "<policy>:" header;
//   - the ValidatingPolicy webhook wrapper `... admission webhook
//     "vpol.validate.kyverno.svc-fail" denied the request: Policy <name>
//     failed: <message>` -> Denial{policy, "", message};
//   - the apiserver's own VAP text `The <resource> "<name>" is invalid: :
//     ValidatingAdmissionPolicy '<policy>' with binding '<binding>' denied
//     request: <message>` -> Denial{policy, binding, message}.
//
// Only the wrappers are stripped; the message text is compared verbatim.
func ParseServerVerdict(text string) (Verdict, error) {
	t := strings.TrimRight(text, "\n")
	if allowedRE.MatchString(t) {
		return Verdict{Allowed: true}, nil
	}
	if m := vpolRE.FindStringSubmatch(t); m != nil {
		return Verdict{Denials: []Denial{{Policy: m[1], Message: m[2]}}}, nil
	}
	if m := vapRE.FindStringSubmatch(t); m != nil {
		return Verdict{Denials: []Denial{{Policy: m[1], Rule: m[2], Message: m[3]}}}, nil
	}
	if loc := cpolHdrRE.FindStringIndex(t); loc != nil {
		body := t[loc[1]:]
		var v Verdict
		policy := ""
		for _, line := range strings.Split(body, "\n") {
			if m := cpolRule.FindStringSubmatch(line); m != nil && policy != "" {
				v.Denials = append(v.Denials, Denial{Policy: policy, Rule: m[1], Message: m[2]})
				continue
			}
			if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "resource ") {
				policy = strings.TrimSuffix(line, ":")
			}
		}
		if len(v.Denials) == 0 {
			return v, fmt.Errorf("kyverno webhook denial without a policy/rule block:\n%s", text)
		}
		sortDenials(v.Denials)
		return v, nil
	}
	return Verdict{}, fmt.Errorf("unrecognised server verdict:\n%s", text)
}

// OfflineVerdict reduces the adapter's findings to the same shape: denied
// when any finding is Blocking, one Denial per Blocking finding.
func OfflineVerdict(r *Result) Verdict {
	v := Verdict{Allowed: true}
	for _, f := range r.Findings {
		if f.Severity != SeverityBlocking {
			continue
		}
		v.Allowed = false
		d := Denial{Policy: f.Policy, Rule: f.Rule, Message: f.Message}
		if f.Source == "ValidatingAdmissionPolicy" {
			d.Rule = f.Binding
		}
		v.Denials = append(v.Denials, d)
	}
	sortDenials(v.Denials)
	return v
}

func sortDenials(ds []Denial) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Policy != b.Policy {
			return a.Policy < b.Policy
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}

// Leaves flattens a YAML/JSON document into RFC 6901 JSON Pointer -> scalar
// (canonical JSON text of the scalar). Empty maps and lists are leaves too.
func Leaves(doc []byte) (map[string]string, error) {
	var v any
	if err := yaml.Unmarshal(doc, &v); err != nil {
		return nil, err
	}
	out := map[string]string{}
	walk("", v, out)
	return out, nil
}

func walk(ptr string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			out[ptr] = "{}"
			return
		}
		for k, e := range t {
			walk(ptr+"/"+escape(k), e, out)
		}
	case []any:
		if len(t) == 0 {
			out[ptr] = "[]"
			return
		}
		for i, e := range t {
			walk(ptr+"/"+strconv.Itoa(i), e, out)
		}
	default:
		b, _ := json.Marshal(t)
		out[ptr] = string(b)
	}
}

func escape(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

// Delta returns the leaves that after adds or changes relative to before.
func Delta(before, after map[string]string) map[string]string {
	d := map[string]string{}
	for k, v := range after {
		if bv, ok := before[k]; !ok || bv != v {
			d[k] = v
		}
	}
	return d
}

// Filter drops leaves whose pointer matches any of the patterns.
func Filter(leaves map[string]string, drop []*regexp.Regexp) map[string]string {
	out := map[string]string{}
next:
	for k, v := range leaves {
		for _, re := range drop {
			if re.MatchString(k) {
				continue next
			}
		}
		out[k] = v
	}
	return out
}
