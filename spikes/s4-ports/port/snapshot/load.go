// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package snapshot reads the objects the ports need (LimitRanges,
// ResourceQuotas with status, RBAC objects, live objects for usage
// recomputation) from a snapshot directory of kubectl-exported YAML. It is the
// stand-in for fathom's internal/snapshot in this spike: the ports never see
// an informer, a lister backed by a client or a live cluster.
package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

// LoadObjects decodes every document in a kubectl-exported YAML file into the
// typed client-go scheme objects, flattening `kind: List`. Unknown kinds are an
// error: the snapshot is expected to hold only core and RBAC types here.
func LoadObjects(path string) ([]runtime.Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeObjects(data)
}

// DecodeObjects is LoadObjects over bytes.
func DecodeObjects(data []byte) ([]runtime.Object, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	deserializer := scheme.Codecs.UniversalDeserializer()
	var out []runtime.Object
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}
		obj, _, err := deserializer.Decode(raw.Raw, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		if list, ok := obj.(*corev1.List); ok {
			for i := range list.Items {
				item, _, err := deserializer.Decode(list.Items[i].Raw, nil, nil)
				if err != nil {
					return nil, fmt.Errorf("decode list item %d: %w", i, err)
				}
				out = append(out, item)
			}
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

// Load reads path and returns the objects of type T in file order; other kinds
// in the file are ignored. A missing file yields an empty slice, so an optional
// snapshot layer (no LimitRange in the namespace) reads as "none".
func Load[T runtime.Object](path string) ([]T, error) {
	objs, err := LoadObjects(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []T
	for _, o := range objs {
		if t, ok := o.(T); ok {
			out = append(out, t)
		}
	}
	return out, nil
}
