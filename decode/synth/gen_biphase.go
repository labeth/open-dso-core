// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the biphase protocols (proto_gen_test.go).

// DALIFrame appends a DALI frame of nbits (8, 16 or 24) of v, most
// significant first, on a bus idling high, then idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func DALIFrame(w *Line, nbits, v int, idleS float64) {
	const h = 1 / 2400.0
	bit := func(b int) {
		if b == 1 {
			w.Add(0, h)
			w.Add(1, h)
		} else {
			w.Add(1, h)
			w.Add(0, h)
		}
	}
	bit(1)
	for k := nbits - 1; k >= 0; k-- {
		bit(v >> k & 1)
	}
	w.Add(1, idleS)
}

// PDEnc5 is the 4b5b line symbol of a nibble (pd4b5b's inverse) and the
// K-codes: 16 Sync-1, 17 Sync-2, 18 Sync-3, 19 RST-1, 20 RST-2, 21 EOP.
var PDEnc5 = [22]int{
	0x1E, 0x09, 0x14, 0x15, 0x0A, 0x0B, 0x0E, 0x0F, 0x12, 0x13, 0x16, 0x17, 0x1A, 0x1B, 0x1C, 0x1D,
	0x18, 0x11, 0x06, 0x07, 0x19, 0x0D,
}

// BMCLine writes biphase-mark bits: a transition at every bit's start and
// another mid-bit for a 1.
type BMCLine struct {
	W   *Line
	Lvl byte    // the level now
	Bit float64 // seconds a bit
}

// Put writes one bit.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func (b *BMCLine) Put(v int) {
	b.Lvl ^= 1
	if v == 1 {
		b.W.Add(b.Lvl, b.Bit/2)
		b.Lvl ^= 1
		b.W.Add(b.Lvl, b.Bit/2)
		return
	}
	b.W.Add(b.Lvl, b.Bit)
}

// Sym writes the five bits of a 4b5b symbol (PDEnc5).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func (b *BMCLine) Sym(s int) {
	for k := range 5 {
		b.Put(PDEnc5[s] >> k & 1)
	}
}

// PDMessage appends a USB PD message at 300 kbit/s: preamble, the ordered
// set (four K-codes of PDEnc5), the header and data objects with their
// CRC-32 (spoiled when badCRC), EOP, then idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func PDMessage(w *Line, sop [4]int, header int, objs []uint32, badCRC bool) {
	b := &BMCLine{W: w, Bit: 1 / 300e3}
	for i := range 64 {
		b.Put(i & 1)
	}
	for _, k := range sop {
		b.Sym(k)
	}
	bytes := []int{header & 0xff, header >> 8}
	for _, o := range objs {
		bytes = append(bytes, int(o&0xff), int(o>>8&0xff), int(o>>16&0xff), int(o>>24))
	}
	crc := CRC32IEEE(bytes)
	if badCRC {
		crc ^= 0x10
	}
	bytes = append(bytes, int(crc&0xff), int(crc>>8&0xff), int(crc>>16&0xff), int(crc>>24))
	for _, v := range bytes {
		b.Sym(v & 0xf)
		b.Sym(v >> 4)
	}
	b.Sym(21)
	b.Lvl ^= 1
	w.Add(b.Lvl, 1/300e3) // the closing transition, then the line rests
	w.Add(b.Lvl, 40e-6)
}

// PDSOP and PDHardReset are ordered sets in PDEnc5's K-code numbers.
var (
	PDSOP       = [4]int{16, 16, 16, 17}
	PDSOPPrime  = [4]int{16, 16, 18, 18}
	PDHardReset = [4]int{19, 19, 19, 20}
)

// SPDIFStream appends S/PDIF frames (two subframes each) of the samples
// (pairs: channel A, channel B) at fs, the first frame opening a block.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func SPDIFStream(w *Line, fs float64, frame0 int, samples [][2]int) {
	cell := 1 / (64 * fs)
	lvl := byte(0)
	run := func(halves int) {
		lvl ^= 1
		w.Add(lvl, float64(halves)*cell/2)
	}
	for f, s := range samples {
		for ch := range 2 {
			switch {
			case ch == 1:
				run(3)
				run(2)
				run(1)
				run(2)
			case (frame0+f)%192 == 0:
				run(3)
				run(1)
				run(1)
				run(3)
			default:
				run(3)
				run(3)
				run(1)
				run(1)
			}
			v := s[ch]
			ones := 0
			for k := range 28 {
				b := 0
				switch {
				case k < 24:
					b = v >> k & 1
				case k == 27:
					b = ones & 1
				}
				ones += b
				if b == 1 {
					run(1)
					run(1)
				} else {
					run(2)
				}
			}
		}
	}
}
