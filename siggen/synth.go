package siggen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/labeth/open-dso-core/decode/synth"
)

// Buses of the core's generators (decode/synth): every protocol and line
// code the decoders read, on any input. An input's signal is one line of a
// bus:
//
//	bus.PROTO.ROLE,0,vpp,offset[,setting=value...][,data=HEX|words=HEX:HEX...][,idle=S]
//
// e.g. bus.i2c.sda,0,3.3,1.65,rate=400k,addr=0x1d. The settings are the
// generator's own (synth.Lookup lists them); data= gives the payload as
// bytes, words= as wider values. The bus is laid out once and repeats every
// one of its periods and idle seconds of idle after it (default a tenth of
// the period; none for a bus that runs on, synth.Bus.Continuous), from t =
// 0; inputs set to lines of the same bus with the same settings stay in
// step. vpp and offset place the low and high levels (a three-level line,
// ARINC 429, spans the swing with its null in the middle). freq is not
// read: the signal's bit rate is the bus's shortest step, its frequency the
// repetition rate. The waveform options apply as to any line.

// synthKind reports a bus of the core's generators (bus.PROTO.ROLE).
// TRLC-LINKS: REQ-DSO-298
func synthKind(kind string) bool { return strings.HasPrefix(kind, "bus.") }

// buildSynthLine lays out the line of kind "bus.PROTO.ROLE" from the
// remaining options: the line, its repetition period and its shortest step.
// TRLC-LINKS: REQ-DSO-298
func buildSynthLine(kind string, m map[string]string) (*lineWave, float64, float64, error) {
	proto, role, ok := strings.Cut(strings.TrimPrefix(kind, "bus."), ".")
	if !ok {
		return nil, 0, 0, fmt.Errorf("%s: want bus.PROTO.ROLE", kind)
	}
	p, found := synth.Lookup(proto)
	if !found {
		return nil, 0, 0, fmt.Errorf("%s: no protocol %q", kind, proto)
	}
	k := slices.Index(p.Roles, role)
	if k < 0 {
		return nil, 0, 0, fmt.Errorf("%s: %s has lines %v", kind, proto, p.Roles)
	}
	spec := synth.Spec{Proto: proto, Set: map[string]string{}}
	idle := -1.0
	for key, v := range m {
		switch key {
		case "data":
			b, err := parseHex(v)
			if err != nil {
				return nil, 0, 0, err
			}
			for _, x := range b {
				spec.Payload = append(spec.Payload, int(x))
			}
		case "words":
			for w := range strings.SplitSeq(v, ":") {
				x, err := strconv.ParseInt(strings.TrimPrefix(w, "0x"), 16, 64)
				if err != nil {
					return nil, 0, 0, fmt.Errorf("words=%q: want hex values parted by ':'", v)
				}
				spec.Payload = append(spec.Payload, int(x))
			}
		case "idle":
			x, err := strconv.ParseFloat(v, 64)
			if err != nil || x < 0 {
				return nil, 0, 0, fmt.Errorf("idle=%q: want seconds ≥ 0", v)
			}
			idle = x
		default:
			spec.Set[key] = v
		}
	}
	bus, err := synth.Make(spec, 0)
	if err != nil {
		return nil, 0, 0, err
	}
	switch {
	case bus.Continuous && idle < 0:
		idle = 0 // an output that runs on: no pause between repeats
	case idle < 0:
		idle = bus.Period / 10
	}
	w := bus.Lines[k].Wave(bus.Period + idle)
	shortest := w.Period
	for _, l := range bus.Lines { // the bus's shortest step on any of its lines
		for i := 1; i < len(l.Steps); i++ {
			if d := l.Steps[i].At - l.Steps[i-1].At; d > 0 {
				shortest = min(shortest, d)
			}
		}
	}
	return &lineWave{t: w.T, lv: w.L, period: w.Period}, w.Period, shortest, nil
}
