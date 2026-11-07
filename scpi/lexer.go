package scpi

import (
	"strconv"
	"strings"
)

// TokenKind classifies a program data element.
type TokenKind int

const (
	TokNumber   TokenKind = iota // decimal numeric (with optional suffix/unit)
	TokNonDec                    // #H / #B / #Q
	TokMnemonic                  // character program data
	TokString                    // quoted string
	TokBlock                     // arbitrary block
	TokExpr                      // ( ... )
)

// Token is one parameter.
type Token struct {
	Kind TokenKind
	Raw  string  // source text (trimmed)
	Num  float64 // TokNumber (before unit multiplier), TokNonDec
	Unit string  // TokNumber suffix, upper-cased
	Str  string  // TokString content, TokMnemonic text
	Data []byte  // TokBlock payload
}

// unit is one program message unit.
type unit struct {
	text   string // trimmed unit text (for error info)
	header string
	params string
}

// TRLC-LINKS: REQ-DSO-024
func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// splitUnits splits a program message at ';' outside strings, blocks and expressions.
// TRLC-LINKS: REQ-DSO-024
func splitUnits(msg []byte) []unit {
	var units []unit
	start := 0
	add := func(end int) {
		t := strings.TrimFunc(string(msg[start:end]), func(r rune) bool { return r < 0x80 && isWS(byte(r)) }) //nolint:gosec // G115: guarded, r < 0x80
		start = end + 1
		if t == "" {
			return
		}
		u := unit{text: t}
		if end < len(msg) {
			u.text += ";" // the -113 information keeps the unit separator ("A1;")
		}
		i := 0
		for i < len(t) && !isWS(t[i]) {
			i++
		}
		u.header = t[:i]
		u.params = strings.TrimLeft(t[i:], " \t\r\n")
		units = append(units, u)
	}
	depth := 0
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		switch {
		case c == '"' || c == '\'':
			for j := i + 1; j < len(msg); j++ {
				if msg[j] == c {
					if j+1 < len(msg) && msg[j+1] == c {
						j++
						continue
					}
					i = j
					break
				}
				if j == len(msg)-1 {
					i = j
				}
			}
		case c == '#' && i+1 < len(msg) && msg[i+1] >= '1' && msg[i+1] <= '9':
			nd := int(msg[i+1] - '0')
			if i+2+nd <= len(msg) {
				if n, err := strconv.Atoi(string(msg[i+2 : i+2+nd])); err == nil && n >= 0 {
					end := min(i+2+nd+n, len(msg))
					i = end - 1
				}
			}
		case c == '#' && i+1 < len(msg) && msg[i+1] == '0':
			i = len(msg) - 1
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			add(i)
		}
	}
	add(len(msg))
	return units
}

// validHeader checks the program header syntax: "*MNEM[?]" or "[:]MNEM{:MNEM}[?]",
// MNEM = letter {letter|digit|_}.
// TRLC-LINKS: REQ-DSO-024
func validHeader(h string) bool {
	s := strings.TrimSuffix(h, "?")
	if s == "" {
		return false
	}
	if s[0] == '*' {
		s = s[1:]
		return validMnemonic(s)
	}
	s = strings.TrimPrefix(s, ":")
	for m := range strings.SplitSeq(s, ":") {
		if !validMnemonic(m) {
			return false
		}
	}
	return true
}

// TRLC-LINKS: REQ-DSO-024
func validMnemonic(m string) bool {
	if m == "" || !isAlpha(m[0]) {
		return false
	}
	for i := 1; i < len(m); i++ {
		c := m[i]
		if !isAlpha(c) && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// TRLC-LINKS: REQ-DSO-024
func isAlpha(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

// lexParams tokenises the program data of a unit. ok=false on a syntax error.
// TRLC-LINKS: REQ-DSO-024
func lexParams(s string) ([]Token, bool) {
	var toks []Token
	i := 0
	skip := func() {
		for i < len(s) && isWS(s[i]) {
			i++
		}
	}
	skip()
	if i == len(s) {
		return nil, true
	}
	for {
		skip()
		if i >= len(s) {
			return toks, false // dangling comma
		}
		start := i
		var t Token
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			var b strings.Builder
			i++
			closed := false
			for i < len(s) {
				if s[i] == c {
					if i+1 < len(s) && s[i+1] == c {
						b.WriteByte(c)
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return toks, false
			}
			t = Token{Kind: TokString, Str: b.String()}
		case c == '#' && i+1 < len(s) && strings.ContainsRune("HhBbQq", rune(s[i+1])):
			base := map[byte]int{'H': 16, 'B': 2, 'Q': 8}[s[i+1]&^0x20]
			i += 2
			j := i
			for i < len(s) && isAlnum(s[i]) {
				i++
			}
			v, err := strconv.ParseInt(s[j:i], base, 64)
			if err != nil {
				return toks, false
			}
			t = Token{Kind: TokNonDec, Num: float64(v)}
		case c == '#' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			nd := int(s[i+1] - '0')
			if nd == 0 {
				t = Token{Kind: TokBlock, Data: []byte(s[i+2:])}
				i = len(s)
				break
			}
			if i+2+nd > len(s) {
				return toks, false
			}
			n, err := strconv.Atoi(s[i+2 : i+2+nd])
			if err != nil || i+2+nd+n > len(s) {
				return toks, false
			}
			t = Token{Kind: TokBlock, Data: []byte(s[i+2+nd : i+2+nd+n])}
			i = i + 2 + nd + n
		case c == '(':
			d := 0
			for i < len(s) {
				if s[i] == '(' {
					d++
				} else if s[i] == ')' {
					d--
					if d == 0 {
						i++
						break
					}
				}
				i++
			}
			if d != 0 {
				return toks, false
			}
			t = Token{Kind: TokExpr, Str: s[start+1 : i-1]}
		case (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.':
			var ok bool
			t, ok = lexNumber(s, &i)
			if !ok {
				return toks, false
			}
		case isAlpha(c):
			for i < len(s) && (isAlnum(s[i]) || s[i] == '_') {
				i++
			}
			t = Token{Kind: TokMnemonic, Str: s[start:i]}
		default:
			return toks, false
		}
		t.Raw = strings.TrimRight(s[start:i], " \t\r\n")
		toks = append(toks, t)
		skip()
		if i >= len(s) {
			return toks, true
		}
		if s[i] != ',' {
			return toks, false
		}
		i++
	}
}

// TRLC-LINKS: REQ-DSO-024
func isAlnum(c byte) bool { return isAlpha(c) || (c >= '0' && c <= '9') }

// TRLC-LINKS: REQ-DSO-024
func lexNumber(s string, pi *int) (Token, bool) {
	i := *pi
	start := i
	if s[i] == '+' || s[i] == '-' {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return Token{}, false
	}
	mant := i
	// exponent: E[+-]digits (only if followed by a digit, otherwise it is a suffix)
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		for j < len(s) && s[j] == ' ' {
			j++
		}
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && s[j] >= '0' && s[j] <= '9' {
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			i = j
			mant = i
		}
	}
	numText := strings.ReplaceAll(s[start:mant], " ", "")
	v, err := strconv.ParseFloat(numText, 64)
	if err != nil {
		return Token{}, false
	}
	t := Token{Kind: TokNumber, Num: v}
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	k := j
	for k < len(s) && (isAlnum(s[k]) || s[k] == '/' || s[k] == '%' || s[k] == '_') {
		k++
	}
	if k > j && (isAlpha(s[j]) || s[j] == '%') {
		t.Unit = strings.ToUpper(s[j:k])
		i = k
	} else if k > i && j == i {
		// digits glued to digits cannot happen; anything else alnum glued = bad
		return Token{}, false
	}
	*pi = i
	return t, true
}
