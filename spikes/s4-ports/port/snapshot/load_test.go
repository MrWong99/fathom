// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package snapshot

import (
	"path/filepath"
	"runtime"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata")
}

func TestLoadListAndMultiDoc(t *testing.T) {
	t.Parallel()
	quotas, err := Load[*corev1.ResourceQuota](filepath.Join(testdata(), "snapshot", "quota-status.yaml"))
	if err != nil || len(quotas) != 2 {
		t.Fatalf("kind: List -> %d quotas, %v", len(quotas), err)
	}
	if q := quotas[0].Status.Used[corev1.ResourceRequestsCPU]; q.String() != "600m" {
		t.Fatalf("status kept: %v", quotas[0].Status.Used)
	}
	crs, err := Load[*rbacv1.ClusterRole](filepath.Join(testdata(), "snapshot", "rbac", "clusterroles.yaml"))
	if err != nil || len(crs) != 3 {
		t.Fatalf("clusterroles: %d, %v", len(crs), err)
	}
	multi, err := LoadObjects(filepath.Join(testdata(), "snapshot", "rbac", "fixture.yaml"))
	if err != nil || len(multi) != 8 {
		t.Fatalf("multi-doc fixture: %d objects, %v", len(multi), err)
	}
	none, err := Load[*corev1.LimitRange](filepath.Join(testdata(), "does-not-exist.yaml"))
	if err != nil || none != nil {
		t.Fatalf("missing file: %v %v", none, err)
	}
}

func TestAttributesAndLister(t *testing.T) {
	t.Parallel()
	objs, err := LoadObjects(filepath.Join(testdata(), "objects", "q-lb.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := CreateAttributes(objs[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.GetResource().Resource != "services" || a.GetNamespace() != "s4" || a.GetName() != "q-lb" || a.GetKind().Kind != "Service" {
		t.Fatalf("attributes: %v %s/%s", a.GetResource(), a.GetNamespace(), a.GetName())
	}
	pods, err := LoadObjects(filepath.Join(testdata(), "objects", "real-pod-a.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := ListerForObjects(append(objs, pods...))(corev1.SchemeGroupVersion.WithResource("pods"))
	if err != nil {
		t.Fatal(err)
	}
	items, _ := l.ByNamespace("s4").List(labels.Everything())
	if len(items) != 1 {
		t.Fatalf("want 1 pod in s4, got %d", len(items))
	}
	if items, _ := l.ByNamespace("other").List(labels.Everything()); len(items) != 0 {
		t.Fatalf("namespace filter: %d", len(items))
	}
	if _, err := l.Get("nope"); err == nil {
		t.Fatal("Get of a missing object must fail")
	}
}
