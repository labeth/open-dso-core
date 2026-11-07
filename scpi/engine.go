package scpi

import (
	"bytes"
	"strings"
	"sync"
)

// Grammar selects how a program message is split, matched and answered.
type Grammar int

// The grammars.
const (
	// IEEE is the libscpi grammar (see the package documentation).
	IEEE Grammar = iota
	// Line is the legacy short-form grammar (see the package documentation).
	Line
)

// Handler runs one program unit. A non-nil error without an error the
// handler reported through Call.Error is answered as -200.
type Handler func(c *Call) error

// Command is one header pattern and its handler. IEEE patterns use the
// libscpi syntax (":CHANnel#:SCALe?", "[...]" optional nodes, '#' numeric
// suffix); Line patterns are the one spelling of a header, with '#' for the
// digit of a source prefix ("C#:VDIV?", "C#-C#:MEAD?", "TDIV").
type Command struct {
	Pattern string
	Handler Handler
}

// Source is a Line-grammar source prefix: Prefix followed by one digit in
// [Min, Max] and ':' ("C1:"), or a pair of distinct ones ("C1-C2:").
type Source struct {
	Prefix   string
	Min, Max int
}

// HeaderRules are a dialect's legacy reply headers (CHDR).
type HeaderRules struct {
	// Long is true when CHDR LONG answers with long headers; otherwise LONG
	// (and ON) select the short form.
	Long bool
	// OffDropsUnit is true when CHDR OFF answers the value without its unit.
	OffDropsUnit bool
}

// Dialect is a scope's command language.
type Dialect struct {
	Name     string
	Grammar  Grammar
	Commands []Command
	Sources  []Source       // Line grammar source prefixes
	Header   HeaderRules    // legacy reply headers
	Errors   map[int]string // error texts; nil: StandardErrors
	Status   Summaries      // status summary rule
	Digits   int            // NR3 significant digits at start (0: 3)
	MaxInput int            // largest program message, bytes (0: no limit)
}

// HeaderMode is the CHDR state.
type HeaderMode int

// The CHDR states.
const (
	HeaderShort HeaderMode = iota
	HeaderLong
	HeaderOff
)

// Engine runs a dialect's commands against an instrument. Every transport
// shares one engine; it executes one program message at a time.
type Engine struct {
	mu     sync.Mutex
	d      Dialect
	inst   Instrument
	errs   errorQueue
	st     status
	hdr    HeaderMode
	digits int
	pushes int

	cmds   []*command
	byRoot map[string][]*command // IEEE: first node (long and short) → commands
	common map[string]*command   // IEEE: "*IDN?" → command
	exact  map[string]*command   // Line: upper-cased pattern → command
}

type command struct {
	Command
	pat Pattern
}

// New is an engine speaking d to inst (which may be nil for a dialect whose
// handlers keep their own state).
func New(d Dialect, inst Instrument) *Engine {
	e := &Engine{d: d, inst: inst, digits: d.Digits}
	if e.digits <= 0 {
		e.digits = 3
	}
	if e.d.Errors == nil {
		e.d.Errors = StandardErrors
	}
	e.st.rule = d.Status
	e.Register(d.Commands...)
	return e
}

// Register adds commands; a pattern registered again replaces the earlier
// handler, so a dialect can override a shared command.
func (e *Engine) Register(cs ...Command) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range cs {
		nc := &command{Command: c, pat: CompilePattern(c.Pattern)}
		replaced := false
		for i, old := range e.cmds {
			if strings.EqualFold(old.Pattern, c.Pattern) {
				e.cmds[i], replaced = nc, true
			}
		}
		if !replaced {
			e.cmds = append(e.cmds, nc)
		}
	}
	e.index()
}

func (e *Engine) index() {
	e.byRoot = map[string][]*command{}
	e.common = map[string]*command{}
	e.exact = map[string]*command{}
	for _, c := range e.cmds {
		e.exact[strings.ToUpper(c.Pattern)] = c
		if c.pat.common {
			e.common[strings.ToUpper(c.Pattern)] = c
			continue
		}
		for _, n := range c.pat.nodes {
			e.byRoot[n.long] = append(e.byRoot[n.long], c)
			if n.short != n.long {
				e.byRoot[n.short] = append(e.byRoot[n.short], c)
			}
			if !n.optional {
				break
			}
		}
	}
}

// Dialect is the engine's dialect.
func (e *Engine) Dialect() *Dialect { return &e.d }

// Instrument is the instrument the engine binds to.
func (e *Engine) Instrument() Instrument { return e.inst }

// Do runs fn with the engine's lock held, so state the handlers keep can be
// changed between messages.
func (e *Engine) Do(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn()
}

// STB is the status byte (a VXI-11 readstb or a USB-TMC status request).
func (e *Engine) STB() uint8 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st.byte(len(e.errs.q) > 0)
}

// lookup finds the IEEE command for a complete header.
func (e *Engine) lookup(header string) (*command, []int) {
	if strings.HasPrefix(header, "*") {
		return e.common[strings.ToUpper(header)], nil
	}
	h := strings.TrimPrefix(header, ":")
	first, _, _ := strings.Cut(h, ":")
	first = strings.ToUpper(strings.TrimSuffix(first, "?"))
	j := len(first)
	for j > 0 && first[j-1] >= '0' && first[j-1] <= '9' {
		j--
	}
	cands := e.byRoot[first]
	if j < len(first) {
		cands = append(cands[:len(cands):len(cands)], e.byRoot[first[:j]]...)
	}
	for _, c := range cands {
		if suf, ok := c.pat.Match(header); ok {
			return c, suf
		}
	}
	return nil, nil
}

// pushError queues an error and sets the ESR bit of its class.
func (e *Engine) pushError(code int, info string) {
	e.pushes++
	if e.st.rule == NoStatus {
		return
	}
	e.errs.push(queued{code: code, info: info})
	e.st.setESR(e.st.esr | esrBit(code))
}

// Exec runs one program message (without its transport terminator) and
// returns the reply bytes, possibly none.
func (e *Engine) Exec(msg []byte) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.d.MaxInput > 0 && len(msg) > e.d.MaxInput {
		e.pushError(ErrInputBufferOverrun, "")
		return nil
	}
	if e.d.Grammar == Line {
		return e.execLine(msg)
	}
	return e.execIEEE(msg)
}

// execIEEE is the libscpi message algorithm: ';' only before the output of a
// query that follows a unit with output, one LF after a message whose last
// executed unit wrote output; an unknown header leaves the output state as
// it was.
func (e *Engine) execIEEE(msg []byte) []byte {
	var out bytes.Buffer
	path := ""
	lastOut := false
	for _, u := range splitUnits(msg) {
		if !validHeader(u.header) {
			e.pushError(ErrInvalidCharacter, "")
			continue
		}
		hdr := u.header
		if !strings.HasPrefix(hdr, ":") && !strings.HasPrefix(hdr, "*") && path != "" {
			hdr = path + hdr
		}
		cmd, suf := e.lookup(hdr)
		if cmd == nil {
			e.pushError(ErrUndefinedHeader, u.text)
			continue
		}
		if !strings.HasPrefix(hdr, "*") {
			path = ""
			if i := strings.LastIndexByte(hdr, ':'); i >= 0 {
				path = hdr[:i+1]
			}
		}
		toks, ok := lexParams(u.params)
		if !ok {
			e.pushError(ErrSyntax, "")
			continue
		}
		c := &Call{e: e, Header: hdr, Suffix: suf, arg: u.params, params: toks, out: &out,
			pendingSemi: lastOut && strings.HasSuffix(hdr, "?")}
		before := e.pushes
		err := cmd.Handler(c)
		pushed := e.pushes != before
		if err != nil && !pushed {
			e.pushError(ErrExecution, "")
			pushed = true
		}
		if !pushed && c.next < len(c.params) {
			e.pushError(ErrParamNotAllowed, "")
		}
		lastOut = c.outputs > 0
	}
	if lastOut {
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// execLine runs each ';'-separated unit on its own (no quoting: the legacy
// grammar has none) and concatenates the replies.
func (e *Engine) execLine(msg []byte) []byte {
	var out bytes.Buffer
	rest := strings.TrimRight(string(msg), "\r\n")
	for more := true; more; {
		var part string
		part, rest, more = strings.Cut(rest, ";")
		if part = strings.TrimSpace(part); part != "" {
			e.lineUnit(part, &out)
		}
	}
	return out.Bytes()
}

// lineUnit runs one legacy unit: [source prefix]HEADER[ argument].
func (e *Engine) lineUnit(unit string, out *bytes.Buffer) {
	up := strings.ToUpper(unit)
	fail := func(code int) {
		out.WriteString(errorText(e.d.Errors, code))
		out.WriteByte('\n')
	}
	key, rest, suf, pair := "", up, []int(nil), false
	for _, s := range e.d.Sources {
		p := s.Prefix
		digit := func(i int) (int, bool) {
			if i < len(up) && up[i] >= '0' && up[i] <= '9' {
				return int(up[i] - '0'), true
			}
			return 0, false
		}
		in := func(n int) bool { return n >= s.Min && n <= s.Max }
		n := len(p)
		if strings.HasPrefix(up, p) && len(up) >= 2*n+4 && up[n+1] == '-' && up[2*n+3] == ':' &&
			up[n+2:2*n+2] == p {
			// The pair form is known by its shape; any character in a digit's
			// place is a suffix out of range.
			a, b := int(up[n])-'0', int(up[2*n+2])-'0'
			if !in(a) || !in(b) || a == b {
				fail(ErrHeaderSuffixRange)
				return
			}
			key, rest, suf, pair = p+"#-"+p+"#:", up[2*n+4:], []int{a, b}, true
			break
		}
		if strings.HasPrefix(up, p) && len(up) >= n+2 && up[n+1] == ':' {
			if a, ok := digit(n); ok {
				if !in(a) {
					fail(ErrHeaderSuffixRange)
					return
				}
				key, rest, suf = p+"#:", up[n+2:], []int{a}
				break
			}
		}
	}
	head, arg := rest, ""
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		head, arg = rest[:i], strings.TrimSpace(rest[i+1:])
	}
	if !pair && (head == "" || strings.HasSuffix(head, ":")) {
		fail(ErrHeaderSeparator)
		return
	}
	cmd := e.exact[key+head]
	if cmd == nil {
		fail(ErrUndefinedHeader)
		return
	}
	c := &Call{e: e, Header: key + head, Suffix: suf, arg: arg, out: out}
	before := e.pushes
	if err := cmd.Handler(c); err != nil && e.pushes == before {
		fail(ErrExecution)
	}
}
