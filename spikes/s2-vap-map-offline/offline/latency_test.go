// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"context"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apiserver/pkg/cel/environment"
)

// hardwareLine describes the machine a latency run happened on so RESULT.md
// can quote it: CPU model and cpufreq governor (Linux, best effort), logical
// CPUs, GOMAXPROCS, Go version and GOGC.
func hardwareLine() string {
	model, governor := "unknown", "unknown"
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "model name") {
				if _, v, ok := strings.Cut(l, ":"); ok {
					model = strings.TrimSpace(v)
				}
				break
			}
		}
	}
	if b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"); err == nil {
		governor = strings.TrimSpace(string(b))
	}
	gogc := os.Getenv("GOGC")
	if gogc == "" {
		gogc = "default(100)"
	}
	return "cpu=" + model + " governor=" + governor +
		" numcpu=" + strconv.Itoa(runtime.NumCPU()) + " gomaxprocs=" + strconv.Itoa(runtime.GOMAXPROCS(0)) +
		" go=" + runtime.Version() + " gogc=" + gogc
}

func defaultCompatibilityVersionString() string {
	return environment.DefaultCompatibilityVersion().String()
}

// Latency holds per-object timings of one evaluation loop.
type Latency struct {
	Iterations int
	Median     time.Duration
	P95        time.Duration
	Min        time.Duration
	Max        time.Duration
}

// Measure times fn n times (compiled policies warm) and reports the median
// and 95th percentile.
func Measure(n int, fn func()) Latency {
	d := make([]time.Duration, n)
	for i := range d {
		start := time.Now()
		fn()
		d[i] = time.Since(start)
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return Latency{
		Iterations: n,
		Median:     d[n/2],
		P95:        d[(n*95)/100],
		Min:        d[0],
		Max:        d[n-1],
	}
}

// TestLatency records per-object latency for one VAP object (vap-deny-pass:
// match condition, two variables and all three validations evaluate, params
// and authorizer bound) and one MAP object (map-reinvoke: two
// ApplyConfiguration policies plus the reinvocation pass), 200 iterations
// each with compiled policies warm. Numbers land in RESULT.md; nothing is
// asserted except that the verdicts stay the same across iterations.
func TestLatency(t *testing.T) {
	eval, _, man := shared(t)
	ctx := context.Background()
	const n = 200
	t.Logf("machine: %s", hardwareLine())
	for _, name := range []string{"vap-deny-pass", "map-reinvoke"} {
		var sc Scenario
		for _, s := range man.Scenarios {
			if s.Name == name {
				sc = s
			}
		}
		req := loadRequest(t, sc)
		// warm-up: first evaluation builds the type converter and CEL programs' caches
		first := eval.Admit(ctx, req.Object)
		lat := Measure(n, func() {
			res := eval.Admit(ctx, req.Object)
			if res.Allowed() != first.Allowed() {
				t.Fatalf("%s: verdict changed between iterations", name)
			}
		})
		t.Logf("latency %-14s iterations=%d median=%s p95=%s min=%s max=%s", name, lat.Iterations, lat.Median, lat.P95, lat.Min, lat.Max)
	}
}
