// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package validate runs the S1 calibration: kubectl-validate's pkg/validator
// offline against the fixture CRDs and objects, a ~100-line ValidateUpdate
// port built from exported k8s.io/apiserver and apiextensions-apiserver APIs,
// and a normaliser that turns kubectl's stderr and the offline error into the
// same list of "path: message" items so the two can be compared.
package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apiextensions-apiserver/pkg/crdserverscheme"
	apiextensionsfeatures "k8s.io/apiextensions-apiserver/pkg/features"
	"k8s.io/apiextensions-apiserver/pkg/registry/customresource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/apiserver/pkg/warning"
	"sigs.k8s.io/kubectl-validate/pkg/openapiclient"
	"sigs.k8s.io/kubectl-validate/pkg/validator"
	"sigs.k8s.io/yaml"
)

// Scenario is one row of testdata/manifest.json, written by the oracle agent.
type Scenario struct {
	Name           string `json:"name"`
	CRDFile        string `json:"crdFile"`
	ObjectFile     string `json:"objectFile"`
	OldObjectFile  string `json:"oldObjectFile,omitempty"`
	Operation      string `json:"operation"`
	ServerAccepted bool   `json:"serverAccepted"`
	ServerError    string `json:"serverError"`
	GoldenFile     string `json:"goldenFile"`
	Notes          string `json:"notes"`
}

// Manifest is testdata/manifest.json.
type Manifest struct {
	Oracle               map[string]string `json:"oracle"`
	BudgetReproduced     bool              `json:"budgetReproduced"`
	RatchetingReproduced bool              `json:"ratchetingReproduced"`
	Scenarios            []Scenario        `json:"scenarios"`
}

// LoadManifest reads the oracle manifest.
func LoadManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

// Result is what one offline run produced, rendered the way kubectl renders
// the same server response so both sides go through the same normaliser.
type Result struct {
	// Accepted is true when validation returned no error (warnings allowed).
	Accepted bool
	// Text is the kubectl-style rendering: "Warning: ..." lines followed by
	// the error, or "" when accepted without warnings.
	Text string
	// Warnings are the admission warnings recorded during validation.
	Warnings []string
	// Err is the raw error.
	Err error
}

// Offline wraps kubectl-validate's validator built from a directory of CRDs.
type Offline struct {
	v *validator.Validator
}

// NewOffline builds the validator from openapiclient.NewLocalCRDFiles over
// crdFS. No builtin schemas are composed in: the local CRD client injects the
// ObjectMeta definitions itself (local_crds_metadata.json), which is all the
// fixture CRDs reference.
func NewOffline(crdFS fs.FS) (*Offline, error) {
	v, err := validator.New(openapiclient.NewLocalCRDFiles(crdFS))
	if err != nil {
		return nil, err
	}
	return &Offline{v: v}, nil
}

// Create validates doc as a create the way kubectl-validate's CLI does:
// strict Parse (structural-schema decoder: unknown fields are errors, defaults
// applied) followed by Validate, which is rest.BeforeCreate on the
// customresource strategy.
func (o *Offline) Create(doc []byte) Result {
	_, obj, err := o.v.Parse(doc)
	if err != nil {
		return render(err, nil)
	}
	return render(o.v.Validate(obj), nil)
}

// CreateIgnoreUnknown emulates fieldValidation=Ignore: the document is decoded
// with a plain YAML decoder (no pruning, no decode-time strictness) and handed
// to Validate directly. kubectl-validate has no non-strict Parse, so this is
// the closest offline equivalent; note that unknown fields are NOT pruned,
// they are simply invisible to the OpenAPI validator.
func (o *Offline) CreateIgnoreUnknown(doc []byte) Result {
	obj, err := decode(doc)
	if err != nil {
		return render(err, nil)
	}
	return render(o.v.Validate(obj), nil)
}

// Update is kubectl-validate's answer to an update: it has no old-object path,
// so this is Validate on the new object alone (documented in RESULT.md).
func (o *Offline) Update(newDoc []byte) Result {
	return o.Create(newDoc)
}

// RatchetingEnabled reports the CRDValidationRatcheting feature gate as the
// apiserver default feature gate sees it in this binary.
func RatchetingEnabled() bool {
	return utilfeature.DefaultFeatureGate.Enabled(apiextensionsfeatures.CRDValidationRatcheting)
}

// Strategy is the piece kubectl-validate is missing: the same
// customresource.NewStrategy the apiserver's CRD handler builds, from a CRD
// decoded with the apiextensions types, plus the update entry point.
type Strategy struct {
	strat      crStrategy
	namespaced bool
}

// crStrategy is what customresource.NewStrategy returns, seen through the
// exported rest interfaces (the concrete type is unexported).
type crStrategy interface {
	rest.RESTCreateStrategy
	rest.RESTUpdateStrategy
}

// NewStrategy builds the strategy for the version of the CRD that gvk names,
// mirroring customresource_handler.go: internal CRD -> GetSchemaForVersion ->
// NewStructural + validation.NewSchemaValidator (ratcheting when the gate is
// on) -> customresource.NewStrategy with the crdserverscheme typer.
func NewStrategy(crdYAML []byte, version string) (*Strategy, error) {
	var crdv1 apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(crdYAML, &crdv1); err != nil {
		return nil, fmt.Errorf("decode CRD: %w", err)
	}
	var crd apiextensions.CustomResourceDefinition
	if err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crdv1, &crd, nil); err != nil {
		return nil, fmt.Errorf("convert CRD: %w", err)
	}
	val, err := apiextensions.GetSchemaForVersion(&crd, version)
	if err != nil {
		return nil, err
	}
	var props *apiextensions.JSONSchemaProps
	if val != nil {
		props = val.OpenAPIV3Schema
	}
	ss, err := structuralschema.NewStructural(props)
	if err != nil {
		return nil, fmt.Errorf("structural schema: %w", err)
	}
	sv, _, err := apiservervalidation.NewSchemaValidator(props)
	if err != nil {
		return nil, fmt.Errorf("schema validator: %w", err)
	}
	gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: version, Kind: crd.Spec.Names.Kind}
	namespaced := crd.Spec.Scope == apiextensions.NamespaceScoped
	strat := customresource.NewStrategy(
		crdserverscheme.NewUnstructuredObjectTyper(),
		namespaced,
		gvk,
		sv, nil, ss, nil, nil, nil,
	)
	return &Strategy{strat: strat, namespaced: namespaced}, nil
}

// ValidateUpdate is rest.BeforeUpdate(strategy, ctx, new, old) with a warning
// recorder on the context, which is where the server emits the ratcheting
// "Warning:" lines from.
func (s *Strategy) ValidateUpdate(newDoc, oldDoc []byte) Result {
	newObj, err := decode(newDoc)
	if err != nil {
		return render(err, nil)
	}
	oldObj, err := decode(oldDoc)
	if err != nil {
		return render(err, nil)
	}
	// kubectl apply sends a patch; the server applies it to the stored object,
	// so the new object carries the stored resourceVersion. Without it
	// rest.ValidateUpdate adds "metadata.resourceVersion: must be specified
	// for an update". UID and creationTimestamp are copied by BeforeUpdate.
	if newObj.GetResourceVersion() == "" {
		newObj.SetResourceVersion(oldObj.GetResourceVersion())
	}
	rec := &recorder{}
	ctx := warning.WithWarningRecorder(request.WithNamespace(context.Background(), namespaceOf(newObj, s.namespaced)), rec)
	err = rest.BeforeUpdate(s.strat, ctx, newObj, oldObj)
	return render(err, rec.warnings)
}

// ValidateCreate is rest.BeforeCreate on the same strategy: the pure
// apiserver path without kubectl-validate's schema round trip, kept for
// cross-checking.
func (s *Strategy) ValidateCreate(doc []byte) Result {
	obj, err := decode(doc)
	if err != nil {
		return render(err, nil)
	}
	rec := &recorder{}
	ctx := warning.WithWarningRecorder(request.WithNamespace(context.Background(), namespaceOf(obj, s.namespaced)), rec)
	rest.FillObjectMetaSystemFields(obj)
	err = rest.BeforeCreate(s.strat, ctx, obj)
	return render(err, rec.warnings)
}

type recorder struct{ warnings []string }

func (r *recorder) AddWarning(_, text string) { r.warnings = append(r.warnings, text) }

func namespaceOf(obj *unstructured.Unstructured, namespaced bool) string {
	if !namespaced {
		return ""
	}
	if ns := obj.GetNamespace(); ns != "" {
		return ns
	}
	return metav1.NamespaceDefault
}

// decode is the plain YAML -> Unstructured path the apiserver's request
// decoder takes for a CR without schema pruning: YAML to JSON, then
// Unstructured.UnmarshalJSON (UnstructuredJSONScheme), which types integers as
// int64 the way CEL rules expect. sigs.k8s.io/yaml.Unmarshal into a map would
// give float64 and every integer rule would fail with "expected int, got
// float64".
func decode(doc []byte) (*unstructured.Unstructured, error) {
	j, err := yaml.YAMLToJSON(doc)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(j); err != nil {
		return nil, err
	}
	return obj, nil
}

// render turns warnings and an error into kubectl's stderr shape
// (k8s.io/kubectl/pkg/cmd/util/helpers.go: "Warning: ..." per warning header,
// then `The <Kind> "<name>" is invalid: ` with one cause inline or the causes
// as "* " lines, duplicates dropped).
func render(err error, warnings []string) Result {
	var b bytes.Buffer
	for _, w := range warnings {
		fmt.Fprintf(&b, "Warning: %s\n", w)
	}
	res := Result{Accepted: err == nil, Warnings: warnings, Err: err}
	if err == nil {
		res.Text = b.String()
		return res
	}
	var status apierrors.APIStatus
	if errors.As(err, &status) && apierrors.IsInvalid(err) && status.Status().Details != nil {
		d := status.Status().Details
		prefix := fmt.Sprintf("The %s %q is invalid", d.Kind, d.Name)
		seen := map[string]bool{}
		var items []string
		for _, c := range d.Causes {
			msg := fmt.Sprintf("%s: %s", c.Field, c.Message)
			if seen[msg] {
				continue
			}
			seen[msg] = true
			items = append(items, msg)
		}
		switch len(items) {
		case 0:
			fmt.Fprintf(&b, "%s: %v\n", prefix, err)
		case 1:
			fmt.Fprintf(&b, "%s: %s\n", prefix, items[0])
		default:
			fmt.Fprintf(&b, "%s: \n", prefix)
			for _, it := range items {
				fmt.Fprintf(&b, "* %s\n", it)
			}
		}
	} else {
		fmt.Fprintf(&b, "%s\n", err.Error())
	}
	res.Text = b.String()
	return res
}

var (
	// kubectl: The Widget "name" is invalid: ...
	kubectlInvalid = regexp.MustCompile(`^The \S+ "[^"]*" is invalid: ?`)
	// strategy (errors.NewInvalid): Widget.spike.fathom.dev "name" is invalid: ...
	strategyInvalid = regexp.MustCompile(`^\S+ "[^"]*" is invalid: ?`)
	// kubectl: Error from server (BadRequest): error when creating "objects/x.yaml": ...
	serverWrapped = regexp.MustCompile(`^Error from server \([A-Za-z]+\): error when [a-z]+ "[^"]*": `)
)

// Normalise reduces server or offline text to the sorted list of field-error
// items ("path: message"), warning lines kept verbatim, wrappers stripped.
// Empty text (accepted, no warnings) normalises to an empty list.
func Normalise(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	var items []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "* "):
			items = append(items, strings.TrimPrefix(line, "* "))
			continue
		case strings.HasPrefix(line, "Warning: "):
			items = append(items, line)
			continue
		}
		for _, re := range []*regexp.Regexp{kubectlInvalid, strategyInvalid, serverWrapped} {
			if loc := re.FindStringIndex(line); loc != nil {
				line = strings.TrimSpace(line[loc[1]:])
				break
			}
		}
		if line != "" {
			items = append(items, line)
		}
	}
	sort.Strings(items)
	return items
}

// Identical reports whether two texts normalise to the same items.
func Identical(server, offline string) bool {
	a, b := Normalise(server), Normalise(offline)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
