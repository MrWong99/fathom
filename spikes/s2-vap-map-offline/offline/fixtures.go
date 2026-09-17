// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package offline evaluates ValidatingAdmissionPolicy and
// MutatingAdmissionPolicy objects against a cluster snapshot without an
// apiserver, using the admission plugin code of k8s.io/apiserver v0.37.0 and
// the copies in ../port for the few unexported pieces.
package offline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"sigs.k8s.io/yaml"
)

// Snapshot is the part of testdata/snapshot the evaluator consumes: objects
// exactly as the oracle apiserver served them (server defaults applied).
type Snapshot struct {
	Namespace   *corev1.Namespace
	ConfigMaps  []*corev1.ConfigMap
	VAPs        []*admissionregistrationv1.ValidatingAdmissionPolicy
	VAPBindings []*admissionregistrationv1.ValidatingAdmissionPolicyBinding
	MAPs        []*admissionregistrationv1.MutatingAdmissionPolicy
	MAPBindings []*admissionregistrationv1.MutatingAdmissionPolicyBinding
	// User is the request identity kubectl used against the oracle
	// (snapshot/whoami.yaml).
	User user.Info
}

// Scenario mirrors one entry of testdata/manifest.json.
type Scenario struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	PolicyFiles    []string `json:"policyFiles"`
	ParamFiles     []string `json:"paramFiles"`
	ObjectFile     string   `json:"objectFile"`
	ServerAllowed  bool     `json:"serverAllowed"`
	ServerMessage  string   `json:"serverMessage"`
	ServerWarnings []string `json:"serverWarnings"`
	GoldenFile     string   `json:"goldenFile"`
	MutatedFile    string   `json:"mutatedFile"`
	Notes          string   `json:"notes"`
}

// Manifest mirrors testdata/manifest.json.
type Manifest struct {
	Server    string     `json:"server"`
	MapServed string     `json:"mapServed"`
	Scenarios []Scenario `json:"scenarios"`
}

// LoadManifest reads testdata/manifest.json.
func LoadManifest(testdata string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(testdata, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	return &m, nil
}

// LoadSnapshot reads testdata/snapshot/*.yaml. Policies and bindings are
// sorted by name so evaluation order is deterministic (the apiserver's
// informer-backed source also yields them sorted by name).
func LoadSnapshot(testdata string) (*Snapshot, error) {
	dir := filepath.Join(testdata, "snapshot")
	s := &Snapshot{}

	if err := readYAML(filepath.Join(dir, "namespace-s2.yaml"), &s.Namespace); err != nil {
		return nil, err
	}
	var cm corev1.ConfigMap
	if err := readYAML(filepath.Join(dir, "configmap-vap-limits.yaml"), &cm); err != nil {
		return nil, err
	}
	// Typed API responses carry no TypeMeta on list items; clear it so the
	// evaluator exercises the same wrappedParam path as the apiserver.
	cm.TypeMeta = metav1.TypeMeta{}
	s.ConfigMaps = append(s.ConfigMaps, &cm)

	if err := readList(filepath.Join(dir, "validatingadmissionpolicies.yaml"), &s.VAPs); err != nil {
		return nil, err
	}
	if err := readList(filepath.Join(dir, "validatingadmissionpolicybindings.yaml"), &s.VAPBindings); err != nil {
		return nil, err
	}
	if err := readList(filepath.Join(dir, "mutatingadmissionpolicies.yaml"), &s.MAPs); err != nil {
		return nil, err
	}
	if err := readList(filepath.Join(dir, "mutatingadmissionpolicybindings.yaml"), &s.MAPBindings); err != nil {
		return nil, err
	}
	sort.Slice(s.VAPs, func(i, j int) bool { return s.VAPs[i].Name < s.VAPs[j].Name })
	sort.Slice(s.VAPBindings, func(i, j int) bool { return s.VAPBindings[i].Name < s.VAPBindings[j].Name })
	sort.Slice(s.MAPs, func(i, j int) bool { return s.MAPs[i].Name < s.MAPs[j].Name })
	sort.Slice(s.MAPBindings, func(i, j int) bool { return s.MAPBindings[i].Name < s.MAPBindings[j].Name })

	var review authenticationv1.SelfSubjectReview
	if err := readYAML(filepath.Join(dir, "whoami.yaml"), &review); err != nil {
		return nil, err
	}
	info := &user.DefaultInfo{
		Name:   review.Status.UserInfo.Username,
		UID:    review.Status.UserInfo.UID,
		Groups: review.Status.UserInfo.Groups,
		Extra:  map[string][]string{},
	}
	for k, v := range review.Status.UserInfo.Extra {
		info.Extra[k] = []string(v)
	}
	s.User = info
	return s, nil
}

// RequestObject is a fixture Deployment prepared the way kubectl sent it.
type RequestObject struct {
	// Verb is "apply" or "create" (the kubectl sub-command the oracle used).
	Verb string
	// Source is the path kubectl was given, relative to testdata (used in
	// kubectl's error wrapper).
	Source string
	Object *appsv1.Deployment
}

// LoadRequestObject reads objects/<file> and reproduces the request body
// kubectl sent: for "apply" (client-side, the default in commands.sh) kubectl
// adds the kubectl.kubernetes.io/last-applied-configuration annotation, whose
// value is the file's content as JSON with metadata.annotations set to {}
// plus a trailing newline; "create" sends the file as is.
func LoadRequestObject(testdata, objectFile, verb string) (*RequestObject, error) {
	raw, err := os.ReadFile(filepath.Join(testdata, objectFile))
	if err != nil {
		return nil, err
	}
	var d appsv1.Deployment
	if err := yaml.UnmarshalStrict(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", objectFile, err)
	}
	switch verb {
	case "create":
	case "apply":
		lastApplied, err := lastAppliedConfiguration(raw)
		if err != nil {
			return nil, err
		}
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations["kubectl.kubernetes.io/last-applied-configuration"] = lastApplied
	default:
		return nil, fmt.Errorf("unknown verb %q", verb)
	}
	return &RequestObject{Verb: verb, Source: objectFile, Object: &d}, nil
}

// lastAppliedConfiguration reproduces kubectl's client-side apply annotation
// (k8s.io/kubectl/pkg/util.CreateApplyAnnotation): the object as kubectl read
// it from the file, unstructured, with the annotation key removed (leaving an
// empty annotations map), JSON-encoded with sorted keys and a trailing newline.
func lastAppliedConfiguration(rawYAML []byte) (string, error) {
	var obj map[string]any
	if err := yaml.Unmarshal(rawYAML, &obj); err != nil {
		return "", err
	}
	meta, _ := obj["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		obj["metadata"] = meta
	}
	ann, _ := meta["annotations"].(map[string]any)
	if ann == nil {
		ann = map[string]any{}
	}
	delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
	meta["annotations"] = ann
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	if err := enc.Encode(obj); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func readYAML(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// readList decodes a `kind: List` document into a slice of typed pointers.
func readList[T any](path string, out *[]*T) error {
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := readYAML(path, &list); err != nil {
		return err
	}
	for _, item := range list.Items {
		var t T
		if err := json.Unmarshal(item, &t); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		*out = append(*out, &t)
	}
	return nil
}
