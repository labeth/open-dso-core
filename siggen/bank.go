package siggen

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Bank is one signal per input, set and read while a board samples it. Its
// methods are safe for concurrent use; a change takes effect at the next
// Signal call, so a record already being sampled keeps the signal it began
// with.
type Bank struct {
	mu   sync.RWMutex
	sigs []Signal
	gen  uint64 // moves with every change
}

// NewBank is a bank of n inputs, each at 0 V.
// TRLC-LINKS: REQ-DSO-298
func NewBank(n int) *Bank {
	b := &Bank{sigs: make([]Signal, n)}
	for i := range b.sigs {
		b.sigs[i] = Signal{Kind: "dc", spec: "dc,0,0,0", Seed: uint64(i) + 1}
	}
	return b
}

// Inputs is how many inputs the bank has.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) Inputs() int { return len(b.sigs) }

// Signal is input in's signal now; the zero signal (0 V) for an input the
// bank does not have.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) Signal(in int) Signal {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if in < 0 || in >= len(b.sigs) {
		return Signal{Kind: "dc"}
	}
	return b.sigs[in]
}

// Generation moves with every change, so a reader can tell the signals
// changed.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) Generation() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.gen
}

// Specs are every input's signal as text.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) Specs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, len(b.sigs))
	for i, s := range b.sigs {
		out[i] = s.String()
	}
	return out
}

// Set sets input in's signal from its text.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) Set(in int, spec string) error {
	return b.SetMany(map[int]string{in: spec})
}

// SetMany sets several inputs at once (a bus's lines), or none of them when
// any text fails to parse.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) SetMany(specs map[int]string) error {
	parsed := map[int]Signal{}
	for in, spec := range specs {
		if in < 0 || in >= len(b.sigs) {
			return fmt.Errorf("input %d: the bank has %d inputs", in+1, len(b.sigs))
		}
		s, err := Parse(spec)
		if err != nil {
			return fmt.Errorf("input %d: %w", in+1, err)
		}
		s.Seed = uint64(in) + 1
		parsed[in] = s
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for in, s := range parsed {
		b.sigs[in] = s
	}
	b.gen++
	return nil
}

// Bus is a protocol bus laid out on several inputs: Lines maps a line name
// ("clk", "mosi", "sda", "tx", "h", ...) to the input it drives.
type Bus struct {
	Kind        string            // "uart", "spi", "i2c", "can", "lin", "manchester", "prbs"
	Lines       map[string]int    // line name to input
	Rate        float64           // bit rate, Hz
	Vpp, Offset float64           // logic levels: offset ± vpp/2
	Opts        map[string]string // the line options (data=HEX, mode=0, ...)
}

// Specs are the bus's lines as per-input signal texts. Every line of one
// bus carries the same options, so they stay in step.
// TRLC-LINKS: REQ-DSO-298
func (bus Bus) Specs() (map[int]string, error) {
	if len(bus.Lines) == 0 {
		return nil, fmt.Errorf("%s: no line placed on an input", bus.Kind)
	}
	var opts []string
	for _, k := range slices.Sorted(maps.Keys(bus.Opts)) {
		opts = append(opts, k+"="+bus.Opts[k])
	}
	used := map[int]string{}
	out := map[int]string{}
	for line, in := range bus.Lines {
		if prev, ok := used[in]; ok {
			return nil, fmt.Errorf("%s: lines %s and %s on one input", bus.Kind, prev, line)
		}
		used[in] = line
		kind := bus.Kind
		switch {
		case bus.Kind == "can" && (line == "h" || line == "l"):
			kind = "can." + line
		case bus.Kind == "spi" || bus.Kind == "i2c" || bus.Kind == "prbs" && line == "clk":
			kind = bus.Kind + "." + line
		}
		parts := []string{
			kind, strconv.FormatFloat(bus.Rate, 'g', -1, 64),
			strconv.FormatFloat(bus.Vpp, 'g', -1, 64), strconv.FormatFloat(bus.Offset, 'g', -1, 64),
		}
		out[in] = strings.Join(append(parts, opts...), ",")
	}
	return out, nil
}

// SetBus lays bus out on its inputs, or changes nothing when it does not
// parse.
// TRLC-LINKS: REQ-DSO-298
func (b *Bank) SetBus(bus Bus) error {
	specs, err := bus.Specs()
	if err != nil {
		return err
	}
	return b.SetMany(specs)
}
