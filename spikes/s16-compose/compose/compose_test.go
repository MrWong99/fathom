// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MrWong99/fathom/spikes/s16-compose/report"
)

var update = flag.Bool("update", false, "rewrite the findings golden file")

const shop = "../testdata/shop"

// dotenv is what the fixture's .env provides; Load reads the file itself,
// this copy is the "deployer mapping" the findings are computed against.
var dotenv = map[string]string{
	"REGISTRY":    "registry.eu-prod.example",
	"DB_PASSWORD": "ref://vault/shop/eu-prod#db",
	"DATA_DIR":    "/srv/shop/data",
	"LOG_LEVEL":   "warn",
}

func load(t *testing.T, env map[string]string) *Result {
	t.Helper()
	res, err := Load(context.Background(), shop, env, []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func withTag(env map[string]string) map[string]string {
	out := map[string]string{"API_TAG": "1.4.2"}
	for k, v := range env {
		out[k] = v
	}
	return out
}

func TestPassOneSurvivesUninterpolatedFields(t *testing.T) {
	t.Parallel()
	res := load(t, dotenv)
	if len(res.Files) != 2 || filepath.Base(res.Files[0]) != "compose.yaml" || filepath.Base(res.Files[1]) != "compose.override.yaml" {
		t.Errorf("config files %v, want compose.yaml + compose.override.yaml discovered", res.Files)
	}
	svcs := res.Model["services"].(map[string]any)
	api := svcs["api"].(map[string]any)
	if img, _ := api["image"].(string); !strings.Contains(img, "${API_TAG:?") {
		t.Errorf("pass 1 interpolated the image: %q", img)
	}
	if _, ok := svcs["metrics"]; !ok {
		t.Errorf("include was not folded into the model: %v", sortedKeys(svcs))
	}
	names := make([]string, 0, len(res.Variables))
	for n := range res.Variables {
		names = append(names, n)
	}
	sort.Strings(names)
	// API_PORT is used only in the `ports` list that compose.override.yaml
	// replaces with `!override`, so the merged model no longer references
	// it: the variable set follows compose's merge semantics, not the raw
	// files.
	want := []string{"API_TAG", "DATA_DIR", "DB_PASSWORD", "DEBUG", "LOG_LEVEL", "METRICS_PORT", "NODE_EXPORTER_TAG", "QUEUE_NAME", "REGISTRY", "SMTP_HOST"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("variables %v, want %v", names, want)
	}
	if _, raw := res.Usages["API_PORT"]; !raw {
		t.Errorf("the raw usage of API_PORT should still be recorded for the editor")
	}
	if v := res.Variables["API_TAG"]; !v.Required {
		t.Errorf("API_TAG should be required (:?)")
	}
	if v := res.Variables["REGISTRY"]; v.DefaultValue != "docker.io" || v.Required {
		t.Errorf("REGISTRY = %+v", v)
	}
	if u := res.Usages["API_TAG"]; len(u) != 2 || u[0].Service != "api" || u[1].Service != "worker" || u[0].Line != 12 {
		t.Errorf("API_TAG usages %+v", u)
	}
	if u := res.Usages["METRICS_PORT"]; len(u) != 1 || u[0].File != "compose.monitoring.yaml" || u[0].Service != "metrics" {
		t.Errorf("included file usages %+v", u)
	}
	if u := res.Usages["DEBUG"]; len(u) != 1 || u[0].File != "compose.override.yaml" {
		t.Errorf("override file usages %+v", u)
	}
}

func TestPassTwoFailsExactlyLikeCompose(t *testing.T) {
	t.Parallel()
	res := load(t, dotenv)
	if res.LoadError == nil {
		t.Fatal("pass 2 should fail: API_TAG is required and unset")
	}
	if !strings.Contains(res.LoadError.Error(), "required variable API_TAG is missing a value") {
		t.Errorf("unexpected pass-2 error: %v", res.LoadError)
	}
	t.Logf("pass 2 (compose's own verdict): %v", res.LoadError)

	ok := load(t, withTag(dotenv))
	if ok.LoadError != nil {
		t.Fatalf("pass 2 with API_TAG: %v", ok.LoadError)
	}
	if img := ok.Project.Services["api"].Image; img != "registry.eu-prod.example/shop/api:1.4.2" {
		t.Errorf("image = %q", img)
	}
	if got := ok.Project.Services["api"].Ports; len(got) != 1 || got[0].Published != "18080" {
		t.Errorf("override !override ports not applied: %+v", got)
	}
	if _, ok := ok.Project.Services["worker"]; !ok {
		t.Errorf("profile * should include worker")
	}
	if _, ok := ok.Project.Services["metrics"]; !ok {
		t.Errorf("included metrics service missing")
	}
}

func TestVariableSchemaIs2020_12(t *testing.T) {
	t.Parallel()
	res := load(t, dotenv)
	b, err := res.VariableSchema()
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", s["$schema"])
	}
	req, _ := s["required"].([]any)
	if len(req) != 3 || req[0] != "API_TAG" || req[1] != "DATA_DIR" || req[2] != "DB_PASSWORD" {
		t.Errorf("required = %v", req)
	}
	props := s["properties"].(map[string]any)
	if props["REGISTRY"].(map[string]any)["default"] != "docker.io" {
		t.Errorf("REGISTRY default missing")
	}
	if props["DB_PASSWORD"].(map[string]any)["x-fathom-secret"] != true {
		t.Errorf("DB_PASSWORD should be marked x-fathom-secret")
	}
	t.Logf("schema: %d variables, %d required, %d bytes", len(props), len(req), len(b))
}

func TestThreeFindingsInReportShape(t *testing.T) {
	t.Parallel()
	res := load(t, dotenv)
	required, err := res.ValidateEnv(dotenv)
	if err != nil {
		t.Fatal(err)
	}
	// Pass 2 needs API_TAG for the deploy findings; the strict-interpolation
	// findings come from pass 1 and the deployer mapping.
	full := load(t, withTag(dotenv))
	all := append(required, full.Findings(dotenv, []string{"x-fathom-*"}, false)...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].RuleID != all[j].RuleID {
			return all[i].RuleID < all[j].RuleID
		}
		if all[i].Resource.Service != all[j].Resource.Service {
			return all[i].Resource.Service < all[j].Resource.Service
		}
		return all[i].InstanceLocation+all[i].ValuesPointer < all[j].InstanceLocation+all[j].ValuesPointer
	})

	byRule := map[string][]report.Finding{}
	for _, f := range all {
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}
	want := map[string]int{
		"variable-required-unset": 2, // API_TAG in api and worker (DATA_DIR and DB_PASSWORD are set)
		"variable-unset-empty":    1, // SMTP_HOST
		"variable-default-used":   4, // DEBUG, METRICS_PORT, NODE_EXPORTER_TAG, QUEUE_NAME (API_PORT is overridden away)
		"extension-unknown":       1, // x-depends-on (x-fathom-* allowed)
		"deploy-swarm-only":       4, // api placement + update_config, worker mode + endpoint_mode
	}
	for rule, n := range want {
		if len(byRule[rule]) != n {
			t.Errorf("%s: %d findings, want %d: %s", rule, len(byRule[rule]), n, describeAll(byRule[rule]))
		}
	}
	// 2020-12 output unit for the required variable, pointing at the usage
	req := byRule["variable-required-unset"][0]
	if req.InstanceLocation != "" || req.KeywordLocation != "/required" || req.ValuesPointer != "/API_TAG" || req.Severity != report.Blocking || req.WouldFailAt != "schema" {
		t.Errorf("required finding shape: %+v", req)
	}
	if req.Source == nil || req.Source.File != "compose.yaml" || req.Source.Line != 12 || req.Resource.Service != "api" {
		t.Errorf("required finding source/resource: %+v %+v", req.Source, req.Resource)
	}
	ext := byRule["extension-unknown"][0]
	if ext.InstanceLocation != "/x-depends-on" || ext.Resource.Service != "" || ext.Severity != report.Info {
		t.Errorf("extension finding: %+v", ext)
	}
	dep := byRule["deploy-swarm-only"]
	if len(dep) == 4 && (dep[0].InstanceLocation != "/services/api/deploy/placement" || dep[1].InstanceLocation != "/services/api/deploy/update_config") {
		t.Errorf("deploy findings: %s", describeAll(dep))
	}
	if len(dep) > 0 && dep[0].Fidelity.Unobserved[0] != "host:engine" {
		t.Errorf("deploy finding should record the missing host snapshot: %+v", dep[0].Fidelity)
	}
	for _, f := range dep {
		if strings.HasSuffix(f.InstanceLocation, "/replicas") || strings.HasSuffix(f.InstanceLocation, "/resources") {
			t.Errorf("honoured deploy key flagged: %s", f.InstanceLocation)
		}
	}
	// Identity: every finding id is unique and derived from the four parts.
	ids := map[string]bool{}
	for _, f := range all {
		if ids[f.ID] {
			t.Errorf("duplicate finding id %s (%s %s)", f.ID, f.RuleID, f.InstanceLocation)
		}
		ids[f.ID] = true
		if f.ID != report.NewID(f.Engine, f.RuleID, f.Resource, f.InstanceLocation, f.KeywordLocation, f.ValuesPointer) {
			t.Errorf("id not derived from (engine, ruleId, resource, instanceLocation, keywordLocation, valuesPointer): %+v", f)
		}
	}

	// The pass criterion: the findings serialise with exactly the section
	// 2.4 fields and no others. The golden file is the record.
	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden := "../testdata/findings.golden.json"
	if *update {
		if err := os.WriteFile(golden, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want2, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(out), bytes.TrimSpace(want2)) {
		t.Errorf("findings differ from golden; rerun with -update if intended\n%s", out)
	}
	t.Logf("%d findings: %s", len(all), summarise(byRule))
}

// TestFindingFieldsAreSection24 pins the record: any field added to
// report.Finding to make Compose fit would change this list.
func TestFindingFieldsAreSection24(t *testing.T) {
	t.Parallel()
	var names []string
	rt := reflect.TypeOf(report.Finding{})
	for i := 0; i < rt.NumField(); i++ {
		names = append(names, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
	}
	want := []string{"id", "severity", "message", "engine", "ruleId", "policyRef", "resource", "instanceLocation", "keywordLocation",
		"valuesPointer", "layer", "source", "lineage", "admissionContext", "fidelity", "wouldFailAt", "side", "suppressedBy", "proposedValue"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Finding fields changed:\n got %v\nwant %v", names, want)
	}
	res := reflect.TypeOf(report.Resource{})
	var rnames []string
	for i := 0; i < res.NumField(); i++ {
		rnames = append(rnames, strings.Split(res.Field(i).Tag.Get("json"), ",")[0])
	}
	if !reflect.DeepEqual(rnames, []string{"file", "apiVersion", "kind", "namespace", "name", "service"}) {
		t.Errorf("Resource fields changed: %v", rnames)
	}
}

func TestSwarmHostSuppressesDeployFindings(t *testing.T) {
	t.Parallel()
	env := withTag(dotenv)
	res := load(t, env)
	for _, f := range res.Findings(env, []string{"x-fathom-*"}, true) {
		if f.RuleID == "deploy-swarm-only" {
			t.Errorf("swarm host should not flag deploy keys: %s", f.InstanceLocation)
		}
	}
}

func describeAll(fs []report.Finding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Resource.Service+":"+f.InstanceLocation+f.ValuesPointer)
	}
	return strings.Join(parts, ", ")
}

func summarise(byRule map[string][]report.Finding) string {
	var rules []string
	for k := range byRule {
		rules = append(rules, k)
	}
	sort.Strings(rules)
	var parts []string
	for _, r := range rules {
		parts = append(parts, r+"="+itoa(len(byRule[r])))
	}
	return strings.Join(parts, " ")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
