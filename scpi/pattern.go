package scpi

import (
	"strconv"
	"strings"
)

// node is one mnemonic of a command pattern.
type node struct {
	long, short string // upper-cased forms
	optional    bool
	suffix      bool // pattern ends with '#': numeric suffix, default 1
}

// Pattern is a compiled header pattern such as ":CHANnel#:SCALe?" or "SYSTem:ERRor[:NEXT]?".
type Pattern struct {
	Text   string
	common bool // "*XXX"
	query  bool
	nodes  []node
}

// CompilePattern compiles a libscpi-style pattern: "[...]" optional nodes, '#' numeric
// suffix, short form = the upper-case prefix (plus trailing digits) of each node.
// TRLC-LINKS: REQ-DSO-024
func CompilePattern(p string) Pattern {
	cp := Pattern{Text: p}
	s := p
	if strings.HasSuffix(s, "?") {
		cp.query = true
		s = s[:len(s)-1]
	}
	if strings.HasPrefix(s, "*") {
		cp.common = true
		cp.nodes = []node{{long: strings.ToUpper(s), short: strings.ToUpper(s)}}
		return cp
	}
	// Tokenise into nodes, tracking brackets.
	opt := false
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		cp.nodes = append(cp.nodes, makeNode(cur.String(), opt))
		cur.Reset()
	}
	for i := range len(s) {
		switch c := s[i]; c {
		case '[':
			flush()
			opt = true
		case ']':
			flush()
			opt = false
		case ':':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return cp
}

// TRLC-LINKS: REQ-DSO-024
func makeNode(m string, optional bool) node {
	n := node{optional: optional}
	if strings.HasSuffix(m, "#") {
		n.suffix = true
		m = m[:len(m)-1]
	}
	// short form: leading upper-case letters/digits/underscore, then any trailing digits
	i := 0
	for i < len(m) && (m[i] < 'a' || m[i] > 'z') {
		i++
	}
	short := m[:i]
	j := len(m)
	for j > i && m[j-1] >= '0' && m[j-1] <= '9' {
		j--
	}
	if j < len(m) && i < len(m) {
		short += m[j:]
	}
	n.long = strings.ToUpper(m)
	n.short = strings.ToUpper(short)
	return n
}

// Match matches an input header (without parameters) against the pattern. It returns the
// numeric suffixes found (one per '#' node, default 1) and whether it matched.
// TRLC-LINKS: REQ-DSO-024
func (p *Pattern) Match(header string) ([]int, bool) {
	h := header
	q := strings.HasSuffix(h, "?")
	if q {
		h = h[:len(h)-1]
	}
	if q != p.query {
		return nil, false
	}
	if p.common {
		return nil, strings.EqualFold(h, p.nodes[0].long)
	}
	if strings.HasPrefix(h, "*") {
		return nil, false
	}
	h = strings.TrimPrefix(h, ":")
	parts := strings.Split(strings.ToUpper(h), ":")
	var suf []int
	if matchNodes(p.nodes, parts, &suf) {
		return suf, true
	}
	return nil, false
}

// TRLC-LINKS: REQ-DSO-024
func matchNodes(nodes []node, parts []string, suf *[]int) bool {
	if len(nodes) == 0 {
		return len(parts) == 0
	}
	n := nodes[0]
	if len(parts) > 0 {
		if v, ok := matchNode(n, parts[0]); ok && matchRest(nodes, parts[1:], suf, v) {
			return true
		}
	}
	return n.optional && matchRest(nodes, parts, suf, 1)
}

// matchRest takes nodes[0] as matched, with suffix value v when it has one,
// and matches the remaining nodes against parts; on a miss the suffixes are
// as they were.
// TRLC-LINKS: REQ-DSO-024
func matchRest(nodes []node, parts []string, suf *[]int, v int) bool {
	save := len(*suf)
	if nodes[0].suffix {
		*suf = append(*suf, v)
	}
	if matchNodes(nodes[1:], parts, suf) {
		return true
	}
	*suf = (*suf)[:save]
	return false
}

// TRLC-LINKS: REQ-DSO-024
func matchNode(n node, tok string) (int, bool) {
	if !n.suffix {
		return 0, tok == n.long || tok == n.short
	}
	j := len(tok)
	for j > 0 && tok[j-1] >= '0' && tok[j-1] <= '9' {
		j--
	}
	name, digits := tok[:j], tok[j:]
	if name != n.long && name != n.short {
		return 0, false
	}
	if digits == "" {
		return 1, true
	}
	v, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return v, true
}

// MatchChoice matches a character-data token against a choice pattern such as "ONEMeg"
// (short form = upper-case prefix, long form = whole word), case-insensitive.
// TRLC-LINKS: REQ-DSO-024
func MatchChoice(pattern, tok string) bool {
	n := makeNode(pattern, false)
	t := strings.ToUpper(tok)
	return t == n.long || t == n.short
}
