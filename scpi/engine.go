package scpi

import (
	"bytes"
	"slices"
	"strings"
	"sync"

	"github.com/labeth/open-dso-core/hal"
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
	// Line: source key ("", "C#:", "C#-C#:") → upper-cased header → command
	exact      map[string]map[string]*command
	singleKeys map[string]string // source prefix → its single key, C#:
	pairKeys   map[string]string // source prefix → its pair key, C#-C#:
}

type command struct {
	Command
	pat Pattern
}

// New is an engine speaking d to inst (which may be nil for a dialect whose
// handlers keep their own state).
// TRLC-LINKS: REQ-DSO-024
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
// TRLC-LINKS: REQ-DSO-024
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

// TRLC-LINKS: REQ-DSO-024
func (e *Engine) index() {
	e.byRoot = map[string][]*command{}
	e.common = map[string]*command{}
	e.exact = map[string]map[string]*command{"": {}}
	e.singleKeys, e.pairKeys = map[string]string{}, map[string]string{}
	for _, s := range e.d.Sources {
		p := strings.ToUpper(s.Prefix)
		e.singleKeys[p], e.pairKeys[p] = p+"#:", p+"#-"+p+"#:"
		e.exact[p+"#:"], e.exact[p+"#-"+p+"#:"] = map[string]*command{}, map[string]*command{}
	}
	for _, c := range e.cmds {
		up := strings.ToUpper(c.Pattern)
		key := ""
		for k := range e.exact {
			if k != "" && strings.HasPrefix(up, k) && len(k) > len(key) {
				key = k
			}
		}
		e.exact[key][up[len(key):]] = c
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
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) Dialect() *Dialect { return &e.d }

// Instrument is the instrument the engine binds to.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) Instrument() Instrument { return e.inst }

// Do runs fn with the engine's lock held, so state the handlers keep can be
// changed between messages.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) Do(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn()
}

// STB is the status byte (a VXI-11 readstb or a USB-TMC status request).
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) STB() uint8 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st.byte(len(e.errs.q) > 0)
}

// lookup finds the IEEE command for a complete header.
// TRLC-LINKS: REQ-DSO-024
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
		cands = append(slices.Clip(cands), e.byRoot[first[:j]]...)
	}
	for _, c := range cands {
		if suf, ok := c.pat.Match(header); ok {
			return c, suf
		}
	}
	return nil, nil
}

// pushError queues an error and sets the ESR bit of its class.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) pushError(code int, info string) {
	e.pushes++
	if e.st.rule == NoStatus {
		return
	}
	e.errs.push(queued{code: code, info: info})
	e.st.setESR(e.st.esr | esrBit(code))
}

// Report queues an error raised outside a program message: a setting a
// command staged that the board refused when the owner committed it. A
// *hal.Refusal is a settings conflict with the refusal as its text; any
// other error is an execution error. A dialect without a status model
// keeps no queue, so nothing is reported there.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) Report(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := hal.AsRefusal(err); ok {
		e.pushError(ErrSettingsConflict, strings.ReplaceAll(r.Error(), `"`, `'`))
		return
	}
	e.pushError(ErrExecution, "")
}

// Exec runs one program message (without its transport terminator) and
// returns the reply bytes, possibly none.
// TRLC-LINKS: REQ-DSO-024
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
// TRLC-LINKS: REQ-DSO-024
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
		c := &Call{
			e: e, Header: hdr, Suffix: suf, arg: u.params, params: toks, out: &out,
			pendingSemi: lastOut && strings.HasSuffix(hdr, "?"),
		}
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
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) execLine(msg []byte) []byte {
	var out bytes.Buffer
	c := &Call{e: e, out: &out}
	rest := strings.TrimRight(string(msg), "\r\n")
	for more := true; more; {
		var part string
		part, rest, more = strings.Cut(rest, ";")
		if part = strings.TrimSpace(part); part != "" {
			e.lineUnit(c, part)
		}
	}
	return out.Bytes()
}

// lineFail answers code in place.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) lineFail(out *bytes.Buffer, code int) {
	out.WriteString(errorText(e.d.Errors, code))
	out.WriteByte('\n')
}

// lineUnit runs one legacy unit, [source prefix]HEADER[ argument], on the
// message's call c.
// TRLC-LINKS: REQ-DSO-024
func (e *Engine) lineUnit(c *Call, unit string) {
	up := strings.ToUpper(unit)
	key, rest, nsuf, pair := "", up, 0, false
	for _, s := range e.d.Sources {
		p, n := s.Prefix, len(s.Prefix)
		if !strings.HasPrefix(up, p) {
			continue
		}
		if len(up) >= 2*n+4 && up[n+1] == '-' && up[2*n+3] == ':' && up[n+2:2*n+2] == p {
			// The pair form is known by its shape; any character in a digit's
			// place is a suffix out of range.
			a, b := int(up[n])-'0', int(up[2*n+2])-'0'
			if a < s.Min || a > s.Max || b < s.Min || b > s.Max || a == b {
				e.lineFail(c.out, ErrHeaderSuffixRange)
				return
			}
			key, rest, pair = e.pairKeys[p], up[2*n+4:], true
			c.sufBuf[0], c.sufBuf[1], nsuf = a, b, 2
			break
		}
		if len(up) >= n+2 && up[n+1] == ':' && up[n] >= '0' && up[n] <= '9' {
			a := int(up[n] - '0')
			if a < s.Min || a > s.Max {
				e.lineFail(c.out, ErrHeaderSuffixRange)
				return
			}
			key, rest = e.singleKeys[p], up[n+2:]
			c.sufBuf[0], nsuf = a, 1
			break
		}
	}
	head, arg := rest, ""
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		head, arg = rest[:i], strings.TrimSpace(rest[i+1:])
	}
	if !pair && (head == "" || strings.HasSuffix(head, ":")) {
		e.lineFail(c.out, ErrHeaderSeparator)
		return
	}
	cmd := e.exact[key][head]
	if cmd == nil {
		e.lineFail(c.out, ErrUndefinedHeader)
		return
	}
	c.Header, c.Suffix, c.arg, c.outputs = cmd.Pattern, c.sufBuf[:nsuf], arg, 0
	before := e.pushes
	if err := cmd.Handler(c); err != nil && e.pushes == before {
		e.lineFail(c.out, ErrExecution)
	}
}
