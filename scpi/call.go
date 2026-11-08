package scpi

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/labeth/open-dso-core/hal"
)

// ErrFailed makes the engine report -200 (a handler that cannot run and
// has no more precise error).
var ErrFailed = errors.New("scpi: execution failed")

// ErrReported is returned by the parameter helpers after they reported an
// SCPI error; a handler passes it on.
var ErrReported = errors.New("scpi: error reported")

// Call is one program unit being executed.
type Call struct {
	e      *Engine
	Header string // the complete header as received (path applied, IEEE)
	Suffix []int  // numeric suffixes, one per '#' node or source digit

	arg         string
	params      []Token
	next        int
	out         *bytes.Buffer
	outputs     int
	pendingSemi bool
	sufBuf      [2]int
}

// Engine is the engine running the call.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Engine() *Engine { return c.e }

// Instrument is the engine's instrument.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Instrument() Instrument { return c.e.inst }

// Query reports whether the header is a query.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Query() bool { return strings.HasSuffix(c.Header, "?") }

// N is the i-th numeric suffix; 1 when absent.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) N(i int) int {
	if i < len(c.Suffix) {
		return c.Suffix[i]
	}
	return 1
}

// Arg is the unit's parameter text as received (Line: upper-cased, trimmed).
// Reading it consumes every parameter.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Arg() string {
	c.next = len(c.params)
	return c.arg
}

// Unlocked runs fn with the engine's lock released: for a callback that may
// itself reach state guarded by the engine (a render, the panel).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Unlocked(fn func()) {
	c.e.mu.Unlock()
	defer c.e.mu.Lock()
	fn()
}

// Error reports an SCPI error: queued (IEEE), or answered in place with its
// text (Line). It returns ErrReported.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Error(code int) error {
	if c.e.d.Grammar == Line {
		c.e.pushes++
		c.out.WriteString(errorText(c.e.d.Errors, code))
		c.out.WriteByte('\n')
		return ErrReported
	}
	c.e.pushError(code, "")
	return ErrReported
}

// Refused reports a setting the instrument refused, as Report does from the
// owner: a *hal.Refusal is -221 with its text (in the IEEE grammar; Line
// answers the code's text), anything else -200. It returns ErrReported.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Refused(err error) error {
	r, ok := hal.AsRefusal(err)
	if !ok {
		return c.Error(ErrExecution)
	}
	if c.e.d.Grammar == Line {
		return c.Error(ErrSettingsConflict)
	}
	c.e.pushError(ErrSettingsConflict, strings.ReplaceAll(r.Error(), `"`, `'`))
	return ErrReported
}

// HeaderMode is the CHDR state.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) HeaderMode() HeaderMode { return c.e.hdr }

// SetHeaderMode sets the CHDR state.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) SetHeaderMode(m HeaderMode) { c.e.hdr = m }

// Digits is the number of significant digits of NR3 replies.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Digits() int { return c.e.digits }

// SetDigits sets the significant digits of NR3 replies (:FORMat:DATA).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) SetDigits(n int) { c.e.digits = max(n, 1) }

// --- parameters (IEEE) ---

// Count is the number of parameters of the unit.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Count() int { return len(c.params) }

// Remaining is the number of parameters not read yet.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Remaining() int { return len(c.params) - c.next }

// Param returns the next parameter; a missing mandatory one is -109.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Param(mandatory bool) (Token, bool, error) {
	if c.next >= len(c.params) {
		if mandatory {
			return Token{}, false, c.Error(ErrMissingParam)
		}
		return Token{}, false, nil
	}
	t := c.params[c.next]
	c.next++
	return t, true, nil
}

// Special numeric values.
const (
	SpecNone = iota
	SpecMin
	SpecMax
	SpecDef
)

// Number reads a decimal parameter with an optional unit (the multiplier
// applied); MIN, MAX and DEF return their special code.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Number(mandatory bool) (v float64, special int, present bool, err error) {
	t, ok, err := c.Param(mandatory)
	if err != nil || !ok {
		return 0, 0, false, err
	}
	switch t.Kind {
	case TokNumber:
		m, _, ok := ResolveUnit(t.Unit)
		if !ok {
			return 0, 0, true, c.Error(ErrInvalidSuffix)
		}
		return t.Num * m, SpecNone, true, nil
	case TokNonDec:
		return t.Num, SpecNone, true, nil
	case TokMnemonic:
		switch {
		case MatchChoice("MINimum", t.Str):
			return 0, SpecMin, true, nil
		case MatchChoice("MAXimum", t.Str):
			return 0, SpecMax, true, nil
		case MatchChoice("DEFault", t.Str):
			return 0, SpecDef, true, nil
		}
	case TokString, TokBlock, TokExpr:
	}
	return 0, 0, true, c.Error(ErrDataType)
}

// Float reads a plain number (MIN/MAX/DEF are -104).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Float(mandatory bool) (float64, bool, error) {
	v, sp, ok, err := c.Number(mandatory)
	if err != nil || !ok {
		return 0, ok, err
	}
	if sp != SpecNone {
		return 0, true, c.Error(ErrDataType)
	}
	return v, true, nil
}

// Int reads an integer parameter (decimal or #H/#B/#Q).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Int(mandatory bool) (int64, bool, error) {
	v, ok, err := c.Float(mandatory)
	if err != nil || !ok {
		return 0, ok, err
	}
	return int64(math.Round(v)), true, nil
}

// Bool reads ON/OFF or a number (non-zero is true).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Bool(mandatory bool) (bool, bool, error) {
	t, ok, err := c.Param(mandatory)
	if err != nil || !ok {
		return false, ok, err
	}
	switch t.Kind {
	case TokNumber, TokNonDec:
		return t.Num != 0, true, nil
	case TokMnemonic:
		switch {
		case strings.EqualFold(t.Str, "ON"):
			return true, true, nil
		case strings.EqualFold(t.Str, "OFF"):
			return false, true, nil
		}
		return false, true, c.Error(ErrIllegalParamValue)
	case TokString, TokBlock, TokExpr:
	}
	return false, true, c.Error(ErrDataType)
}

// Choice matches the next parameter against choice patterns (short or long
// form; a numeric token by its text, so "20M" and "1MIN" are choices) and
// returns the index; an unknown choice is -224.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Choice(mandatory bool, choices ...string) (int, bool, error) {
	t, ok, err := c.Param(mandatory)
	if err != nil || !ok {
		return -1, ok, err
	}
	var txt string
	switch t.Kind {
	case TokMnemonic, TokString:
		txt = t.Str
	case TokNumber:
		txt = strings.ReplaceAll(t.Raw, " ", "")
	case TokNonDec, TokBlock, TokExpr:
		return -1, true, c.Error(ErrDataType)
	}
	for i, ch := range choices {
		if MatchChoice(ch, txt) || strings.EqualFold(ch, txt) {
			return i, true, nil
		}
	}
	return -1, true, c.Error(ErrIllegalParamValue)
}

// String reads a quoted string (character data is accepted as well).
// TRLC-LINKS: REQ-DSO-024
func (c *Call) String(mandatory bool) (string, bool, error) {
	t, ok, err := c.Param(mandatory)
	if err != nil || !ok {
		return "", ok, err
	}
	switch t.Kind {
	case TokString, TokMnemonic:
		return t.Str, true, nil
	case TokNumber:
		return t.Raw, true, nil
	case TokNonDec, TokBlock, TokExpr:
	}
	return "", true, c.Error(ErrDataType)
}

// --- replies ---

// Text writes one reply value: IEEE values of one unit are joined by ','
// and units by ';'; a Line reply ends with its own LF.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Text(s string) {
	if c.e.d.Grammar == Line {
		c.out.WriteString(s)
		c.out.WriteByte('\n')
		c.outputs++
		return
	}
	switch {
	case c.outputs == 0 && c.pendingSemi:
		c.out.WriteByte(';')
	case c.outputs > 0:
		c.out.WriteByte(',')
	}
	c.out.WriteString(s)
	c.outputs++
}

// Raw writes bytes as they are (binary blocks, a screen image): no
// delimiter after the first value and, in the Line grammar, no LF.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Raw(b []byte) {
	if c.outputs == 0 && c.pendingSemi {
		c.out.WriteByte(';')
	}
	c.out.Write(b)
	c.outputs++
}

// NR3 writes v in %E form with the engine's significant digits.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) NR3(v float64) { c.Text(FormatNR3(v, c.e.digits)) }

// NR1 writes an integer.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) NR1(v int64) { c.Text(strconv.FormatInt(v, 10)) }

// Reply writes a legacy reply under its short header: "<header> <value>"
// in CHDR SHORT and LONG, the value alone in OFF. Common commands and
// long-form headers of the IEEE grammar never carry a header.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Reply(header, value string) { c.Legacy(header, "", value, "") }

// Legacy writes a legacy reply: the short or long header (long only when
// the dialect has long headers; "" falls back to the short one), the value
// and its unit, which CHDR OFF drops when the dialect says so.
// TRLC-LINKS: REQ-DSO-024
func (c *Call) Legacy(short, long, value, unit string) {
	if c.e.d.Grammar == IEEE && (strings.HasPrefix(short, "*") || strings.HasPrefix(short, ":")) {
		c.Text(value)
		return
	}
	switch c.e.hdr {
	case HeaderOff:
		if c.e.d.Header.OffDropsUnit {
			unit = ""
		}
		c.Text(value + unit)
	case HeaderLong:
		h := short
		if c.e.d.Header.Long && long != "" {
			h = long
		}
		c.Text(h + " " + value + unit)
	case HeaderShort:
		c.Text(short + " " + value + unit)
	}
}

// FormatNR3 formats v like printf("%.<digits-1>E").
// TRLC-LINKS: REQ-DSO-024
func FormatNR3(v float64, digits int) string {
	return fmt.Sprintf("%.*E", max(digits, 1)-1, v)
}
