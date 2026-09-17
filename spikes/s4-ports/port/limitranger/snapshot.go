// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package limitranger

import (
	"path/filepath"

	corev1 "k8s.io/api/core/v1"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

// SnapshotRanges is the RangeSource over a snapshot directory: the LimitRanges
// live in <dir>/limitrange.yaml (a single object, a multi-document file or a
// `kind: List`) and are filtered by namespace at call time, in file order.
func SnapshotRanges(dir string) RangeSource {
	return SnapshotRangesFile(filepath.Join(dir, "limitrange.yaml"))
}

// SnapshotRangesFile is SnapshotRanges over one named file.
func SnapshotRangesFile(path string) RangeSource {
	return func(namespace string) ([]*corev1.LimitRange, error) {
		all, err := snapshot.Load[*corev1.LimitRange](path)
		if err != nil {
			return nil, err
		}
		var out []*corev1.LimitRange
		for _, lr := range all {
			if lr.Namespace == namespace {
				out = append(out, lr)
			}
		}
		return out, nil
	}
}

// NewFromSnapshot is NewLimitRanger with the default actions over a snapshot directory.
func NewFromSnapshot(dir string) (*LimitRanger, error) {
	return NewFromSnapshotFile(filepath.Join(dir, "limitrange.yaml"))
}

// NewFromSnapshotFile is NewFromSnapshot over one named LimitRange file.
func NewFromSnapshotFile(path string) (*LimitRanger, error) {
	return NewLimitRanger(&DefaultLimitRangerActions{}, SnapshotRangesFile(path))
}
