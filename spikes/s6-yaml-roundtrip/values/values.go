// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Package values is the S6 spike: layered values files as yaml.v3 node trees,
// RFC 6901 pointers, a Helm-semantics fold that records the source layer of
// every leaf, and a write-back that edits one layer file surgically so every
// untouched byte stays byte-identical.
package values

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer is one values file in the stack, lowest rank first when folded.
type Layer struct {
	Name  string
	Path  string
	Owner string // "human" or "machine:<tool>"; machine-owned layers refuse edits
	Src   []byte
	Doc   *yaml.Node
}

// LoadLayer parses a values file into a node tree and keeps the raw bytes.
func LoadLayer(name, path, owner string) (*Layer, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseLayer(name, path, owner, src)
}

// ParseLayer is LoadLayer over bytes already in memory.
func ParseLayer(name, path, owner string, src []byte) (*Layer, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 {
		// empty file: synthesise an empty document so inserts have a root
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map", Line: 1, Column: 1}}}
	}
	return &Layer{Name: name, Path: path, Owner: owner, Src: src, Doc: &doc}, nil
}

// Machine reports whether the layer is owned by a bot and must not be edited.
func (l *Layer) Machine() bool { return strings.HasPrefix(l.Owner, "machine:") }

// --- RFC 6901 -------------------------------------------------------------

// Parse splits a JSON Pointer into unescaped reference tokens.
func Parse(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if p[0] != '/' {
		return nil, fmt.Errorf("pointer %q must start with /", p)
	}
	parts := strings.Split(p[1:], "/")
	for i, s := range parts {
		parts[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(s)
	}
	return parts, nil
}

// Escape encodes one reference token.
func Escape(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}

// Join builds a pointer from tokens.
func Join(tokens []string) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString("/")
		b.WriteString(Escape(t))
	}
	return b.String()
}

// --- fold -------------------------------------------------------------------

// Leaf is one effective scalar (or empty container) with its provenance.
type Leaf struct {
	Pointer string
	Value   any
	Layer   string // layer that provided the value
	Line    int    // position in that layer (0 when the layer defined it through an alias the decoder resolved)
	Column  int
}

// Effective folds the layers with Helm's coalesce semantics: maps merge
// recursively, scalars and lists replace, and an explicit null in a higher
// layer deletes the key (helm docs, "Deleting a default key"). Aliases and
// merge keys are resolved by yaml.v3's decoder before folding. The returned
// map is keyed by pointer and also lists keys deleted by null so the UI can
// show "removed by envs/eu-prod".
func Effective(layers []*Layer) (leaves map[string]Leaf, deleted map[string]string, err error) {
	leaves = map[string]Leaf{}
	deleted = map[string]string{}
	merged := map[string]any{}
	for _, l := range layers {
		var m map[string]any
		if err := l.Doc.Decode(&m); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", l.Path, err)
		}
		coalesce(merged, m, "", l, leaves, deleted)
	}
	return leaves, deleted, nil
}

func coalesce(dst, src map[string]any, prefix string, l *Layer, leaves map[string]Leaf, deleted map[string]string) {
	for k, v := range src {
		p := prefix + "/" + Escape(k)
		if v == nil {
			if _, existed := dst[k]; existed {
				deleted[p] = l.Name
				dropPrefix(leaves, p)
			}
			delete(dst, k)
			continue
		}
		sm, srcIsMap := v.(map[string]any)
		dm, dstIsMap := dst[k].(map[string]any)
		if srcIsMap {
			if !dstIsMap {
				// a map replacing nothing or a scalar: start empty and let the
				// recursion drop nulls (Helm's cleanNilValues) and record leaves
				dm = map[string]any{}
				dst[k] = dm
				dropPrefix(leaves, p)
				delete(deleted, p)
				if len(sm) == 0 {
					leaves[p] = leafAt(p, dm, l)
				}
			}
			coalesce(dm, sm, p, l, leaves, deleted)
			continue
		}
		dropPrefix(leaves, p)
		delete(deleted, p)
		dst[k] = v
		record(v, p, l, leaves)
	}
}

func dropPrefix(leaves map[string]Leaf, p string) {
	for k := range leaves {
		if k == p || strings.HasPrefix(k, p+"/") {
			delete(leaves, k)
		}
	}
}

func record(v any, p string, l *Layer, leaves map[string]Leaf) {
	switch t := v.(type) {
	case nil:
		return // a null inside a list or fresh subtree is not a value
	case map[string]any:
		if len(t) == 0 {
			leaves[p] = leafAt(p, v, l)
		}
		for k, c := range t {
			record(c, p+"/"+Escape(k), l, leaves)
		}
	case []any:
		if len(t) == 0 {
			leaves[p] = leafAt(p, v, l)
		}
		for i, c := range t {
			record(c, p+"/"+strconv.Itoa(i), l, leaves)
		}
	default:
		leaves[p] = leafAt(p, v, l)
	}
}

func leafAt(p string, v any, l *Layer) Leaf {
	leaf := Leaf{Pointer: p, Value: v, Layer: l.Name}
	tokens, _ := Parse(p)
	if loc := locate(l.Doc, tokens); loc.val != nil {
		leaf.Line, leaf.Column = loc.val.Line, loc.val.Column
	}
	return leaf
}

// --- node navigation ------------------------------------------------------

// location is the result of walking a pointer through a node tree.
type location struct {
	parent   *yaml.Node // mapping or sequence holding val
	idx      int        // index of val in parent.Content
	key      *yaml.Node // key node for mapping entries, nil for sequence items
	val      *yaml.Node // nil when the pointer does not fully resolve
	deepest  *yaml.Node // deepest container reached
	rest     []string   // tokens not resolved below deepest
	viaAlias bool       // the walk passed through an alias node
	alias    string     // the anchor name it passed through
}

// locate walks tokens from the document root without following aliases
// into shared subtrees blindly: passing through an alias is recorded so the
// caller can refuse to edit a value that belongs to another key.
func locate(doc *yaml.Node, tokens []string) location {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	loc := location{deepest: root, val: root, rest: tokens}
	cur := root
	for i, tok := range tokens {
		if cur.Kind == yaml.AliasNode {
			loc.viaAlias, loc.alias = true, cur.Value
			cur = cur.Alias
		}
		switch cur.Kind {
		case yaml.MappingNode:
			found := false
			for j := 0; j+1 < len(cur.Content); j += 2 {
				k, v := cur.Content[j], cur.Content[j+1]
				if k.Value == tok && k.Tag != "!!merge" {
					loc = location{parent: cur, idx: j + 1, key: k, val: v, deepest: cur, rest: tokens[i+1:], viaAlias: loc.viaAlias, alias: loc.alias}
					cur = v
					found = true
					break
				}
			}
			if !found {
				// look through merge keys (<<: *x) for the token, read-only
				for j := 0; j+1 < len(cur.Content); j += 2 {
					if cur.Content[j].Tag == "!!merge" {
						if m := cur.Content[j+1]; m.Kind == yaml.AliasNode && m.Alias != nil {
							if sub := locate(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m.Alias}}, tokens[i:]); sub.val != nil && len(sub.rest) == 0 {
								sub.viaAlias, sub.alias = true, m.Value
								return sub
							}
						}
					}
				}
				loc.val, loc.deepest, loc.rest = nil, cur, tokens[i:]
				return loc
			}
		case yaml.SequenceNode:
			n, err := strconv.Atoi(tok)
			if err != nil || n < 0 || n >= len(cur.Content) {
				loc.val, loc.deepest, loc.rest = nil, cur, tokens[i:]
				return loc
			}
			loc = location{parent: cur, idx: n, val: cur.Content[n], deepest: cur, rest: tokens[i+1:], viaAlias: loc.viaAlias, alias: loc.alias}
			cur = cur.Content[n]
		default:
			loc.val, loc.deepest, loc.rest = nil, cur, tokens[i:]
			return loc
		}
	}
	if cur.Kind == yaml.AliasNode {
		loc.viaAlias, loc.alias = true, cur.Value
	}
	loc.rest = nil
	return loc
}

// --- line geometry ----------------------------------------------------------

// lineOffsets returns the byte offset of every line start plus one past the
// end, so lines i (1-based) span [off[i-1], off[i]).
func lineOffsets(src []byte) []int {
	off := []int{0}
	for i, b := range src {
		if b == '\n' {
			off = append(off, i+1)
		}
	}
	if len(src) == 0 || src[len(src)-1] != '\n' {
		off = append(off, len(src))
	}
	return off
}

func lineText(src []byte, off []int, line int) string {
	if line < 1 || line >= len(off) {
		return ""
	}
	return strings.TrimRight(string(src[off[line-1]:off[line]]), "\n")
}

func indentOf(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// allLines collects the start line of every node in the document except
// those inside skip's subtree.
func allLines(doc *yaml.Node, skip *yaml.Node) []int {
	var lines []int
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == skip {
			return
		}
		if n.Kind != yaml.DocumentNode {
			lines = append(lines, n.Line)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	sort.Ints(lines)
	return lines
}

// endLine finds the last line a value occupies: the last non-blank line
// before the next node that is not a comment at the owner's indentation or
// less (such comments belong to the following key).
func endLine(l *Layer, val *yaml.Node, ownerIndent int) int {
	off := lineOffsets(l.Src)
	next := len(off) // one past the last line
	for _, ln := range allLines(l.Doc, val) {
		if ln > val.Line {
			next = ln
			break
		}
	}
	for ln := next - 1; ln > val.Line; ln-- {
		t := lineText(l.Src, off, ln)
		trim := strings.TrimSpace(t)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") && indentOf(t) <= ownerIndent {
			continue
		}
		return ln
	}
	return val.Line
}

// detectIndent returns the indentation step used by the file (default 2).
func detectIndent(doc *yaml.Node) int {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	var find func(n *yaml.Node) int
	find = func(n *yaml.Node) int {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if v.Kind == yaml.MappingNode && len(v.Content) > 0 && v.Line > k.Line && v.Column > k.Column {
					return v.Column - k.Column
				}
				if d := find(v); d > 0 {
					return d
				}
			}
		}
		return 0
	}
	if d := find(root); d > 0 {
		return d
	}
	return 2
}

// Reencode is the naive alternative: decode to a node tree and encode again.
// The tests measure how many lines it changes, which is the reason the
// product splices bytes instead.
func Reencode(src []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(detectIndent(&doc))
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
