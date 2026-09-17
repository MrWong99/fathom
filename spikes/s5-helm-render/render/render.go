// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package render is the S5 spike: Helm 4.3 SDK render with snapshot-backed
// Capabilities and lookup, deterministic replacements for the non-deterministic
// template functions, and a 2020-12 values.schema.json with x-fathom-* keywords.
package render

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/common/util"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	sigyaml "sigs.k8s.io/yaml"
)

// Snapshot is the subset of a fathom snapshot this spike needs: the server
// version and discovery document (Capabilities) and an inventory of objects
// (lookup).
type Snapshot struct {
	KubeVersion string
	APIVersions []string
	Objects     []*unstructured.Unstructured
}

// Capabilities builds Helm's Capabilities from the snapshot instead of a live
// discovery call. HelmVersion is whatever the SDK reports; it is part of the
// render inputs and therefore of the report digest.
func (s *Snapshot) Capabilities() (*common.Capabilities, error) {
	kv, err := common.ParseKubeVersion(s.KubeVersion)
	if err != nil {
		return nil, fmt.Errorf("snapshot kube version %q: %w", s.KubeVersion, err)
	}
	return &common.Capabilities{
		KubeVersion: *kv,
		APIVersions: common.VersionSet(s.APIVersions),
		HelmVersion: common.DefaultCapabilities.HelmVersion,
	}, nil
}

// Lookup returns a function with the signature of Helm's `lookup` template
// function, answered from the snapshot inventory. It mirrors the engine's
// behaviour: a named object that is not found yields an empty map, not an
// error; a list yields an object with "items".
func (s *Snapshot) Lookup() func(apiVersion, kind, namespace, name string) (map[string]any, error) {
	return func(apiVersion, kind, namespace, name string) (map[string]any, error) {
		if name != "" {
			for _, o := range s.Objects {
				if o.GetAPIVersion() == apiVersion && o.GetKind() == kind && o.GetNamespace() == namespace && o.GetName() == name {
					return runtime.DeepCopyJSON(o.Object), nil
				}
			}
			return map[string]any{}, nil
		}
		items := []any{}
		for _, o := range s.Objects {
			if o.GetAPIVersion() == apiVersion && o.GetKind() == kind && (namespace == "" || o.GetNamespace() == namespace) {
				items = append(items, runtime.DeepCopyJSON(o.Object))
			}
		}
		return map[string]any{"apiVersion": apiVersion, "kind": kind + "List", "items": items}, nil
	}
}

// ClientProvider is the design's path (design 3.3 P2 names
// RenderWithClientProvider): a Helm engine.ClientProvider backed by client-go's
// fake dynamic client seeded with the snapshot inventory. The spike keeps it to
// show it works, and records in RESULT.md why Render below does not use it.
func (s *Snapshot) ClientProvider() engine.ClientProvider {
	objs := make([]runtime.Object, 0, len(s.Objects))
	for _, o := range s.Objects {
		objs = append(objs, o)
	}
	return snapshotClientProvider{dyn: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objs...)}
}

type snapshotClientProvider struct {
	dyn *dynamicfake.FakeDynamicClient
}

var clusterScopedKinds = map[string]bool{
	"Namespace": true, "Node": true, "PersistentVolume": true, "StorageClass": true, "ClusterRole": true,
	"ClusterRoleBinding": true, "CustomResourceDefinition": true, "IngressClass": true, "PriorityClass": true,
}

func (p snapshotClientProvider) GetClientFor(apiVersion, kind string) (dynamic.NamespaceableResourceInterface, bool, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, false, err
	}
	gvr, _ := meta.UnsafeGuessKindToResource(gv.WithKind(kind))
	return p.dyn.Resource(gvr), !clusterScopedKinds[kind], nil
}

// Options are the render inputs that are not values.
type Options struct {
	ReleaseName string
	Namespace   string
	// Seed feeds the deterministic functions; the product derives it from the
	// report inputs (values digest, snapshot digest, chart digest).
	Seed string
	// Now replaces the wall clock for `now` and `ago`.
	Now time.Time
	// Deterministic switches the replacement functions on. Off reproduces the
	// stock engine, which the tests use as the negative control.
	Deterministic bool
	// SkipSchemaValidation mirrors helm's --skip-schema-validation.
	SkipSchemaValidation bool
}

// Load loads a chart directory or archive with the v2 loader.
func Load(path string) (*chart.Chart, error) {
	return loader.Load(path)
}

func renderValues(chrt *chart.Chart, vals map[string]any, snap *Snapshot, opts Options) (common.Values, error) {
	caps, err := snap.Capabilities()
	if err != nil {
		return nil, err
	}
	return util.ToRenderValuesWithSchemaValidation(chrt, vals, common.ReleaseOptions{
		Name: opts.ReleaseName, Namespace: opts.Namespace, Revision: 1, IsInstall: true,
	}, caps, opts.SkipSchemaValidation)
}

// Render renders the chart against the snapshot. Values are coalesced with the
// chart defaults and validated against every values.schema.json exactly as
// helm template does (ToRenderValuesWithSchemaValidation), then the engine
// runs with the snapshot-backed lookup and, when asked, the deterministic
// function replacements installed through CustomTemplateFuncs.
//
// Note on the design's RenderWithClientProvider: in helm.sh/helm/v4 v4.3.0
// the Engine's clientProvider field is unexported and the two constructors
// that set it (New(*rest.Config), RenderWithClientProvider) leave no way to
// also set CustomTemplateFuncs. Because initFunMap copies CustomTemplateFuncs
// last, a custom `lookup` wins over the built-in one, so the snapshot-backed
// lookup goes in through CustomTemplateFuncs and no ClientProvider is needed.
func Render(chrt *chart.Chart, vals map[string]any, snap *Snapshot, opts Options) (map[string]string, error) {
	rv, err := renderValues(chrt, vals, snap, opts)
	if err != nil {
		return nil, err
	}
	e := engine.Engine{CustomTemplateFuncs: map[string]any{}}
	if opts.Deterministic {
		e.CustomTemplateFuncs = DeterministicFuncs(opts.Seed, opts.Now)
	}
	e.CustomTemplateFuncs["lookup"] = snap.Lookup()
	return e.RenderWithContext(context.Background(), chrt, rv)
}

// RenderDesignPath is the literal design 3.3 P2 path: RenderWithClientProvider
// with the fake-dynamic-client provider. No deterministic functions can be
// installed on this path (see Render).
func RenderDesignPath(chrt *chart.Chart, vals map[string]any, snap *Snapshot, opts Options) (map[string]string, error) {
	rv, err := renderValues(chrt, vals, snap, opts)
	if err != nil {
		return nil, err
	}
	return engine.RenderWithClientProvider(chrt, rv, snap.ClientProvider())
}

// Doc is one rendered Kubernetes document with its source template.
type Doc struct {
	Source    string
	Kind      string
	Name      string
	Canonical string // sorted-key YAML, used for byte comparison
}

// Docs splits a render result into non-empty documents in a stable order
// (source path, then document index) and canonicalises each one through a
// JSON round-trip so key order never enters the comparison.
func Docs(rendered map[string]string) ([]Doc, error) {
	sources := make([]string, 0, len(rendered))
	for k := range rendered {
		sources = append(sources, k)
	}
	sort.Strings(sources)
	var out []Doc
	for _, src := range sources {
		base := src[strings.LastIndex(src, "/")+1:]
		if base == "NOTES.txt" || strings.HasPrefix(base, "_") {
			continue
		}
		for _, raw := range SplitDocs(rendered[src]) {
			d, err := canonicalDoc(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", src, err)
			}
			if d == nil {
				continue
			}
			d.Source = src
			out = append(out, *d)
		}
	}
	return out, nil
}

// SplitDocs splits a multi-document YAML string on document markers.
func SplitDocs(s string) []string {
	var docs []string
	for _, part := range strings.Split("\n"+s, "\n---") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		docs = append(docs, part)
	}
	return docs
}

func canonicalDoc(raw string) (*Doc, error) {
	var m map[string]any
	if err := sigyaml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	b, err := sigyaml.Marshal(m) // sigs.k8s.io/yaml marshals via JSON: keys sorted
	if err != nil {
		return nil, err
	}
	d := &Doc{Canonical: string(b)}
	d.Kind, _ = m["kind"].(string)
	if md, ok := m["metadata"].(map[string]any); ok {
		d.Name, _ = md["name"].(string)
	}
	return d, nil
}

// Join renders documents the way helm template prints them, for digests.
func Join(docs []Doc) string {
	var b strings.Builder
	for _, d := range docs {
		b.WriteString("---\n# Source: ")
		b.WriteString(d.Source)
		b.WriteString("\n")
		b.WriteString(d.Canonical)
	}
	return b.String()
}
