// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package gates pins the upstream feature gates the ports evaluate.
//
// Upstream reads utilfeature.DefaultFeatureGate.Enabled(features.X) at
// request time; fathom has no feature-gate registry and runs the ports at the
// snapshot's minor, so every gate the ported files branch on is a constant
// here. Values are the defaults of kubernetes v1.37.0
// (pkg/features/kube_features.go at tag v1.37.0). On a per-minor refresh, diff
// that file for the gates below and update the constants; a gate that turned
// GA and LockToDefault can have its else branch deleted from the port.
package gates

const (
	// PodLevelResources: beta, default on since 1.34 (pkg/features/kube_features.go).
	// Read by limitranger (Pod-level min/max sums) and quota pods.go (Constraints, usage).
	PodLevelResources = true

	// PodLevelResourcesFixKubeletQOSClass: beta, default on since 1.37.
	// Read by qos.go (ComputePodQOS), which quota's BestEffort scope uses.
	PodLevelResourcesFixKubeletQOSClass = true

	// InPlacePodVerticalScaling: GA and locked on since 1.35.
	// quota pods.go passes it as PodResourcesOptions.UseStatusResources.
	InPlacePodVerticalScaling = true

	// VolumeAttributesClass: GA since 1.34, locked on since 1.36.
	// quota persistent_volume_claims.go scope matching; else branches deleted.
	VolumeAttributesClass = true

	// RecoverVolumeExpansionFailure: GA and locked on since 1.34.
	// quota persistent_volume_claims.go getStorageUsage; else branch deleted.
	RecoverVolumeExpansionFailure = true
)
