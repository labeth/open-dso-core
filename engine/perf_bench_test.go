// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/sample"
)

// packetSpans is a decode of n packets: a start of frame, then size bytes.
// TRLC-LINKS: REQ-SDS-013
func packetSpans(n, size int) []decode.Span {
	var sp []decode.Span
	x := 0
	for p := range n {
		sp = append(sp, decode.Span{I0: x, I1: x, Text: "SOF", Kind: "sof"})
		for b := range size {
			sp = append(sp, decode.Span{I0: x + 10*b, I1: x + 10*b + 9, Text: "41", Kind: "data", Val: (p*7 + b) & 0xff})
		}
		x += 10*size + 40
	}
	return sp
}

// TRLC-LINKS: REQ-SDS-013
func BenchmarkMatchPackets(b *testing.B) {
	sp := append(packetSpans(150, 2), packetSpans(50, 8)...)
	want := []int{0x55, 0xAA, 0x01}
	for b.Loop() {
		matchPackets(sp, want)
	}
}

// TRLC-LINKS: REQ-SDS-013
func BenchmarkMatchBytes(b *testing.B) {
	sp := packetSpans(1, 12)
	want := []int{0x03, 0x04}
	for b.Loop() {
		matchBytes(sp, want)
	}
}

// TRLC-LINKS: REQ-SDS-126
func BenchmarkUniRingStats(b *testing.B) {
	var u UniRing
	u.Reset()
	disc := make([]uint8, 4096)
	for k := range uniDepth {
		for i := range disc {
			disc[i] = uint8(128 + 60*((i/97)%2*2-1) + (i*k)%7)
		}
		u.Push(disc, 4096, 2048)
	}
	for b.Loop() {
		u.Stats()
	}
}

// uniRing.stats reads as the column-by-column float sums did, bit for bit.
// TRLC-LINKS: REQ-SDS-126
func TestUniRingStatsExact(t *testing.T) {
	ref := func(u *UniRing) (std, stdRaw, worst float64) {
		colStd := func(ring [][]uint8, col int) float64 {
			var s, s2 float64
			for k := range u.cnt {
				v := float64(ring[k][col])
				s += v
				s2 += v * v
			}
			m := s / float64(u.cnt)
			v := s2/float64(u.cnt) - m*m
			if v < 0 {
				v = 0
			}
			return math.Sqrt(v)
		}
		var sumC, sumR float64
		for c := range uniCols {
			sc := colStd(u.centred, c)
			sumC += sc
			sumR += colStd(u.raw, c)
			if sc > worst {
				worst = sc
			}
		}
		return sumC / uniCols, sumR / uniCols, worst
	}
	var u UniRing
	u.Reset()
	disc := make([]uint8, 3000)
	for k := range uniDepth + 3 {
		for i := range disc {
			disc[i] = uint8(128 + 60*((i/97)%2*2-1) + (i*k*31)%17)
		}
		u.Push(disc, 3000, float64(1400+k))
		a1, b1, c1 := u.Stats()
		a2, b2, c2 := ref(&u)
		if a1 != a2 || b1 != b2 || c1 != c2 {
			t.Fatalf("after %d pushes: %v %v %v, want %v %v %v", k+1, a1, b1, c1, a2, b2, c2)
		}
	}
}

// TRLC-LINKS: REQ-SDS-012
func BenchmarkEresQ8(b *testing.B) {
	src := make([]uint16, 20000)
	for i := range src {
		src[i] = uint16(32768 + (i*7919)%3000)
	}
	work, scratch := make([]uint16, len(src)), make([]uint16, len(src))
	for b.Loop() {
		copy(work, src)
		EresQ8(work, scratch, 15)
	}
}

// TRLC-LINKS: REQ-SDS-012
func BenchmarkSramAverage(b *testing.B) {
	const n = 20000
	f := &sample.Frame{TriggerAt: 9000.25, WinPoints: n, Traces: []sample.Trace{{Codes8: make([]uint8, n), Valid: n}, {Ch: 1, Codes8: make([]uint8, n), Valid: n}}}
	var a SramAverage
	for b.Loop() {
		f.SetLen(n)
		f.TriggerAt = 9000.25
		f.WinPoints = n
		f.Traces[0].Precision, f.Traces[1].Precision = QBuffer(f.Precision(0), n), QBuffer(f.Precision(1), n)
		for i := range f.Precision(0)[:n] {
			f.Precision(0)[i], f.Precision(1)[i] = uint16(32768+i), uint16(30000+i)
		}
		f.Traces[0].Codes8, f.Traces[1].Codes8 = f.Codes8(0)[:n], f.Codes8(1)[:n]
		a.Push(f, 16)
	}
}

// buildMaskRef is the column-by-column dilation BuildMaskFromEnvelope replaced.
// TRLC-LINKS: REQ-SDS-014
func buildMaskRef(lo, hi []uint8, winCols, tolCols, tolCodes int) (outLo, outHi []uint8) {
	nLo, nHi := make([]uint8, winCols), make([]uint8, winCols)
	for j := range winCols {
		if lo[j] > hi[j] {
			nLo[j], nHi[j] = 0, 255
		} else {
			nLo[j], nHi[j] = lo[j], hi[j]
		}
	}
	outLo, outHi = make([]uint8, winCols), make([]uint8, winCols)
	for j := range winCols {
		mn, mx := 255, 0
		for k := max(j-tolCols, 0); k <= min(j+tolCols, winCols-1); k++ {
			mn, mx = min(mn, int(nLo[k])), max(mx, int(nHi[k]))
		}
		outLo[j], outHi[j] = uint8(max(mn-tolCodes, 0)), uint8(min(mx+tolCodes, 255))
	}
	return outLo, outHi
}

// The running dilation builds the mask the column-by-column one did.
// TRLC-LINKS: REQ-SDS-014
func TestBuildMaskDilation(t *testing.T) {
	for _, w := range []int{1, 2, 7, 50, 333} {
		lo, hi := make([]uint8, w), make([]uint8, w)
		for j := range lo {
			lo[j], hi[j] = uint8((j*7919)%200), uint8(50+(j*104729)%200)
		}
		for _, tol := range []int{0, 1, 2, 5, 13, 400} {
			for _, codes := range []int{0, 8, 300} {
				m := BuildMaskFromEnvelope(lo, hi, w, tol, codes, 0)
				rl, rh := buildMaskRef(lo, hi, w, tol, codes)
				if string(m.Lo) != string(rl) || string(m.Hi) != string(rh) {
					t.Fatalf("w %d tol %d codes %d: dilation differs", w, tol, codes)
				}
			}
		}
	}
}

// TRLC-LINKS: REQ-SDS-014
func BenchmarkBuildMask(b *testing.B) {
	lo, hi := make([]uint8, 12000), make([]uint8, 12000)
	for j := range lo {
		lo[j], hi[j] = uint8(60+(j/97)%2*100), uint8(90+(j/97)%2*100)
	}
	for b.Loop() {
		BuildMaskFromEnvelope(lo, hi, len(lo), 5, 8, 0)
		BuildMaskFromEnvelope(lo, hi, len(lo), 20, 24, 0)
	}
}

// TRLC-LINKS: REQ-SDS-013, REQ-SDS-229
func BenchmarkClockRate(b *testing.B) {
	codes := make([]uint8, 20000)
	for i := range codes {
		codes[i] = uint8(60 + 140*((i/5)%2)) // a clock of ten samples
	}
	for b.Loop() {
		ClockRate(codes, 2e-9, false)
	}
}

// TRLC-LINKS: REQ-SDS-013
func BenchmarkMatchSequence(b *testing.B) {
	var sp []decode.Span
	for t := range 100 { // I2C transactions: start, address, direction, two bytes, stop
		x := t * 100
		sp = append(sp, decode.Span{I0: x, Kind: "start"}, decode.Span{I0: x + 1, Kind: "addr", Val: 0x50 + t%3},
			decode.Span{I0: x + 9, Kind: "rw", Text: "W"}, decode.Span{I0: x + 10, Kind: "data", Val: t & 0xff},
			decode.Span{I0: x + 20, Kind: "data", Val: 0x11}, decode.Span{I0: x + 30, Kind: "stop"})
	}
	p := SerialParams{Proto: SerI2C, Addr: 0x52, RW: 0, Pattern: []PatternElem{{Kind: PatternData, Value: 0x99, Mask: 0xff}, {Kind: PatternData, Value: 0x11, Mask: 0xff}}}
	for b.Loop() {
		MatchSequence(sp, p)
	}
}

// TRLC-LINKS: REQ-SDS-013
func BenchmarkSerialDecoder(b *testing.B) {
	for b.Loop() {
		LookupSerialDecoder(SerUART)
		LookupSerialDecoder(SerI2C)
	}
}

// The stepped snapshot index is j·periods/m for every record length.
// TRLC-LINKS: REQ-SDS-231
func TestRecordStuckIndex(t *testing.T) {
	for periods := 1; periods < 3000; periods += 7 {
		m := min(periods, 512)
		at, rem := 0, 0
		step, frac := periods/m, periods%m
		for j := range m {
			if at != j*periods/m {
				t.Fatalf("periods %d j %d: %d, want %d", periods, j, at, j*periods/m)
			}
			at += step
			if rem += frac; rem >= m {
				rem -= m
				at++
			}
		}
	}
}

// fundamentalHzRef is the float-compare reading fundamentalHz replaced.
// TRLC-LINKS: REQ-SDS-066
func fundamentalHzRef(sig []uint8, sampleS float64) float64 {
	n := len(sig)
	if n < 8 {
		return 0
	}
	var sum float64
	for _, v := range sig {
		sum += float64(v)
	}
	mean := sum / float64(n)
	const hyst = 2.0
	firstX, lastX, crossings, armed := -1.0, -1.0, 0, false
	for i := 1; i < n; i++ {
		a, b := float64(sig[i-1]), float64(sig[i])
		if !armed && b < mean-hyst {
			armed = true
		}
		if armed && a < mean && b >= mean {
			x := float64(i-1) + (mean-a)/(b-a)
			if firstX < 0 {
				firstX = x
			}
			lastX = x
			crossings++
			armed = false
		}
	}
	if crossings < 2 || lastX <= firstX {
		return 0
	}
	p := (lastX - firstX) / float64(crossings-1)
	if p <= 1 {
		return 0
	}
	return 1.0 / (p * sampleS)
}

// TRLC-LINKS: REQ-SDS-066
func TestFundamentalHzSame(t *testing.T) {
	for seed := range 200 {
		n := 50 + seed*37
		sig := make([]uint8, n)
		for i := range sig {
			sig[i] = uint8(128 + 60*math.Sin(2*math.Pi*float64(i)*(0.01+float64(seed)/5000)) + float64((i*seed)%5))
		}
		if got, want := FundamentalHz(sig, 1e-6), fundamentalHzRef(sig, 1e-6); got != want {
			t.Fatalf("seed %d: %v, want %v", seed, got, want)
		}
	}
}

// TRLC-LINKS: REQ-SDS-265
func BenchmarkSoundRingRead(b *testing.B) {
	r := NewSoundRing(16)
	raw := make([]byte, 4*40000)
	for i := range raw {
		raw[i] = byte(i)
	}
	now := time.Unix(0, 0)
	r.PushWords(raw, SoundScale{}, false, now)
	r.PushWords(raw, SoundScale{}, false, now) // past the end: the read wraps
	var q1, q2 []uint16
	for b.Loop() {
		blk := r.Read(r.Next-30000, 30000, q1, q2, 61e3)
		q1, q2 = blk.Q1, blk.Q2
	}
}

// The ring reads back what was pushed, across its wrap.
// TRLC-LINKS: REQ-SDS-265
func TestSoundRingReadWraps(t *testing.T) {
	r := NewSoundRing(8)
	for k := range 5 {
		raw := make([]byte, 4*97)
		for i := range 97 {
			v := uint16(k*1000 + i)
			raw[4*i], raw[4*i+1], raw[4*i+2], raw[4*i+3] = byte(v), byte(v>>8), byte(^v), byte(^v>>8)
		}
		r.PushWords(raw, SoundScale{}, false, time.Unix(0, 0))
	}
	blk := r.Read(r.Next-200, 200, nil, nil, 1)
	for i := range blk.N {
		o := blk.First + uint64(i)
		want := uint16(int(o/97)*1000 + int(o%97))
		if blk.Q1[i] != want || blk.Q2[i] != ^want {
			t.Fatalf("sample %d: %d %d, want %d %d", o, blk.Q1[i], blk.Q2[i], want, ^want)
		}
	}
}

// firstCrossingFromRef is the negating loop firstCrossingFrom replaced.
// TRLC-LINKS: REQ-SDS-275
func firstCrossingFromRef(q []uint16, level, hyst int, rising bool, start int) (int, bool) {
	armed := false
	for i, w := range q {
		c, l := int(w>>8), level
		if !rising {
			c, l = -c, -l
		}
		switch {
		case c <= l-hyst:
			armed = true
		case armed && c >= l:
			if i >= start {
				return i, true
			}
			armed = false
		}
	}
	return 0, false
}

// TRLC-LINKS: REQ-SDS-275
func TestFirstCrossingFromSame(t *testing.T) {
	q := make([]uint16, 3000)
	for i := range q {
		q[i] = uint16(128+int(100*math.Sin(float64(i)/37))) << 8
	}
	for _, rising := range []bool{true, false} {
		for _, level := range []int{0, 60, 128, 200, 255} {
			for _, start := range []int{0, 500, 2999, 4000} {
				for _, hyst := range []int{0, 4, 30} {
					a, b := FirstCrossingFrom(q, level, hyst, rising, start)
					c, d := firstCrossingFromRef(q, level, hyst, rising, start)
					if a != c || b != d {
						t.Fatalf("rising %v level %d start %d hyst %d: %d %v, want %d %v", rising, level, start, hyst, a, b, c, d)
					}
				}
			}
		}
	}
}

// TRLC-LINKS: REQ-SDS-275
func BenchmarkFirstCrossing(b *testing.B) {
	q := make([]uint16, 30000)
	for i := range q {
		q[i] = uint16(128+int(100*math.Sin(float64(i)/37))) << 8
	}
	for b.Loop() {
		FirstCrossingFrom(q, 128, 4, false, 29000)
	}
}
