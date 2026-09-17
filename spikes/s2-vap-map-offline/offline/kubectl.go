// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

// RenderKubectl reproduces what `kubectl <verb> --dry-run=server -f <source>`
// prints (stdout and stderr merged, as golden/<scenario>.server.txt was
// captured) and its exit code for the given admission result.
//
// Nothing is stripped from either side. The rules are the ones in
// k8s.io/kubectl/pkg/cmd/util/helpers.go (checkErr, StatusCausesToAggrError,
// MultilineError) and k8s.io/kubectl/pkg/cmd/util.AddSourceToErr:
//
//   - every warning header becomes a line "Warning: <text>" (kubectl's default
//     WarningHandler), printed before the result;
//   - admitted: "<singular>.<group>/<name> created (server dry run)";
//   - a StatusError with reason Invalid is rendered from its Details, not its
//     Message: `The <details.kind> "<details.name>" is invalid: ` followed by
//     the causes as "<field>: <message>" (one cause: appended on the same
//     line, so an empty field yields the ": :" in the goldens; several: one
//     "* " bullet per cause on its own line, identical cause strings printed
//     once as statusCausesToAggrError does). With nil Details kubectl prints
//     "The request is invalid" plus ": <message>" when Message is set; with
//     Details but empty Kind and Name it keeps "The request is invalid".
//     AddSourceToErr's `error when creating "<file>": ` prefix lands in
//     Message, which the Details path never prints;
//   - any other StatusError: `Error from server (<reason>): error when
//     creating "<source>": <message>`.
//
// The exit code is 1 for any error, 0 otherwise.
//
// Only the one-cause Invalid branch and the admitted branch are exercised by
// the oracle goldens. The other branches are transcribed from kubectl v1.36
// (k8s.io/kubectl is not a dependency of this module) and are checked by
// kubectl_test.go for self-consistency only.
func RenderKubectl(req *RequestObject, res Result) (output string, exitCode int) {
	var sb strings.Builder
	for _, w := range res.Warnings {
		fmt.Fprintf(&sb, "Warning: %s\n", w)
	}
	if res.Err == nil {
		gvk := req.Object.GroupVersionKind()
		resource := strings.ToLower(gvk.Kind)
		if gvk.Group != "" {
			resource += "." + gvk.Group
		}
		fmt.Fprintf(&sb, "%s/%s %s (server dry run)\n", resource, req.Object.Name, pastTense(req.Verb))
		return sb.String(), 0
	}
	status, isStatus := res.Err.(apierrors.APIStatus)
	if !isStatus {
		fmt.Fprintf(&sb, "error: %v\n", res.Err)
		return sb.String(), 1
	}
	st := status.Status()
	if apierrors.IsInvalid(res.Err) {
		s := "The request is invalid"
		if st.Details == nil {
			// checkErr: no details, include the server's message if present.
			if len(st.Message) > 0 {
				s += ": " + st.Message
			}
			sb.WriteString(s + "\n")
			return sb.String(), 1
		}
		if len(st.Details.Kind) != 0 || len(st.Details.Name) != 0 {
			s = fmt.Sprintf("The %s %q is invalid", st.Details.Kind, st.Details.Name)
		}
		if len(st.Details.Causes) == 0 {
			sb.WriteString(s + "\n")
			return sb.String(), 1
		}
		// statusCausesToAggrError: duplicate "<field>: <message>" strings are
		// dropped, first occurrence kept.
		causes := make([]string, 0, len(st.Details.Causes))
		seen := make(map[string]bool, len(st.Details.Causes))
		for _, c := range st.Details.Causes {
			msg := fmt.Sprintf("%s: %s", c.Field, c.Message)
			if seen[msg] {
				continue
			}
			seen[msg] = true
			causes = append(causes, msg)
		}
		if len(causes) == 1 {
			fmt.Fprintf(&sb, "%s: %s\n", s, causes[0])
		} else {
			fmt.Fprintf(&sb, "%s: \n", s)
			for _, c := range causes {
				fmt.Fprintf(&sb, "* %s\n", c)
			}
		}
		return sb.String(), 1
	}
	fmt.Fprintf(&sb, "Error from server (%s): error when %s %q: %s\n", st.Reason, presentParticiple(req.Verb), req.Source, st.Message)
	return sb.String(), 1
}

func pastTense(verb string) string {
	// kubectl apply and create both print "created" for a new object.
	return "created"
}

func presentParticiple(verb string) string {
	// AddSourceToErr("creating", ...) in both apply and create paths.
	return "creating"
}

// ApplyNativeDefaults sets the fields the apiserver's apps/v1 and core/v1
// defaulting functions set on a Deployment before admission runs (the request
// body is defaulted at decode time, so every admission plugin, including MAP,
// sees a defaulted object; the plugin also re-runs the defaulter after each
// patch). Those functions live in k8s.io/kubernetes/pkg/apis/{apps,core}/v1,
// which fathom must not import, and the served OpenAPI v3 carries no default
// values for them, so this is the observed 1.37 output written down by hand,
// set-if-absent, restricted to what these fixtures exercise. Defaulting is a
// separate pipeline stage (design 3.3) and not what S2 measures; it is applied
// here only so the offline mutation result can be compared with the oracle's
// fully defaulted object.
func ApplyNativeDefaults(d *appsv1.Deployment) {
	if d.Spec.Replicas == nil {
		d.Spec.Replicas = ptr(int32(1))
	}
	if d.Spec.ProgressDeadlineSeconds == nil {
		d.Spec.ProgressDeadlineSeconds = ptr(int32(600))
	}
	if d.Spec.RevisionHistoryLimit == nil {
		d.Spec.RevisionHistoryLimit = ptr(int32(10))
	}
	if d.Spec.Strategy.Type == "" {
		d.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
	}
	if d.Spec.Strategy.Type == appsv1.RollingUpdateDeploymentStrategyType {
		if d.Spec.Strategy.RollingUpdate == nil {
			d.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{}
		}
		if d.Spec.Strategy.RollingUpdate.MaxSurge == nil {
			d.Spec.Strategy.RollingUpdate.MaxSurge = ptr(intstr.FromString("25%"))
		}
		if d.Spec.Strategy.RollingUpdate.MaxUnavailable == nil {
			d.Spec.Strategy.RollingUpdate.MaxUnavailable = ptr(intstr.FromString("25%"))
		}
	}
	ps := &d.Spec.Template.Spec
	if ps.DNSPolicy == "" {
		ps.DNSPolicy = corev1.DNSClusterFirst
	}
	if ps.RestartPolicy == "" {
		ps.RestartPolicy = corev1.RestartPolicyAlways
	}
	if ps.SchedulerName == "" {
		ps.SchedulerName = corev1.DefaultSchedulerName
	}
	if ps.SecurityContext == nil {
		ps.SecurityContext = &corev1.PodSecurityContext{}
	}
	if ps.TerminationGracePeriodSeconds == nil {
		ps.TerminationGracePeriodSeconds = ptr(int64(corev1.DefaultTerminationGracePeriodSeconds))
	}
	for i := range ps.Containers {
		c := &ps.Containers[i]
		if c.ImagePullPolicy == "" {
			if strings.HasSuffix(c.Image, ":latest") || !strings.Contains(c.Image, ":") {
				c.ImagePullPolicy = corev1.PullAlways
			} else {
				c.ImagePullPolicy = corev1.PullIfNotPresent
			}
		}
		if c.TerminationMessagePath == "" {
			c.TerminationMessagePath = corev1.TerminationMessagePathDefault
		}
		if c.TerminationMessagePolicy == "" {
			c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
		}
	}
}

func ptr[T any](v T) *T { return &v }

// Canonical converts an object to a JSON-shaped map with the server-assigned
// and per-run fields removed: metadata.managedFields, resourceVersion, uid,
// creationTimestamp, generation and the top-level status. Maps compare
// key-order independent; CanonicalJSON renders them with sorted keys.
func Canonical(obj runtime.Object) (map[string]any, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(raw)
}

// CanonicalYAML is Canonical for a YAML document (a golden file).
func CanonicalYAML(doc []byte) (map[string]any, error) {
	raw, err := yaml.YAMLToJSON(doc)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(raw)
}

func canonicalJSON(raw []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if meta, ok := m["metadata"].(map[string]any); ok {
		for _, k := range []string{"managedFields", "resourceVersion", "uid", "creationTimestamp", "generation"} {
			delete(meta, k)
		}
	}
	delete(m, "status")
	return m, nil
}

// CanonicalJSON renders a canonical map with sorted keys, one key per line.
func CanonicalJSON(m map[string]any) string {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(raw)
}
