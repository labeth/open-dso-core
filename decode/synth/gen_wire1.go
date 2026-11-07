// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the single-wire buses beyond the first thirty
// (proto_gen_test.go): STM8 SWIM, TI SDQ, PJDL with PJON frames, DSI and
// Qi.

// SWIMFrame appends a SWIM frame at a bit of T seconds: the start bit
// (dir: 0 the host, 1 the device), nb bits of v most significant first,
// their even parity (flipped by badPar) and the acknowledge ack; a 0 is low
// for 20 of the bit's 22 SWIM clocks, a 1 for 2. Then gap seconds of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func SWIMFrame(w *Line, dir, nb, v int, period float64, ack int, badPar bool, gap float64) {
	bits := []int{dir}
	p := 0
	for i := nb - 1; i >= 0; i-- {
		bits = append(bits, v>>i&1)
		p ^= v >> i & 1
	}
	if badPar {
		p ^= 1
	}
	bits = append(bits, p, ack)
	for _, b := range bits {
		lo := 2.0
		if b == 0 {
			lo = 20
		}
		w.Add(0, lo/22*period)
		w.Add(1, (22-lo)/22*period)
	}
	w.Add(1, gap)
}

// SWIMSync appends a SWIM synchronization frame: 128 SWIM clocks of low
// at a bit of T (22 clocks), then idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func SWIMSync(w *Line, period float64) {
	w.Add(0, 128.0/22*period)
	w.Add(1, 4*period)
}

// SDQBreak appends an SDQ break (two bits low) and a bit of high.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func SDQBreak(w *Line, period float64) {
	w.Add(0, 2*period)
	w.Add(1, period)
}

// SDQByte appends an SDQ byte, least significant bit first: a 1 low for a
// quarter bit, a 0 for three quarters.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func SDQByte(w *Line, v int, period float64) {
	for i := range 8 {
		lo := 0.75
		if v>>i&1 == 1 {
			lo = 0.25
		}
		w.Add(0, lo*period)
		w.Add(1, (1-lo)*period)
	}
}

// PJONPacket is a PJON packet: receiver id, header, length, the header's
// CRC-8, payload and the CRC-8 of all before it (badEnd spoils it).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func PJONPacket(rx, header int, payload []int, badEnd bool) []int {
	b := []int{rx, header, 4 + len(payload) + 1}
	b = append(b, CRC8PJON(b))
	b = append(b, payload...)
	c := CRC8PJON(b)
	if badEnd {
		c ^= 0x5A
	}
	return append(b, c)
}

// PJDLFrame appends a PJDL frame in mode m (the line idles low): three
// sync pads (high for the pad time, then a data time low), then each byte
// after its own sync pad, eight data times least significant bit first;
// then idle seconds.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func PJDLFrame(w *Line, m int, b []int, idle float64) {
	data, pad := PJDLModes[m][0]*1e-6, PJDLModes[m][1]*1e-6
	for range 3 {
		w.Add(1, pad)
		w.Add(0, data)
	}
	for _, v := range b {
		w.Add(1, pad)
		w.Add(0, data)
		for k := range 8 {
			w.Add(byte(v>>k&1), data)
		}
	}
	w.Add(0, idle)
}

// DSIFrame appends a DSI frame on an idle-low line: the start bit 0 and
// eight bits of v most significant first, Manchester at 600 bit/s (a 1
// high then low), then idle seconds.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func DSIFrame(w *Line, v int, idle float64) {
	const half = 1 / 1200.0
	bits := []int{0}
	for i := 7; i >= 0; i-- {
		bits = append(bits, v>>i&1)
	}
	for _, b := range bits {
		w.Add(lvl(b), half)
		w.Add(lvl(1-b), half)
	}
	w.Add(0, idle)
}

// QiPacket appends a Qi packet at 2 kbit/s, biphase mark: eleven preamble
// 1s, then the header, message and checksum (spoiled when badCk), each a
// start 0, eight bits least significant first, odd parity and a stop 1;
// then a transition (the stop bit's end, which sigrok needs to see) and
// idle seconds.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func QiPacket(w *Line, b []int, badCk bool, idle float64) {
	l := &BMCLine{W: w, Bit: 500e-6}
	if k := len(w.Bits); k > 0 {
		l.Lvl = w.Bits[k-1]
	}
	for range 11 {
		l.Put(1)
	}
	x := 0
	for _, v := range b {
		x ^= v
	}
	if badCk {
		x ^= 0x21
	}
	for _, v := range append(append([]int{}, b...), x) {
		l.Put(0)
		p := 1
		for i := range 8 {
			l.Put(v >> i & 1)
			p ^= v >> i & 1
		}
		l.Put(p)
		l.Put(1)
	}
	w.Add(l.Lvl^1, idle)
}
