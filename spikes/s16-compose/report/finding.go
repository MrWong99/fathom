// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package report is the finding record of design section 2.4, transcribed
// field for field so the S16 pass criterion ("no domain-model field
// changes") is a compile-time statement: the Compose findings must fit this
// struct without adding anything to it.
package report

import (
	"crypto/sha256"
	"encoding/hex"
)

// Severity is fathom's fixed three-level scale.
type Severity string

const (
	Info     Severity = "Info"
	Warning  Severity = "Warning"
	Blocking Severity = "Blocking"
)

// Lineage says how a finding was mapped back to a values pointer.
type Lineage string

const (
	LineageTargets     Lineage = "targets"
	LineageFingerprint Lineage = "fingerprint"
	LineageSentinel    Lineage = "sentinel"
	LineageNone        Lineage = "none"
)

// PolicyRef names the policy or contract a rule came from.
type PolicyRef struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Resource identifies the object a finding is about: Kubernetes manifests by
// file + apiVersion/kind/namespace/name, Compose services by file + service.
type Resource struct {
	File       string `json:"file"`
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	Service    string `json:"service,omitempty"`
}

// ID is the resource's identity for the finding hash.
func (r Resource) ID() string {
	if r.Service != "" {
		return r.File + "|" + r.Service
	}
	return r.File + "|" + r.APIVersion + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name
}

// Source is a position in a committed file (yaml.v3 line and column).
type Source struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// AdmissionContext is the identity the finding was evaluated with.
type AdmissionContext struct {
	User         string `json:"user"`
	Operation    string `json:"operation"`
	ExpandedFrom string `json:"expandedFrom,omitempty"`
}

// Fidelity is the tier and exactness of the evaluation.
type Fidelity struct {
	Tier       string   `json:"tier"`      // t0a, t0b, t2
	Exactness  string   `json:"exactness"` // exact, approximate, predicted
	Unobserved []string `json:"unobserved"`
}

// Finding is design section 2.4.
type Finding struct {
	ID               string           `json:"id"`
	Severity         Severity         `json:"severity"`
	Message          string           `json:"message"`
	Engine           string           `json:"engine"`
	RuleID           string           `json:"ruleId"`
	PolicyRef        *PolicyRef       `json:"policyRef,omitempty"`
	Resource         Resource         `json:"resource"`
	InstanceLocation string           `json:"instanceLocation"`
	KeywordLocation  string           `json:"keywordLocation"`
	ValuesPointer    string           `json:"valuesPointer,omitempty"`
	Layer            string           `json:"layer,omitempty"`
	Source           *Source          `json:"source,omitempty"`
	Lineage          Lineage          `json:"lineage"`
	AdmissionContext AdmissionContext `json:"admissionContext"`
	Fidelity         Fidelity         `json:"fidelity"`
	WouldFailAt      string           `json:"wouldFailAt"` // schema, admission, scheduling, runtime
	Side             string           `json:"side"`        // deployer, developer, platform
	SuppressedBy     string           `json:"suppressedBy,omitempty"`
	ProposedValue    any              `json:"proposedValue,omitempty"`
}

// NewID hashes the finding identity. Design section 2.2 defines it as
// (engine, ruleId, resourceId, instanceLocation); S16 showed that JSON Schema
// output units for `required` and for defaulted properties anchor at the
// parent instance (instanceLocation ""), so two missing variables in one
// service collide. keywordLocation and valuesPointer are therefore part of
// the identity as well. This is a rule change in 2.2, not a field change.
func NewID(engine, ruleID string, r Resource, instanceLocation, keywordLocation, valuesPointer string) string {
	sum := sha256.Sum256([]byte(engine + "\x00" + ruleID + "\x00" + r.ID() + "\x00" + instanceLocation + "\x00" + keywordLocation + "\x00" + valuesPointer))
	return hex.EncodeToString(sum[:8])
}
