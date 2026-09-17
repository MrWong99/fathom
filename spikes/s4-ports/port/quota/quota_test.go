// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package quota

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/utils/clock"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/oracle"
	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

func loadAll(t *testing.T, files ...string) []runtime.Object {
	t.Helper()
	var out []runtime.Object
	for _, f := range files {
		objs, err := snapshot.LoadObjects(filepath.Join(oracle.Testdata(), f))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, objs...)
	}
	return out
}

// TestRecomputeMatchesControllerStatus feeds the real objects of each phase
// through the ported Usage functions (quota.CalculateUsage, as the quota
// controller does) and compares with the status.used the controller wrote
// into the snapshot. count/configmaps is left out: the snapshot does not
// carry kube-root-ca.crt.
func TestRecomputeMatchesControllerStatus(t *testing.T) {
	t.Parallel()
	phases := []struct {
		status string
		real   []string
	}{
		{"snapshot/quota-status.yaml", []string{"objects/real-pod-a.yaml", "objects/real-pod-high.yaml", "objects/real-pvc-a.yaml", "objects/real-svc-lb.yaml"}},
		{"snapshot/quota-status-3pods.yaml", []string{"objects/real-pod-a.yaml", "objects/real-pod-high.yaml", "objects/real-pod-b.yaml", "objects/real-pvc-a.yaml", "objects/real-svc-lb.yaml"}},
	}
	counted := []schema.GroupResource{{Resource: "pods"}, {Resource: "persistentvolumeclaims"}, {Resource: "services"}}
	for _, ph := range phases {
		ph := ph
		t.Run(filepath.Base(ph.status), func(t *testing.T) {
			t.Parallel()
			quotas, err := LoadQuotas(oracle.Testdata(), ph.status)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry(snapshot.ListerForObjects(loadAll(t, ph.real...)), clock.RealClock{})
			for i := range quotas {
				q := &quotas[i]
				used, err := RecomputeUsage(q, registry, counted)
				if err != nil {
					t.Fatal(err)
				}
				for name, want := range q.Status.Used {
					if name == corev1.ResourceConfigMaps || name == "count/configmaps" {
						continue
					}
					got, ok := used[name]
					if !ok {
						t.Errorf("%s: %s not recomputed", q.Name, name)
						continue
					}
					if got.Cmp(want) != 0 {
						t.Errorf("%s: %s recomputed %s, controller wrote %s", q.Name, name, got.String(), want.String())
					}
				}
				t.Logf("%s: recomputed %v", q.Name, used)
			}
		})
	}
}

func testPod(prio string, req, lim corev1.ResourceList) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "s4"},
		Spec: corev1.PodSpec{PriorityClassName: prio, Containers: []corev1.Container{{
			Name: "app", Resources: corev1.ResourceRequirements{Requests: req, Limits: lim},
		}}},
	}
}

func TestPodMatchingScopes(t *testing.T) {
	t.Parallel()
	ev := NewPodEvaluator(snapshot.ListerForObjects(nil), clock.RealClock{})
	cpu := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}
	deadline := int64(60)
	terminating := testPod("", cpu, cpu)
	terminating.Spec.ActiveDeadlineSeconds = &deadline
	sel := func(name corev1.ResourceQuotaScope, op corev1.ScopeSelectorOperator, values ...string) corev1.ScopedResourceSelectorRequirement {
		return corev1.ScopedResourceSelectorRequirement{ScopeName: name, Operator: op, Values: values}
	}
	cases := []struct {
		name string
		pod  *corev1.Pod
		sel  corev1.ScopedResourceSelectorRequirement
		want bool
	}{
		{"PriorityClass In match", testPod("s4-high", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpIn, "s4-high"), true},
		{"PriorityClass In other", testPod("low", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpIn, "s4-high"), false},
		{"PriorityClass In unset", testPod("", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpIn, "s4-high"), false},
		{"PriorityClass NotIn unset", testPod("", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpNotIn, "s4-high"), true},
		{"PriorityClass Exists", testPod("x", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpExists), true},
		{"PriorityClass DoesNotExist", testPod("", cpu, cpu), sel(corev1.ResourceQuotaScopePriorityClass, corev1.ScopeSelectorOpDoesNotExist), true},
		{"BestEffort", testPod("", nil, nil), sel(corev1.ResourceQuotaScopeBestEffort, corev1.ScopeSelectorOpExists), true},
		{"NotBestEffort burstable", testPod("", cpu, nil), sel(corev1.ResourceQuotaScopeNotBestEffort, corev1.ScopeSelectorOpExists), true},
		{"Terminating", terminating, sel(corev1.ResourceQuotaScopeTerminating, corev1.ScopeSelectorOpExists), true},
		{"NotTerminating", testPod("", cpu, cpu), sel(corev1.ResourceQuotaScopeNotTerminating, corev1.ScopeSelectorOpExists), true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ev.MatchingScopes(tc.pod, []corev1.ScopedResourceSelectorRequirement{tc.sel})
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) == 1) != tc.want {
				t.Fatalf("matched %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPodConstraintsMessage(t *testing.T) {
	t.Parallel()
	ev := NewPodEvaluator(snapshot.ListerForObjects(nil), clock.RealClock{})
	p := testPod("", nil, nil)
	p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "b"})
	err := ev.Constraints([]corev1.ResourceName{corev1.ResourceRequestsCPU, corev1.ResourceLimitsMemory, corev1.ResourcePods}, p)
	want := "must specify limits.memory for: app,b; requests.cpu for: app,b"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	// pod-level resources lift the per-container requirement
	p.Spec.Resources = &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}
	if err := ev.Constraints([]corev1.ResourceName{corev1.ResourceRequestsCPU}, p); err != nil {
		t.Fatal(err)
	}
}

func TestServiceAndPVCUsage(t *testing.T) {
	t.Parallel()
	svcEv := NewServiceEvaluator(snapshot.ListerForObjects(nil))
	off := false
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, AllocateLoadBalancerNodePorts: &off,
		Ports: []corev1.ServicePort{{Port: 80}, {Port: 443, NodePort: 30443}}}}
	u, err := svcEv.Usage(svc)
	if err != nil {
		t.Fatal(err)
	}
	lb, np, svcs := u[corev1.ResourceServicesLoadBalancers], u[corev1.ResourceServicesNodePorts], u[corev1.ResourceServices]
	if lb.Value() != 1 || np.Value() != 1 || svcs.Value() != 1 {
		t.Fatalf("service usage: %v", u)
	}
	pvcEv := NewPersistentVolumeClaimEvaluator(snapshot.ListerForObjects(nil))
	class := "gold"
	pvc := &corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &class,
		Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1.5Gi")}}}}
	u, err = pvcEv.Usage(pvc)
	if err != nil {
		t.Fatal(err)
	}
	st, cst, ccl := u[corev1.ResourceRequestsStorage], u["gold.storageclass.storage.k8s.io/requests.storage"], u["gold.storageclass.storage.k8s.io/persistentvolumeclaims"]
	if st.String() != "1536Mi" || cst.String() != "1536Mi" || ccl.Value() != 1 {
		t.Fatalf("pvc usage: %v", u)
	}
	if !quota.Equals(quota.Mask(u, []corev1.ResourceName{corev1.ResourcePersistentVolumeClaims}), corev1.ResourceList{corev1.ResourcePersistentVolumeClaims: *resource.NewQuantity(1, resource.DecimalSI)}) {
		t.Fatalf("pvc count: %v", u)
	}
}

func TestRegistryCoversAliases(t *testing.T) {
	t.Parallel()
	r := NewRegistry(snapshot.ListerForObjects(nil), clock.RealClock{})
	for _, gr := range []schema.GroupResource{{Resource: "pods"}, {Resource: "services"}, {Resource: "persistentvolumeclaims"}, {Resource: "configmaps"}, {Resource: "secrets"}} {
		if r.Get(gr) == nil {
			t.Errorf("no evaluator for %s", gr)
		}
	}
	if len(r.List()) != 7 {
		t.Errorf("want 7 evaluators, got %d", len(r.List()))
	}
}
