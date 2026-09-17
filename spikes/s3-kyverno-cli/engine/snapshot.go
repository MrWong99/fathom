// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// snapshot is the side data read from SnapshotFiles.
type snapshot struct {
	configMaps  []map[string]any // v1 ConfigMap documents
	namespaces  []map[string]any // v1 Namespace documents
	others      []map[string]any // any other namespaced/cluster object (VAP params of other kinds)
	bindings    []map[string]any // RoleBinding and ClusterRoleBinding documents
	identity    *Identity
	identitySrc string
}

func loadSnapshot(files []string) (*snapshot, error) {
	s := &snapshot{}
	for _, f := range files {
		docs, _, err := yamlDocs(f)
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			switch str(d, "kind") {
			case "ConfigMap":
				s.configMaps = append(s.configMaps, d)
			case "Namespace":
				s.namespaces = append(s.namespaces, d)
			case "RoleBinding", "ClusterRoleBinding":
				s.bindings = append(s.bindings, d)
			case "SelfSubjectReview":
				ui, ok := get(d, "status", "userInfo").(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: SelfSubjectReview without status.userInfo", f)
				}
				s.identity = &Identity{Username: str(ui, "username"), Groups: strList(ui["groups"])}
				s.identitySrc = f
			case "List":
				s.others = append(s.others, list(d, "items")...)
			default:
				s.others = append(s.others, d)
			}
		}
	}
	return s, nil
}

func (s *snapshot) find(kind, namespace, name string) map[string]any {
	pools := [][]map[string]any{s.configMaps, s.namespaces, s.others}
	for _, pool := range pools {
		for _, d := range pool {
			if str(d, "kind") == kind && str(d, "metadata", "namespace") == namespace && str(d, "metadata", "name") == name {
				return d
			}
		}
	}
	return nil
}

func loadObjects(files []string) ([]Object, error) {
	var out []Object
	for _, f := range files {
		docs, raws, err := yamlDocs(f)
		if err != nil {
			return nil, err
		}
		for i, d := range docs {
			r := Resource{
				APIVersion: str(d, "apiVersion"),
				Kind:       str(d, "kind"),
				Namespace:  str(d, "metadata", "namespace"),
				Name:       str(d, "metadata", "name"),
			}
			if r.Kind == "" || r.Name == "" {
				return nil, fmt.Errorf("%s: document %d has no kind or metadata.name", f, i)
			}
			out = append(out, Object{Resource: r, Original: raws[i]})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no objects to admit")
	}
	return out, nil
}

// sideFiles is the set of generated CLI inputs.
type sideFiles struct {
	args  []string
	roles map[string]string
}

// writeSideFiles generates, from the snapshot:
//
//   - values.yaml (cli.kyverno.io/v1alpha1 Values): one
//     policies[].rules[].values.<context name> entry with the ConfigMap's
//     data and metadata for every ClusterPolicy configMap context entry the
//     snapshot can satisfy, plus namespaceSelector[] with every Namespace's
//     labels (the only way the CLI matches a namespaceSelector offline);
//   - context.yaml (cli.kyverno.io/v1alpha1 Context): spec.resources with
//     every ConfigMap, Namespace and other snapshot object, which is what
//     CEL policies (ValidatingPolicy resource.get/list) read offline;
//   - userinfo.yaml (cli.kyverno.io/v1alpha1 UserInfo) from the identity and
//     the bindings that name it (Kyverno's own RoleBinding -> "<ns>:<role>",
//     ClusterRole ref -> clusterRoles rule);
//   - param-<ns>-<name>.yaml for every VAP binding paramRef found in the
//     snapshot, passed with --parameter-resource.
//
// A ConfigMap context or VAP parameter the snapshot lacks is left out on
// purpose: the CLI then reports result error for that rule or policy and
// the adapter turns that into a tool error rather than a clean verdict. A
// MAP parameter the snapshot lacks is refused here instead, because the CLI
// silently evaluates nothing for it.
func writeSideFiles(dir string, pol *policySet, snap *snapshot) (*sideFiles, error) {
	sf := &sideFiles{roles: map[string]string{}}
	write := func(role, name string, doc any) (string, error) {
		b, err := yaml.Marshal(doc)
		if err != nil {
			return "", err
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			return "", err
		}
		sf.roles[role] = p
		return p, nil
	}

	// values.yaml
	type ruleValues struct {
		Name   string         `json:"name"`
		Values map[string]any `json:"values"`
	}
	type policyValues struct {
		Name  string       `json:"name"`
		Rules []ruleValues `json:"rules"`
	}
	byPolicy := map[string]*policyValues{}
	var policyOrder []string
	for _, c := range pol.configMapContexts {
		cm := snap.find("ConfigMap", c.namespace, c.cmName)
		if cm == nil {
			continue
		}
		pv := byPolicy[c.policy]
		if pv == nil {
			pv = &policyValues{Name: c.policy}
			byPolicy[c.policy] = pv
			policyOrder = append(policyOrder, c.policy)
		}
		var rv *ruleValues
		for i := range pv.Rules {
			if pv.Rules[i].Name == c.rule {
				rv = &pv.Rules[i]
			}
		}
		if rv == nil {
			pv.Rules = append(pv.Rules, ruleValues{Name: c.rule, Values: map[string]any{}})
			rv = &pv.Rules[len(pv.Rules)-1]
		}
		rv.Values[c.name] = map[string]any{
			"data":     cm["data"],
			"metadata": map[string]any{"name": c.cmName, "namespace": c.namespace},
		}
	}
	var nsSel []map[string]any
	for _, ns := range snap.namespaces {
		labels, _ := get(ns, "metadata", "labels").(map[string]any)
		nsSel = append(nsSel, map[string]any{"name": str(ns, "metadata", "name"), "labels": labels})
	}
	values := map[string]any{
		"apiVersion": "cli.kyverno.io/v1alpha1",
		"kind":       "Values",
		"metadata":   map[string]any{"name": "fathom-snapshot"},
	}
	if len(policyOrder) > 0 {
		var pvs []policyValues
		for _, n := range policyOrder {
			pvs = append(pvs, *byPolicy[n])
		}
		values["policies"] = pvs
	}
	if len(nsSel) > 0 {
		values["namespaceSelector"] = nsSel
	}
	if len(policyOrder) > 0 || len(nsSel) > 0 {
		p, err := write("values", "values.yaml", values)
		if err != nil {
			return nil, err
		}
		sf.args = append(sf.args, "--values-file", p)
	}

	// context.yaml
	var resources []map[string]any
	resources = append(resources, snap.configMaps...)
	resources = append(resources, snap.namespaces...)
	resources = append(resources, snap.others...)
	if len(resources) > 0 {
		ctxDoc := map[string]any{
			"apiVersion": "cli.kyverno.io/v1alpha1",
			"kind":       "Context",
			"metadata":   map[string]any{"name": "fathom-snapshot"},
			"spec":       map[string]any{"resources": resources},
		}
		p, err := write("context", "context.yaml", ctxDoc)
		if err != nil {
			return nil, err
		}
		sf.args = append(sf.args, "--context-file", p)
	}

	// userinfo.yaml
	if snap.identity != nil {
		roles, clusterRoles := rolesFor(snap.identity, snap.bindings)
		ui := map[string]any{
			"apiVersion":   "cli.kyverno.io/v1alpha1",
			"kind":         "UserInfo",
			"metadata":     map[string]any{"name": strings.ReplaceAll(snap.identity.Username, ":", "-")},
			"roles":        roles,
			"clusterRoles": clusterRoles,
			"userInfo": map[string]any{
				"username": snap.identity.Username,
				"groups":   snap.identity.Groups,
			},
		}
		p, err := write("userinfo", "userinfo.yaml", ui)
		if err != nil {
			return nil, err
		}
		sf.args = append(sf.args, "--userinfo", p)
	}

	// parameter resources
	for _, pr := range pol.params {
		obj := snap.find(pr.kind, pr.namespace, pr.name)
		if obj == nil {
			if pr.mutating {
				return nil, fmt.Errorf("engine: MutatingAdmissionPolicy %s paramRef %s %s/%s is not in the snapshot; kyverno CLI would silently apply nothing", pr.policy, pr.kind, pr.namespace, pr.name)
			}
			continue
		}
		role := "parameter:" + pr.namespace + "/" + pr.name
		p, err := write(role, "param-"+strings.ToLower(pr.kind)+"-"+pr.namespace+"-"+pr.name+".yaml", obj)
		if err != nil {
			return nil, err
		}
		sf.args = append(sf.args, "--parameter-resource", p)
	}
	return sf, nil
}

// rolesFor applies Kyverno's binding resolution: a RoleBinding whose
// subjects name the identity contributes "<namespace>:<role>" when it
// references a Role and the ClusterRole name when it references a
// ClusterRole; a ClusterRoleBinding contributes its ClusterRole name.
func rolesFor(id *Identity, bindings []map[string]any) (roles, clusterRoles []string) {
	roles, clusterRoles = []string{}, []string{}
	for _, b := range bindings {
		ns := str(b, "metadata", "namespace")
		if !subjectsMatch(id, list(b, "subjects"), ns) {
			continue
		}
		refKind, refName := str(b, "roleRef", "kind"), str(b, "roleRef", "name")
		switch {
		case str(b, "kind") == "ClusterRoleBinding":
			clusterRoles = append(clusterRoles, refName)
		case refKind == "ClusterRole":
			clusterRoles = append(clusterRoles, refName)
		default:
			roles = append(roles, ns+":"+refName)
		}
	}
	sort.Strings(roles)
	sort.Strings(clusterRoles)
	return roles, clusterRoles
}

func subjectsMatch(id *Identity, subjects []map[string]any, bindingNS string) bool {
	for _, s := range subjects {
		name := str(s, "name")
		switch str(s, "kind") {
		case "User":
			if name == id.Username {
				return true
			}
		case "Group":
			for _, g := range id.Groups {
				if g == name {
					return true
				}
			}
		case "ServiceAccount":
			ns := str(s, "namespace")
			if ns == "" {
				ns = bindingNS
			}
			if id.Username == "system:serviceaccount:"+ns+":"+name {
				return true
			}
		}
	}
	return false
}
