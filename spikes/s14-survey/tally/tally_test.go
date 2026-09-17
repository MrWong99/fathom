// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package tally

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T) []Response {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "testdata", "responses.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rs, err := ReadCSV(f)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestReadCSV(t *testing.T) {
	t.Parallel()
	rs := load(t)
	if len(rs) != 5 {
		t.Fatalf("want 5 responses, got %d", len(rs))
	}
	if rs[0]["A1"] != "Consultant" || rs[3]["A1"] != "Developer" {
		t.Errorf("roles not read: %q %q", rs[0]["A1"], rs[3]["A1"])
	}
}

func TestBacklogOrder(t *testing.T) {
	t.Parallel()
	order := BacklogOrder(load(t))
	if order[0].Kind != 10 {
		t.Errorf("missing reference (10) should rank first, got kind %d", order[0].Kind)
	}
	// kind 10: r1 monthly(2)*runtime(3) + r2 weekly(4)*runtime(3) + r3 daily(8)*customer(4) = 50;
	// named first by r2 and r3 in B3.
	if order[0].Score != 50 || order[0].TopThree != 2 {
		t.Errorf("kind 10 score/topthree = %.1f/%d, want 50/2", order[0].Score, order[0].TopThree)
	}
	byKind := map[int]KindScore{}
	for _, s := range order {
		byKind[s.Kind] = s
	}
	// kind 8: r1 weekly(4)*runtime(3) + r2 monthly(2)*runtime(3) + r3 once(1)*runtime(3) = 21
	if byKind[8].Score != 21 {
		t.Errorf("ResourceQuota score = %.1f, want 21", byKind[8].Score)
	}
	// kind 1: r1 once(1)*before pushing(0.5) + r4 monthly(2)*before pushing(0.5) = 1.5
	if byKind[1].Score != 1.5 {
		t.Errorf("YAML score = %.1f, want 1.5", byKind[1].Score)
	}
	if byKind[14].Score != 2 {
		t.Errorf("opaque webhook score = %.1f, want 2", byKind[14].Score)
	}
}

func TestWeightsTolerateLabelVariants(t *testing.T) {
	t.Parallel()
	cases := map[string]float64{
		"Never": 0, "once or twice": 1, "About monthly": 2, "About weekly": 4, "Daily or more": 8, "": 0, "unknown": 0,
	}
	for in, want := range cases {
		if got := frequencyWeight(in); got != want {
			t.Errorf("frequencyWeight(%q) = %v, want %v", in, got, want)
		}
	}
	stages := map[string]float64{
		"Before pushing, on my machine": 0.5, "In CI, before merge": 1,
		"At GitOps sync or `helm upgrade`, after merge": 2, "At GitOps sync or helm upgrade, after merge": 2,
		"Pods Pending, crashing or at runtime": 3, "The customer noticed first": 4, "": 0,
	}
	for in, want := range stages {
		if got := latenessWeight(in); got != want {
			t.Errorf("latenessWeight(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()
	ds := Decide(load(t))
	want := map[string]string{
		"2 OpenShift-first":     "OpenShift-first confirmed",              // 5 of 9 rows
		"3 GitLab timing":       "GitLab adapter lands in months 5 to 6",  // 5 GitLab of 7 Git rows
		"4 layout reader order": "Argo/Flux readers first",                // plain 3 (aks, lap, lap) vs Argo+Flux 6
		"5 Compose timing":      "Compose host snapshot stays in phase 2", // 2 of 9 rows in production
	}
	for _, d := range ds {
		if w, ok := want[d.Rule]; ok && d.Outcome != w {
			t.Errorf("%s: got %q (%s), want %q", d.Rule, d.Outcome, d.Evidence, w)
		}
	}
	if len(ds) != len(want) {
		t.Errorf("got %d decisions, want %d", len(ds), len(want))
	}
}

func TestCustomerRowsSplitsMultiSelect(t *testing.T) {
	t.Parallel()
	counts, rows := CustomerRows(load(t), "C5")
	if rows != 9 {
		t.Errorf("rows = %d, want 9", rows)
	}
	if counts["Kyverno"] != 5 || counts["ValidatingAdmissionPolicy or MutatingAdmissionPolicy"] != 5 {
		t.Errorf("multi-select not split: %v", counts)
	}
}

// TestSurveyMentionsEveryID keeps SURVEY.md and the tally in sync: every
// question ID the tally reads must appear in the survey text.
func TestSurveyMentionsEveryID(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "SURVEY.md"))
	if err != nil {
		t.Fatal(err)
	}
	survey := string(b)
	ids := []string{"B3.first", "B3.second", "B3.third"}
	for k := 1; k <= Kinds; k++ {
		ids = append(ids, fmt.Sprintf("B1.%d", k))
	}
	for _, q := range []string{"C1", "C3", "C4", "C12"} {
		ids = append(ids, q+" ")
	}
	for _, id := range ids {
		if !strings.Contains(survey, id) {
			t.Errorf("SURVEY.md does not mention %q", id)
		}
	}
	if !strings.Contains(survey, "Rows: B2.1 to B2.18") {
		t.Errorf("SURVEY.md B2 grid rows changed; update Kinds")
	}
}

func TestHeaderIDHandlesGoogleFormsExport(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"B1.3": "B1.3",
		"B1 Wie oft ... [B1.3 Manifest abgelehnt]":    "B1.3",
		"B2 Wo entdeckt? [B2.17 Docker Compose: ...]": "B2.17",
		"C1.2 Distribution (Kunde 2)":                 "C1.2",
		"B3.first Teuerste Fehlerart":                 "B3.first",
		"D6.registry ... Registry":                    "D6.registry",
		"Zeitstempel":                                 "Zeitstempel",
		"A1 Rolle":                                    "A1",
	}
	for in, want := range cases {
		if got := HeaderID(in); got != want {
			t.Errorf("HeaderID(%q) = %q, want %q", in, got, want)
		}
	}
	rs, err := ReadCSV(strings.NewReader("Zeitstempel,A1 Rolle,\"B1 Grid [B1.10 Referenz fehlt]\",\"B2 Grid [B2.10 Referenz fehlt]\",B3.first x\n2026-09-18,Consultant,About weekly,The customer noticed first,10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rs[0]["A1"] != "Consultant" || rs[0]["B1.10"] != "About weekly" || rs[0]["B3.first"] != "10" {
		t.Errorf("google forms header mapping failed: %v", rs[0])
	}
	if got := BacklogOrder(rs)[0]; got.Kind != 10 || got.Score != 16 {
		t.Errorf("kind 10 from forms export = %+v, want score 16 (weekly 4 x customer 4)", got)
	}
}

func TestMarkdownRenders(t *testing.T) {
	t.Parallel()
	md := Markdown(load(t))
	for _, want := range []string{"Responses: 5", "| 1 | 10 missing reference |", "2 OpenShift-first", "C1 (9 rows):"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}
