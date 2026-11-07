// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the clocked two-line protocols (proto_gen_test.go): a and b
// advance together.

// PS2Frame appends a PS/2 frame at a clock period per: a device's (11
// clocks, data changed while the clock is high) or a host's (the request,
// then 12 clocks with the device's acknowledge), then gap of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func PS2Frame(t Pair, v int, host, badParity bool, per, gap float64) {
	ones := 0
	bits := []byte{0}
	for i := range 8 {
		b := byte(v >> i & 1)
		ones += int(b)
		bits = append(bits, b)
	}
	p := byte(1 - ones&1)
	if badParity {
		p ^= 1
	}
	bits = append(bits, p, 1)
	if !host {
		for _, b := range bits {
			t.Add(1, b, per/4) // data set while the clock is high
			t.Add(1, b, per/4)
			t.Add(0, b, per/2) // read at the fall
		}
		t.Add(1, 1, gap)
		return
	}
	// request to send: clock held low, then data low, then clock released
	t.Add(0, 1, 120e-6)
	t.Add(0, 0, 10e-6)
	t.Add(1, 0, per/2)
	for k := range 12 {
		d := byte(0) // the device's acknowledge on the twelfth clock
		if k < 11 {
			d = bits[k]
		}
		// at fall k the line holds bit k, the host's next bit comes while low
		t.Add(0, d, per/4)
		nd := byte(1)
		if k+1 < 11 {
			nd = bits[k+1]
		} else if k+1 == 11 {
			nd = 0
		}
		t.Add(0, nd, per/4)
		t.Add(1, nd, per/2)
	}
	t.Add(1, 1, gap)
}

// SWDLine writes SWD cycles, each a low half then a high half of the
// clock. A host bit holds for its cycle (set at the fall, read at the
// rise); a target's is driven from a rise and read at the next fall.
type SWDLine struct {
	P   Pair    // A the clock, B SWDIO
	Per float64 // the clock period
}

// Cycles writes clock cycles: lo[i] on SWDIO in cycle i's low half, hi[i]
// in its high half.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func (s *SWDLine) Cycles(lo, hi []byte) {
	for i := range lo {
		s.P.Add(0, lo[i], s.Per/2)
		s.P.Add(1, hi[i], s.Per/2)
	}
}

// Host drives bits in turn.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func (s *SWDLine) Host(b ...byte) { s.Cycles(b, b) }

// LineReset clocks 56 cycles with SWDIO high, then two idle cycles.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func (s *SWDLine) LineReset() {
	for range 56 {
		s.Host(1)
	}
	s.Host(0, 0)
}

// Transfer appends an SWD transfer: the request, the acknowledge, and for
// OK the data word and its parity (spoiled when badParity), then idle cycles.
// The target's bits (acknowledge, read data) are read at the falls of the
// cycles after the turnaround's: each cycle's low half holds the bit read
// at its fall, the previous cycle's high half drives it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func (s *SWDLine) Transfer(ap, read bool, addr, ack int, data uint32, badParity bool, idle int) {
	b := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	a2, a3 := byte(addr>>2&1), byte(addr>>3&1)
	s.Host(1, b(ap), b(read), a2, a3, (b(ap)+b(read)+a2+a3)&1, 0, 1)
	par := byte(0)
	for k := range 32 {
		par ^= byte(data >> k & 1)
	}
	if badParity {
		par ^= 1
	}
	// the target's bits, read at the falls of the cycles after the turnaround
	tb := []byte{byte(ack & 1), byte(ack >> 1 & 1), byte(ack >> 2 & 1)}
	if ack == 1 && read {
		for k := range 32 {
			tb = append(tb, byte(data>>k&1))
		}
		tb = append(tb, par)
	}
	// the turnaround cycle: low half the host's park, high half the first target bit
	lo, hi := []byte{1}, []byte{tb[0]}
	for k := range tb {
		next := byte(1) // released after the last
		if k+1 < len(tb) {
			next = tb[k+1]
		}
		lo, hi = append(lo, tb[k]), append(hi, next)
	}
	s.Cycles(lo, hi)
	if ack == 1 && !read {
		s.Host(1) // the turnaround back to the host
		for k := range 32 {
			s.Host(byte(data >> k & 1))
		}
		s.Host(par)
	}
	for range idle {
		s.Host(0)
	}
}

// MDIOFrame appends an MDIO frame (st 1: clause 22, 0: clause 45) at clock
// period per: the preamble of pre ones, start, opcode, addresses, the
// turnaround (taBit2 its second bit) and the data, then idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func MDIOFrame(t Pair, per float64, pre, st, op, phy, reg, data int, taBit2 byte) {
	bit := func(b byte) { // data set while the clock is low, read at the rise
		t.Add(0, b, per/2)
		t.Add(1, b, per/2)
	}
	for range pre {
		bit(1)
	}
	put := func(v, n int) {
		for k := n - 1; k >= 0; k-- {
			bit(byte(v >> k & 1))
		}
	}
	put(st, 2)
	put(op, 2)
	put(phy, 5)
	put(reg, 5)
	bit(1)
	bit(taBit2)
	put(data, 16)
	t.Add(0, 1, per/2)
	t.Add(1, 1, 20*per)
}

// WiegandFrame appends a Wiegand frame of the bits (MSB first given as
// a string of 0 and 1): 50 us pulses on D0 or D1 every interval, then
// 30 ms of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func WiegandFrame(t Pair, bits string, interval float64) {
	for _, c := range bits {
		if c == '0' {
			t.Add(0, 1, 50e-6)
		} else {
			t.Add(1, 0, 50e-6)
		}
		t.Add(1, 1, interval-50e-6)
	}
	t.Add(1, 1, 30e-3)
}

// WiegandBits is a 26- or 34-bit frame of facility fc and card with its
// parities, as WiegandFrame takes it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-236
func WiegandBits(nb, fc, card int) string {
	fcBits := 8
	if nb == 34 {
		fcBits = 16
	}
	var body []byte
	for k := fcBits - 1; k >= 0; k-- {
		body = append(body, byte('0'+fc>>k&1))
	}
	for k := 15; k >= 0; k-- {
		body = append(body, byte('0'+card>>k&1))
	}
	h := (nb - 2) / 2
	cnt := func(s []byte) int {
		c := 0
		for _, x := range s {
			c += int(x - '0')
		}
		return c
	}
	even := byte('0' + cnt(body[:h])&1)
	odd := byte('0' + 1 - cnt(body[h:])&1)
	return string(even) + string(body) + string(odd)
}
