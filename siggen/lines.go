package siggen

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Protocol line generators (signal.go): each bus is laid out once as a timeline of level
// changes on all its lines over one repetition period; a channel set to one line of the bus
// samples that line. Channels set to lines of the same bus with the same options therefore stay
// in step (clock and data on two channels).

// timeline records level changes of named lines.
type timeline struct {
	now   float64
	lines map[string]*lineWave
}

// TRLC-LINKS: REQ-DSO-298
func newTimeline(init map[string]float64) *timeline {
	tl := &timeline{lines: map[string]*lineWave{}}
	for n, l := range init {
		tl.lines[n] = &lineWave{t: []float64{0}, lv: []float64{l}}
	}
	return tl
}

// set changes line n to level l now.
// TRLC-LINKS: REQ-DSO-298
func (tl *timeline) set(n string, l float64) {
	w := tl.lines[n]
	last := len(w.t) - 1
	if w.lv[last] == l {
		return
	}
	if w.t[last] == tl.now {
		w.lv[last] = l
		return
	}
	w.t = append(w.t, tl.now)
	w.lv = append(w.lv, l)
}

// TRLC-LINKS: REQ-DSO-298
func (tl *timeline) adv(dt float64) { tl.now += dt }

// finish closes the period after idle and returns the line.
// TRLC-LINKS: REQ-DSO-298
func (tl *timeline) finish(n string, idle float64) (*lineWave, float64) {
	p := tl.now + idle
	for _, w := range tl.lines {
		w.period = p
	}
	return tl.lines[n], p
}

// parseHex parses "48656c" or "48:65:6c" (also ' ' or '_' separators).
// TRLC-LINKS: REQ-DSO-298
func parseHex(s string) ([]byte, error) {
	s = strings.NewReplacer(":", "", " ", "", "_", "", "0x", "").Replace(s)
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("data %q: want hex bytes", s)
	}
	return b, nil
}

type lineOpts struct {
	m   map[string]string
	err error
}

// TRLC-LINKS: REQ-DSO-298
func (o *lineOpts) str(k, def string) string {
	v, ok := o.m[k]
	if !ok {
		return def
	}
	delete(o.m, k)
	return v
}

// TRLC-LINKS: REQ-DSO-298
func (o *lineOpts) num(k string, def float64) float64 {
	v, ok := o.m[k]
	if !ok {
		return def
	}
	delete(o.m, k)
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || x < 0 {
		o.fail(fmt.Errorf("%s=%q: want a number ≥ 0", k, v))
	}
	return x
}

// TRLC-LINKS: REQ-DSO-298
func (o *lineOpts) int(k string, def, lo, hi int) int {
	v, ok := o.m[k]
	if !ok {
		return def
	}
	delete(o.m, k)
	x, err := strconv.ParseInt(v, 0, 64)
	if err != nil || x < int64(lo) || x > int64(hi) {
		o.fail(fmt.Errorf("%s=%q: want %d..%d", k, v, lo, hi))
	}
	return int(x)
}

// TRLC-LINKS: REQ-DSO-298
func (o *lineOpts) data(k, def string) []byte {
	b, err := parseHex(o.str(k, def))
	if err != nil {
		o.fail(err)
	}
	return b
}

// TRLC-LINKS: REQ-DSO-298
func (o *lineOpts) fail(err error) {
	if o.err == nil {
		o.err = err
	}
}

// maxLineBits bounds one frame (keeps the timelines small).
const maxLineBits = 1 << 16

// buildLine lays out the bus of a line kind ("uart", "spi.clk", ...) at the given bit rate
// and returns that line's wave and the repetition period.
// TRLC-LINKS: REQ-DSO-298
func buildLine(kind string, rate float64, m map[string]string) (*lineWave, float64, error) {
	bus, line, _ := strings.Cut(kind, ".")
	if rate <= 0 || rate > 5e9 {
		return nil, 0, fmt.Errorf("%s: bit rate %g out of range", kind, rate)
	}
	o := &lineOpts{m: m}
	bt := 1 / rate
	idleDefault := 20 * bt
	if bus == "prbs" {
		idleDefault = 0 // the sequence repeats without a gap
	}
	idle := o.num("idle", idleDefault)
	var tl *timeline
	switch bus {
	case "uart":
		if line != "" && line != "tx" {
			return nil, 0, fmt.Errorf("uart line %q: want uart", line)
		}
		line = "tx"
		tl = uartBus(o, bt)
	case "spi":
		if line == "" {
			line = "mosi"
		}
		tl = spiBus(o, bt)
	case "i2c":
		if line == "" {
			line = "sda"
		}
		tl = i2cBus(o, bt)
	case "can":
		line = "rx"
		tl = canBus(o, bt)
	case "lin":
		line = "tx"
		tl = linBus(o, bt)
	case "manchester":
		line = "tx"
		tl = manchesterBus(o, bt)
	case "prbs":
		if line == "" {
			line = "data"
		}
		tl = prbsBus(o, bt)
	default:
		return nil, 0, fmt.Errorf("unknown kind %q (dc sine square pulse triangle ramp noise uart spi.* i2c.* can can.h can.l lin manchester prbs prbs.clk)", kind)
	}
	if o.err != nil {
		return nil, 0, fmt.Errorf("%s: %w", kind, o.err)
	}
	if len(o.m) > 0 {
		return nil, 0, fmt.Errorf("%s: unknown options %v", kind, keys(o.m))
	}
	if tl.lines[line] == nil {
		return nil, 0, fmt.Errorf("%s: no line %q", kind, line)
	}
	if tl.now*rate > maxLineBits {
		return nil, 0, fmt.Errorf("%s: frame longer than %d bits", kind, maxLineBits)
	}
	w, p := tl.finish(line, idle)
	return w, p, nil
}

// uartByte appends one character (start, data, parity, stop) on line n.
// TRLC-LINKS: REQ-DSO-298
func uartByte(tl *timeline, n string, b uint, bits int, parity string, stop, bt float64, msb bool) {
	tl.set(n, 0)
	tl.adv(bt)
	ones := 0
	for i := range bits {
		k := i
		if msb {
			k = bits - 1 - i
		}
		v := float64(b >> uint(k) & 1)
		ones += int(v)
		tl.set(n, v)
		tl.adv(bt)
	}
	switch parity {
	case "e":
		tl.set(n, float64(ones&1))
		tl.adv(bt)
	case "o":
		tl.set(n, float64(1-ones&1))
		tl.adv(bt)
	case "m":
		tl.set(n, 1)
		tl.adv(bt)
	case "s":
		tl.set(n, 0)
		tl.adv(bt)
	}
	tl.set(n, 1)
	tl.adv(stop * bt)
}

// TRLC-LINKS: REQ-DSO-298
func uartBus(o *lineOpts, bt float64) *timeline {
	data := o.data("data", "48656c6c6f")
	bits := o.int("bits", 8, 5, 9)
	parity := strings.ToLower(o.str("parity", "n"))
	switch parity {
	case "n", "e", "o", "m", "s":
	default:
		o.fail(fmt.Errorf("parity=%q: want n|e|o|m|s", parity))
	}
	stop := o.num("stop", 1)
	if stop != 1 && stop != 1.5 && stop != 2 {
		o.fail(fmt.Errorf("stop=%g: want 1, 1.5 or 2", stop))
	}
	gap := o.num("gap", 0)
	msb := o.int("lsb", 1, 0, 1) == 0
	tl := newTimeline(map[string]float64{"tx": 1})
	tl.adv(2 * bt) // idle before the first start bit
	for _, b := range data {
		uartByte(tl, "tx", uint(b), bits, parity, stop, bt, msb)
		tl.adv(gap)
	}
	return tl
}

// TRLC-LINKS: REQ-DSO-298
func spiBus(o *lineOpts, bt float64) *timeline {
	mosi := o.data("data", "a53c")
	miso := o.data("miso", "")
	mode := o.int("mode", 0, 0, 3)
	bits := o.int("bits", 8, 4, 32)
	lsb := o.int("lsb", 0, 0, 1) == 1
	csgap := o.num("csgap", bt)
	cpol, cpha := float64(mode>>1), mode&1 == 1
	tl := newTimeline(map[string]float64{"clk": cpol, "mosi": 0, "miso": 0, "cs": 1})
	tl.adv(bt)
	tl.set("cs", 0)
	word := func(d []byte, i int) uint64 { // word i of bits from the byte stream, MSB first
		var w uint64
		for k := range bits {
			bit := i*bits + k
			if bit/8 < len(d) {
				w = w<<1 | uint64(d[bit/8]>>(7-uint(bit%8))&1)
			} else {
				w <<= 1
			}
		}
		return w
	}
	nw := (len(mosi)*8 + bits - 1) / bits
	nw = max(nw, (len(miso)*8+bits-1)/bits)
	for i := range nw {
		wo, wi := word(mosi, i), word(miso, i)
		for k := range bits {
			sh := uint(bits - 1 - k)
			if lsb {
				sh = uint(k)
			}
			if !cpha {
				if k == 0 {
					tl.adv(csgap / 2)
				}
				tl.set("mosi", float64(wo>>sh&1))
				tl.set("miso", float64(wi>>sh&1))
				tl.adv(bt / 2)
				tl.set("clk", 1-cpol) // leading edge: sample
				tl.adv(bt / 2)
				tl.set("clk", cpol)
			} else {
				if k == 0 {
					tl.adv(csgap / 2)
				}
				tl.set("clk", 1-cpol) // leading edge: shift out
				tl.set("mosi", float64(wo>>sh&1))
				tl.set("miso", float64(wi>>sh&1))
				tl.adv(bt / 2)
				tl.set("clk", cpol) // trailing edge: sample
				tl.adv(bt / 2)
			}
		}
	}
	tl.adv(bt / 2)
	tl.set("cs", 1)
	return tl
}

// TRLC-LINKS: REQ-DSO-298
func i2cBus(o *lineOpts, bt float64) *timeline {
	addr := o.int("addr", 0x50, 0, 0x7f)
	rw := strings.ToLower(o.str("rw", "w"))
	data := o.data("data", "00a5")
	if rw != "w" && rw != "r" {
		o.fail(fmt.Errorf("rw=%q: want w|r", rw))
	}
	tl := newTimeline(map[string]float64{"scl": 1, "sda": 1})
	q := bt / 4
	tl.adv(bt)
	tl.set("sda", 0) // START: SDA falls while SCL is high
	tl.adv(bt / 2)
	tl.set("scl", 0)
	bit := func(v float64) {
		tl.adv(q)
		tl.set("sda", v)
		tl.adv(q)
		tl.set("scl", 1)
		tl.adv(2 * q)
		tl.set("scl", 0)
	}
	byte9 := func(b byte, ack float64) {
		for k := 7; k >= 0; k-- {
			bit(float64(b >> uint(k) & 1))
		}
		bit(ack)
	}
	r := 0
	if rw == "r" {
		r = 1
	}
	byte9(byte(addr<<1|r), 0) //nolint:gosec // G115: a 7-bit address
	for i, b := range data {
		ack := 0.0
		if rw == "r" && i == len(data)-1 {
			ack = 1 // the master NACKs the last byte it reads
		}
		byte9(b, ack)
	}
	tl.adv(q)
	tl.set("sda", 0) // STOP: SDA rises while SCL is high
	tl.adv(q)
	tl.set("scl", 1)
	tl.adv(bt / 2)
	tl.set("sda", 1)
	return tl
}

// crc15 is the CAN CRC (polynomial 0x4599) over the bits.
// TRLC-LINKS: REQ-DSO-298
func crc15(bits []byte) uint16 {
	var crc uint16
	for _, b := range bits {
		nxt := (b ^ byte(crc>>14&1)) & 1
		crc = crc << 1 & 0x7fff
		if nxt == 1 {
			crc ^= 0x4599
		}
	}
	return crc
}

// TRLC-LINKS: REQ-DSO-298
func canBus(o *lineOpts, bt float64) *timeline {
	id := o.int("id", 0x123, 0, 0x1fffffff)
	ext := o.int("ext", 0, 0, 1) == 1
	rtr := o.int("rtr", 0, 0, 1)
	data := o.data("data", "deadbeef")
	if len(data) > 8 {
		o.fail(fmt.Errorf("data: %d bytes, classic CAN carries at most 8", len(data)))
		data = data[:8]
	}
	if !ext && id > 0x7ff {
		o.fail(fmt.Errorf("id=%#x: a standard id has 11 bits (ext=1 for 29)", id))
	}
	var f []byte
	put := func(v uint64, n int) {
		for k := n - 1; k >= 0; k-- {
			f = append(f, byte(v>>uint(k)&1))
		}
	}
	uid, urtr := uint64(id), uint64(rtr) //nolint:gosec // G115: o.int bounds both to their non-negative ranges
	put(0, 1)                            // SOF
	if ext {
		put(uid>>18, 11)
		put(1, 1) // SRR
		put(1, 1) // IDE
		put(uid&0x3ffff, 18)
		put(urtr, 1)
		put(0, 2) // r1 r0
	} else {
		put(uid, 11)
		put(urtr, 1)
		put(0, 2) // IDE r0
	}
	put(uint64(len(data)), 4)
	if rtr == 0 {
		for _, b := range data {
			put(uint64(b), 8)
		}
	}
	put(uint64(crc15(f)), 15)
	// bit stuffing from SOF to the end of the CRC sequence
	var s []byte
	run, last := 0, byte(2)
	for _, b := range f {
		s = append(s, b)
		if b == last {
			run++
		} else {
			run, last = 1, b
		}
		if run == 5 {
			s = append(s, 1-b)
			run, last = 1, 1-b
		}
	}
	s = append(s, 1, 0, 1)             // CRC delimiter, ACK slot (a receiver ACKs), ACK delimiter
	s = append(s, 1, 1, 1, 1, 1, 1, 1) // EOF
	s = append(s, 1, 1, 1)             // intermission
	tl := newTimeline(map[string]float64{"rx": 1})
	tl.adv(bt)
	for _, b := range s {
		tl.set("rx", float64(b))
		tl.adv(bt)
	}
	return tl
}

// linPID adds the two parity bits to a 6-bit frame id (LIN 2.x).
// TRLC-LINKS: REQ-DSO-298
func linPID(id int) byte {
	b := func(k int) int { return id >> uint(k) & 1 }
	p0 := b(0) ^ b(1) ^ b(2) ^ b(4)
	p1 := 1 ^ (b(1) ^ b(3) ^ b(4) ^ b(5))
	return byte(id&0x3f | p0<<6 | p1<<7) //nolint:gosec // G115: 8 bits by construction
}

// linChecksum is the inverted 8-bit sum with carry (enhanced: PID included).
// TRLC-LINKS: REQ-DSO-298
func linChecksum(pid byte, data []byte, enhanced bool) byte {
	s := 0
	if enhanced {
		s = int(pid)
	}
	for _, b := range data {
		s += int(b)
		if s > 0xff {
			s -= 0xff
		}
	}
	return ^byte(s)
}

// TRLC-LINKS: REQ-DSO-298
func linBus(o *lineOpts, bt float64) *timeline {
	id := o.int("id", 0x10, 0, 0x3f)
	data := o.data("data", "0102")
	cs := strings.ToLower(o.str("checksum", "enh"))
	if cs != "enh" && cs != "cls" {
		o.fail(fmt.Errorf("checksum=%q: want enh|cls", cs))
	}
	if len(data) > 8 {
		o.fail(fmt.Errorf("data: %d bytes, LIN carries at most 8", len(data)))
	}
	pid := linPID(id)
	tl := newTimeline(map[string]float64{"tx": 1})
	tl.adv(bt)
	tl.set("tx", 0) // break: 13 dominant bits, then a 1-bit delimiter
	tl.adv(13 * bt)
	tl.set("tx", 1)
	tl.adv(bt)
	uartByte(tl, "tx", 0x55, 8, "n", 1, bt, false)
	uartByte(tl, "tx", uint(pid), 8, "n", 1, bt, false)
	for _, b := range data {
		uartByte(tl, "tx", uint(b), 8, "n", 1, bt, false)
	}
	uartByte(tl, "tx", uint(linChecksum(pid, data, cs == "enh")), 8, "n", 1, bt, false)
	return tl
}

// TRLC-LINKS: REQ-DSO-298
func manchesterBus(o *lineOpts, bt float64) *timeline {
	data := o.data("data", "a5f0")
	lsb := o.int("lsb", 0, 0, 1) == 1
	tl := newTimeline(map[string]float64{"tx": 0})
	tl.adv(2 * bt)
	for _, b := range data {
		for i := range 8 {
			k := 7 - i
			if lsb {
				k = i
			}
			v := float64(b >> uint(k) & 1)
			tl.set("tx", 1-v) // first half is the inverse; the mid-bit edge goes to the bit
			tl.adv(bt / 2)
			tl.set("tx", v)
			tl.adv(bt / 2)
		}
	}
	tl.set("tx", 0)
	return tl
}

// prbsBus is a maximal-length pseudo-random bit sequence (PRBS7 by default:
// x^7 + x^6 + 1) as NRZ data with its bit clock beside it (the clock rises
// in the middle of each bit, where a receiver samples). One period is the
// whole sequence; idle defaults to none, so the sequence repeats seamlessly.
// TRLC-LINKS: REQ-DSO-298
func prbsBus(o *lineOpts, bt float64) *timeline {
	order := o.int("order", 7, 3, 15)
	// x^n + x^k + 1 for each order n: maximal-length taps
	taps := map[int]int{3: 2, 4: 3, 5: 3, 6: 5, 7: 6, 9: 5, 10: 7, 11: 9, 15: 14}
	tap, ok := taps[order]
	if !ok {
		o.fail(fmt.Errorf("order=%d: want 3, 4, 5, 6, 7, 9, 10, 11 or 15", order))
		tap = order - 1
	}
	tl := newTimeline(map[string]float64{"data": 0, "clk": 0})
	mask := uint32(1)<<order - 1
	state := mask
	for range mask {
		bit := (state>>(order-1) ^ state>>(tap-1)) & 1
		state = (state<<1 | bit) & mask
		tl.set("data", float64(bit))
		tl.set("clk", 0)
		tl.adv(bt / 2)
		tl.set("clk", 1)
		tl.adv(bt / 2)
	}
	return tl
}
