// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Setting, role and protocol names the generators share.
const (
	setBaud     = "baud"
	setParity   = "parity"
	parityEven  = "even"
	setRate     = "rate"
	setMode     = "mode"
	setBits     = "bits"
	setPeriodUS = "period_us"
	setCarrier  = "carrier"
	payBytes    = "bytes"
	roleData    = "data"
	protoUART   = "uart"
	protoMDIO   = "mdio"
	rate100k    = "100k"
)

// Bus is a bus laid out on its lines, with what a decoder must read from
// it.
type Bus struct {
	Proto string   // the decoder's name (the decode package's Result.Proto)
	Name  string   // what was written: "UART 115200 8N1"
	Roles []string // the lines' roles in order: "tx"; "clk", "data"; "scl", "sda"; ...
	Lines []*Line  // one per role, written in step
	// Period is the bus's length in seconds: a signal source repeats it.
	Period float64
	// Continuous is a bus a source repeats with no idle between: an output
	// that runs on (PWM), whose period a pause would break.
	Continuous bool
	// SR is the sample rate the lines were written at (0: steps only), and
	// NaturalSR the rate the decoders' own tests read this bus at.
	SR, NaturalSR float64
	Truth         Truth
	Spec          Spec // the request, its defaults filled in
}

// Truth is the decode's ground truth.
type Truth struct {
	// Bytes is what the decoder's Result.Bytes holds for this bus, in order
	// (nil: the decode is checked by Contains alone).
	Bytes []int
	// Contains are pieces of the decoder's Result.Text (its tokens as the
	// decode strip shows them) the decode must hold.
	Contains []string
	// Alike are the other names a detection may give this bus without
	// being wrong (a LIN frame is also UART characters).
	Alike []string
	// Layer is the application layer the bus carries, the one a layer
	// detection must name ("" none).
	Layer string
	// Mirrored is what the decoder's Result.Bytes holds for this bus read
	// with its two lines the other way round, when that reads as the same
	// bus (nil: it does not): a quadrature encoder's direction is which of
	// its lines is called A, so a detection may call either and read the
	// steps backwards.
	Mirrored []int
	// Messages are the bus's messages as the decode's text shows each, for
	// a bus of several: a record framed to one message (AUTO in BUS frames
	// the longest) holds a whole one of them and maybe not the rest.
	Messages []string
}

// Spec asks for a bus: the protocol, its settings (each protocol's names:
// Settings lists them; missing ones take their defaults) and the payload
// (nil: the protocol's default).
type Spec struct {
	Proto   string
	Set     map[string]string
	Payload []int
}

// String names the spec as a bench line would: "uart baud=9600 parity=even".
// TRLC-LINKS: REQ-DSO-018
func (s Spec) String() string {
	b := strings.Builder{}
	b.WriteString(s.Proto)
	for _, k := range slices.Sorted(maps.Keys(s.Set)) {
		fmt.Fprintf(&b, " %s=%s", k, s.Set[k])
	}
	if len(s.Payload) > 0 {
		b.WriteString(" data=")
		for _, v := range s.Payload {
			fmt.Fprintf(&b, "%02x", v)
		}
	}
	return b.String()
}

// Protocol describes one protocol's generator.
type Protocol struct {
	Proto    string
	Title    string
	Roles    []string          // the lines, in the order Bus.Lines holds them
	Defaults map[string]string // every setting and its default
	Payload  string            // what the payload is: "bytes", "words16", ...
	// Variants are the settings the AUTO sweep runs (up to three): rates,
	// modes and framings the bus is met at.
	Variants []map[string]string
	make     func(c *ctx) error
}

// Protocols lists every protocol the generators write, by name.
// TRLC-LINKS: REQ-DSO-018
func Protocols() []Protocol {
	out := slices.Clone(protocols)
	slices.SortFunc(out, func(a, b Protocol) int { return strings.Compare(a.Proto, b.Proto) })
	return out
}

// Lookup finds a protocol's generator.
// TRLC-LINKS: REQ-DSO-018
func Lookup(proto string) (Protocol, bool) {
	for _, p := range protocols {
		if p.Proto == proto {
			return p, true
		}
	}
	return Protocol{}, false
}

// Make lays the bus s asks for out at sr samples a second (0: steps only,
// for a model that samples them itself).
// TRLC-LINKS: REQ-DSO-018
func Make(s Spec, sr float64) (Bus, error) {
	p, ok := Lookup(s.Proto)
	if !ok {
		return Bus{}, fmt.Errorf("synth: no protocol %q", s.Proto)
	}
	set := maps.Clone(p.Defaults)
	for k, v := range s.Set {
		if _, known := p.Defaults[k]; !known {
			return Bus{}, fmt.Errorf("synth: %s has no setting %q (%s)", s.Proto, k, strings.Join(slices.Sorted(maps.Keys(p.Defaults)), " "))
		}
		set[k] = v
	}
	c := &ctx{set: set, payload: s.Payload, sr: sr}
	c.bus = Bus{Proto: p.Proto, Roles: p.Roles, SR: sr, Spec: Spec{Proto: p.Proto, Set: set, Payload: s.Payload}}
	if err := p.make(c); err != nil {
		return Bus{}, fmt.Errorf("synth: %s: %w", s.Proto, err)
	}
	if c.err != nil {
		return Bus{}, fmt.Errorf("synth: %s: %w", s.Proto, c.err)
	}
	b := c.bus
	if len(b.Lines) != len(b.Roles) {
		return Bus{}, fmt.Errorf("synth: %s wrote %d lines for %d roles", s.Proto, len(b.Lines), len(b.Roles))
	}
	for _, l := range b.Lines {
		b.Period = max(b.Period, l.T)
	}
	b.Spec.Payload = c.payload
	return b, nil
}

// Variants are the specs of a protocol's AUTO sweep: its variants with
// the default payload (the defaults alone when it has none).
// TRLC-LINKS: REQ-DSO-018
func Variants(proto string) []Spec {
	p, ok := Lookup(proto)
	if !ok {
		return nil
	}
	if len(p.Variants) == 0 {
		return []Spec{{Proto: proto}}
	}
	out := make([]Spec, 0, len(p.Variants))
	for _, v := range p.Variants {
		out = append(out, Spec{Proto: proto, Set: maps.Clone(v)})
	}
	return out
}

// ctx is one Make: the settings, the payload and the bus being written.
type ctx struct {
	set     map[string]string
	payload []int
	sr      float64
	bus     Bus
	err     error
}

// TRLC-LINKS: REQ-DSO-018
func (c *ctx) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// num is a numeric setting (a rate may carry k or M).
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) num(k string) float64 {
	v := c.set[k]
	mul := 1.0
	switch {
	case strings.HasSuffix(v, "k"):
		mul, v = 1e3, strings.TrimSuffix(v, "k")
	case strings.HasSuffix(v, "M"):
		mul, v = 1e6, strings.TrimSuffix(v, "M")
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil {
		c.fail(fmt.Errorf("%s=%q: not a number", k, c.set[k]))
		return 0
	}
	return x * mul
}

// integer is an integer setting (0x for hex).
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) integer(k string) int {
	x, err := strconv.ParseInt(c.set[k], 0, 64)
	if err != nil {
		c.fail(fmt.Errorf("%s=%q: not an integer", k, c.set[k]))
	}
	return int(x)
}

// TRLC-LINKS: REQ-DSO-018
func (c *ctx) str(k string) string { return c.set[k] }

// TRLC-LINKS: REQ-DSO-018
func (c *ctx) flag(k string) bool {
	v := c.set[k]
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// data is the payload, the default when none was given, each value
// masked to bits.
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) data(def []int, bits int) []int {
	if c.payload == nil {
		c.payload = append([]int(nil), def...)
	}
	out := make([]int, len(c.payload))
	for i, v := range c.payload {
		out[i] = v & (1<<bits - 1)
	}
	c.payload = out
	return out
}

// line starts a line at the bus's sample rate.
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) line() *Line { return NewLine(c.sr) }

// pair starts two lines at the bus's sample rate.
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) pair() Pair { return NewPair(c.sr) }

// done finishes the bus: its name, natural sample rate, lines and truth.
// TRLC-LINKS: REQ-DSO-018
func (c *ctx) done(name string, natural float64, t Truth, lines ...*Line) {
	c.bus.Name, c.bus.NaturalSR, c.bus.Truth = name, natural, t
	c.bus.Lines = lines
}

// chunks splits b into groups of n (the last may be shorter).
// TRLC-LINKS: REQ-DSO-018
func chunks(b []int, n int) [][]int {
	var out [][]int
	for len(b) > 0 {
		k := min(n, len(b))
		out = append(out, b[:k])
		b = b[k:]
	}
	return out
}

// Waves are the bus's lines as periodic signals, the bus repeating after
// idle seconds of idle (idle < 0: a tenth of its length), in Roles' order.
// TRLC-LINKS: REQ-DSO-018
func (b Bus) Waves(idle float64) []Wave {
	if idle < 0 {
		idle = b.Period / 10
	}
	out := make([]Wave, len(b.Lines))
	for i, l := range b.Lines {
		out[i] = l.Wave(b.Period + idle)
	}
	return out
}
