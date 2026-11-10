package siggen

import (
	"math"
	"testing"
)

func mustSig(t *testing.T, spec string) Signal {
	t.Helper()
	s, err := Parse(spec)
	if err != nil {
		t.Fatalf("%s: %v", spec, err)
	}
	return s
}

// The original four kinds keep their shape (the default signals and the regression runs
// depend on them).
// TRLC-LINKS: REQ-DSO-298
func TestSignalClassicKinds(t *testing.T) {
	for _, c := range []struct {
		spec string
		t, v float64
	}{
		{"square,1e3,3,1.5", 0.1e-3, 3},
		{"square,1e3,3,1.5", 0.6e-3, 0},
		{"sine,1e6,1,0", 0.25e-6, 0.5},
		{"triangle,1e4,0.2,0", 0.25e-4, 0},
		{"triangle,1e4,0.2,0", 0.5e-4, 0.1},
		{"dc,0,0,0.7", 1, 0.7},
	} {
		if got := mustSig(t, c.spec).V(c.t); math.Abs(got-c.v) > 1e-9 {
			t.Errorf("%s at %g: %g, want %g", c.spec, c.t, got, c.v)
		}
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestSignalWaveformOptions(t *testing.T) {
	p := mustSig(t, "pulse,1e3,2,0,width=1e-4")
	if p.V(0.05e-3) != 1 || p.V(0.15e-3) != -1 {
		t.Errorf("pulse width: %g %g", p.V(0.05e-3), p.V(0.15e-3))
	}
	sq := mustSig(t, "square,1e3,2,0,duty=0.25,rise=1e-5")
	if v := sq.V(0); math.Abs(v) > 1e-9 { // mid-edge at the period start
		t.Errorf("rise: V(0) = %g, want 0 (half way up)", v)
	}
	if v := sq.V(0.1e-3); v != 1 {
		t.Errorf("duty: %g", v)
	}
	r := mustSig(t, "ramp,1e3,2,0")
	if v := r.V(0.75e-3); math.Abs(v-0.5) > 1e-9 {
		t.Errorf("ramp: %g", v)
	}
	b := mustSig(t, "sine,1e3,2,0,burst=2:3")
	if b.V(2.25e-3) != 0 || math.Abs(b.V(1.25e-3)-1) > 1e-9 {
		t.Errorf("burst: %g %g", b.V(2.25e-3), b.V(1.25e-3))
	}
	g := mustSig(t, "square,1e3,2,0,glitch=2:1e-5:1.5")
	if v := g.V(0.751e-3); math.Abs(v-0.5) > 1e-9 {
		t.Errorf("glitch in period 0: %g, want 0.5", v)
	}
	if v := g.V(1.751e-3); v != -1 {
		t.Errorf("no glitch in period 1: %g", v)
	}
	n := mustSig(t, "dc,0,0,1,noise=0.1")
	sum, sq2 := 0.0, 0.0
	for i := range 20000 {
		v := n.V(float64(i)*1e-6) - 1
		sum += v
		sq2 += v * v
	}
	if rms := math.Sqrt(sq2 / 20000); math.Abs(rms-0.1) > 0.01 || math.Abs(sum/20000) > 0.01 {
		t.Errorf("noise rms %g mean %g", rms, sum/20000)
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestSignalParseErrors(t *testing.T) {
	for _, s := range []string{
		"sine,1e3", "sine,x,1,0", "sine,1e3,1,0,duty=2", "sine,1e3,1,0,bogus=1", "wobble,1,1,0",
		"uart,0,3.3,0", "uart,9600,3.3,0,data=zz", "uart,9600,3.3,0,parity=q", "can,5e5,3,0,id=0x800",
		"can,5e5,3,0,data=010203040506070809", "spi.foo,1e6,3,0", "i2c.scl,1e5,3,0,rw=x", "lin,19200,3,0,id=64",
		"square,1e3,2,0,glitch=2:1e-5,1.5",
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%s: no error", s)
		}
	}
}

// sampleBits reads a line at the middle of each bit from t0.
func sampleBits(s Signal, t0, bt float64, n int) []int {
	out := make([]int, n)
	for i := range out {
		if s.V(t0+(float64(i)+0.5)*bt) > s.Offset {
			out[i] = 1
		}
	}
	return out
}

// TRLC-LINKS: REQ-DSO-298
func TestUARTLine(t *testing.T) {
	s := mustSig(t, "uart,9600,3.3,1.65,data=a5,parity=e")
	bt := 1 / 9600.0
	got := sampleBits(s, 2*bt, bt, 11)
	want := []int{0, 1, 0, 1, 0, 0, 1, 0, 1, 0, 1} // start, A5 LSB first, even parity 0, stop
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bits %v, want %v", got, want)
		}
	}
	if s.V(0) != 3.3 { // idle high
		t.Errorf("idle %g", s.V(0))
	}
	if p := 1 / s.Freq; math.Abs(p-(13*bt+20*bt)) > 1e-12 {
		t.Errorf("period %g", p)
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestSPILinesInStep(t *testing.T) {
	clk := mustSig(t, "spi.clk,1e6,1,0.5,data=c3,mode=0")
	mosi := mustSig(t, "spi.mosi,1e6,1,0.5,data=c3,mode=0")
	cs := mustSig(t, "spi.cs,1e6,1,0.5,data=c3,mode=0")
	var bits []int
	prev := clk.V(0)
	for i := 1; i < 200000; i++ {
		tt := float64(i) * 1e-10
		v := clk.V(tt)
		if prev < 0.5 && v >= 0.5 && cs.V(tt) < 0.5 { // rising edge while selected
			if mosi.V(tt) > 0.5 {
				bits = append(bits, 1)
			} else {
				bits = append(bits, 0)
			}
		}
		prev = v
	}
	want := []int{1, 1, 0, 0, 0, 0, 1, 1}
	if len(bits) != 8 {
		t.Fatalf("%d clock edges, want 8: %v", len(bits), bits)
	}
	for i := range want {
		if bits[i] != want[i] {
			t.Fatalf("bits %v, want %v", bits, want)
		}
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestCANWires(t *testing.T) {
	h := mustSig(t, "can.h,5e5,2,2.5,id=0x123,data=01")
	l := mustSig(t, "can.l,5e5,2,2.5,id=0x123,data=01")
	bt := 2e-6
	if h.V(0.5*bt) != 2.5 || l.V(0.5*bt) != 2.5 { // recessive idle
		t.Errorf("recessive: %g %g", h.V(0.5*bt), l.V(0.5*bt))
	}
	if h.V(1.5*bt) != 3.5 || l.V(1.5*bt) != 1.5 { // SOF dominant
		t.Errorf("dominant: %g %g", h.V(1.5*bt), l.V(1.5*bt))
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestCRC15(t *testing.T) {
	// A message followed by its CRC leaves a zero remainder.
	msg := []byte{0, 1, 0, 0, 1, 0, 0, 0, 1, 1, 0, 1, 0, 0, 0, 0, 0, 1}
	c := crc15(msg)
	for k := 14; k >= 0; k-- {
		msg = append(msg, byte(c>>uint(k)&1))
	}
	if r := crc15(msg); r != 0 {
		t.Errorf("remainder %#x", r)
	}
}

// TRLC-LINKS: REQ-DSO-298
func TestLINChecksum(t *testing.T) {
	if p := linPID(0x10); p != 0x50 {
		t.Errorf("PID(0x10) = %#x, want 0x50", p)
	}
	if c := linChecksum(0x50, []byte{1, 2}, true); c != 0xac {
		t.Errorf("checksum %#x, want 0xac", c)
	}
}

// Noise depends only on the time and the seed: a record read twice is the
// same record, and two inputs' noise differs.
// TRLC-LINKS: REQ-DSO-298
func TestNoiseIsAFunctionOfTime(t *testing.T) {
	a := mustSig(t, "sine,1e3,1,0,noise=0.05")
	b := a
	b.Seed = 2
	if a.V(1.25e-4) != a.V(1.25e-4) {
		t.Fatal("the same instant read two values")
	}
	if a.V(1.25e-4) == b.V(1.25e-4) {
		t.Fatal("two seeds gave the same noise")
	}
}

// PRBS7 is a maximal-length sequence: 127 bits a period, 64 ones, and its
// clock rises in the middle of each bit.
// TRLC-LINKS: REQ-DSO-298
func TestPRBSLine(t *testing.T) {
	d := mustSig(t, "prbs,1e6,1,0.5")
	clk := mustSig(t, "prbs.clk,1e6,1,0.5")
	bt := 1e-6
	if p := 1 / d.Freq; math.Abs(p-127*bt) > 1e-12 {
		t.Fatalf("period %g, want 127 bits", p)
	}
	bits := sampleBits(d, 0, bt, 127)
	ones := 0
	for _, b := range bits {
		ones += b
	}
	if ones != 64 {
		t.Fatalf("%d ones in a period, want 64", ones)
	}
	if clk.V(0.25*bt) > 0.5 || clk.V(0.75*bt) < 0.5 {
		t.Fatalf("clock %g %g", clk.V(0.25*bt), clk.V(0.75*bt))
	}
}

// A bank changes a signal while it is read, sets a bus's lines together
// and changes nothing when one line fails.
// TRLC-LINKS: REQ-DSO-298
func TestBank(t *testing.T) {
	b := NewBank(4)
	if b.Signal(0).V(1) != 0 || b.Inputs() != 4 {
		t.Fatal("a new bank is 0 V")
	}
	if err := b.Set(0, "square,1e3,3,1.5"); err != nil {
		t.Fatal(err)
	}
	g := b.Generation()
	err := b.SetBus(Bus{
		Kind: "spi", Lines: map[string]int{"clk": 1, "mosi": 2, "cs": 3}, Rate: 1e6, Vpp: 3.3, Offset: 1.65,
		Opts: map[string]string{"data": "c3", "mode": "0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	specs := b.Specs()
	if specs[0] != "square,1e3,3,1.5" || specs[1] != "spi.clk,1e+06,3.3,1.65,data=c3,mode=0" || specs[3] != "spi.cs,1e+06,3.3,1.65,data=c3,mode=0" {
		t.Fatalf("specs %q", specs)
	}
	if b.Generation() == g {
		t.Fatal("the generation did not move")
	}
	if err := b.SetMany(map[int]string{0: "sine,1,1,0", 1: "bogus,1,1,0"}); err == nil || b.Specs()[0] != "square,1e3,3,1.5" {
		t.Fatalf("a failing spec must change nothing: %v", err)
	}
	if err := b.Set(4, "dc,0,0,1"); err == nil {
		t.Fatal("an input the bank does not have")
	}
	if _, err := (Bus{Kind: "i2c", Lines: map[string]int{"scl": 0, "sda": 0}}).Specs(); err == nil {
		t.Fatal("two lines on one input")
	}
	if got := b.Signal(1); !got.IsLine() || got.BitRate != 1e6 {
		t.Fatalf("spi clock %+v", got)
	}
}

// BenchmarkSquare is one sample of an edged square wave with noise, the
// cost a simulated record pays per sample.
// TRLC-LINKS: REQ-DSO-298
func BenchmarkSquare(b *testing.B) {
	s, _ := Parse("square,1e6,2,0,rise=5e-9,noise=0.01")
	dst := make([]float64, 4096)
	for b.Loop() {
		s.Sample(dst, 0.1, 1e-9)
	}
}

// stretch holds a pulse's high part longer in every EVERY-th period and
// adds no edge: the other periods keep their width.
// TRLC-LINKS: REQ-DSO-298
func TestStretch(t *testing.T) {
	s, err := Parse("pulse,2500,3.3,1.65,width=100e-6,stretch=7:12e-6")
	if err != nil {
		t.Fatal(err)
	}
	high := func(t0 float64) bool { return s.V(t0) > 1.65 }
	T := 1 / 2500.0
	if !high(105e-6) || high(115e-6) { // period 0 is stretched by 12 us
		t.Fatal("period 0: want high to 112 us")
	}
	if high(T + 105e-6) { // period 1 is not
		t.Fatal("period 1: want low after 100 us")
	}
	if !high(7*T + 110e-6) {
		t.Fatal("period 7: want stretched")
	}
}

// A counter counts same-polarity edges, including transitions between frames.
// TRLC-LINKS: REQ-DSO-298
func TestCounterFrequency(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want float64
	}{
		{"sine,1000,2,0", 1000},
		{"uart,115200,3.3,1.65,data=55", 18000},
		{"uart,115200,3.3,1.65,data=00", 3600},
		{"uart,115200,3.3,1.65,data=55,inv=1", 18000},
		{"uart,115200,3.3,1.65,data=48656c6c6f", 115200.0 * 16 / 72},
	} {
		if got := mustSig(t, tc.spec).CounterFrequency(); math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: got %g want %g", tc.spec, got, tc.want)
		}
	}
}
