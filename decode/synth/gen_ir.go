// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the infrared remote protocols (proto_gen_test.go): marks
// and spaces on an IRLine, written active-low (a receiver's output) unless
// activeHigh, a mark modulated at carrierHz when that is set.

// IRLine writes marks and spaces.
type IRLine struct {
	W          *Line
	ActiveHigh bool    // marks high (an emitter's drive), not low (a receiver's output)
	CarrierHz  float64 // a mark's carrier; 0: the mark is a level
}

// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func (l IRLine) level(mark bool) byte {
	if mark == l.ActiveHigh {
		return 1
	}
	return 0
}

// Mark appends a mark of d seconds.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func (l IRLine) Mark(d float64) {
	if l.CarrierHz <= 0 {
		l.W.Add(l.level(true), d)
		return
	}
	half := 0.5 / l.CarrierHz
	for t := 0.0; t < d-1e-12; t += 2 * half {
		on := min(half, d-t)
		l.W.Add(l.level(true), on)
		if off := min(half, d-t-on); off > 0 {
			l.W.Add(l.level(false), off)
		}
	}
}

// Space appends a space of d seconds.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func (l IRLine) Space(d float64) { l.W.Add(l.level(false), d) }

// NECFrame appends an NEC frame of addr and cmd (addrHi < 0: the address's
// inverse) and the 40 ms after it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func NECFrame(l IRLine, addr, addrHi, cmd int) {
	if addrHi < 0 {
		addrHi = addr ^ 0xff
	}
	l.Mark(9e-3)
	l.Space(4.5e-3)
	for _, v := range []int{addr, addrHi, cmd, cmd ^ 0xff} {
		for i := range 8 {
			l.Mark(562.5e-6)
			if v>>i&1 == 1 {
				l.Space(1687.5e-6)
			} else {
				l.Space(562.5e-6)
			}
		}
	}
	l.Mark(562.5e-6)
	l.Space(40e-3)
}

// NECRepeat appends an NEC repeat code and the 96 ms after it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func NECRepeat(l IRLine) {
	l.Mark(9e-3)
	l.Space(2.25e-3)
	l.Mark(562.5e-6)
	l.Space(96e-3)
}

// SIRCFrame appends a SIRC frame of nbits (12, 15 or 20) and the gap to 45 ms.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func SIRCFrame(l IRLine, nbits, cmd, addr, ext int) {
	bits := make([]int, 0, nbits)
	push := func(v, n int) {
		for i := range n {
			bits = append(bits, v>>i&1)
		}
	}
	push(cmd, 7)
	switch nbits {
	case 12:
		push(addr, 5)
	case 15:
		push(addr, 8)
	case 20:
		push(addr, 5)
		push(ext, 8)
	}
	l.Mark(2.4e-3)
	used := 2.4e-3
	for _, b := range bits {
		l.Space(0.6e-3)
		d := 0.6e-3
		if b == 1 {
			d = 1.2e-3
		}
		l.Mark(d)
		used += 0.6e-3 + d
	}
	l.Space(45e-3 - used)
}

// RC5Frame appends an RC-5 frame (address 5 bits, command 6 or 7 bits:
// RC-5X) and 100 ms of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func RC5Frame(l IRLine, toggle, addr, cmd int) {
	s2 := 1 - (cmd>>6)&1
	bits := []int{1, s2, toggle}
	for k := 4; k >= 0; k-- {
		bits = append(bits, addr>>k&1)
	}
	for k := 5; k >= 0; k-- {
		bits = append(bits, cmd>>k&1)
	}
	const h = 889e-6
	for _, b := range bits {
		if b == 1 {
			l.Space(h)
			l.Mark(h)
		} else {
			l.Mark(h)
			l.Space(h)
		}
	}
	l.Space(100e-3)
}

// RC6Frame appends an RC-6 frame of the mode and payload bytes and 100 ms
// of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-234
func RC6Frame(l IRLine, mode, toggle int, payload []int) {
	const t = 444e-6
	bit := func(b int, u float64) {
		if b == 1 {
			l.Mark(u)
			l.Space(u)
		} else {
			l.Space(u)
			l.Mark(u)
		}
	}
	l.Mark(6 * t)
	l.Space(2 * t)
	bit(1, t)
	for k := 2; k >= 0; k-- {
		bit(mode>>k&1, t)
	}
	bit(toggle, 2*t)
	for _, v := range payload {
		for k := 7; k >= 0; k-- {
			bit(v>>k&1, t)
		}
	}
	l.Space(100e-3)
}
