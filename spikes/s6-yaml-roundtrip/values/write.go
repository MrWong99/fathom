// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package values

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrMachineOwned is returned for edits to a bot-managed layer.
var ErrMachineOwned = errors.New("layer is machine-owned; edit refused")

// ErrAlias is returned when the pointer passes through a YAML alias: the
// value is shared with the anchor's other users, so an in-place edit would
// change more than the requested pointer.
var ErrAlias = errors.New("pointer resolves through an alias; edit the anchor or override the key with a literal")

// Edit is one byte-range replacement in a layer's source. Everything outside
// [Start, End) is untouched by construction, which is what the pass
// criterion "byte-identical untouched regions" asks for.
type Edit struct {
	Layer string
	Kind  string // "replace", "insert", "remove"
	Start int
	End   int
	Old   string
	New   string
}

// Apply returns the edited source.
func (e Edit) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)-(e.End-e.Start)+len(e.New))
	out = append(out, src[:e.Start]...)
	out = append(out, e.New...)
	out = append(out, src[e.End:]...)
	return out
}

// Set writes value at pointer in the layer. An existing leaf is replaced in
// place (style and trailing comment preserved); a missing path is inserted
// under its deepest existing ancestor with the file's indentation. A nil value
// writes `null`, which is Helm's way of deleting a lower layer's key.
func Set(l *Layer, pointer string, value any) (Edit, error) {
	if l.Machine() {
		return Edit{}, fmt.Errorf("%s (%s): %w", l.Path, l.Owner, ErrMachineOwned)
	}
	tokens, err := Parse(pointer)
	if err != nil {
		return Edit{}, err
	}
	if len(tokens) == 0 {
		return Edit{}, errors.New("cannot write the document root")
	}
	loc := locate(l.Doc, tokens)
	if loc.viaAlias {
		return Edit{}, fmt.Errorf("%s at %s (*%s): %w", l.Path, pointer, loc.alias, ErrAlias)
	}
	if loc.val != nil && len(loc.rest) == 0 {
		return replace(l, loc, value)
	}
	return insert(l, loc, tokens, value)
}

// Remove deletes the key (and its value) at pointer from the layer.
func Remove(l *Layer, pointer string) (Edit, error) {
	if l.Machine() {
		return Edit{}, fmt.Errorf("%s (%s): %w", l.Path, l.Owner, ErrMachineOwned)
	}
	tokens, err := Parse(pointer)
	if err != nil {
		return Edit{}, err
	}
	loc := locate(l.Doc, tokens)
	if loc.val == nil || len(loc.rest) != 0 || loc.key == nil {
		return Edit{}, fmt.Errorf("%s: %s is not a mapping entry in this layer", l.Path, pointer)
	}
	if loc.viaAlias {
		return Edit{}, fmt.Errorf("%s at %s: %w", l.Path, pointer, ErrAlias)
	}
	if len(loc.parent.Content) == 2 {
		return Edit{}, fmt.Errorf("%s: %s is the last key of its mapping; remove the parent key instead", l.Path, pointer)
	}
	off := lineOffsets(l.Src)
	first := loc.key.Line
	last := endLine(l, loc.val, loc.key.Column-1)
	return Edit{Layer: l.Name, Kind: "remove", Start: off[first-1], End: off[last], Old: string(l.Src[off[first-1]:off[last]])}, nil
}

// replace rewrites an existing value node in place.
func replace(l *Layer, loc location, value any) (Edit, error) {
	off := lineOffsets(l.Src)
	val := loc.val
	ownerIndent := 0
	if loc.key != nil {
		ownerIndent = loc.key.Column - 1
	} else {
		ownerIndent = max(val.Column-3, 0) // "- " before a sequence item
	}
	fileIndent := detectIndent(l.Doc)
	valueIndent := ownerIndent + fileIndent

	newNode, err := encodeNode(value, val)
	if err != nil {
		return Edit{}, err
	}
	single := isSingleLine(val, l, off)
	if newNode.Kind == yaml.ScalarNode && single && !strings.Contains(newNode.Value, "\n") && newNode.Style != yaml.LiteralStyle && newNode.Style != yaml.FoldedStyle {
		// same-line scalar: replace the value text only, keep the comment
		line := lineText(l.Src, off, val.Line)
		start := off[val.Line-1] + val.Column - 1
		end := off[val.Line-1] + len(line)
		if c := lineComment(loc); c != "" {
			if i := strings.LastIndex(line, c); i >= val.Column-1 {
				end = off[val.Line-1] + len(strings.TrimRight(line[:i], " \t"))
			}
		}
		text, err := marshalNode(newNode, fileIndent)
		if err != nil {
			return Edit{}, err
		}
		return Edit{Layer: l.Name, Kind: "replace", Start: start, End: end, Old: string(l.Src[start:end]), New: text}, nil
	}

	// multi-line value (block scalar, mapping, sequence): replace from the
	// value's first line to its last line with a re-encoded block.
	first, last := val.Line, endLine(l, val, ownerIndent)
	text, err := marshalNode(newNode, fileIndent)
	if err != nil {
		return Edit{}, err
	}
	var repl string
	if val.Line == loc.keyLine() {
		// value starts on the key line ("key: |" or "key: [..]"): keep the
		// key line's prefix up to the value column, then the block. A
		// top-level block scalar is already indented by fileIndent relative
		// to its indicator, so only the owner's indentation is added.
		line := lineText(l.Src, off, val.Line)
		prefix := line[:val.Column-1]
		repl = prefix + indentBlock(text, ownerIndent, true) + "\n"
	} else {
		repl = indentBlock(text, valueIndent, false) + "\n"
	}
	start, end := off[first-1], off[last]
	return Edit{Layer: l.Name, Kind: "replace", Start: start, End: end, Old: string(l.Src[start:end]), New: repl}, nil
}

func (loc location) keyLine() int {
	if loc.key != nil {
		return loc.key.Line
	}
	return loc.val.Line
}

// insert adds the missing tail of the pointer under the deepest existing
// mapping.
func insert(l *Layer, loc location, tokens []string, value any) (Edit, error) {
	container := loc.deepest
	if container.Kind != yaml.MappingNode {
		return Edit{}, fmt.Errorf("%s: cannot insert %s: %s is not a mapping in this layer", l.Path, Join(tokens), Join(tokens[:len(tokens)-len(loc.rest)]))
	}
	if container.Style == yaml.FlowStyle || (len(container.Content) == 0 && !isRoot(l, container)) {
		return Edit{}, fmt.Errorf("%s: %s is a flow-style or empty mapping ({}); rewrite it as a block mapping first", l.Path, Join(tokens[:len(tokens)-len(loc.rest)]))
	}
	fileIndent := detectIndent(l.Doc)
	off := lineOffsets(l.Src)

	// Build the nested value for the remaining tokens.
	var v any = value
	for i := len(loc.rest) - 1; i >= 1; i-- {
		v = map[string]any{loc.rest[i]: v}
	}
	entry := map[string]any{loc.rest[0]: v}
	node := &yaml.Node{}
	if err := node.Encode(entry); err != nil {
		return Edit{}, err
	}
	if len(loc.rest) == 1 {
		if leaf := node.Content[1]; leaf.Kind == yaml.ScalarNode && strings.Contains(leaf.Value, "\n") {
			leaf.Style = yaml.LiteralStyle
		}
	}
	text, err := marshalNode(node, fileIndent)
	if err != nil {
		return Edit{}, err
	}

	keyIndent := 0
	var at int // byte offset to insert at
	if len(container.Content) == 0 {
		at = len(l.Src)
	} else {
		keyIndent = container.Content[0].Column - 1
		lastKey, lastVal := container.Content[len(container.Content)-2], container.Content[len(container.Content)-1]
		last := endLine(l, lastVal, lastKey.Column-1)
		at = off[last]
	}
	block := indentBlock(text, keyIndent, false) + "\n"
	if at > 0 && l.Src[at-1] != '\n' {
		block = "\n" + block
	}
	return Edit{Layer: l.Name, Kind: "insert", Start: at, End: at, New: block}, nil
}

func isRoot(l *Layer, n *yaml.Node) bool {
	return len(l.Doc.Content) > 0 && l.Doc.Content[0] == n
}

// lineComment returns the trailing comment on the entry's line, which
// yaml.v3 attaches to the key for same-line scalars and to the value
// otherwise.
func lineComment(loc location) string {
	if loc.val.LineComment != "" {
		return loc.val.LineComment
	}
	if loc.key != nil {
		return loc.key.LineComment
	}
	return ""
}

// isSingleLine reports whether the value occupies only its start line.
func isSingleLine(val *yaml.Node, l *Layer, off []int) bool {
	if val.Kind != yaml.ScalarNode {
		return false
	}
	if val.Style == yaml.LiteralStyle || val.Style == yaml.FoldedStyle {
		return false
	}
	return !strings.Contains(val.Value, "\n") && endLine(l, val, 0) == val.Line
}

// encodeNode turns a Go value into a node, carrying over the old node's
// quoting style for single-line strings.
func encodeNode(value any, old *yaml.Node) (*yaml.Node, error) {
	n := &yaml.Node{}
	if value == nil {
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!null", "null"
		return n, nil
	}
	if err := n.Encode(value); err != nil {
		return nil, err
	}
	if n.Kind == yaml.ScalarNode {
		if strings.Contains(n.Value, "\n") {
			n.Style = yaml.LiteralStyle
		} else if old != nil && old.Kind == yaml.ScalarNode && (old.Style == yaml.SingleQuotedStyle || old.Style == yaml.DoubleQuotedStyle) && n.Tag == "!!str" {
			n.Style = old.Style
		}
	}
	return n, nil
}

// marshalNode encodes a node with the file's indentation and no trailing
// newline.
func marshalNode(n *yaml.Node, indent int) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n"), nil
}

// indentBlock prefixes every line (or every line but the first when
// skipFirst is set) with indent spaces.
func indentBlock(text string, indent int, skipFirst bool) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	for i := range lines {
		if (i == 0 && skipFirst) || lines[i] == "" {
			continue
		}
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
