// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of 125 kHz RFID (proto_gen_test.go): an EM4100 tag's frames
// on a demodulated line, and a reader's commands as its field (a carrier
// of edges every half field clock) or the field's envelope.

// RFIDFieldClock is a field clock at 125 kHz.
const RFIDFieldClock = 8e-6

// RFIDField is a reader's line: the field on or off for a while, as a
// carrier or an envelope.
type RFIDField struct {
	W       *Line
	Carrier bool // the field as its carrier; false: as its envelope
}

// On adds d seconds of field.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func (f RFIDField) On(d float64) {
	if !f.Carrier {
		f.W.Add(1, d)
		return
	}
	half := RFIDFieldClock / 2
	k := int(d/half + 0.5)
	for i := range k {
		f.W.Add(byte(1-i&1), half)
	}
}

// Off adds d seconds of gap.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func (f RFIDField) Off(d float64) { f.W.Add(0, d) }

// EM4305Command writes an EM4305 command after a first field stop: a 0 is
// 18 field clocks of field and a 20-clock gap, a 1 32 clocks of field; then
// 400 clocks of field.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func EM4305Command(f RFIDField, bits []int) {
	f.Off(55 * RFIDFieldClock)
	for _, b := range bits {
		if b == 1 {
			f.On(32 * RFIDFieldClock)
		} else {
			f.On(18 * RFIDFieldClock)
			f.Off(20 * RFIDFieldClock)
		}
	}
	f.On(400 * RFIDFieldClock)
}

// EM4305Bits are an EM4305 Login (addr < 0) or Write's bits: the 0, the
// command and parity, the address (four bits least significant first, two
// zeros, parity), the word's bytes with parities, column parity, stop 0;
// badPar breaks the first byte's parity.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func EM4305Bits(cmd, addr int, word [4]int, badPar bool) []int {
	b := []int{0}
	par := func(v []int) int {
		p := 0
		for _, x := range v {
			p ^= x
		}
		return p
	}
	c := []int{cmd >> 2 & 1, cmd >> 1 & 1, cmd & 1}
	b = append(append(b, c...), par(c))
	if addr >= 0 {
		a := []int{addr & 1, addr >> 1 & 1, addr >> 2 & 1, addr >> 3 & 1, 0, 0}
		b = append(append(b, a...), par(a))
	}
	col := 0
	for k, v := range word {
		var r []int
		for i := 7; i >= 0; i-- {
			r = append(r, v>>i&1)
		}
		p := par(r)
		if badPar && k == 0 {
			p ^= 1
		}
		b = append(append(b, r...), p)
		col ^= v
	}
	for i := 7; i >= 0; i-- {
		b = append(b, col>>i&1)
	}
	return append(b, 0)
}

// T55xxCommand writes a T55xx command after a start gap: each bit 24 (0) or
// 56 (1) field clocks of field and a 25-clock gap; then 400 clocks of field.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func T55xxCommand(f RFIDField, bits []int) {
	f.Off(30 * RFIDFieldClock)
	for _, b := range bits {
		if b == 1 {
			f.On(56 * RFIDFieldClock)
		} else {
			f.On(24 * RFIDFieldClock)
		}
		f.Off(25 * RFIDFieldClock)
	}
	f.On(400 * RFIDFieldClock)
}

// T55xxBits are a T55xx write: opcode, the password when pwd >= 0, the lock
// bit, the 32-bit word and the 3-bit block, most significant bits first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func T55xxBits(op int, pwd int64, lock int, word uint32, blk int) []int {
	b := []int{op >> 1 & 1, op & 1}
	if pwd >= 0 {
		for i := 31; i >= 0; i-- {
			b = append(b, int(pwd>>i&1))
		}
	}
	b = append(b, lock)
	for i := 31; i >= 0; i-- {
		b = append(b, int(word>>i&1))
	}
	return append(b, blk>>2&1, blk>>1&1, blk&1)
}

// EM4100Frame appends an EM4100 frame of five bytes (the version or
// customer byte, then the data) as Manchester bits of hp-second halves, a
// 1 high then low; badRow breaks the first row's parity.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-252
func EM4100Frame(w *Line, b [5]int, hp float64, badRow bool) {
	bits := []int{1, 1, 1, 1, 1, 1, 1, 1, 1}
	col := 0
	for k := range 10 {
		nib := b[k/2] >> (4 * (1 - k%2)) & 15
		col ^= nib
		p := 0
		for i := 3; i >= 0; i-- {
			bits = append(bits, nib>>i&1)
			p ^= nib >> i & 1
		}
		if badRow && k == 0 {
			p ^= 1
		}
		bits = append(bits, p)
	}
	for i := 3; i >= 0; i-- {
		bits = append(bits, col>>i&1)
	}
	bits = append(bits, 0)
	for _, x := range bits {
		w.Add(lvl(x), hp)
		w.Add(lvl(1-x), hp)
	}
}
