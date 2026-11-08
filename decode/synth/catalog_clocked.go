// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import (
	"fmt"
	"strings"
)

// packMSB packs bits into bytes, most significant first.
func packMSB(bits []int) []int {
	var out []int
	for i, b := range bits {
		if i%8 == 0 {
			out = append(out, 0)
		}
		out[len(out)-1] |= b << (7 - i%8)
	}
	return out
}

// The two-line buses' generators: the clocked buses beyond SPI and I2C,
// Wiegand's two data lines, the motion signals.

func init() {
	protocols = append(protocols,
		Protocol{Proto: "ps2", Title: "PS/2", Roles: []string{"clk", roleData}, Payload: payBytes, Defaults: map[string]string{}, make: makePS2},
		Protocol{
			Proto: "swd", Title: "SWD", Roles: []string{"swclk", "swdio"}, Payload: "read data word", Defaults: map[string]string{setRate: "1M"},
			Variants: []map[string]string{{setRate: "1M"}, {setRate: "4M"}}, make: makeSWD,
		},
		Protocol{
			Proto: protoMDIO, Title: "MDIO", Roles: []string{"mdc", protoMDIO}, Payload: "register values", Defaults: map[string]string{setRate: "2.5M", "phy": "1"},
			Variants: []map[string]string{{setRate: "2.5M"}, {setRate: "1M"}}, make: makeMDIO,
		},
		Protocol{
			Proto: "wiegand", Title: "Wiegand", Roles: []string{"d0", "d1"}, Payload: "facility, card", Defaults: map[string]string{setBits: "26"},
			Variants: []map[string]string{{setBits: "26"}, {setBits: "34"}}, make: makeWiegand,
		},
		Protocol{
			Proto: "quad", Title: "Quadrature", Roles: []string{"a", "b"}, Payload: "steps (+1/-1)", Defaults: map[string]string{setPeriodUS: "100"},
			Variants: []map[string]string{{setPeriodUS: "100"}, {setPeriodUS: "20"}}, make: makeQuad,
		},
		Protocol{Proto: "stepper", Title: "Stepper", Roles: []string{"step", "dir"}, Payload: "directions", Defaults: map[string]string{setPeriodUS: "200"}, make: makeStepper},
		Protocol{Proto: "sdcard", Title: "SD card (CMD)", Roles: []string{"clk", "cmd"}, Payload: "command index, argument", Defaults: map[string]string{setRate: "250k"}, make: makeSDCard},
		Protocol{Proto: "pdi", Title: "AVR PDI", Roles: []string{"clk", roleData}, Payload: payBytes, Defaults: map[string]string{setRate: "250k"}, make: makePDI},
		Protocol{Proto: "cjtag", Title: "cJTAG", Roles: []string{"tckc", "tmsc"}, Payload: "IDCODE", Defaults: map[string]string{setRate: "250k"}, make: makeCJTAG},
		Protocol{Proto: "maple", Title: "Maple bus", Roles: []string{"sdcka", "sdckb"}, Payload: payBytes, Defaults: map[string]string{}, make: makeMaple},
		Protocol{Proto: "caliper", Title: "Caliper", Roles: []string{"clk", roleData}, Payload: "counts", Defaults: map[string]string{}, make: makeCaliper},
	)
}

func makePS2(c *ctx) error {
	d := c.data([]int{0x1C, 0xF0, 0x1C, 0x32, 0xF0, 0x32}, 8)
	l := c.pair()
	l.Add(1, 1, 300e-6)
	for _, v := range d {
		PS2Frame(l, v, false, false, 80e-6, 300e-6)
	}
	c.done("PS/2", 1e6, Truth{Bytes: d}, l.A, l.B)
	return nil
}

func makeSWD(c *ctx) error {
	per := 1 / c.num(setRate)
	d := c.data([]int{0x2BA01477}, 32)
	s := &SWDLine{P: c.pair(), Per: per}
	s.Host(0, 0)
	s.LineReset()
	s.Transfer(false, true, 0, 1, uint32(d[0]), false, 2) //nolint:gosec // G115: a 32-bit word
	s.Transfer(false, false, 8, 1, 0x000000F0, false, 2)
	s.Transfer(true, true, 0xC, 1, 0x12345678, false, 2)
	c.done("SWD", 20/per, Truth{Contains: []string{"LINERESET", fmt.Sprintf("RDP0 OK %08X", d[0]&0xffffffff), "WDP8 OK 000000F0", "RAPC OK 12345678"}}, s.P.A, s.P.B)
	return nil
}

func makeMDIO(c *ctx) error {
	per := 1 / c.num(setRate)
	phy := c.integer("phy") & 0x1f
	d := c.data([]int{0x796D, 0x1140, 0x0000}, 16)
	l := c.pair()
	l.Add(1, 1, 2e-6)
	ops := []struct{ op, reg int }{{2, 0x02}, {1, 0x00}, {2, 0x01}}
	var want []string
	for i, v := range d {
		o := ops[i%len(ops)]
		MDIOFrame(l, per, 32, 1, o.op, phy, o.reg, v, 0)
		want = append(want, fmt.Sprintf("%s %02X.%02X %04X", map[int]string{1: "WR", 2: "RD"}[o.op], phy, o.reg, v))
	}
	c.done("MDIO", 10/per, Truth{Contains: []string{strings.Join(want, " ")}}, l.A, l.B)
	return nil
}

func makeWiegand(c *ctx) error {
	nb := c.integer(setBits)
	if nb != 26 && nb != 34 {
		return fmt.Errorf("bits=%d: want 26 or 34", nb)
	}
	fc, card := pair2(c, []int{12, 4660}, 16)
	l := c.pair()
	l.Add(1, 1, 2e-3)
	WiegandFrame(l, WiegandBits(nb, fc, card), 1e-3)
	c.done(fmt.Sprintf("Wiegand %d", nb), 200e3, Truth{Bytes: []int{fc, card}}, l.A, l.B)
	return nil
}

func makeQuad(c *ctx) error {
	per := c.num(setPeriodUS) * 1e-6
	moves := c.data(func() []int {
		m := make([]int, 30)
		for i := range m {
			m[i] = 1
		}
		return m
	}(), 32)
	for i, m := range moves {
		if m != 1 {
			moves[i] = -1
		}
	}
	l := c.pair()
	l.Add(0, 0, 3*per)
	QuadMoves(l, moves, per, 10*per)
	c.done("Quadrature", 10/per, Truth{Bytes: append([]int{}, moves...)}, l.A, l.B)
	return nil
}

func makeStepper(c *ctx) error {
	per := c.num(setPeriodUS) * 1e-6
	dirs := c.data(func() []int {
		d := make([]int, 30)
		for i := range d {
			d[i] = i / 15
		}
		return d
	}(), 1)
	b := make([]byte, len(dirs))
	for i, v := range dirs {
		b[i] = lvl(v)
	}
	l := c.pair()
	l.Add(0, 1, 1.5*per)
	StepperMoves(l, b, per, 5*per)
	c.done("Stepper", 5/per, Truth{Bytes: append([]int{}, dirs...)}, l.A, l.B)
	return nil
}

func makeSDCard(c *ctx) error {
	per := 1 / c.num(setRate)
	p := c.data([]int{17, 0x1000, 13, 0x12340000}, 32)
	var bits, want []int
	bits = append(bits, SDIdle(16)...)
	for _, ia := range chunks(p, 2) {
		arg := 0
		if len(ia) > 1 {
			arg = ia[1]
		}
		cmd := SDToken(true, ia[0]&0x3f, uint32(arg), false)
		resp := SDToken(false, ia[0]&0x3f, 0x900, false)
		bits = append(append(append(append(bits, cmd...), SDIdle(4)...), resp...), SDIdle(16)...)
		want = append(append(want, packMSB(cmd)...), packMSB(resp)...)
	}
	l := c.pair()
	ClockBits(l, bits, per)
	c.done("SD card", 16/per, Truth{Bytes: want}, l.A, l.B)
	return nil
}

func makePDI(c *ctx) error {
	per := 1 / c.num(setRate)
	d := c.data([]int{0xC2, 0x59, 0x80, 0x07, 0x4C, 0xCA, 0x01, 0x00, 0x01, 0x55, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x22}, 8)
	bits := []int{1, 1, 1, 1, 1, 1, 1, 1}
	for _, v := range d {
		bits = append(bits, PDIChar(v, false)...)
	}
	bits = append(bits, 1, 1, 1, 1, 1, 1, 1, 1)
	l := c.pair()
	ClockBits(l, bits, per)
	c.done("PDI", 8/per, Truth{Bytes: d}, l.A, l.B)
	return nil
}

func makeCJTAG(c *ctx) error {
	per := 1 / c.num(setRate)
	d := c.data([]int{0x4BA00477}, 32)
	j := &CJTAGLine{P: c.pair(), Per: per}
	for range 4 {
		j.Rise(0)
	}
	j.Escape(6)
	for _, b := range []int{1, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 1} {
		j.Rise(b)
	}
	j.TCK(0, 0, 0, true)
	j.Scan(true, 4, 0x5, 0x1)
	j.Scan(false, 32, 0x12345678, uint32(d[0])) //nolint:gosec // G115: a 32-bit IDCODE
	c.done("cJTAG", 8/per, Truth{Contains: []string{fmt.Sprintf("DR12345678 TDO%X", d[0]&0xffffffff)}}, j.P.A, j.P.B)
	return nil
}

func makeMaple(c *ctx) error {
	d := c.data([]int{0x00, 0x00, 0x00, 0x01}, 8)
	m := &MapleLine{P: c.pair(), Step: 250e-9}
	m.Set(1, 1)
	req := MapleBytes(0x20, 0x00, 0x01, nil, false)
	m.Frame(req)
	for range 20 {
		m.Set(1, 1)
	}
	resp := MapleBytes(0x00, 0x20, 0x05, d, false)
	m.Frame(resp)
	m.Set(1, 1)
	c.done("Maple", 20e6, Truth{Bytes: append(append([]int{}, req...), resp...)}, m.P.A, m.P.B)
	return nil
}

func makeCaliper(c *ctx) error {
	d := c.data([]int{1234, 1235, 1236}, 16)
	l := c.pair()
	l.Add(0, 0, 20e-3)
	var want []int
	for _, v := range d {
		CaliperFrame(l, v, false, false, 80e-6, 30e-3)
		want = append(want, v&0xff, v>>8&0xff, 0)
	}
	c.done("Caliper", 200e3, Truth{Bytes: want}, l.A, l.B)
	return nil
}
