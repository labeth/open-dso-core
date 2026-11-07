package session

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Signal is one input's signal, described abstractly so every target can
// produce it its own way.
type Signal struct {
	// Kind: a waveform ("dc", "square", "sine", "pulse", "triangle") or a
	// protocol line ("uart", "spi", "i2c").
	Kind string `json:"kind"`
	// Hz is a waveform's repetition rate; Rate a protocol line's bit rate.
	Hz   float64 `json:"hz,omitempty"`
	Rate float64 `json:"rate,omitempty"`
	// Vpp and Offset place the signal: a waveform swings Vpp about Offset;
	// a logic line's low is Offset − Vpp/2 and its high Offset + Vpp/2; DC
	// sits at Offset.
	Vpp    float64 `json:"vpp,omitempty"`
	Offset float64 `json:"offset,omitempty"`
	// Duty is a square's or pulse's high fraction.
	Duty float64 `json:"duty,omitempty"`
	// Text or Hex are a protocol's bytes: printable text, or hex pairs.
	Text string `json:"text,omitempty"`
	Hex  string `json:"hex,omitempty"`
	// Line is which line of a bus this input carries: "tx" (UART), "clk",
	// "mosi", "miso", "cs" (SPI), "scl", "sda" (I2C).
	Line string `json:"line,omitempty"`
	// Opts are the protocol's settings in the generator's words: mode=0
	// (SPI), addr=0x24, rw=w (I2C), parity=n, bits=8, stop=1 (UART).
	Opts map[string]string `json:"opts,omitempty"`
}

// waveforms are the waveform kinds.
var waveforms = map[string]bool{"dc": true, "square": true, "sine": true, "pulse": true, "triangle": true}

// busLines are each protocol's lines; the first is the default.
var busLines = map[string][]string{
	"uart": {"tx"},
	"spi":  {"clk", "mosi", "miso", "cs"},
	"i2c":  {"scl", "sda"},
}

// Validate reports a signal no target could produce.
// TRLC-LINKS: REQ-DSO-295
func (s Signal) Validate() error {
	switch {
	case waveforms[s.Kind]:
		if s.Kind != "dc" && (s.Hz <= 0 || s.Vpp <= 0) {
			return fmt.Errorf("%s: want hz and vpp", s.Kind)
		}
	case busLines[s.Kind] != nil:
		if s.Rate <= 0 || s.Vpp <= 0 {
			return fmt.Errorf("%s: want rate and vpp", s.Kind)
		}
		if s.Line != "" && !slices.Contains(busLines[s.Kind], s.Line) {
			return fmt.Errorf("%s: line %q: want one of %v", s.Kind, s.Line, busLines[s.Kind])
		}
		if _, err := s.Bytes(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("kind %q unknown", s.Kind)
	}
	return nil
}

// Bytes are the protocol's bytes.
// TRLC-LINKS: REQ-DSO-295
func (s Signal) Bytes() ([]byte, error) {
	if s.Hex != "" {
		b, err := hex.DecodeString(strings.ReplaceAll(s.Hex, ":", ""))
		if err != nil {
			return nil, fmt.Errorf("hex %q: %w", s.Hex, err)
		}
		return b, nil
	}
	for _, r := range s.Text {
		if r > 0x7e || (r < 0x20 && r != '\n' && r != '\r' && r != '\t') {
			return nil, fmt.Errorf("text %q: only printable ASCII; use hex", s.Text)
		}
	}
	return []byte(s.Text), nil
}

// LineName is the signal's bus line, its protocol's first by default.
// TRLC-LINKS: REQ-DSO-295
func (s Signal) LineName() string {
	if s.Line != "" || busLines[s.Kind] == nil {
		return s.Line
	}
	return busLines[s.Kind][0]
}

// Describe is the signal in words: "UART 9600 'hello'", "DC 0 V".
// TRLC-LINKS: REQ-DSO-295
func (s Signal) Describe() string {
	si := strconv.FormatFloat
	switch {
	case s.Kind == "dc":
		return fmt.Sprintf("DC %s V", si(s.Offset, 'g', -1, 64))
	case waveforms[s.Kind]:
		d := fmt.Sprintf("%s %s Hz %s Vpp", s.Kind, si(s.Hz, 'g', -1, 64), si(s.Vpp, 'g', -1, 64))
		if s.Offset != 0 {
			d += fmt.Sprintf(" +%s V", si(s.Offset, 'g', -1, 64))
		}
		if s.Duty != 0 {
			d += fmt.Sprintf(" duty %g", s.Duty)
		}
		return d
	}
	data, _ := s.Bytes()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s %s", strings.ToUpper(s.Kind), si(s.Rate, 'g', -1, 64), s.LineName(), quoteBytes(data))
	for _, k := range slices.Sorted(maps.Keys(s.Opts)) {
		b.WriteString(" " + k + "=" + s.Opts[k])
	}
	fmt.Fprintf(&b, " (%g V logic)", s.Offset+s.Vpp/2)
	return b.String()
}

// quoteBytes is b as quoted text when it is printable, else as hex.
// TRLC-LINKS: REQ-DSO-295
func quoteBytes(b []byte) string {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return "0x" + hex.EncodeToString(b)
		}
	}
	return "'" + string(b) + "'"
}

// DescribeSignals is every input's signal in words, by input.
// TRLC-LINKS: REQ-DSO-295
func DescribeSignals(sigs map[string]Signal) string {
	var parts []string
	for _, in := range slices.Sorted(maps.Keys(sigs)) {
		parts = append(parts, sigs[in].Describe()+" on "+in)
	}
	return strings.Join(parts, ", ")
}

// Same reports whether a and b are the same signal, to the generator's
// precision: a part in a thousand in time, 50 mV in level.
// TRLC-LINKS: REQ-DSO-295
func Same(a, b Signal) bool {
	near := func(x, y, tol float64) bool { return math.Abs(x-y) <= tol }
	rel := func(x, y float64) bool { return near(x, y, 1e-3*math.Max(math.Abs(x), math.Abs(y))) }
	ab, errA := a.Bytes()
	bb, errB := b.Bytes()
	duty := func(s Signal) float64 {
		if s.Duty == 0 && (s.Kind == "square") {
			return 0.5
		}
		return s.Duty
	}
	return a.Kind == b.Kind && a.LineName() == b.LineName() && rel(a.Hz, b.Hz) && rel(a.Rate, b.Rate) &&
		near(a.Vpp, b.Vpp, 0.05) && near(a.Offset, b.Offset, 0.05) && near(duty(a), duty(b), 1e-3) &&
		errA == nil && errB == nil && string(ab) == string(bb) && maps.Equal(a.Opts, b.Opts)
}

// SimRequests are the requests a simulated board's bench takes to put
// sigs on its inputs: one {"signals":{…}} for the waveforms and the
// single-line protocols, one bus request per multi-line protocol.
// TRLC-LINKS: REQ-DSO-295
func SimRequests(sigs map[string]Signal) (signals map[string]string, buses []map[string]any, err error) {
	signals = map[string]string{}
	byKind := map[string]map[string]any{}
	for _, in := range slices.Sorted(maps.Keys(sigs)) {
		s := sigs[in]
		if err := s.Validate(); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", in, err)
		}
		if waveforms[s.Kind] || s.Kind == "uart" {
			signals[in] = genSpec(s)
			continue
		}
		bus := byKind[s.Kind]
		if bus == nil {
			b, _ := s.Bytes()
			opts := map[string]string{"data": hex.EncodeToString(b)}
			maps.Copy(opts, s.Opts)
			bus = map[string]any{"kind": s.Kind, "lines": map[string]string{}, "rate": s.Rate, "vpp": s.Vpp, "offset": s.Offset, "opts": opts}
			byKind[s.Kind] = bus
			buses = append(buses, bus)
		}
		lines, ok := bus["lines"].(map[string]string)
		if !ok {
			return nil, nil, errors.New("bus lines")
		}
		lines[s.LineName()] = in
	}
	return signals, buses, nil
}

// genSpec is a waveform's or a UART line's text in the generator's
// syntax (package siggen): kind,freq,vpp,offset[,key=value...].
// TRLC-LINKS: REQ-DSO-295
func genSpec(s Signal) string {
	g := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	if s.Kind == "dc" {
		return "dc,0,0," + g(s.Offset)
	}
	freq := s.Hz
	if s.Kind == "uart" {
		freq = s.Rate
	}
	parts := []string{s.Kind, g(freq), g(s.Vpp), g(s.Offset)}
	if s.Duty != 0 {
		parts = append(parts, "duty="+g(s.Duty))
	}
	if s.Kind == "uart" {
		b, _ := s.Bytes()
		parts = append(parts, "data="+hex.EncodeToString(b))
		for _, k := range slices.Sorted(maps.Keys(s.Opts)) {
			parts = append(parts, k+"="+s.Opts[k])
		}
	}
	return strings.Join(parts, ",")
}
