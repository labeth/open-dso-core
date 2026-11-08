// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/decode/synth"
	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/siggen"
)

// synthRecord is bus spec repeating as a signal source repeats it (its
// period and a tenth of it idle, none for a bus that runs on), reps times,
// as the two lines the detection reads, sampled at the bus's natural rate.
func synthRecord(t *testing.T, s synth.Spec, reps int) (c1, c2 []uint8, sampleS float64, b synth.Bus) {
	t.Helper()
	b, err := synth.Make(s, 0)
	if err != nil {
		t.Fatal(err)
	}
	sr := b.NaturalSR
	idle := b.Period / 10
	if b.Continuous {
		idle = 0
	}
	n := int((b.Period + idle) * sr * float64(reps))
	line := func(l *synth.Line) []uint8 {
		w := l.Wave(b.Period + idle)
		out := make([]uint8, n)
		for i := range out {
			out[i] = uint8(math.Round(56 + 144*w.At(float64(i)/sr)))
		}
		return out
	}
	c1 = line(b.Lines[0])
	if len(b.Lines) > 1 {
		c2 = line(b.Lines[1])
	} else {
		c2 = slices.Repeat([]uint8{c1[0]}, n)
	}
	return c1, c2, 1 / sr, b
}

// idleFires lists where a pulse qualifier firing on line's runs at the
// idle level (high or low) longer than w fires: the run's end.
func idleFires(line []uint8, high bool, w, sampleS float64) []int {
	var out []int
	start, state := 0, line[0] > 128
	for i, v := range line {
		if s := v > 128; s != state {
			if state == high && float64(i-start)*sampleS > w {
				out = append(out, i)
			}
			start, state = i, s
		}
	}
	return out
}

// noStartPulse are the buses whose line holds no pulse that starts their
// messages: a PWM output runs on with no idle, and SWD's turnarounds and
// idle cycles run its clock between the transfers.
var noStartPulse = map[string]bool{"pwm": true, "swd": true}

// Every bus the generators write, but a few that run on, has a pulse on
// its first line that starts its messages (startPulse): a pulse trigger
// longer than the width found fires only between a message's start and
// where the pulse that opens it ends, never later inside one, and at least
// once a record of three repetitions. Where the board's trigger has no
// receiver for a bus, AUTO in BUS frames it with that pulse.
// TRLC-LINKS: REQ-DSO-223
func TestStartPulseFramesEveryBus(t *testing.T) {
	for _, p := range synth.Protocols() {
		for _, s := range synth.Variants(p.Proto) {
			c1, c2, sampleS, b := synthRecord(t, s, 3)
			res, st := decode.AutodetectFast(c1, c2, sampleS, "hex")
			if res.Proto != b.Proto {
				continue // named as its own on the scope's records (the decode sweep); here only framed
			}
			k := max(1, int(math.Round(st/sampleS)))
			src := c1
			if res.Roles.ChA == 1 {
				src = c2
			}
			var line []uint8
			for i := 0; i < len(src); i += k {
				line = append(line, src[i])
			}
			ps := anaPackets(res, len(line))
			var m busMsgs
			m.startPulse(line, ps, st)
			if m.startW == 0 {
				if !noStartPulse[p.Proto] {
					t.Errorf("%s: no pulse starts its messages", s)
				}
				continue
			}
			fires := idleFires(line, m.startHigh, m.startW, st)
			if len(fires) == 0 {
				t.Errorf("%s: a pulse over %g s never ends in the record", s, m.startW)
			}
			for _, at := range fires {
				for _, q := range ps {
					if at > q.I0 && at < q.I1 && float64(at-q.I0)*st > m.startLead*1.01+2*st {
						t.Errorf("%s: the pulse over %g s ends %g s into a message (lead %g s)", s, m.startW, float64(at-q.I0)*st, m.startLead)
					}
				}
			}
		}
	}
}

// refusingEng is a scope whose board's protocol trigger takes no bus
// request: its instrument state says so once the serial trigger is armed.
type refusingEng struct{ *bodeEng }

// TrigTypes marks a scope whose instrument state says what decides the
// trigger (Stats.SerialBackend).
func (refusingEng) TrigTypes() []instrument.TypeAvail { return nil }

// SetSerialMode arms the serial trigger, which the board refuses.
func (e refusingEng) SetSerialMode(m int) {
	e.bodeEng.SetSerialMode(m)
	e.set(func(st *instrument.Stats) {
		st.SerialBackend = ""
		if m != 0 {
			st.SerialBackend = "none: no receiver for this protocol: decode only"
		}
	})
}

// A board whose trigger has no receiver for the bus's frame start (the
// factory PL of a scope that matches a few protocols' own conditions)
// gets a trigger it runs instead: the pulse that starts the bus's messages
// on its first line, longer than the width measured, fired where it ends;
// with no such pulse, an edge on that line. Where the board takes the
// request the frame-start trigger stays.
// TRLC-LINKS: REQ-DSO-223, REQ-DSO-013
func TestFrameStartOnABoardWithoutTheReceiver(t *testing.T) {
	r := newViewRig(t)
	eng := refusingEng{r.eng}
	r.c.eng = eng
	set(t, r.c, "bus.protocol", "DMX512")
	set(t, r.c, "bus.source", "CH2")
	note, lead, own := r.c.frameTriggerAt(busMsgs{startW: 600e-6, startHigh: true, startLead: 0})
	st := eng.Snapshot()
	if !own || lead != 0 || st.SerialMode != 0 || st.TrigType != 1 || st.TrigSource != 1 || !st.TrigRising ||
		st.TrigQual.PulseCond != engine.CondGreater || st.TrigQual.PulseMaxNs != 600e3 || !strings.Contains(note, "frame start (high > 600us") {
		t.Fatalf("framed with %q (own %v lead %g): %+v", note, own, lead, st)
	}
	note, _, own = r.c.frameTriggerAt(busMsgs{})
	if st = eng.Snapshot(); !own || st.SerialMode != 0 || st.TrigType != 0 || st.TrigSource != 1 || !strings.HasPrefix(note, "TRIG: edge") {
		t.Fatalf("no pulse to start on: %q, %+v", note, st)
	}
	// the board that takes it keeps the frame-start trigger
	r.c.eng = r.eng
	note, _, own = r.c.frameTriggerAt(busMsgs{startW: 600e-6, startHigh: true})
	if st = r.eng.Snapshot(); own || st.SerialMode == 0 || !strings.Contains(note, "frame start") {
		t.Fatalf("a board with the receiver: %q, %+v", note, st)
	}
}

// A look waits for a sampled record as long as three of the records the
// search read lately took, when that is longer than its own wait: a slow
// board's records (an emulated firmware's two seconds at 50ms/div) are not
// given up on. The longest is forgotten by an eighth a look.
// TRLC-LINKS: REQ-DSO-214
func TestCaptureWaitFollowsTheRecords(t *testing.T) {
	c, _, _ := newC(t)
	if w := c.captureWait(decimatedWait, true); w != decimatedWait {
		t.Fatalf("no record read yet: %v", w)
	}
	c.tookToCapture(2*time.Second, true)
	if w := c.captureWait(decimatedWait, true); w != 6*time.Second {
		t.Fatalf("records of 2 s: wait %v", w)
	}
	if w := c.captureWait(detectWait, false); w != detectWait {
		t.Fatalf("the full-rate looks keep their own: %v", w)
	}
	c.tookToCapture(100*time.Millisecond, true)
	if w := c.captureWait(decimatedWait, true); w != 3*(2*time.Second-2*time.Second/8) {
		t.Fatalf("forgotten by an eighth: %v", w)
	}
	c.tookToCapture(time.Minute, true)
	if w := c.captureWait(decimatedWait, true); w != captureWaitMax {
		t.Fatalf("bounded: %v", w)
	}
}

// A PWM find of a slow period whose pulses take two widths is a weak find
// the search looks on from (a sped-up DCF77's seconds read as "PWM 10%
// 20%"); a PWM output's sweep of widths, or a fast one, is not.
// TRLC-LINKS: REQ-DSO-214
func TestTwoDutyPWMIsWeak(t *testing.T) {
	// a 5ms/div screen of a bus search (32768 samples) of the minute's
	// seconds, no minute marker in it
	b, err := synth.Make(synth.Spec{Proto: "dcf77", Set: map[string]string{"second_ms": "3"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	w := b.Lines[0].Wave(b.Period * 1.1)
	const n = 32768
	dt := 50e-3 / n
	c1, c2 := make([]uint8, n), make([]uint8, n)
	for i := range c1 {
		c1[i], c2[i] = uint8(math.Round(56+144*w.At(b.Period*1.1/8+float64(i)*dt))), 56
	}
	res, _ := decode.AutodetectFast(c1, c2, dt, "hex")
	if res.Proto != "pwm" || !weakFind(res) {
		t.Fatalf("a part of a DCF77 minute: %s %q, weak %v", res.Proto, res.Text, weakFind(res))
	}
	for _, s := range synth.Variants("pwm") {
		a, b, ss, _ := synthRecord(t, s, 2)
		if r, _ := decode.AutodetectFast(a, b, ss, "hex"); r.Proto != "pwm" || weakFind(r) {
			t.Fatalf("%s: %s %q weak %v", s, r.Proto, r.Text, weakFind(r))
		}
	}
}

// A UART found at DMX512's rate is a weak find the search looks on from:
// a record of a universe's slots with no break in it names the line UART.
// TRLC-LINKS: REQ-DSO-214
func TestDMXRateUARTIsWeak(t *testing.T) {
	c1, c2, sampleS, b := synthRecord(t, synth.Spec{Proto: "dmx512"}, 1)
	// the slots after the break: a quarter of the universe on
	from := int(b.Period / 4 / sampleS)
	res, _ := decode.AutodetectFast(c1[from:from+len(c1)/4], c2[from:from+len(c1)/4], sampleS, "hex")
	if res.Proto != protoUART || !weakFind(res) {
		t.Fatalf("a universe's slots: %s at %d Bd, weak %v", res.Proto, res.Baud, weakFind(res))
	}
	u1, u2, us, _ := synthRecord(t, synth.Spec{Proto: "uart", Set: map[string]string{"baud": "115200", "parity": "even"}}, 3)
	if r, _ := decode.AutodetectFast(u1, u2, us, "hex"); r.Proto != protoUART || dmxRateUART(r) {
		t.Fatalf("a 115200 Bd UART: %s %d Bd", r.Proto, r.Baud)
	}
}

// A UART's idle trigger does not hang on where the record starts: a
// screen that cuts a message and misframes its characters from the middle
// (run on through the idle into the next message) reads its idle as no
// longer than the characters' high times; one that shows no idle between
// two messages says so (idle 0), and AUTO reads a slower screen.
// TRLC-LINKS: REQ-DSO-223
func TestUARTIdleWhereverTheScreenStarts(t *testing.T) {
	s, err := siggen.Parse("uart,9600,3.3,1.65,data=68656c6c6f")
	if err != nil {
		t.Fatal(err)
	}
	const n = 20000
	for _, tdiv := range []float64{500e-6, 1e-3, 2e-3} {
		dt := 10 * tdiv / n
		for ph := range 20 {
			c := make([]uint8, n)
			t0 := float64(ph) / 20 / s.Freq
			for i := range c {
				c[i] = uint8(56 + 144*s.V(t0+float64(i)*dt)/3.3)
			}
			m := msgsOf(decode.UART(c, dt, decode.UARTCfg{Baud: 9600}), dt, n)
			idle := uartIdle(m) > 0
			switch {
			case tdiv >= 2e-3 && !idle:
				t.Errorf("%g s/div from %d/20: no idle trigger (idle %g s, high %g s)", tdiv, ph, m.idle, m.highRun)
			case !idle && m.idle > 0:
				t.Errorf("%g s/div from %d/20: an idle of %g s read as no longer than a character's %g s", tdiv, ph, m.idle, m.highRun)
			}
		}
	}
}
