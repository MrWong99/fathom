// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package tally computes the pre-registered S14 decisions from a CSV export of
// the survey answers. The rules are the ones written in SURVEY.md, sender
// annex, "Pre-registered decision rules"; change them there first.
package tally

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Response is one respondent: question ID -> answer text as exported.
type Response map[string]string

// Kinds is the number of failure kinds in grid B1/B2.
const Kinds = 18

// Customers is the number of customer columns in Part 3.
const Customers = 5

// KindNames maps a B1 row number to a short label for the output.
var KindNames = map[int]string{
	1: "YAML syntax/typing", 2: "values type/unit", 3: "manifest schema", 4: "removed API",
	5: "CRD CEL rule", 6: "admission policy denial", 7: "PodSecurity", 8: "ResourceQuota",
	9: "LimitRange", 10: "missing reference", 11: "image pull", 12: "controller RBAC",
	13: "immutable/ownership", 14: "opaque webhook", 15: "scheduling", 16: "effective value/drift",
	17: "Compose", 18: "OpenShift SCC/UID/Route",
}

// ReadCSV parses the export: header row of question IDs, one row per respondent.
func ReadCSV(r io.Reader) ([]Response, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("csv needs a header and at least one response row")
	}
	head := rows[0]
	var out []Response
	for i, row := range rows[1:] {
		if len(row) > len(head) {
			return nil, fmt.Errorf("row %d has %d fields, header has %d", i+2, len(row), len(head))
		}
		resp := Response{}
		for j, v := range row {
			resp[HeaderID(head[j])] = strings.TrimSpace(v)
		}
		out = append(out, resp)
	}
	return out, nil
}

var idPattern = regexp.MustCompile(`\b([A-E][0-9]+(?:\.[A-Za-z0-9_-]+)?)\b`)

// HeaderID reduces an export column header to its question ID. Google Forms
// exports a grid as "Question title [Row label]" and a plain question as its
// title, so the form is built with the ID at the start of every title and
// row label ("B1.3 Rendered manifest rejected ..."); the ID inside the
// brackets wins, then the leading ID of the title, then the raw header.
func HeaderID(h string) string {
	h = strings.TrimSpace(h)
	if i := strings.LastIndex(h, "["); i >= 0 {
		if m := idPattern.FindStringSubmatch(h[i:]); m != nil {
			return m[1]
		}
	}
	if m := idPattern.FindStringSubmatch(h); m != nil && strings.HasPrefix(h, m[1]) {
		return m[1]
	}
	return h
}

// has reports whether the lower-cased answer contains any of the substrings.
// Labels are matched in English (SURVEY.md) and German (SURVEY.de.md, the
// Google Forms version), so an export in either language tallies the same.
func has(l string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// frequencyWeight maps a B1 grid answer to the rule-1 weight.
func frequencyWeight(s string) float64 {
	l := strings.ToLower(s)
	switch {
	case l == "":
		return 0
	case has(l, "never", "nie"):
		return 0
	case has(l, "once", "ein- oder zweimal", "einmal"):
		return 1
	case has(l, "month", "monat"):
		return 2
	case has(l, "week", "wöchentlich", "woche"):
		return 4
	case has(l, "daily", "täglich"):
		return 8
	}
	return 0
}

// latenessWeight maps a B2 grid answer to the rule-1 weight.
func latenessWeight(s string) float64 {
	l := strings.ToLower(s)
	switch {
	case l == "":
		return 0
	case has(l, "before pushing", "vor dem push"):
		return 0.5
	case has(l, "in ci", "in der ci", "in ci,"):
		return 1
	case has(l, "sync", "after merge", "nach dem merge"):
		return 2
	case has(l, "pending", "runtime", "laufzeit"):
		return 3
	case has(l, "customer", "kunde"):
		return 4
	}
	return 0
}

// KindScore is one row of the backlog order.
type KindScore struct {
	Kind        int
	Score       float64
	TopThree    int  // times named in B3
	Discrepancy bool // top five by score but never named in B3
}

// BacklogOrder implements rule 1.
func BacklogOrder(rs []Response) []KindScore {
	scores := make([]KindScore, Kinds)
	for k := 1; k <= Kinds; k++ {
		scores[k-1].Kind = k
	}
	for _, r := range rs {
		for k := 1; k <= Kinds; k++ {
			f := frequencyWeight(r[fmt.Sprintf("B1.%d", k)])
			l := latenessWeight(r[fmt.Sprintf("B2.%d", k)])
			if f > 0 && l == 0 {
				l = 1 // frequency given, stage blank: count as caught in CI
			}
			scores[k-1].Score += f * l
		}
		for _, col := range []string{"B3.first", "B3.second", "B3.third"} {
			if n, err := strconv.Atoi(strings.TrimSpace(r[col])); err == nil && n >= 1 && n <= Kinds {
				scores[n-1].TopThree++
			}
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Score != scores[j].Score {
			return scores[i].Score > scores[j].Score
		}
		if scores[i].TopThree != scores[j].TopThree {
			return scores[i].TopThree > scores[j].TopThree
		}
		return scores[i].Kind < scores[j].Kind
	})
	for i := range scores {
		if i < 5 && scores[i].Score > 0 && scores[i].TopThree == 0 {
			scores[i].Discrepancy = true
		}
	}
	return scores
}

// CustomerRows counts, over all respondents and customer columns of question q,
// how many rows named each option. Multi-select cells are split on ';'.
// Returns the counts and the number of non-empty rows.
func CustomerRows(rs []Response, q string) (map[string]int, int) {
	counts := map[string]int{}
	rows := 0
	for _, r := range rs {
		for c := 1; c <= Customers; c++ {
			cell := strings.TrimSpace(r[fmt.Sprintf("%s.%d", q, c)])
			if cell == "" {
				continue
			}
			rows++
			for _, opt := range strings.Split(cell, ";") {
				opt = strings.TrimSpace(opt)
				if opt != "" {
					counts[opt]++
				}
			}
		}
	}
	return counts, rows
}

// Decision is one pre-registered rule with its outcome and the numbers behind it.
type Decision struct {
	Rule     string
	Outcome  string
	Evidence string
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

func sumMatching(counts map[string]int, pred func(string) bool) int {
	n := 0
	for k, v := range counts {
		if pred(strings.ToLower(k)) {
			n += v
		}
	}
	return n
}

// Decide implements rules 2 to 5.
func Decide(rs []Response) []Decision {
	var ds []Decision

	// Rule 2: OpenShift-first.
	dist, distRows := CustomerRows(rs, "C1")
	os := sumMatching(dist, func(s string) bool { return strings.Contains(s, "openshift") })
	plurality, pluralityN := "", 0
	for k, v := range dist {
		if v > pluralityN || (v == pluralityN && k < plurality) {
			plurality, pluralityN = k, v
		}
	}
	out := "OpenShift-first NOT confirmed"
	if distRows > 0 && (os >= pluralityN || pct(os, distRows) >= 40) {
		out = "OpenShift-first confirmed"
	}
	ds = append(ds, Decision{"2 OpenShift-first", out,
		fmt.Sprintf("OpenShift %d of %d rows (%.0f%%); plurality %q with %d", os, distRows, pct(os, distRows), plurality, pluralityN)})

	// Rule 3: GitLab adapter timing.
	scm, _ := CustomerRows(rs, "C4")
	noGit := sumMatching(scm, func(s string) bool { return has(s, "no git", "kein git") })
	gitRows := 0
	for _, v := range scm {
		gitRows += v
	}
	gitRows -= noGit
	gitlab := sumMatching(scm, func(s string) bool { return strings.Contains(s, "gitlab") })
	out = "GitLab adapter stays after month 6"
	if gitRows > 0 && pct(gitlab, gitRows) > 30 {
		out = "GitLab adapter lands in months 5 to 6"
	}
	ds = append(ds, Decision{"3 GitLab timing", out,
		fmt.Sprintf("GitLab %d of %d Git rows (%.0f%%); handover without Git %d rows", gitlab, gitRows, pct(gitlab, gitRows), noGit)})

	// Rule 4: plain reader priority.
	helm, _ := CustomerRows(rs, "C3")
	plain := sumMatching(helm, func(s string) bool { return strings.Contains(s, "helm upgrade") })
	argoFlux := sumMatching(helm, func(s string) bool { return strings.Contains(s, "argo") || strings.Contains(s, "flux") })
	helmfile := sumMatching(helm, func(s string) bool { return strings.Contains(s, "helmfile") })
	out = "Argo/Flux readers first"
	if plain > argoFlux {
		out = "plain reader first"
	}
	ds = append(ds, Decision{"4 layout reader order", out,
		fmt.Sprintf("plain (CI or laptop helm upgrade) %d, Argo+Flux %d, Helmfile %d", plain, argoFlux, helmfile)})

	// Rule 5: Compose host snapshot timing.
	compose, composeRows := CustomerRows(rs, "C12")
	prod := sumMatching(compose, func(s string) bool { return has(s, "production", "produktion") })
	out = "Compose host snapshot stays in phase 2"
	if composeRows > 0 && pct(prod, composeRows) > 40 {
		out = "Compose host snapshot: open decision 9 goes to the owner"
	}
	ds = append(ds, Decision{"5 Compose timing", out,
		fmt.Sprintf("Compose in production %d of %d rows (%.0f%%)", prod, composeRows, pct(prod, composeRows))})

	return ds
}

// Markdown renders the whole tally for RESULT.md.
func Markdown(rs []Response) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Responses: %d\n\n", len(rs))
	b.WriteString("| Rank | Kind | Score | Named in B3 | Note |\n|---|---|---|---|---|\n")
	for i, s := range BacklogOrder(rs) {
		note := ""
		if s.Discrepancy {
			note = "top five by score, never named in B3"
		}
		fmt.Fprintf(&b, "| %d | %d %s | %.1f | %d | %s |\n", i+1, s.Kind, KindNames[s.Kind], s.Score, s.TopThree, note)
	}
	b.WriteString("\n| Rule | Outcome | Evidence |\n|---|---|---|\n")
	for _, d := range Decide(rs) {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", d.Rule, d.Outcome, d.Evidence)
	}
	for _, q := range []string{"C1", "C3", "C4", "C5", "C6", "C8", "C10", "C11"} {
		counts, rows := CustomerRows(rs, q)
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if counts[keys[i]] != counts[keys[j]] {
				return counts[keys[i]] > counts[keys[j]]
			}
			return keys[i] < keys[j]
		})
		fmt.Fprintf(&b, "\n%s (%d rows):", q, rows)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%d", k, counts[k])
		}
		b.WriteString("\n")
	}
	return b.String()
}
