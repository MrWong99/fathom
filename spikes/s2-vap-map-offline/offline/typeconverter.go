// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apiserver/pkg/admission/plugin/policy/mutating/patch"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
)

// SnapshotTypeConverterManager implements patch.TypeConverterManager over an
// openapi.Client that serves saved /openapi/v3 documents (here
// kubectl-validate's openapiclient.NewLocalSchemaFiles over testdata/openapi).
//
// It does what the apiserver's typeConverterManager does
// (k8s.io/apiserver/pkg/admission/plugin/policy/mutating/patch/typeconverter.go)
// minus the 5 s discovery poll: Paths() is read once at construction because a
// snapshot never changes, and GetTypeConverter builds
// managedfields.NewTypeConverter(components.schemas, false) per group-version on
// first use. There is no static converter for native types; the apiserver
// passes nil there too and resolves apps/v1 through the served OpenAPI.
type SnapshotTypeConverterManager struct {
	paths map[schema.GroupVersion]openapi.GroupVersion

	mu    sync.Mutex
	cache map[schema.GroupVersion]managedfields.TypeConverter
}

var _ patch.TypeConverterManager = &SnapshotTypeConverterManager{}

// NewSnapshotTypeConverterManager lists the client's paths once.
func NewSnapshotTypeConverterManager(client openapi.Client) (*SnapshotTypeConverterManager, error) {
	paths, err := client.Paths()
	if err != nil {
		return nil, fmt.Errorf("openapi paths: %w", err)
	}
	parsed := make(map[schema.GroupVersion]openapi.GroupVersion, len(paths))
	for path, entry := range paths {
		if !strings.HasPrefix(path, "apis/") && !strings.HasPrefix(path, "api/") {
			continue
		}
		path = strings.TrimPrefix(path, "apis/")
		path = strings.TrimPrefix(path, "api/")
		gv, err := schema.ParseGroupVersion(path)
		if err != nil {
			return nil, fmt.Errorf("openapi path %q: %w", path, err)
		}
		parsed[gv] = entry
	}
	return &SnapshotTypeConverterManager{paths: parsed, cache: map[schema.GroupVersion]managedfields.TypeConverter{}}, nil
}

// GetTypeConverter implements patch.TypeConverterManager. It returns nil when
// the snapshot has no document for the group-version, which the dispatcher
// turns into a ServiceUnavailable error exactly as the apiserver does.
func (m *SnapshotTypeConverterManager) GetTypeConverter(gvk schema.GroupVersionKind) managedfields.TypeConverter {
	gv := gvk.GroupVersion()
	m.mu.Lock()
	defer m.mu.Unlock()
	if tc, ok := m.cache[gv]; ok {
		return tc
	}
	entry, ok := m.paths[gv]
	if !ok {
		return nil
	}
	raw, err := entry.Schema(runtime.ContentTypeJSON)
	if err != nil {
		return nil
	}
	var doc spec3.OpenAPI
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	tc, err := managedfields.NewTypeConverter(doc.Components.Schemas, false)
	if err != nil {
		return nil
	}
	m.cache[gv] = tc
	return tc
}

// Run implements patch.TypeConverterManager; a snapshot needs no refresh loop.
func (m *SnapshotTypeConverterManager) Run(context.Context) {}

// GroupVersions lists the group-versions the snapshot has schemas for.
func (m *SnapshotTypeConverterManager) GroupVersions() []schema.GroupVersion {
	out := make([]schema.GroupVersion, 0, len(m.paths))
	for gv := range m.paths {
		out = append(out, gv)
	}
	return out
}
