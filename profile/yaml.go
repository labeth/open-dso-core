package profile

import (
	"fmt"
	"strconv"
	"strings"
)

// The profile file is YAML, read by this minimal parser of the subset a
// profile needs, so the app module takes no YAML dependency. JSON is a
// subset of YAML too, but a profile is a human-edited data sheet whose
// lines want comments, which JSON has no syntax for.
//
// The subset: block mappings and sequences indented with spaces, "- key:
// value" items, comments, plain, 'single' and "double" quoted scalars, and
// flow collections ([a, b], {k: v}), which may continue over lines.
// Anchors, aliases, tags, block scalars (| and >), multi-line scalars and
// several documents are refused with the line they appear on.

type kind uint8

const (
	nullNode kind = iota
	scalarNode
	mapNode
	seqNode
)

// node is one parsed value.
type node struct {
	kind   kind
	value  string // scalarNode
	quoted bool   // a quoted scalar is always a string
	keys   []string
	m      map[string]*node
	items  []*node
	line   int
}

// TRLC-LINKS: REQ-DSO-1002
func (n *node) describe() string {
	switch n.kind {
	case scalarNode:
		return "a scalar"
	case mapNode:
		return "a mapping"
	case seqNode:
		return "a list"
	case nullNode:
	}
	return "nothing"
}

// equal reports whether two nodes hold the same value.
// TRLC-LINKS: REQ-DSO-1002
func (n *node) equal(o *node) bool {
	if n.kind != o.kind {
		return false
	}
	switch n.kind {
	case nullNode:
		// Two empty values are equal.
	case scalarNode:
		return n.value == o.value
	case seqNode:
		if len(n.items) != len(o.items) {
			return false
		}
		for i := range n.items {
			if !n.items[i].equal(o.items[i]) {
				return false
			}
		}
	case mapNode:
		if len(n.keys) != len(o.keys) {
			return false
		}
		for _, k := range n.keys {
			v, ok := o.m[k]
			if !ok || !n.m[k].equal(v) {
				return false
			}
		}
	}
	return true
}

// line is one significant source line.
type line struct {
	indent int
	text   string // without indentation and comment
	no     int
	open   int // flow collections still open at the line's end
}

// flowDepth counts the [ and { left open in s, outside quotes.
// TRLC-LINKS: REQ-DSO-1002
func flowDepth(s string) int {
	depth := 0
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"' && c == '\\':
			i++
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" :[{,", rune(s[i-1]))):
			q = c
		case q == 0 && (c == '[' || c == '{'):
			depth++
		case q == 0 && (c == ']' || c == '}'):
			depth--
		}
	}
	return depth
}

type parser struct {
	lines []line
	pos   int
}

// parseYAML parses one document of the subset.
// TRLC-LINKS: REQ-DSO-1002
func parseYAML(src []byte) (*node, error) {
	p := &parser{}
	for i, raw := range strings.Split(string(src), "\n") {
		raw = strings.TrimRight(raw, " \r")
		body := strings.TrimLeft(raw, " ")
		if strings.HasPrefix(body, "\t") {
			return nil, fmt.Errorf("line %d: tab in indentation", i+1)
		}
		text, err := stripComment(body)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if text == "" {
			continue
		}
		if text == "---" && len(p.lines) == 0 {
			continue
		}
		if text == "---" || text == "..." {
			return nil, fmt.Errorf("line %d: one document only", i+1)
		}
		if n := len(p.lines); n > 0 && p.lines[n-1].open > 0 {
			// A flow collection continues: join the line to its start.
			prev := &p.lines[n-1]
			prev.text += " " + text
			prev.open = flowDepth(prev.text)
			continue
		}
		p.lines = append(p.lines, line{indent: len(raw) - len(body), text: text, no: i + 1, open: flowDepth(text)})
	}
	if n := len(p.lines); n > 0 && p.lines[n-1].open > 0 {
		return nil, fmt.Errorf("line %d: unclosed flow collection", p.lines[n-1].no)
	}
	if len(p.lines) == 0 {
		return &node{kind: nullNode}, nil
	}
	n, err := p.block(p.lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		l := p.lines[p.pos]
		return nil, fmt.Errorf("line %d: unexpected indentation", l.no)
	}
	return n, nil
}

// stripComment removes a trailing comment outside quotes.
// TRLC-LINKS: REQ-DSO-1002
func stripComment(s string) (string, error) {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"' && c == '\\':
			i++
		case q == '\'' && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" :[{,-", rune(s[i-1]))):
			q = c
		case q == 0 && c == '#' && (i == 0 || s[i-1] == ' '):
			return strings.TrimRight(s[:i], " "), nil
		}
	}
	return s, nil
}

// block parses the mapping or sequence whose lines start at indent.
// TRLC-LINKS: REQ-DSO-1002
func (p *parser) block(indent int) (*node, error) {
	l := p.lines[p.pos]
	if l.indent != indent {
		return nil, fmt.Errorf("line %d: unexpected indentation", l.no)
	}
	if l.text == "-" || strings.HasPrefix(l.text, "- ") {
		return p.sequence(indent)
	}
	return p.mapping(indent)
}

// TRLC-LINKS: REQ-DSO-1002
func (p *parser) sequence(indent int) (*node, error) {
	n := &node{kind: seqNode, line: p.lines[p.pos].no}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent {
			break
		}
		if l.indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indentation", l.no)
		}
		if l.text != "-" && !strings.HasPrefix(l.text, "- ") {
			break // a list at its key's indentation ends at the next key
		}
		rest := strings.TrimLeft(strings.TrimPrefix(l.text, "-"), " ")
		if rest == "" {
			p.pos++
			item, err := p.nested(indent, l.no, false)
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, item)
			continue
		}
		if _, _, ok := splitKey(rest); ok {
			// "- key: value": the item is a mapping whose keys line up
			// with the first key's column.
			col := l.indent + len(l.text) - len(rest)
			p.lines[p.pos] = line{indent: col, text: rest, no: l.no}
			item, err := p.mapping(col)
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, item)
			continue
		}
		item, err := inlineValue(rest, l.no)
		if err != nil {
			return nil, err
		}
		n.items = append(n.items, item)
		p.pos++
	}
	return n, nil
}

// TRLC-LINKS: REQ-DSO-1002
func (p *parser) mapping(indent int) (*node, error) {
	n := &node{kind: mapNode, m: map[string]*node{}, line: p.lines[p.pos].no}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent {
			break
		}
		if l.indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indentation", l.no)
		}
		key, rest, ok := splitKey(l.text)
		if !ok {
			return nil, fmt.Errorf("line %d: want \"key: value\"", l.no)
		}
		if _, dup := n.m[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", l.no, key)
		}
		p.pos++
		var v *node
		var err error
		if rest == "" {
			v, err = p.nested(indent, l.no, true)
		} else {
			v, err = inlineValue(rest, l.no)
		}
		if err != nil {
			return nil, err
		}
		n.keys = append(n.keys, key)
		n.m[key] = v
	}
	return n, nil
}

// nested parses the block under a "key:" or "-" line, or null when the next
// line is not indented further. Under a key (sameIndentList) a list may sit
// at the key's own indentation.
// TRLC-LINKS: REQ-DSO-1002
func (p *parser) nested(indent, no int, sameIndentList bool) (*node, error) {
	if p.pos >= len(p.lines) {
		return &node{kind: nullNode, line: no}, nil
	}
	next := p.lines[p.pos]
	if next.indent > indent {
		return p.block(next.indent)
	}
	if sameIndentList && next.indent == indent && (next.text == "-" || strings.HasPrefix(next.text, "- ")) {
		return p.sequence(indent)
	}
	return &node{kind: nullNode, line: no}, nil
}

// splitKey splits "key: rest" or "key:" outside quotes and brackets.
// TRLC-LINKS: REQ-DSO-1002
func splitKey(s string) (key, rest string, ok bool) {
	if s == "" || strings.ContainsRune("[{", rune(s[0])) {
		return "", "", false
	}
	if s[0] == '"' || s[0] == '\'' {
		end := quotedEnd(s)
		if end < 0 || end+1 >= len(s) || s[end+1] != ':' {
			return "", "", false
		}
		k, err := unquote(s[:end+1])
		if err != nil {
			return "", "", false
		}
		r := s[end+2:]
		if r != "" && r[0] != ' ' {
			return "", "", false
		}
		return k, strings.TrimSpace(r), true
	}
	for i := range len(s) {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ') {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), i > 0
		}
	}
	return "", "", false
}

// quotedEnd is the index of the quote closing the scalar s starts with.
// TRLC-LINKS: REQ-DSO-1002
func quotedEnd(s string) int {
	q := s[0]
	for i := 1; i < len(s); i++ {
		switch {
		case q == '"' && s[i] == '\\':
			i++
		case q == '\'' && s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case s[i] == q:
			return i
		}
	}
	return -1
}

// TRLC-LINKS: REQ-DSO-1002
func unquote(s string) (string, error) {
	if s[0] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	return strconv.Unquote(s)
}

// inlineValue parses a value written on its key's or item's line.
// TRLC-LINKS: REQ-DSO-1002
func inlineValue(s string, no int) (*node, error) {
	f := &flow{s: s, no: no}
	n, err := f.value()
	if err != nil {
		return nil, err
	}
	f.space()
	if f.i != len(f.s) {
		return nil, fmt.Errorf("line %d: unexpected %q", no, f.s[f.i:])
	}
	return n, nil
}

// flow parses a value written on one (joined) line: a scalar or a flow
// collection.
type flow struct {
	s  string
	i  int
	no int
}

// TRLC-LINKS: REQ-DSO-1002
func (f *flow) space() {
	for f.i < len(f.s) && f.s[f.i] == ' ' {
		f.i++
	}
}

// TRLC-LINKS: REQ-DSO-1002
func (f *flow) errf(format string, a ...any) error {
	return fmt.Errorf("line %d: %s", f.no, fmt.Sprintf(format, a...))
}

// TRLC-LINKS: REQ-DSO-1002
func (f *flow) value() (*node, error) {
	f.space()
	if f.i >= len(f.s) {
		return &node{kind: nullNode, line: f.no}, nil
	}
	switch c := f.s[f.i]; c {
	case '[':
		return f.collection(']')
	case '{':
		return f.collection('}')
	case '"', '\'':
		end := quotedEnd(f.s[f.i:])
		if end < 0 {
			return nil, f.errf("unterminated quote")
		}
		v, err := unquote(f.s[f.i : f.i+end+1])
		if err != nil {
			return nil, f.errf("bad quoted scalar: %v", err)
		}
		f.i += end + 1
		return &node{kind: scalarNode, value: v, quoted: true, line: f.no}, nil
	case '&', '*', '!', '|', '>', '%', '@', '`':
		return nil, f.errf("%q is outside the profile's YAML subset", c)
	}
	return f.plain(), nil
}

// plain reads a plain scalar up to a flow delimiter.
// TRLC-LINKS: REQ-DSO-1002
func (f *flow) plain() *node {
	start := f.i
	for f.i < len(f.s) && !strings.ContainsRune(",]}", rune(f.s[f.i])) {
		if f.s[f.i] == ':' && (f.i+1 == len(f.s) || f.s[f.i+1] == ' ') {
			break
		}
		f.i++
	}
	v := strings.TrimSpace(f.s[start:f.i])
	if v == "~" || v == "null" {
		return &node{kind: nullNode, line: f.no}
	}
	return &node{kind: scalarNode, value: v, line: f.no}
}

// TRLC-LINKS: REQ-DSO-1002
func (f *flow) collection(closer byte) (*node, error) {
	f.i++ // the opener
	n := &node{kind: seqNode, line: f.no}
	if closer == '}' {
		n = &node{kind: mapNode, m: map[string]*node{}, line: f.no}
	}
	for {
		f.space()
		if f.i >= len(f.s) {
			return nil, f.errf("unclosed %q", closer)
		}
		if f.s[f.i] == closer {
			f.i++
			return n, nil
		}
		if closer == '}' {
			k, err := f.value()
			if err != nil {
				return nil, err
			}
			if k.kind != scalarNode {
				return nil, f.errf("a key must be a scalar")
			}
			f.space()
			if f.i >= len(f.s) || f.s[f.i] != ':' {
				return nil, f.errf("want \"key: value\" in {}")
			}
			f.i++
			v, err := f.value()
			if err != nil {
				return nil, err
			}
			if _, dup := n.m[k.value]; dup {
				return nil, f.errf("duplicate key %q", k.value)
			}
			n.keys = append(n.keys, k.value)
			n.m[k.value] = v
		} else {
			v, err := f.value()
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, v)
		}
		f.space()
		if f.i < len(f.s) && f.s[f.i] == ',' {
			f.i++
			continue
		}
		if f.i >= len(f.s) || f.s[f.i] != closer {
			return nil, f.errf("want ',' or %q", closer)
		}
	}
}
