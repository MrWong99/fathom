// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package chain runs the three ports in the apiserver's order for one
// object: LimitRanger mutate, LimitRanger validate, ResourceQuota last
// (design 3.3). It is the spike's stand-in for internal/admit.
package chain

import (
	"context"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/utils/clock"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/limitranger"
	"github.com/MrWong99/fathom/spikes/s4-ports/port/quota"
	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

// Chain holds the snapshot-fed plugins.
type Chain struct {
	limitRanger *limitranger.LimitRanger
	quotas      []corev1.ResourceQuota
	registry    quotav1.Registry
}

// New reads the LimitRanges (limitrange.yaml) and the ResourceQuotas (with
// status, from quotaStatusFile, default quota-status.yaml) of a snapshot
// directory. The quota registry lists nothing: usage comes from status.used,
// never from a recount.
func New(dir, quotaStatusFile string) (*Chain, error) {
	return NewFiles(dir, "", quotaStatusFile)
}

// NewFiles is New with the LimitRange file named too (default limitrange.yaml).
func NewFiles(dir, limitRangeFile, quotaStatusFile string) (*Chain, error) {
	if limitRangeFile == "" {
		limitRangeFile = "limitrange.yaml"
	}
	lr, err := limitranger.NewFromSnapshotFile(filepath.Join(dir, limitRangeFile))
	if err != nil {
		return nil, err
	}
	quotas, err := quota.LoadQuotas(dir, quotaStatusFile)
	if err != nil {
		return nil, err
	}
	noLister := snapshot.ListerForObjects(nil)
	return &Chain{
		limitRanger: lr,
		quotas:      quotas,
		registry:    quota.NewRegistry(noLister, clock.RealClock{}),
	}, nil
}

// Result is the outcome for one object.
type Result struct {
	// Object is the (possibly mutated) object; the input is mutated in place.
	Object runtime.Object
	// Err is the server's error (a Forbidden StatusError) or nil when admitted.
	Err error
	// Quotas is the quota list with the usage this object would add, when admitted.
	Quotas []corev1.ResourceQuota
	// QuotaErrs lists every exceeded quota (one Forbidden per quota, by name)
	// when Err comes from ResourceQuota; the server reports only one of them.
	QuotaErrs []error
}

// Admit runs the chain for a create of obj by userInfo (nil = anonymous).
func (c *Chain) Admit(ctx context.Context, obj runtime.Object, userInfo user.Info) Result {
	a, err := snapshot.CreateAttributes(obj, userInfo)
	if err != nil {
		return Result{Object: obj, Err: fmt.Errorf("attributes: %w", err)}
	}
	// M: LimitRanger mutate (defaults)
	if err := c.limitRanger.Admit(ctx, a, nil); err != nil {
		return Result{Object: obj, Err: err}
	}
	// V: LimitRanger validate (min, max, ratio, pod sums, pvc min/max)
	if err := c.limitRanger.Validate(ctx, a, nil); err != nil {
		return Result{Object: obj, Err: err}
	}
	// V, last: ResourceQuota
	quotas, err := quota.Check(c.quotas, a, c.registry)
	if err != nil {
		return Result{Object: obj, Err: err, QuotaErrs: quota.CheckAll(c.quotas, a, c.registry)}
	}
	return Result{Object: obj, Quotas: quotas}
}
