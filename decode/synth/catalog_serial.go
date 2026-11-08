// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import (
	"errors"
	"fmt"
	"strings"
)

// The serial buses' generators: UART and the protocols framed as UART
// characters, the clocked buses, the automotive and avionics buses, USB.

var errParity = errors.New("parity: want none, even or odd")

func init() {
	protocols = append(protocols,
		Protocol{
			Proto: protoUART, Title: "UART", Roles: []string{"tx"}, Payload: payBytes,
			Defaults: map[string]string{setBaud: "115200", setParity: "none", "stop": "1"},
			Variants: []map[string]string{{setBaud: "9600"}, {setBaud: "115200", setParity: parityEven}, {setBaud: "1M"}},
			make:     makeUART,
		},
		Protocol{
			Proto: "lin", Title: "LIN", Roles: []string{"tx"}, Payload: payBytes,
			Defaults: map[string]string{setBaud: "19200", "id": "0x12", "checksum": "enhanced"},
			Variants: []map[string]string{{setBaud: "19200"}, {setBaud: "9600"}, {setBaud: "10400", "checksum": "classic"}},
			make:     makeLIN,
		},
		Protocol{
			Proto: "dmx512", Title: "DMX512", Roles: []string{"tx"}, Payload: "slots",
			Defaults: map[string]string{"break_us": "100", "mab_us": "12", "start": "0"},
			make:     makeDMX,
		},
		Protocol{
			Proto: "midi", Title: "MIDI", Roles: []string{"tx"}, Payload: payBytes,
			Defaults: map[string]string{},
			make:     makeMIDI,
		},
		Protocol{
			Proto: "modbus", Title: "Modbus RTU", Roles: []string{"tx"}, Payload: payBytes,
			Defaults: map[string]string{setBaud: "19200", setParity: "even", "stop": "1"},
			Variants: []map[string]string{{setBaud: "19200", setParity: "even"}, {setBaud: "9600", setParity: "none", "stop": "2"}, {setBaud: "38400", setParity: "odd"}},
			make:     makeModbus,
		},
		Protocol{
			Proto: "i2c", Title: "I2C", Roles: []string{"scl", "sda"}, Payload: payBytes,
			Defaults: map[string]string{setRate: rate100k, "addr": "0x50", "read": "0"},
			Variants: []map[string]string{{setRate: rate100k}, {setRate: "400k"}, {setRate: "400k", "addr": "0x1d", "read": "1"}},
			make:     makeI2C,
		},
		Protocol{
			Proto: "spi", Title: "SPI", Roles: []string{"clk", roleData}, Payload: payBytes,
			Defaults: map[string]string{setRate: "1M", setMode: "0", "gap": "1"},
			Variants: []map[string]string{{setRate: "1M", setMode: "0"}, {setRate: "500k", setMode: "3"}, {setRate: "5M", setMode: "1", "gap": "2"}},
			make:     makeSPI,
		},
		Protocol{
			Proto: "canfd", Title: "CAN", Roles: []string{"rx"}, Payload: payBytes,
			Defaults: map[string]string{setBaud: "500k", "id": "0x123", "ext": "0", "fd": "0"},
			Variants: []map[string]string{{setBaud: "500k"}, {setBaud: "125k", "ext": "1", "id": "0x18DAF110"}, {setBaud: "1M", "fd": "1"}},
			make:     makeCAN,
		},
		Protocol{
			Proto: "usbls", Title: "USB", Roles: []string{"j", "k"}, Payload: payBytes,
			Defaults: map[string]string{"speed": "low", "addr": "0", "ep": "0", "gap": "40"},
			Variants: []map[string]string{{"speed": "low"}, {"speed": "full"}, {"speed": "full", "gap": "3"}},
			make:     makeUSB,
		},
		Protocol{
			Proto: "flexray", Title: "FlexRay", Roles: []string{"bus"}, Payload: payBytes,
			Defaults: map[string]string{setRate: "10M", "id": "17", "cycle": "5"},
			Variants: []map[string]string{{setRate: "10M"}, {setRate: "5M"}, {setRate: "2.5M"}},
			make:     makeFlexRay,
		},
		Protocol{
			Proto: "sent", Title: "SENT", Roles: []string{"tx"}, Payload: "nibbles",
			Defaults: map[string]string{"tick_us": "3", "status": "0", "pause": "0"},
			Variants: []map[string]string{{"tick_us": "3"}, {"tick_us": "10"}, {"tick_us": "3", "pause": "1"}},
			make:     makeSENT,
		},
		Protocol{
			Proto: "mil1553", Title: "MIL-STD-1553", Roles: []string{"bus"}, Payload: "words16",
			Defaults: map[string]string{},
			make:     makeMIL1553,
		},
		Protocol{
			Proto: "arinc429", Title: "ARINC 429", Roles: []string{"bus"}, Payload: "words: label, data19",
			Defaults: map[string]string{setRate: rate100k},
			Variants: []map[string]string{{setRate: rate100k}, {setRate: "12.5k"}},
			make:     makeARINC,
		},
		Protocol{
			Proto: "manchester", Title: "Manchester", Roles: []string{"tx"}, Payload: payBytes,
			Defaults: map[string]string{setRate: "1M", "ieee": "1"},
			Variants: []map[string]string{{setRate: "1M"}, {setRate: "2M"}, {setRate: rate100k}},
			make:     makeManchester,
		},
	)
}

// protocols is every generator, in registration order.
var protocols []Protocol

// uartFraming reads parity and stop bits.
func uartFraming(c *ctx) (parity string, stops int, err error) {
	parity = c.str(setParity)
	switch parity {
	case "none", "even", "odd":
	default:
		return "", 0, errParity
	}
	stops = c.integer("stop")
	if stops < 1 || stops > 2 {
		return "", 0, fmt.Errorf("stop=%d: want 1 or 2", stops)
	}
	return parity, stops, nil
}

func makeUART(c *ctx) error {
	baud := c.num(setBaud)
	parity, stops, err := uartFraming(c)
	if err != nil {
		return err
	}
	b := c.data([]int{'H', 'e', 'l', 'l', 'o', ',', ' ', 'w', 'o', 'r', 'l', 'd', '!'}, 8)
	w := c.line()
	w.Idle(30 / baud)
	for _, v := range b {
		UARTChar(w, v, baud, parity, stops)
	}
	w.Idle(30 / baud)
	c.done(fmt.Sprintf("UART %g 8%s%d", baud, strings.ToUpper(parity[:1]), stops), 32*baud, Truth{Bytes: b}, w)
	return nil
}

func makeLIN(c *ctx) error {
	baud := c.num(setBaud)
	id := c.integer("id") & 0x3f
	classic := c.str("checksum") == "classic"
	d := c.data([]int{0xA5, 0x5A, 0x01, 0x02}, 8)
	w := c.line()
	w.Idle(300e-6)
	LINFrame(w, baud, 13, id, d, classic)
	LINFrame(w, baud, 13, id, d, classic)
	pid := LINPID(id)
	frame := append(append([]int{pid}, d...), LINChecksum(pid, d, !classic && id != 0x3c && id != 0x3d))
	c.done(fmt.Sprintf("LIN %g id %#x", baud, id), 1e6, Truth{Bytes: append(append([]int{}, frame...), frame...), Alike: []string{protoUART}}, w)
	return nil
}

func makeDMX(c *ctx) error {
	slots := c.data(func() []int {
		s := make([]int, 120)
		for i := range s {
			s[i] = i * 3 & 0xff
		}
		return s
	}(), 8)
	start := c.integer("start")
	w := c.line()
	w.Idle(40e-6)
	DMXPacket(w, c.num("break_us"), c.num("mab_us"), start, slots)
	DMXPacket(w, c.num("break_us"), c.num("mab_us"), start, slots)
	one := append([]int{start}, slots...)
	c.done(fmt.Sprintf("DMX512 %d slots", len(slots)), 5e6, Truth{Bytes: append(append([]int{}, one...), one...), Alike: []string{protoUART}}, w)
	return nil
}

func makeMIDI(c *ctx) error {
	b := c.data([]int{0x90, 0x3C, 0x64, 0x40, 0x64, 0x43, 0x64, 0x80, 0x3C, 0x00, 0xB0, 0x07, 0x50, 0xF8, 0xC0, 0x05}, 8)
	w := c.line()
	w.Idle(200e-6)
	for _, v := range b {
		UARTChar(w, v, 31250, "none", 1)
	}
	w.Idle(1e-3)
	c.done("MIDI", 1e6, Truth{Bytes: b, Alike: []string{protoUART}}, w)
	return nil
}

func makeModbus(c *ctx) error {
	baud := c.num(setBaud)
	parity, stops, err := uartFraming(c)
	if err != nil {
		return err
	}
	b := c.data([]int{0x01, 0x03, 0x00, 0x6B, 0x00, 0x03}, 8)
	w := c.line()
	w.Idle(3e-3)
	ModbusFrame(w, baud, parity, stops, b)
	ModbusFrame(w, baud, parity, stops, b)
	crc := ModbusCRC(b)
	one := append(append([]int{}, b...), crc&0xff, crc>>8)
	c.done(fmt.Sprintf("Modbus %g 8%s%d", baud, strings.ToUpper(parity[:1]), stops), 2e6,
		Truth{Bytes: append(append([]int{}, one...), one...), Alike: []string{protoUART}}, w)
	return nil
}

func makeI2C(c *ctx) error {
	hz := c.num(setRate)
	addr := c.integer("addr") & 0x7f
	read := c.flag("read")
	d := c.data([]int{0x00, 0x10, 0xA5, 0x5A}, 8)
	ts := []I2CTransfer{{Addr: addr, Read: read, Data: d}, {Addr: addr, Read: read, Data: d}}
	l := I2CBus(hz, c.sr, ts, nil)
	rw := 0
	if read {
		rw = 1
	}
	c.done(fmt.Sprintf("I2C %g addr %#x", hz, addr), 50*hz, Truth{
		Bytes:    append(append([]int{}, d...), d...),
		Contains: []string{fmt.Sprintf("START %02X %s ACK", addr, map[int]string{0: "W", 1: "R"}[rw])},
	}, l.A, l.B)
	return nil
}

func makeSPI(c *ctx) error {
	hz := c.num(setRate)
	mode := c.integer(setMode)
	if mode < 0 || mode > 3 {
		return fmt.Errorf("mode=%d: want 0..3", mode)
	}
	d := c.data([]int{0x9F, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x10, 0x32, 0x54}, 8)
	i := 0
	s := SPIShape{Hz: hz, Duty: 0.5, ByteGap: c.num("gap"), CPOL: mode >= 2, CPHA: mode%2 == 1}
	l := SPIBus(s, c.sr, func() int { v := d[i%len(d)]; i++; return v }, 2*len(d), nil)
	c.done(fmt.Sprintf("SPI %g mode %d", hz, mode), 40*hz, Truth{Bytes: append(append([]int{}, d...), d...)}, l.A, l.B)
	return nil
}

func makeCAN(c *ctx) error {
	baud := c.num(setBaud)
	id := c.integer("id")
	ext := c.flag("ext")
	fd := c.flag("fd")
	d := c.data([]int{0xDE, 0xAD, 0xBE, 0xEF}, 8)
	if len(d) > 8 {
		return errors.New("a classic frame carries 8 bytes at most")
	}
	w := c.line()
	var wire []int
	name := fmt.Sprintf("CAN %g id %#x", baud, id)
	if fd {
		wire = CANFDFrame(id&0x7ff, len(d), d)
		name = fmt.Sprintf("CAN FD %g id %#x", baud, id&0x7ff)
	} else {
		if !ext {
			id &= 0x7ff
		}
		wire, _, _ = CANWire(CANFrame{ID: id, DLC: len(d), Data: d, Ext: ext})
	}
	WireBits(w, baud, 20, 20, wire, wire)
	c.done(name, 20*baud, Truth{Bytes: append(append([]int{}, d...), d...)}, w)
	return nil
}

func makeUSB(c *ctx) error {
	rate := USBLowSpeed
	if c.str("speed") == "full" {
		rate = USBFullSpeed
	}
	d := c.data([]int{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00}, 8)
	p := c.pair()
	const setup, data0 = 0xD, 0x3
	USBWave(p, rate, 0, 0, [][]int{
		USBPacketBits(setup, USBTokenBits(c.integer("addr"), c.integer("ep"))),
		USBPacketBits(data0, USBDataBits(d, 0)),
	}, c.num("gap")) // bit times of idle between packets: two at the least
	var hex strings.Builder
	hex.WriteString("DATA0")
	for _, v := range d {
		fmt.Fprintf(&hex, " %02X", v)
	}
	// the lines in the order the decoders read them: the one idling high
	// (J) first, D- at low speed, D+ at full speed
	j, k := p.B, p.A
	if rate > USBLowSpeed {
		j, k = p.A, p.B
	}
	c.done(fmt.Sprintf("USB %s speed", c.str("speed")), 20*rate, Truth{Contains: []string{"SETUP", hex.String()}}, j, k)
	return nil
}

func makeFlexRay(c *ctx) error {
	rate := c.num(setRate)
	d := c.data([]int{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}, 8)
	if len(d)%2 != 0 {
		d = append(d, 0)
		c.payload = d
	}
	fb := FlexRayFrame(c.integer("id"), c.integer("cycle"), d)
	w := c.line()
	FlexRayLine(w, rate, [][]int{fb, fb}, 5, 20)
	c.done(fmt.Sprintf("FlexRay %g", rate), 10*rate, Truth{Bytes: append(append([]int{}, fb...), fb...)}, w)
	return nil
}

func makeSENT(c *ctx) error {
	tick := c.num("tick_us") * 1e-6
	d := c.data([]int{0xA, 0x5, 0xF, 0x0, 0xC, 0x3}, 4)
	nib := SENTNibbles(c.integer("status"), d)
	pause := 0
	if c.flag("pause") {
		pause = 12 + 56 + 15*8 // a constant-length frame's pause
	}
	w := c.line()
	SENTFrames(w, tick, [][]int{nib, nib, nib}, pause)
	c.done(fmt.Sprintf("SENT %g us", tick*1e6), 15/tick, Truth{Bytes: append(append(append([]int{}, nib...), nib...), nib...)}, w)
	return nil
}

func makeMIL1553(c *ctx) error {
	words := c.data([]int{0x1234, 0xABCD, 0x0F0F}, 16)
	cmd := make([]bool, len(words))
	cmd[0] = true
	w := c.line()
	MIL1553Words(w, 1e-6, words, cmd)
	c.done("MIL-STD-1553", 40e6, Truth{Bytes: append([]int{}, words...), Alike: []string{"manchester"}}, w)
	return nil
}

func makeARINC(c *ctx) error {
	rate := c.num(setRate)
	// payload: pairs of label (octal-coded byte) and 19-bit data
	p := c.data([]int{0o312, 0x2ABCD, 0o107, 0x15A3F}, 19)
	var words [][]int
	var b []int
	for _, lw := range chunks(p, 2) {
		data := 0
		if len(lw) > 1 {
			data = lw[1]
		}
		bits := ARINCWord(lw[0]&0xff, 0, data, 3)
		words = append(words, bits)
		v := 0
		for i := range 32 {
			v |= bits[i] << i
		}
		b = append(b, v&0xff, v>>8&0xff, v>>16&0xff, v>>24&0xff)
	}
	w := c.line()
	ARINCWords(w, 1/rate, words, 8)
	c.done(fmt.Sprintf("ARINC 429 %g", rate), 40*rate, Truth{Bytes: b}, w)
	return nil
}

func makeManchester(c *ctx) error {
	rate := c.num(setRate)
	d := c.data([]int{0x4D, 0x31, 0x55, 0xAA, 0x0F}, 8)
	bits := make([]int, 0, 8*(len(d)+2))
	for range 16 {
		bits = append(bits, 1, 0)
	}
	for _, v := range d {
		bits = append(bits, BitsMSB(v, 8)...)
	}
	w := c.line()
	ManchesterBits(w, bits, 1/rate, c.flag("ieee"), 0, 10/rate)
	c.done(fmt.Sprintf("Manchester %g", rate), 20*rate, Truth{Bytes: append([]int{0xAA, 0xAA, 0xAA, 0xAA}, d...)}, w)
	return nil
}
