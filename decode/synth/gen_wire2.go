// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the two-wire buses beyond the first thirty
// (proto_gen_test.go): line a the clock (TCKC, SDCKA), line b the data.

// ClockBits appends bits on line b, each over one clock period per on line
// a: the clock low for the first half (the data set there) and high for the
// second (read at its rise).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func ClockBits(l Pair, bits []int, per float64) {
	for _, b := range bits {
		l.Add(0, lvl(b), per/2)
		l.Add(1, lvl(b), per/2)
	}
}

// SDToken is an SD command (host) or response's bits: start 0, the
// transmission bit, six index bits, the 32-bit argument, CRC-7 (spoiled by
// badCRC) and the end bit.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func SDToken(host bool, idx int, arg uint32, badCRC bool) []int {
	b := []int{0, 0}
	if host {
		b[1] = 1
	}
	for i := 5; i >= 0; i-- {
		b = append(b, idx>>i&1)
	}
	for i := 31; i >= 0; i-- {
		b = append(b, int(arg>>i&1))
	}
	c := CRC7(b)
	if badCRC {
		c ^= 0x11
	}
	for i := 6; i >= 0; i-- {
		b = append(b, c>>i&1)
	}
	return append(b, 1)
}

// SDR2 is an R2 response's 136 bits: start, transmission 0, six reserved
// 1s, the 120-bit register (reg's bytes) with its CRC-7 and the end bit.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func SDR2(reg [15]int) []int {
	b := []int{0, 0, 1, 1, 1, 1, 1, 1}
	var r []int
	for _, v := range reg {
		for i := 7; i >= 0; i-- {
			r = append(r, v>>i&1)
		}
	}
	c := CRC7(r)
	b = append(b, r...)
	for i := 6; i >= 0; i-- {
		b = append(b, c>>i&1)
	}
	return append(b, 1)
}

// SDIdle is n clock periods of the CMD line high.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func SDIdle(n int) []int {
	b := make([]int, n)
	for i := range b {
		b[i] = 1
	}
	return b
}

// PDIChar is a PDI character's bits: start 0, eight bits least significant
// first, even parity (flipped by badPar) and two stop 1s.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func PDIChar(v int, badPar bool) []int {
	b := []int{0}
	p := 0
	for i := range 8 {
		b = append(b, v>>i&1)
		p ^= v >> i & 1
	}
	if badPar {
		p ^= 1
	}
	return append(b, p, 1, 1)
}

// CJTAGLine writes compact JTAG on TCKC (a) and TMSC (b) at a TCKC period
// per.
type CJTAGLine struct {
	P   Pair    // A TCKC, B TMSC
	Per float64 // the TCKC period
	TMS byte    // TMSC's level now
}

// Rise writes one TCKC cycle with TMSC at v (set a quarter period after
// TCKC falls: a change at the fall itself would count as an escape edge).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (c *CJTAGLine) Rise(v int) {
	c.P.Add(0, c.TMS, c.Per/4)
	c.TMS = lvl(v)
	c.P.Add(0, c.TMS, c.Per/4)
	c.P.Add(1, c.TMS, c.Per/2)
}

// Escape holds TCKC high while TMSC toggles n times, then one cycle low.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (c *CJTAGLine) Escape(n int) {
	c.P.Add(0, c.TMS, c.Per/2)
	c.P.Add(1, c.TMS, c.Per/2)
	for range n {
		c.TMS ^= 1
		c.P.Add(1, c.TMS, c.Per/2)
	}
	c.P.Add(1, c.TMS, c.Per/2)
}

// TCK writes an OScan1 TCK: nTDI, TMS and TDO on TMSC (first omits nTDI,
// as the first after the activation code does).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (c *CJTAGLine) TCK(tdi, tms, tdo int, first bool) {
	if !first {
		c.Rise(1 - tdi)
	}
	c.Rise(tms)
	c.Rise(tdo)
}

// Scan shifts n bits of tdi (out on tdo) through IR (ir) or DR from
// Run-Test/Idle and back to it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (c *CJTAGLine) Scan(ir bool, n int, tdi, tdo uint32) {
	c.TCK(0, 1, 0, false) // select DR
	if ir {
		c.TCK(0, 1, 0, false) // select IR
	}
	c.TCK(0, 0, 0, false) // capture
	c.TCK(0, 0, 0, false) // shift
	for i := range n {
		tms := 0
		if i == n-1 {
			tms = 1 // exit1 with the last bit
		}
		c.TCK(int(tdi>>i&1), tms, int(tdo>>i&1), false)
	}
	c.TCK(0, 1, 0, false) // update
	c.TCK(0, 0, 0, false) // run-test/idle
}

// MapleLine writes Maple bus frames on SDCKA (a) and SDCKB (b), a step of
// st seconds each line change.
type MapleLine struct {
	P      Pair    // A SDCKA, B SDCKB
	Step   float64 // seconds a line change
	la, lb byte
}

// Set moves the lines to a, b over one step.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (m *MapleLine) Set(a, b byte) {
	m.la, m.lb = a, b
	m.P.Add(a, b, m.Step)
}

// Frame writes a start pattern, the bytes (each bit at a fall of the
// clocking line, the other line holding it) and the end pattern.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func (m *MapleLine) Frame(bytes []int) {
	m.Set(1, 1)
	m.Set(0, 1)
	for range 4 {
		m.Set(0, 0)
		m.Set(0, 1)
	}
	m.Set(1, 1)
	k := 0
	for _, v := range bytes {
		for i := 7; i >= 0; i-- {
			bit := byte(v >> i & 1)
			if k%2 == 0 {
				// SDCKA clocks: SDCKB holds the bit (it may fall only
				// before a frame's first bit)
				m.Set(m.la, bit)
				if m.la == 0 {
					m.Set(1, bit)
				}
				m.Set(0, bit)
			} else {
				// SDCKB clocks: SDCKA holds the bit
				m.Set(bit, m.lb)
				if m.lb == 0 {
					m.Set(bit, 1)
				}
				m.Set(bit, 0)
			}
			k++
		}
	}
	// end: SDCKA high, then it falls twice with SDCKB low, then both rise
	m.Set(1, 0)
	m.Set(0, 0)
	m.Set(1, 0)
	m.Set(0, 0)
	m.Set(1, 1)
	m.Set(1, 1)
}

// MapleBytes is a Maple frame's bytes: the length in words, source,
// destination, command, the data words' bytes and the checksum.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func MapleBytes(src, dst, cmd int, data []int, badCk bool) []int {
	b := []int{len(data) / 4, src, dst, cmd}
	b = append(b, data...)
	x := 0
	for _, v := range b {
		x ^= v
	}
	if badCk {
		x ^= 0x40
	}
	return append(b, x)
}

// CaliperFrame writes a caliper's 24 bits (the count, then the flags,
// least significant first) at a clock period per, then rest seconds of
// clock low.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func CaliperFrame(l Pair, count int, neg, inch bool, per, rest float64) {
	flags := 0
	if neg {
		flags |= 1 << 4
	}
	if inch {
		flags |= 1 << 7
	}
	var bits []int
	for i := range 16 {
		bits = append(bits, count>>i&1)
	}
	for i := range 8 {
		bits = append(bits, flags>>i&1)
	}
	ClockBits(l, bits, per)
	l.Add(0, 0, rest)
}
