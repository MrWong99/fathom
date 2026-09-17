// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package limitranger

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/admission"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/MrWong99/fathom/spikes/s4-ports/port/oracle"
	"github.com/MrWong99/fathom/spikes/s4-ports/port/snapshot"
)

func rl(cpu, mem string) corev1.ResourceList {
	out := corev1.ResourceList{}
	if cpu != "" {
		out[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if mem != "" {
		out[corev1.ResourceMemory] = resource.MustParse(mem)
	}
	return out
}

func pod(name string, containers ...corev1.Container) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "s4"},
		Spec:       corev1.PodSpec{Containers: containers},
	}
}

func container(name string, req, lim corev1.ResourceList) corev1.Container {
	return corev1.Container{Name: name, Image: "registry.k8s.io/pause:3.10", Resources: corev1.ResourceRequirements{Requests: req, Limits: lim}}
}

func newRanger(t *testing.T) *LimitRanger {
	t.Helper()
	lr, err := NewFromSnapshot(filepath.Join(oracle.Testdata(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	return lr
}

func admitAndValidate(t *testing.T, lr *LimitRanger, p *corev1.Pod) error {
	t.Helper()
	a, err := snapshot.CreateAttributes(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lr.Admit(context.Background(), a, nil); err != nil {
		return err
	}
	return lr.Validate(context.Background(), a, nil)
}

func TestSnapshotRangesFiltersByNamespace(t *testing.T) {
	t.Parallel()
	src := SnapshotRanges(filepath.Join(oracle.Testdata(), "snapshot"))
	s4, err := src("s4")
	if err != nil || len(s4) != 1 || s4[0].Name != "s4-limits" {
		t.Fatalf("s4: %v %v", s4, err)
	}
	other, err := src("other")
	if err != nil || len(other) != 0 {
		t.Fatalf("other: %v %v", other, err)
	}
	missing, err := SnapshotRanges(t.TempDir())("s4")
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file must read as no LimitRange: %v %v", missing, err)
	}
}

// TestPodDefaults pins the mutation rules beyond the goldens: init containers
// get the same defaults with their own annotation prefix, and a limit is
// never copied from a request (nor a request from a limit).
func TestPodDefaults(t *testing.T) {
	t.Parallel()
	lr := newRanger(t)
	p := pod("defaults", container("app", nil, rl("300m", "")))
	restart := corev1.ContainerRestartPolicyAlways
	init := container("setup", rl("", "64Mi"), nil)
	init.RestartPolicy = &restart
	p.Spec.InitContainers = []corev1.Container{init}
	if err := admitAndValidate(t, lr, p); err != nil {
		t.Fatalf("unexpected denial: %v", err)
	}
	want := "LimitRanger plugin set: cpu, memory request for container app; memory limit for container app; cpu request for init container setup; cpu, memory limit for init container setup"
	if got := p.Annotations[limitRangerAnnotation]; got != want {
		t.Errorf("annotation\n got %q\nwant %q", got, want)
	}
	app := p.Spec.Containers[0].Resources
	if app.Limits.Cpu().String() != "300m" || app.Requests.Cpu().String() != "100m" || app.Limits.Memory().String() != "256Mi" || app.Requests.Memory().String() != "128Mi" {
		t.Errorf("app resources: %v", app)
	}
	got := p.Spec.InitContainers[0].Resources
	if got.Requests.Memory().String() != "64Mi" || got.Limits.Memory().String() != "256Mi" || got.Requests.Cpu().String() != "100m" {
		t.Errorf("init resources: %v", got)
	}
}

func TestPodValidateMessages(t *testing.T) {
	t.Parallel()
	lr := newRanger(t)
	restart := corev1.ContainerRestartPolicyAlways
	sidecar := container("side", rl("1", "256Mi"), rl("1", "256Mi"))
	sidecar.RestartPolicy = &restart
	cases := []struct {
		name string
		pod  *corev1.Pod
		want string // "" = admitted
	}{
		{
			name: "explicit within range",
			pod:  pod("ok", container("app", rl("100m", "128Mi"), rl("200m", "256Mi"))),
		},
		{
			name: "memory under min after request-only",
			pod:  pod("mem", container("app", rl("100m", "32Mi"), nil)),
			want: `pods "mem" is forbidden: minimum memory usage per Container is 64Mi, but request is 32Mi`,
		},
		{
			name: "limit under min is reported after request",
			pod:  pod("lim", container("app", rl("50m", "64Mi"), rl("20m", "64Mi"))),
			want: `pods "lim" is forbidden: minimum cpu usage per Container is 50m, but limit is 20m`,
		},
		{
			name: "pod max counts a restartable init container",
			pod: func() *corev1.Pod {
				p := pod("side", container("a", rl("1", "256Mi"), rl("1", "256Mi")), container("b", rl("500m", "256Mi"), rl("500m", "256Mi")))
				p.Spec.InitContainers = []corev1.Container{sidecar}
				return p
			}(),
			want: `pods "side" is forbidden: maximum cpu usage per Pod is 2, but limit is 2500m`,
		},
		{
			name: "pod max takes the max over plain init containers",
			pod: func() *corev1.Pod {
				p := pod("init", container("a", rl("1", "256Mi"), rl("1", "256Mi")))
				p.Spec.InitContainers = []corev1.Container{container("i", rl("1", "256Mi"), rl("1", "256Mi"))}
				return p
			}(),
		},
		{
			name: "ratio message uses %f",
			pod:  pod("ratio", container("app", rl("100m", "128Mi"), rl("450m", "128Mi"))),
			want: `pods "ratio" is forbidden: cpu max limit to request ratio per Container is 4, but provided ratio is 4.500000`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := admitAndValidate(t, lr, tc.pod)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("want admitted, got %v", err)
			case tc.want != "" && err == nil:
				t.Fatalf("want %q, got admitted", tc.want)
			case tc.want != "" && err.Error() != tc.want:
				t.Fatalf("\n got %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

// TestPodLevelResources pins upstream's pod-level rule for the Pod min/max
// sums: spec.resources overrides the container aggregate for cpu and memory
// only (supportedPodLevelResources in upstream admission.go), while
// component-helpers' IsSupportedPodLevelResource also accepts hugepages-*.
// The oracle scenario lr-pod-level-hugepages is the server-side witness.
func TestPodLevelResources(t *testing.T) {
	t.Parallel()
	hp := corev1.ResourceName("hugepages-2Mi")
	withHuge := func(cpu, mem, huge string) corev1.ResourceList {
		out := rl(cpu, mem)
		out[hp] = resource.MustParse(huge)
		return out
	}
	podLevel := func(p *corev1.Pod, req, lim corev1.ResourceList) *corev1.Pod {
		p.Spec.Resources = &corev1.ResourceRequirements{Requests: req, Limits: lim}
		return p
	}
	shared := newRanger(t)
	huge, err := NewFromSnapshotFile(filepath.Join(oracle.Testdata(), "snapshot", "limitrange-hugepages.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		lr   *LimitRanger
		pod  *corev1.Pod
		want string // "" = admitted
	}{
		{
			name: "pod-level cpu overrides the container sum (oracle lr-pod-level-cpu)",
			lr:   shared,
			pod:  podLevel(pod("lr-pod-level-cpu", container("a", rl("1", "256Mi"), rl("1", "256Mi"))), rl("3", "512Mi"), rl("3", "512Mi")),
			want: `pods "lr-pod-level-cpu" is forbidden: maximum cpu usage per Pod is 2, but limit is 3`,
		},
		{
			name: "pod-level memory overrides the container sum",
			lr:   shared,
			pod:  podLevel(pod("mem", container("a", rl("100m", "256Mi"), rl("100m", "256Mi"))), rl("100m", "3Gi"), rl("100m", "3Gi")),
			want: `pods "mem" is forbidden: maximum memory usage per Pod is 2Gi, but limit is 3Gi`,
		},
		{
			name: "pod-level hugepages is ignored, container sum is checked (oracle lr-pod-level-hugepages)",
			lr:   huge,
			pod:  podLevel(pod("lr-pod-level-hugepages", container("a", withHuge("100m", "64Mi", "32Mi"), withHuge("100m", "64Mi", "32Mi"))), withHuge("200m", "128Mi", "128Mi"), withHuge("200m", "128Mi", "128Mi")),
		},
		{
			name: "container hugepages sum over the Pod max",
			lr:   huge,
			pod:  podLevel(pod("sum", container("a", withHuge("100m", "64Mi", "48Mi"), withHuge("100m", "64Mi", "48Mi")), container("b", withHuge("100m", "64Mi", "48Mi"), withHuge("100m", "64Mi", "48Mi"))), withHuge("200m", "128Mi", "96Mi"), withHuge("200m", "128Mi", "96Mi")),
			want: `pods "sum" is forbidden: maximum hugepages-2Mi usage per Pod is 64Mi, but limit is 96Mi`,
		},
		{
			name: "a Pod max on hugepages denies a pod without a hugepages limit",
			lr:   huge,
			pod:  pod("none", container("a", rl("100m", "128Mi"), rl("100m", "128Mi"))),
			want: `pods "none" is forbidden: maximum hugepages-2Mi usage per Pod is 64Mi.  No limit is specified`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := admitAndValidate(t, tc.lr, tc.pod)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("want admitted, got %v", err)
			case tc.want != "" && err == nil:
				t.Fatalf("want %q, got admitted", tc.want)
			case tc.want != "" && err.Error() != tc.want:
				t.Fatalf("\n got %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

// TestPodLimitsDiffersFromComponentHelpers records the drift the port must
// not inherit: component-helpers' pod-level override includes hugepages-*.
func TestPodLimitsDiffersFromComponentHelpers(t *testing.T) {
	t.Parallel()
	hp := corev1.ResourceName("hugepages-2Mi")
	p := pod("h", container("a", nil, corev1.ResourceList{hp: resource.MustParse("32Mi"), corev1.ResourceCPU: resource.MustParse("100m")}))
	p.Spec.Resources = &corev1.ResourceRequirements{Limits: corev1.ResourceList{hp: resource.MustParse("128Mi"), corev1.ResourceCPU: resource.MustParse("200m")}}
	qs := func(l corev1.ResourceList, n corev1.ResourceName) string { q := l[n]; return q.String() }
	got := podLimits(p, podResourcesOptions{PodLevelResourcesEnabled: true})
	if qs(got, hp) != "32Mi" || qs(got, corev1.ResourceCPU) != "200m" {
		t.Fatalf("port: hugepages %s (want 32Mi, container sum), cpu %s (want 200m, pod-level)", qs(got, hp), qs(got, corev1.ResourceCPU))
	}
	ch := resourcehelper.PodLimits(p, resourcehelper.PodResourcesOptions{ExcludeOverhead: true, UseStatusResources: false})
	if qs(ch, hp) != "128Mi" {
		t.Fatalf("component-helpers no longer overrides hugepages at pod level (%s); the local override may be droppable", qs(ch, hp))
	}
	off := podLimits(p, podResourcesOptions{PodLevelResourcesEnabled: false})
	if qs(off, corev1.ResourceCPU) != "100m" {
		t.Fatalf("gate off must ignore spec.resources: cpu %s", qs(off, corev1.ResourceCPU))
	}
}

func TestPVCValidate(t *testing.T) {
	t.Parallel()
	lr := newRanger(t)
	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta:   metav1.TypeMeta{Kind: "PersistentVolumeClaim", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "s4"},
		Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")},
		}},
	}
	a, err := snapshot.CreateAttributes(pvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lr.Admit(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	if err := lr.Validate(context.Background(), a, nil); err != nil {
		t.Fatalf("2Gi within [1Gi, 20Gi]: %v", err)
	}
	if pvc.Spec.Resources.Limits != nil {
		t.Fatal("PVCs are never defaulted")
	}
	// PVC update is still validated; Pod update is not (containers are immutable)
	a2 := admission.NewAttributesRecord(pvc, pvc, a.GetKind(), "s4", "c", a.GetResource(), "", admission.Update, nil, false, nil)
	if !(&DefaultLimitRangerActions{}).SupportsAttributes(a2) {
		t.Fatal("PVC update must be supported")
	}
	p := pod("u", container("app", rl("100m", "128Mi"), rl("200m", "256Mi")))
	pa, _ := snapshot.CreateAttributes(p, nil)
	pu := admission.NewAttributesRecord(p, p, pa.GetKind(), "s4", "u", pa.GetResource(), "", admission.Update, nil, false, nil)
	if (&DefaultLimitRangerActions{}).SupportsAttributes(pu) {
		t.Fatal("Pod update must be ignored")
	}
	if !strings.Contains(PluginName, "LimitRanger") {
		t.Fatal(PluginName)
	}
}
