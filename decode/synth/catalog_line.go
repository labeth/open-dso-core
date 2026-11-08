// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import (
	"fmt"
	"strings"
)

// The single-wire line codes' generators: pulse-coded buses, IR remotes,
// biphase codes, radio and RFID codes.

func init() {
	protocols = append(protocols,
		Protocol{
			Proto: "onewire", Title: "1-Wire", Roles: []string{"dq"}, Payload: payBytes,
			Defaults: map[string]string{"overdrive": "0"},
			Variants: []map[string]string{{"overdrive": "0"}, {"overdrive": "1"}},
			make:     makeOneWire,
		},
		Protocol{
			Proto: "ws281x", Title: "WS281x", Roles: []string{"din"}, Payload: payBytes,
			Defaults: map[string]string{setRate: "800k"},
			Variants: []map[string]string{{setRate: "800k"}, {setRate: "400k"}}, make: makeWS281x,
		},
		Protocol{Proto: "dht", Title: "DHT11/22", Roles: []string{roleData}, Payload: "4 bytes", Defaults: map[string]string{}, make: makeDHT},
		Protocol{Proto: "cec", Title: "HDMI-CEC", Roles: []string{"cec"}, Payload: payBytes, Defaults: map[string]string{}, make: makeCEC},
		Protocol{Proto: "j1850", Title: "J1850 VPW", Roles: []string{"bus"}, Payload: payBytes, Defaults: map[string]string{}, make: makeJ1850},
		Protocol{
			Proto: "irnec", Title: "IR NEC", Roles: []string{"ir"}, Payload: "addr, cmd",
			Defaults: map[string]string{"emitter": "0"}, Variants: []map[string]string{{"emitter": "0"}, {"emitter": "1"}}, make: makeNEC,
		},
		Protocol{
			Proto: "irsirc", Title: "IR SIRC", Roles: []string{"ir"}, Payload: "cmd, addr",
			Defaults: map[string]string{setBits: "12"}, Variants: []map[string]string{{setBits: "12"}, {setBits: "15"}, {setBits: "20"}}, make: makeSIRC,
		},
		Protocol{Proto: "irrc5", Title: "IR RC-5", Roles: []string{"ir"}, Payload: "addr, cmd", Defaults: map[string]string{}, make: makeRC5},
		Protocol{Proto: "irrc6", Title: "IR RC-6", Roles: []string{"ir"}, Payload: "addr, cmd", Defaults: map[string]string{}, make: makeRC6},
		Protocol{Proto: "dali", Title: "DALI", Roles: []string{"bus"}, Payload: "frames", Defaults: map[string]string{}, make: makeDALI},
		Protocol{Proto: "usbpd", Title: "USB PD", Roles: []string{"cc"}, Payload: "header, objects", Defaults: map[string]string{}, make: makeUSBPD},
		Protocol{
			Proto: "spdif", Title: "S/PDIF", Roles: []string{roleData}, Payload: "samples",
			Defaults: map[string]string{"fs": "48000"}, Variants: []map[string]string{{"fs": "48000"}, {"fs": "44100"}}, make: makeSPDIF,
		},
		Protocol{
			Proto: "pwm", Title: "PWM", Roles: []string{"out"}, Payload: "duty %",
			Defaults: map[string]string{setPeriodUS: "100"}, Variants: []map[string]string{{setPeriodUS: "100"}, {setPeriodUS: "20"}, {setPeriodUS: "50"}}, make: makePWM,
		},
		Protocol{Proto: "rcencode", Title: "RC encoder", Roles: []string{roleData}, Payload: "trits", Defaults: map[string]string{"short_us": "350"}, make: makeRCEncode},
		Protocol{Proto: "ook", Title: "OOK (Oregon)", Roles: []string{roleData}, Payload: "nibbles", Defaults: map[string]string{}, make: makeOOK},
		Protocol{
			Proto: "miller", Title: "Miller", Roles: []string{roleData}, Payload: setBits,
			Defaults: map[string]string{"modified": "0"}, Variants: []map[string]string{{"modified": "0"}, {"modified": "1"}}, make: makeMiller,
		},
		Protocol{
			Proto: "morse", Title: "Morse", Roles: []string{"key"}, Payload: "text", Defaults: map[string]string{"unit_ms": "60"},
			Variants: []map[string]string{{"unit_ms": "60"}, {"unit_ms": "2"}}, make: makeMorse,
		},
		Protocol{
			Proto: "dcf77", Title: "DCF77", Roles: []string{roleData}, Payload: "minute, hour, day, dow, month, year (BCD)",
			Defaults: map[string]string{"second_ms": "1000"}, Variants: []map[string]string{{"second_ms": "3"}}, make: makeDCF77,
		},
		Protocol{Proto: "em4100", Title: "EM4100", Roles: []string{roleData}, Payload: "5 bytes", Defaults: map[string]string{}, make: makeEM4100},
		Protocol{
			Proto: "em4305", Title: "EM4305", Roles: []string{"field"}, Payload: "word",
			Defaults: map[string]string{setCarrier: "0"}, Variants: []map[string]string{{setCarrier: "0"}, {setCarrier: "1"}}, make: makeEM4305,
		},
		Protocol{
			Proto: "t55xx", Title: "T55xx", Roles: []string{"field"}, Payload: "word",
			Defaults: map[string]string{setCarrier: "0"}, Variants: []map[string]string{{setCarrier: "0"}, {setCarrier: "1"}}, make: makeT55xx,
		},
		Protocol{Proto: "swim", Title: "SWIM", Roles: []string{"swim"}, Payload: payBytes, Defaults: map[string]string{}, make: makeSWIM},
		Protocol{Proto: "sdq", Title: "SDQ", Roles: []string{"sdq"}, Payload: payBytes, Defaults: map[string]string{}, make: makeSDQ},
		Protocol{
			Proto: "pjdl", Title: "PJDL / PJON", Roles: []string{"bus"}, Payload: payBytes, Defaults: map[string]string{setMode: "1"},
			Variants: []map[string]string{{setMode: "1"}, {setMode: "4"}}, make: makePJDL,
		},
		Protocol{Proto: "dsi", Title: "DSI", Roles: []string{"bus"}, Payload: payBytes, Defaults: map[string]string{}, make: makeDSI},
		Protocol{Proto: "qi", Title: "Qi", Roles: []string{roleData}, Payload: payBytes, Defaults: map[string]string{}, make: makeQi},
		Protocol{
			Proto: "irmp", Title: "IR (multi)", Roles: []string{"ir"}, Payload: "addr, cmd",
			Defaults: map[string]string{"code": "samsung32"},
			Variants: []map[string]string{{"code": "samsung32"}, {"code": "kaseikyo"}, {"code": "jvc"}}, make: makeIRMP,
		},
	)
}

// pair2 is the first two payload values (address and command), defaults
// when missing.
func pair2(c *ctx, def []int, bits int) (int, int) {
	p := c.data(def, bits)
	a, b := def[0], def[1]
	if len(p) > 0 {
		a = p[0]
	}
	if len(p) > 1 {
		b = p[1]
	}
	return a, b
}

func makeOneWire(c *ctx) error {
	od := c.flag("overdrive")
	// a Skip ROM and Convert T, then a Read ROM answered by the payload (a
	// ROM id: its CRC is written after it)
	d := c.data([]int{0x28, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, 8)
	rom := append(append([]int{}, d...), CRC8Maxim(d))
	w := c.line()
	w.Idle(100e-6)
	OneWireReset(w, od, true)
	OneWireByte(w, 0xCC, od)
	OneWireByte(w, 0x44, od)
	w.Idle(300e-6)
	OneWireReset(w, od, true)
	OneWireByte(w, 0x33, od)
	for _, v := range rom {
		OneWireByte(w, v, od)
	}
	w.Idle(100e-6)
	var romText strings.Builder
	romText.WriteString("RDROM")
	for _, v := range rom {
		fmt.Fprintf(&romText, " %02X", v)
	}
	c.done("1-Wire", 2e6*map[bool]float64{true: 5, false: 1}[od], Truth{
		Bytes:    append([]int{0xCC, 0x44, 0x33}, rom...),
		Messages: []string{"SKIP 44", romText.String()}, // a transaction each: a reset, its ROM command and the bytes after
	}, w)
	return nil
}

func makeWS281x(c *ctx) error {
	rate := c.num(setRate)
	d := c.data([]int{0xFF, 0, 0, 0, 0xFF, 0, 0, 0, 0xFF, 0x10, 0x20, 0x30}, 8)
	w := c.line()
	w.Add(0, 60e-6)
	WS281xFrame(w, rate, d)
	WS281xFrame(w, rate, d)
	c.done(fmt.Sprintf("WS281x %g", rate), 50e6, Truth{Bytes: append(append([]int{}, d...), d...)}, w)
	return nil
}

func makeDHT(c *ctx) error {
	d := c.data([]int{0x02, 0x8C, 0x01, 0x5F}, 8)
	var b [4]int
	copy(b[:], d)
	w := c.line()
	w.Idle(1e-3)
	DHTReading(w, 1000, b, -1)
	c.done("DHT22", 1e6, Truth{Bytes: append(b[:], (b[0]+b[1]+b[2]+b[3])&0xff)}, w)
	return nil
}

func makeCEC(c *ctx) error {
	d := c.data([]int{0x40, 0x04}, 8)
	w := c.line()
	w.Idle(5e-3)
	acks := make([]int, len(d))
	CECMessage(w, d, acks)
	CECMessage(w, d, acks)
	c.done("HDMI-CEC", 100e3, Truth{Bytes: append(append([]int{}, d...), d...)}, w)
	return nil
}

func makeJ1850(c *ctx) error {
	d := c.data([]int{0x68, 0x6A, 0xF1, 0x01, 0x00}, 8)
	w := c.line()
	w.Add(0, 500e-6)
	J1850Frame(w, d, false)
	J1850Frame(w, d, false)
	one := append(append([]int{}, d...), J1850CRC(d))
	c.done("J1850 VPW", 1e6, Truth{Bytes: append(append([]int{}, one...), one...)}, w)
	return nil
}

// irBus writes an IR remote's frames on a line idling as a receiver's
// output (marks low) unless high.
func irBus(c *ctx, high bool, f func(l IRLine)) *Line {
	w := c.line()
	l := IRLine{W: w, ActiveHigh: high}
	l.Space(20e-3)
	f(l)
	return w
}

func makeNEC(c *ctx) error {
	addr, cmd := pair2(c, []int{0x04, 0x08}, 8)
	w := irBus(c, c.flag("emitter"), func(l IRLine) { NECFrame(l, addr, -1, cmd); NECFrame(l, addr, -1, cmd) })
	c.done("IR NEC", 100e3, Truth{Bytes: []int{addr, addr ^ 0xff, cmd, cmd ^ 0xff, addr, addr ^ 0xff, cmd, cmd ^ 0xff}}, w)
	return nil
}

func makeSIRC(c *ctx) error {
	nb := c.integer(setBits)
	if nb != 12 && nb != 15 && nb != 20 {
		return fmt.Errorf("bits=%d: want 12, 15 or 20", nb)
	}
	cmd, addr := pair2(c, []int{0x15, 0x01}, 8)
	w := irBus(c, false, func(l IRLine) { SIRCFrame(l, nb, cmd, addr, 0); SIRCFrame(l, nb, cmd, addr, 0) })
	one := []int{cmd & 0x7f, addr & 0x1f}
	switch nb {
	case 15:
		one[1] = addr & 0xff
	case 20:
		one = append(one, 0)
	}
	c.done(fmt.Sprintf("IR SIRC %d", nb), 100e3, Truth{Bytes: append(append([]int{}, one...), one...)}, w)
	return nil
}

func makeRC5(c *ctx) error {
	addr, cmd := pair2(c, []int{0x00, 0x0C}, 7)
	w := irBus(c, false, func(l IRLine) { RC5Frame(l, 0, addr&0x1f, cmd); RC5Frame(l, 0, addr&0x1f, cmd) })
	c.done("IR RC-5", 100e3, Truth{Bytes: []int{addr & 0x1f, cmd, addr & 0x1f, cmd}}, w)
	return nil
}

func makeRC6(c *ctx) error {
	addr, cmd := pair2(c, []int{0x00, 0x0C}, 8)
	w := irBus(c, false, func(l IRLine) { RC6Frame(l, 0, 0, []int{addr, cmd}); RC6Frame(l, 0, 0, []int{addr, cmd}) })
	c.done("IR RC-6", 100e3, Truth{Bytes: []int{addr, cmd, addr, cmd}}, w)
	return nil
}

func makeDALI(c *ctx) error {
	d := c.data([]int{0x01, 0x90, 0x5A}, 8)
	w := c.line()
	w.Idle(5e-3)
	// a forward frame of the first two bytes, a backward frame of the third
	fwd := d[0]<<8 | d[1%len(d)]
	DALIFrame(w, 16, fwd, 10e-3)
	if len(d) > 2 {
		DALIFrame(w, 8, d[2], 10e-3)
	}
	msgs := []string{fmt.Sprintf("A%02X D%02X", d[0], d[1%len(d)])}
	if len(d) > 2 {
		msgs = append(msgs, fmt.Sprintf("R%02X", d[2]))
	}
	c.done("DALI", 100e3, Truth{Bytes: d, Messages: msgs}, w)
	return nil
}

func makeUSBPD(c *ctx) error {
	p := c.data([]int{0x1161, 0x0801912C}, 32)
	header := p[0] & 0xffff
	var objs []uint32
	for _, o := range p[1:] {
		objs = append(objs, uint32(o)) //nolint:gosec // G115: a 32-bit data object
	}
	w := c.line()
	w.Add(0, 20e-6)
	PDMessage(w, PDSOP, header, objs, false)
	PDMessage(w, PDSOP, 0x0041, nil, false)
	msg := func(h int, objs []uint32) []int {
		b := []int{h & 0xff, h >> 8}
		for _, o := range objs {
			b = append(b, int(o&0xff), int(o>>8&0xff), int(o>>16&0xff), int(o>>24))
		}
		crc := CRC32IEEE(b)
		return append(b, int(crc&0xff), int(crc>>8&0xff), int(crc>>16&0xff), int(crc>>24))
	}
	c.done("USB PD", 25e6, Truth{Bytes: append(msg(header, objs), msg(0x0041, nil)...)}, w)
	return nil
}

func makeSPDIF(c *ctx) error {
	fs := c.num("fs")
	p := c.data(func() []int {
		var s []int
		for i := range 8 {
			s = append(s, i*0x11111&0xffffff, 0xABCDEF-i)
		}
		return s
	}(), 24)
	var s [][2]int
	for _, ab := range chunks(p, 2) {
		if len(ab) == 2 {
			s = append(s, [2]int{ab[0], ab[1]})
		}
	}
	w := c.line()
	w.Add(0, 50e-9)
	SPDIFStream(w, fs, 0, s)
	// every subframe but the last, which no preamble after it closes
	c.done(fmt.Sprintf("S/PDIF %g", fs), 500e6, Truth{Bytes: append([]int{}, p[:len(s)*2-1]...)}, w)
	return nil
}

func makePWM(c *ctx) error {
	per := c.num(setPeriodUS) * 1e-6
	p := c.data(func() []int {
		var d []int
		for i := range 24 {
			d = append(d, []int{30, 35, 40, 50, 55, 60, 70}[i%7])
		}
		return d
	}(), 7)
	duties := make([]float64, len(p))
	for i, v := range p {
		duties[i] = float64(v) / 100
	}
	// whole periods only, a source repeating the bus keeping one period as
	// an output running on does: the line opens on the last period's low
	// and ends on its high
	w := c.line()
	last := duties[len(duties)-1]
	w.Add(0, (1-last)*per)
	PWMWave(w, per, duties[:len(duties)-1])
	w.Add(1, last*per)
	var want strings.Builder
	for i, v := range p[:min(len(p), 7)] {
		if i > 0 {
			want.WriteByte(' ')
		}
		fmt.Fprintf(&want, "%.1f%%", float64(v))
	}
	c.done(fmt.Sprintf("PWM %g us", per*1e6), 80/per, Truth{Contains: []string{want.String()}}, w)
	c.bus.Continuous = true
	return nil
}

func makeRCEncode(c *ctx) error {
	a := c.num("short_us") * 1e-6
	t := c.data([]int{0, 1, 2, 0, 0, 1, 1, 2, 0, 1, 0, 0}, 2)
	w := c.line()
	w.Add(0, 40*a)
	RC2262Frame(w, t, a)
	RC2262Frame(w, t, a)
	c.done("RC encoder PT2262", 200e3, Truth{Bytes: append(append([]int{}, t...), t...)}, w)
	return nil
}

func makeOOK(c *ctx) error {
	n := c.data([]int{0xF, 0x8, 0x2, 0x4, 0x1, 0x3, 0xA, 0x0, 0x5, 0x3, 0x2, 0x0, 0x4, 0x5, 0x0}, 4)
	w := c.line()
	w.Add(0, 20e-3)
	for range 2 {
		OregonBurst(w, 3, n, 1/2048.0, false, 40e-3)
	}
	sum := 0
	for _, x := range n {
		sum += x
	}
	one := append(append([]int{}, n...), sum&15, sum>>4&15)
	c.done("Oregon OOK", 100e3, Truth{Bytes: append(append([]int{}, one...), one...), Alike: []string{"manchester"}}, w)
	return nil
}

func makeMiller(c *ctx) error {
	bits := c.data([]int{1, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 0, 1, 1, 0, 0}, 1)
	T := 1.0 / 106000
	pause := 0.0
	if c.flag("modified") {
		pause = 2e-6
	}
	w := c.line()
	w.Add(1, 40e-6)
	for range 3 {
		MillerMessage(w, bits, T, pause, 60e-6)
	}
	// the start 0 and the bits, packed least significant first
	all := append([]int{0}, bits...)
	var one []int
	for i, b := range all {
		if i%8 == 0 {
			one = append(one, 0)
		}
		one[len(one)-1] |= b << (i % 8)
	}
	c.done("Miller", 4e6, Truth{Bytes: append(append(append([]int{}, one...), one...), one...)}, w)
	return nil
}

func makeMorse(c *ctx) error {
	u := c.num("unit_ms") * 1e-3
	text := c.data([]int{'C', 'Q', ' ', 'D', 'E', ' ', 'S', 'M', '5', 'A', 'B', 'C'}, 8)
	var s strings.Builder
	for _, v := range text {
		fmt.Fprintf(&s, "%c", v)
	}
	w := c.line()
	w.Add(0, 8*u)
	MorseText(w, s.String(), u, 10)
	var letters []int
	for _, v := range text {
		if v != ' ' {
			letters = append(letters, v)
		}
	}
	c.done("Morse", 40/u, Truth{Bytes: letters}, w)
	return nil
}

func makeDCF77(c *ctx) error {
	sec := c.num("second_ms") * 1e-3
	f := c.data([]int{0x37, 0x12, 0x18, 0x7, 0x10, 0x26}, 8)
	for len(f) < 6 {
		f = append(f, 1)
	}
	w := c.line()
	w.Add(0, 0.3*sec)
	DCF77MinuteAt(w, sec, f[0], f[1], f[2], f[3], f[4], f[5], false)
	DCF77MinuteAt(w, sec, f[0]+1, f[1], f[2], f[3], f[4], f[5], false)
	// the second minute: the first minute marker starts it
	c.done("DCF77", 1000/sec*0.125*8, Truth{Bytes: []int{f[0] + 1, f[1], f[2], f[3], f[4], f[5]}}, w)
	return nil
}

func makeEM4100(c *ctx) error {
	d := c.data([]int{0x01, 0x23, 0x45, 0x67, 0x89}, 8)
	var b [5]int
	copy(b[:], d)
	w := c.line()
	w.Add(1, 768e-6)
	for range 3 {
		EM4100Frame(w, b, 256e-6, false)
	}
	c.done("EM4100", 500e3, Truth{Bytes: append(append(append([]int{}, b[:]...), b[:]...), b[:]...), Alike: []string{"manchester"}}, w)
	return nil
}

// rfidLine starts a reader's line: the field as its carrier or its
// envelope, and the rate the tests read each at.
func rfidLine(c *ctx) (RFIDField, float64) {
	carrier := c.flag(setCarrier)
	sr := 1e6
	if carrier {
		sr = 4e6
	}
	f := RFIDField{W: c.line(), Carrier: carrier}
	f.On(500 * RFIDFieldClock)
	return f, sr
}

func makeEM4305(c *ctx) error {
	d := c.data([]int{0xDE, 0xAD, 0xBE, 0xEF}, 8)
	var word [4]int
	copy(word[:], d)
	f, sr := rfidLine(c)
	login := [4]int{0x12, 0x34, 0x56, 0x78}
	EM4305Command(f, EM4305Bits(2, 5, word, false))
	EM4305Command(f, EM4305Bits(1, -1, login, false))
	c.done("EM4305", sr, Truth{Bytes: append(append(append([]int{2, 5}, word[:]...), 1), login[:]...)}, f.W)
	return nil
}

func makeT55xx(c *ctx) error {
	d := c.data([]int{0x00148040, 0xDEADBEEF}, 32)
	f, sr := rfidLine(c)
	var want []int
	for i, v := range d {
		T55xxCommand(f, T55xxBits(2, -1, 0, uint32(v), 1+i%7)) //nolint:gosec // G115: a 32-bit word
		want = append(want, 2, 0, v>>24&0xff, v>>16&0xff, v>>8&0xff, v&0xff, 1+i%7)
	}
	c.done("T55xx", sr, Truth{Bytes: want}, f.W)
	return nil
}

func makeSWIM(c *ctx) error {
	const T = 22 / 4e6
	d := c.data([]int{0x5A, 0xC3}, 8)
	w := c.line()
	w.Add(1, 20e-6)
	SWIMSync(w, T)
	for _, f := range [][3]int{{0, 3, 1}, {0, 8, 2}, {0, 8, 0}, {0, 8, 0x50}, {0, 8, 0x0A}} {
		SWIMFrame(w, f[0], f[1], f[2], T, 1, false, 3*T)
	}
	for _, v := range d {
		SWIMFrame(w, 1, 8, v, T, 1, false, 3*T)
	}
	var want []string
	for _, v := range d {
		want = append(want, fmt.Sprintf("R%02X", v))
	}
	c.done("SWIM", 20e6, Truth{Contains: []string{"SYNC", strings.Join(want, " ")}}, w)
	return nil
}

func makeSDQ(c *ctx) error {
	T := 1 / 98425.0
	d := c.data([]int{0x33, 0x01, 0xFE, 0x80, 0x55}, 8)
	w := c.line()
	w.Add(1, 30e-6)
	SDQBreak(w, T)
	for _, v := range d {
		SDQByte(w, v, T)
	}
	w.Add(1, 50e-6)
	c.done("SDQ", 10e6, Truth{Bytes: d}, w)
	return nil
}

func makePJDL(c *ctx) error {
	m := c.integer(setMode)
	if m < 1 || m > 4 {
		return fmt.Errorf("mode=%d: want 1..4", m)
	}
	d := c.data([]int{0x11, 0x22, 0x33}, 8)
	pk := PJONPacket(0x2A, 0x02, d, false)
	w := c.line()
	w.Add(0, 500e-6)
	PJDLFrame(w, m, pk, 1e-3)
	PJDLFrame(w, m, pk, 1e-3)
	c.done(fmt.Sprintf("PJDL mode %d", m), 2e6, Truth{Bytes: append(append([]int{}, pk...), pk...)}, w)
	return nil
}

func makeDSI(c *ctx) error {
	d := c.data([]int{0x00, 0xFF, 0x80, 0x55}, 8)
	w := c.line()
	w.Add(0, 10e-3)
	for _, v := range d {
		DSIFrame(w, v, 12e-3)
	}
	c.done("DSI", 100e3, Truth{Bytes: d}, w)
	return nil
}

func makeQi(c *ctx) error {
	d := c.data([]int{0x01, 0x80}, 8)
	w := c.line()
	w.Add(0, 5e-3)
	QiPacket(w, d, false, 8e-3)
	QiPacket(w, d, false, 8e-3)
	x := 0
	for _, v := range d {
		x ^= v
	}
	one := append(append([]int{}, d...), x)
	c.done("Qi", 200e3, Truth{Bytes: append(append([]int{}, one...), one...)}, w)
	return nil
}

func makeIRMP(c *ctx) error {
	code := c.str("code")
	var f func(l IRLine)
	switch code {
	case "samsung32":
		a, b := pair2(c, []int{0x0707, 0xFD02}, 16)
		f = func(l IRLine) { IRSamsung32(l, a, b); IRSamsung32(l, a, b) }
	case "kaseikyo":
		_, cmd := pair2(c, []int{0x2002, 0x123}, 16)
		f = func(l IRLine) { IRKaseikyo(l, 0x2002, 0x8, 0x0, cmd&0x3ff, 0x1) }
	case "jvc":
		a, b := pair2(c, []int{0x3, 0x5A1}, 12)
		f = func(l IRLine) { IRJVC(l, a&0xf, b, false); IRJVC(l, a&0xf, b, true) }
	case "denon":
		a, b := pair2(c, []int{0x0A, 0x2C4}, 10)
		f = func(l IRLine) { IRDenon(l, a&0x1f, b) }
	default:
		return fmt.Errorf("code=%q: want samsung32, kaseikyo, jvc or denon", code)
	}
	w := irBus(c, false, f)
	t := Truth{}
	p := c.payload
	switch code {
	case "samsung32", "jvc":
		t.Bytes = []int{p[0], p[1], p[0], p[1]}
		if code == "jvc" {
			t.Bytes[0], t.Bytes[2] = p[0]&0xf, p[0]&0xf
		}
	case "kaseikyo":
		t.Contains = []string{"KASEIKYO A2002"}
	case "denon":
		t.Contains = []string{fmt.Sprintf("A%X", p[0]&0x1f)}
	}
	c.done("IR "+code, 100e3, t, w)
	return nil
}
