// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package compose is the S16 spike: compose-go two-pass load of a Compose
// project, ExtractVariables into a 2020-12 values.schema.json over the
// interpolation variables, and three findings in the report shape of design
// section 2.4 with {file, service} identity.
package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"gopkg.in/yaml.v3"

	"github.com/MrWong99/fathom/spikes/s16-compose/report"
)

// Usage is one `${VAR…}` occurrence with its position and owning service.
type Usage struct {
	File    string `json:"file"`
	Service string `json:"service,omitempty"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
}

// Result holds both passes.
type Result struct {
	Dir   string
	Files []string // config files in load order, relative to Dir
	// Pass 1: raw model with interpolation, validation, consistency and
	// environment resolution skipped, so ${VAR} strings survive in typed
	// fields.
	Model     map[string]any
	Variables map[string]template.Variable
	Usages    map[string][]Usage
	// Pass 2: the fully loaded project, or the error compose itself would
	// raise (a required variable missing, a schema violation).
	Project   *types.Project
	LoadError error
}

// Load runs both passes. env is the deployer's mapping (from .env plus the
// UI); the process environment is never consulted, which keeps the pipeline
// pure. profiles selects services for pass 2 ("*" for all).
func Load(ctx context.Context, dir string, env map[string]string, profiles []string) (*Result, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	sort.Strings(envSlice)

	common := []cli.ProjectOptionsFn{
		cli.WithWorkingDirectory(dir),
		cli.WithEnv(envSlice),
		cli.WithDotEnv,
		cli.WithConfigFileEnv,
		cli.WithDefaultConfigPath,
	}

	// Pass 1
	raw, err := cli.NewProjectOptions(nil, append(common, cli.WithLoadOptions(func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipValidation = true
		o.SkipConsistencyCheck = true
		o.SkipResolveEnvironment = true
	}))...)
	if err != nil {
		return nil, err
	}
	model, err := raw.LoadModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}
	res := &Result{Dir: dir, Model: model, Variables: template.ExtractVariables(model, nil)}
	for _, p := range raw.ConfigPaths {
		rel, _ := filepath.Rel(dir, p)
		res.Files = append(res.Files, rel)
	}
	res.Usages, err = scanUsages(dir, res.Files)
	if err != nil {
		return nil, err
	}

	// Pass 2
	full, err := cli.NewProjectOptions(nil, append(common, cli.WithProfiles(profiles))...)
	if err != nil {
		return nil, err
	}
	res.Project, res.LoadError = full.LoadProject(ctx)
	return res, nil
}

// includedFiles returns the `include:` entries of one raw config file. The
// loader removes the key from the model after folding the files in, so the
// raw file is the only place left to read it.
func includedFiles(src []byte) []string {
	var doc struct {
		Include []any `yaml:"include"`
	}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil
	}
	var out []string
	for _, i := range doc.Include {
		switch v := i.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			switch p := v["path"].(type) {
			case string:
				out = append(out, p)
			case []any:
				for _, s := range p {
					out = append(out, fmt.Sprint(s))
				}
			}
		}
	}
	return out
}

var usagePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)

// scanUsages records the position of every ${VAR occurrence and the service
// whose block contains it (via yaml.v3 node lines), following `include:`
// entries. Positions come from the raw files, so a usage that an override
// later replaces (`!override`) is still listed; the variable set itself comes
// from the merged model.
func scanUsages(dir string, files []string) (map[string][]Usage, error) {
	usages := map[string][]Usage{}
	seen := map[string]bool{}
	queue := append([]string{}, files...)
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if seen[f] {
			continue
		}
		seen[f] = true
		src, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return nil, err
		}
		for _, inc := range includedFiles(src) {
			queue = append(queue, filepath.Join(filepath.Dir(f), inc))
		}
		ranges := serviceRanges(src)
		for i, line := range strings.Split(string(src), "\n") {
			for _, m := range usagePattern.FindAllStringSubmatchIndex(line, -1) {
				name := line[m[2]:m[3]]
				u := Usage{File: f, Line: i + 1, Col: m[0] + 1}
				for _, r := range ranges {
					if u.Line >= r.from && u.Line <= r.to {
						u.Service = r.name
					}
				}
				usages[name] = append(usages[name], u)
			}
		}
	}
	return usages, nil
}

type lineRange struct {
	name     string
	from, to int
}

func serviceRanges(src []byte) []lineRange {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	var out []lineRange
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "services" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		svcs := root.Content[i+1]
		for j := 0; j+1 < len(svcs.Content); j += 2 {
			r := lineRange{name: svcs.Content[j].Value, from: svcs.Content[j].Line, to: 1 << 30}
			if j+2 < len(svcs.Content) {
				r.to = svcs.Content[j+2].Line - 1
			} else if i+2 < len(root.Content) {
				r.to = root.Content[i+2].Line - 1
			}
			out = append(out, r)
		}
	}
	return out
}

// VariableSchema emits the contract over the variable set: every variable a
// string property, `:?` variables required, `:-` defaults carried as
// `default`, usages recorded under x-fathom-compose for the form and for
// back-mapping.
func (r *Result) VariableSchema() ([]byte, error) {
	props := map[string]any{}
	var required []string
	names := make([]string, 0, len(r.Variables))
	for n := range r.Variables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := r.Variables[n]
		p := map[string]any{"type": "string", "x-fathom-persona": "deployer"}
		if v.DefaultValue != "" {
			p["default"] = v.DefaultValue
		}
		if v.Required {
			required = append(required, n)
		}
		up := strings.ToUpper(n)
		if strings.Contains(up, "PASSWORD") || strings.Contains(up, "SECRET") || strings.Contains(up, "TOKEN") {
			p["x-fathom-secret"] = true
		}
		p["x-fathom-compose"] = map[string]any{"usages": r.Usages[n]}
		props[n] = p
	}
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"title":                "compose variables",
		"type":                 "object",
		"properties":           props,
		"additionalProperties": true,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return json.MarshalIndent(schema, "", "  ")
}

// ValidateEnv validates the deployer's mapping against the variable schema
// and returns the 2020-12 output units as findings (one per missing required
// variable and usage service).
func (r *Result) ValidateEnv(env map[string]string) ([]report.Finding, error) {
	schemaBytes, err := r.VariableSchema()
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("file:///values.schema.json", doc); err != nil {
		return nil, err
	}
	sch, err := c.Compile("file:///values.schema.json")
	if err != nil {
		return nil, err
	}
	inst := map[string]any{}
	for k, v := range env {
		inst[k] = v
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil, nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return nil, err
	}
	var out []report.Finding
	var walk func(e *jsonschema.ValidationError, unit *jsonschema.OutputUnit)
	walk = func(e *jsonschema.ValidationError, unit *jsonschema.OutputUnit) {
		if req, ok := e.ErrorKind.(*kind.Required); ok {
			for _, name := range req.Missing {
				for _, u := range r.usagesOrProject(name) {
					res := report.Resource{File: u.File, Service: u.Service}
					out = append(out, report.Finding{
						ID:               report.NewID("compose-contract", "variable-required-unset", res, unit.InstanceLocation, unit.KeywordLocation, "/"+name),
						Severity:         report.Blocking,
						Message:          fmt.Sprintf("required variable %s is not set; %s will fail at `docker compose config`", name, describe(res)),
						Engine:           "compose-contract",
						RuleID:           "variable-required-unset",
						PolicyRef:        &report.PolicyRef{Name: "values.schema.json"},
						Resource:         res,
						InstanceLocation: unit.InstanceLocation,
						KeywordLocation:  unit.KeywordLocation,
						ValuesPointer:    "/" + name,
						Layer:            ".env",
						Source:           &report.Source{File: u.File, Line: u.Line, Col: u.Col},
						Lineage:          report.LineageTargets,
						AdmissionContext: report.AdmissionContext{User: "docker compose", Operation: "config"},
						Fidelity:         report.Fidelity{Tier: "t0a", Exactness: "exact", Unobserved: []string{}},
						WouldFailAt:      "schema",
						Side:             "deployer",
					})
				}
			}
		}
		for i, c := range e.Causes {
			if i < len(unit.Errors) {
				walk(c, &unit.Errors[i])
			}
		}
	}
	walk(verr, verr.DetailedOutput())
	return out, nil
}

func (r *Result) usagesOrProject(name string) []Usage {
	if u := r.Usages[name]; len(u) > 0 {
		return u
	}
	return []Usage{{File: r.Files[0]}}
}

func describe(res report.Resource) string {
	if res.Service != "" {
		return "service " + res.Service + " in " + res.File
	}
	return res.File
}

// Findings computes the three S16 findings from both passes:
//  1. strict interpolation over the variable set (required unset comes from
//     ValidateEnv as a schema output unit; unset-without-default and
//     defaulted are produced here, where compose itself stays silent),
//  2. unknown x- extension keys at project and service level,
//  3. deploy.* keys that docker compose ignores outside Swarm.
func (r *Result) Findings(env map[string]string, allowedExtensions []string, swarm bool) []report.Finding {
	var out []report.Finding
	names := make([]string, 0, len(r.Variables))
	for n := range r.Variables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := r.Variables[n]
		if _, set := env[n]; set || v.Required {
			continue
		}
		for _, u := range r.usagesOrProject(n) {
			res := report.Resource{File: u.File, Service: u.Service}
			f := report.Finding{
				Engine:           "compose-interpolation",
				Resource:         res,
				InstanceLocation: "",
				KeywordLocation:  "/properties/" + n,
				ValuesPointer:    "/" + n,
				Layer:            ".env",
				Source:           &report.Source{File: u.File, Line: u.Line, Col: u.Col},
				Lineage:          report.LineageTargets,
				AdmissionContext: report.AdmissionContext{User: "docker compose", Operation: "config"},
				Fidelity:         report.Fidelity{Tier: "t0a", Exactness: "exact", Unobserved: []string{}},
				WouldFailAt:      "runtime",
				Side:             "deployer",
			}
			if v.DefaultValue != "" {
				f.RuleID, f.Severity = "variable-default-used", report.Warning
				f.Message = fmt.Sprintf("%s is not set; %s uses the default %q", n, describe(res), v.DefaultValue)
				f.ProposedValue = v.DefaultValue
			} else {
				f.RuleID, f.Severity = "variable-unset-empty", report.Blocking
				f.Message = fmt.Sprintf("%s is not set and has no default; docker compose substitutes an empty string in %s", n, describe(res))
			}
			f.ID = report.NewID(f.Engine, f.RuleID, res, f.InstanceLocation, f.KeywordLocation, f.ValuesPointer)
			out = append(out, f)
		}
	}

	allowed := func(k string) bool {
		for _, a := range allowedExtensions {
			if k == a || (strings.HasSuffix(a, "*") && strings.HasPrefix(k, strings.TrimSuffix(a, "*"))) {
				return true
			}
		}
		return false
	}
	file := r.Files[0]
	extension := func(ptr string, key string, res report.Resource) {
		if !strings.HasPrefix(key, "x-") || allowed(key) {
			return
		}
		f := report.Finding{
			Severity:         report.Info,
			Message:          fmt.Sprintf("extension key %s is not in the contract's allow-list; docker compose ignores it", key),
			Engine:           "compose-contract",
			RuleID:           "extension-unknown",
			PolicyRef:        &report.PolicyRef{Name: "values.schema.json"},
			Resource:         res,
			InstanceLocation: ptr + "/" + key,
			KeywordLocation:  "/x-fathom-extensions",
			Lineage:          report.LineageNone,
			AdmissionContext: report.AdmissionContext{User: "docker compose", Operation: "config"},
			Fidelity:         report.Fidelity{Tier: "t0a", Exactness: "exact", Unobserved: []string{}},
			WouldFailAt:      "runtime",
			Side:             "developer",
		}
		f.ID = report.NewID(f.Engine, f.RuleID, res, f.InstanceLocation, f.KeywordLocation, f.ValuesPointer)
		out = append(out, f)
	}
	for _, k := range sortedKeys(r.Model) {
		extension("", k, report.Resource{File: file})
	}
	if svcs, ok := r.Model["services"].(map[string]any); ok {
		for _, name := range sortedKeys(svcs) {
			if svc, ok := svcs[name].(map[string]any); ok {
				for _, k := range sortedKeys(svc) {
					extension("/services/"+name, k, report.Resource{File: file, Service: name})
				}
			}
		}
	}

	if r.Project != nil && !swarm {
		for _, name := range r.Project.ServiceNames() {
			svc := r.Project.Services[name]
			if svc.Deploy == nil {
				continue
			}
			d := svc.Deploy
			var ignored []string
			if d.Mode != "" {
				ignored = append(ignored, "mode")
			}
			if len(d.Placement.Constraints) > 0 || len(d.Placement.Preferences) > 0 || d.Placement.MaxReplicas > 0 {
				ignored = append(ignored, "placement")
			}
			if d.UpdateConfig != nil {
				ignored = append(ignored, "update_config")
			}
			if d.RollbackConfig != nil {
				ignored = append(ignored, "rollback_config")
			}
			if d.EndpointMode != "" {
				ignored = append(ignored, "endpoint_mode")
			}
			if len(d.Labels) > 0 {
				ignored = append(ignored, "labels")
			}
			for _, k := range ignored {
				res := report.Resource{File: file, Service: name}
				f := report.Finding{
					Severity:         report.Warning,
					Message:          fmt.Sprintf("deploy.%s on service %s is only honoured by Swarm; this host runs docker compose", k, name),
					Engine:           "compose-host",
					RuleID:           "deploy-swarm-only",
					Resource:         res,
					InstanceLocation: "/services/" + name + "/deploy/" + k,
					KeywordLocation:  "/x-fathom-host/swarm",
					Lineage:          report.LineageNone,
					AdmissionContext: report.AdmissionContext{User: "docker compose", Operation: "up"},
					Fidelity:         report.Fidelity{Tier: "t0a", Exactness: "exact", Unobserved: []string{"host:engine"}},
					WouldFailAt:      "runtime",
					Side:             "developer",
				}
				f.ID = report.NewID(f.Engine, f.RuleID, res, f.InstanceLocation, f.KeywordLocation, f.ValuesPointer)
				out = append(out, f)
			}
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
