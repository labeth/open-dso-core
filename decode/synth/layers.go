// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import (
	"fmt"
	"slices"
)

// Application layers on the buses: a chip's or a protocol's transactions
// carried by SPI, I2C, a UART, 1-Wire, MDIO or USB (the analysis package's
// layers). Each layer here is one whose transactions prove it (a detection
// names it from them); MakeLayer writes a session of them on its bus, and
// the bus generators (SPIPackets, I2CBus, UARTChar, OneWireByte, MDIOFrame,
// USBWave) carry any other layer's transactions.

// LayerSpec is a layer's session: its bus and what it carries.
type LayerSpec struct {
	ID    string // the layer (analysis.Layer's ID)
	Title string
	Bus   string // the bus protocol it rides on (a decoder's name)
	make  func(sr float64) Bus
}

// layers are the layers a detection names, in the analysis package's order.
var layers = []LayerSpec{
	{ID: "spiflash", Title: "SPI flash session", Bus: "spi", make: func(sr float64) Bus {
		return spiLayer(sr, [][]int{
			{0x9f, 0, 0, 0},
			{0x06},
			{0x02, 0, 1, 0, 1, 2, 3, 4},
			{0x05, 0},
			{0x03, 0, 0x10, 0, 0, 0, 0, 0},
			{0x0b, 1, 2, 3, 0, 9, 9},
			{0x20, 0, 0x10, 0},
			{0xd8, 1, 0, 0},
			{0xab},
			{0x90, 0, 0, 0, 0xef, 0x13},
			{0xc7},
		})
	}},
	{ID: "sdcard", Title: "SD card init over SPI", Bus: "spi", make: func(sr float64) Bus {
		b := spiLayer(sr, [][]int{
			{0xff, 0x40, 0, 0, 0, 0, 0x95, 0xff, 0xff},
			{0x48, 0, 0, 1, 0xaa, 0x87},
			{0x77, 0, 0, 0, 0, 0x65},
			{0x69, 0x40, 0, 0, 0, 0x77},
			{0x51, 0, 0, 0x10, 0, 0x01},
			{0x58, 0, 0, 0, 8, 0x12},
			{0xfe, 1, 2, 3, 4},
		})
		// an SD card's commands in SPI mode are the SD bus's command tokens
		// bit for bit (start bits, index, argument, CRC-7, end bit): the
		// clock and MOSI alone also read as the SD bus's CLK and CMD
		b.Truth.Alike = []string{"sdcard"}
		return b
	}},
	{ID: "avrisp", Title: "AVR ISP programming", Bus: "spi", make: func(sr float64) Bus {
		return spiLayer(sr, [][]int{
			{0xac, 0x53, 0, 0},
			{0x30, 0, 0, 0, 0x30, 0, 1, 0, 0x30, 0, 2, 0},
			{0xac, 0x80, 0, 0},
			{0x50, 0, 0, 0},
			{0x58, 8, 0, 0},
			{0xa0, 1, 0x23, 0},
			{0x28, 0x01, 0x23, 0},
			{0x40, 0, 5, 0xab},
			{0x4c, 1, 2, 0},
		})
	}},
	{ID: "edid", Title: "EDID read in two halves", Bus: "i2c", make: func(sr float64) Bus {
		e := EDIDBlock()
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x50, Data: []int{0}},
			{Addr: 0x50, Read: true, Data: e[:64]},
			{Addr: 0x50, Data: []int{64}},
			{Addr: 0x50, Read: true, Data: e[64:]},
		})
	}},
	{ID: "eeprom24xx", Title: "24xx EEPROM, 2-byte addresses", Bus: "i2c", make: func(sr float64) Bus {
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x57, Read: true, Data: []int{1}},
			{Addr: 0x57, Data: []int{0x01, 0x00}},
			{Addr: 0x57, Read: true, Data: []int{1, 2}},
			{Addr: 0x57, Data: []int{0x02, 0x00, 7, 8}},
		})
	}},
	{ID: "ds1307", Title: "DS1307 RTC", Bus: "i2c", make: func(sr float64) Bus {
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x68, Data: []int{0}},
			{Addr: 0x68, Read: true, Data: []int{0x56, 0x34, 0x12, 0x07, 0x18, 0x10, 0x26}},
			{Addr: 0x68, Data: []int{0, 0x00, 0x45, 0x71, 0x02, 0x31, 0x12, 0x99}},
			{Addr: 0x68, Data: []int{7, 0x10}},
		})
	}},
	{ID: "rtc8564", Title: "RTC-8564", Bus: "i2c", make: func(sr float64) Bus {
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x51, Data: []int{2}},
			{Addr: 0x51, Read: true, Data: []int{0x56, 0x34, 0x12, 0x18, 0x00, 0x10, 0x26}},
			{Addr: 0x51, Data: []int{0, 0, 0}},
			{Addr: 0x51, Read: true, Data: []int{0x12, 0x45}},
		})
	}},
	{ID: "nunchuk", Title: "Wii Nunchuk", Bus: "i2c", make: func(sr float64) Bus {
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x52, Data: []int{0x40, 0x00}},
			{Addr: 0x52, Data: []int{0x00}},
			{Addr: 0x52, Read: true, Data: []int{0x80, 0x7f, 0x80, 0x81, 0xb0, 0x5d}},
			{Addr: 0x52, Data: []int{0xf0, 0x55}},
			{Addr: 0x52, Data: []int{0xfb, 0x00}},
		})
	}},
	{ID: "hdcp", Title: "HDCP authentication", Bus: "i2c", make: func(sr float64) Bus {
		return i2cLayer(sr, []I2CTransfer{
			{Addr: 0x3a, Data: []int{0x00}},
			{Addr: 0x3a, Read: true, Data: []int{1, 2, 3, 4, 5}},
			{Addr: 0x3a, Data: []int{0x41}},
			{Addr: 0x3a, Read: true, Data: []int{0x02, 0x10}},
			{Addr: 0x3a, Data: []int{0x50}},
			{Addr: 0x3a, Read: true, Data: []int{4}},
			{Addr: 0x3a, Data: []int{0x70}},
			{Addr: 0x3a, Read: true, Data: []int{0x10, 0x05}},
		})
	}},
	{ID: "xfp", Title: "XFP module identification", Bus: "i2c", make: func(sr float64) Bus {
		x := XFPPage()
		return i2cLayer(sr, []I2CTransfer{{Addr: 0x50, Data: []int{0}}, {Addr: 0x50, Read: true, Data: x}})
	}},
	{ID: "sbus", Title: "S.BUS servo frames", Bus: protoUART, make: func(sr float64) Bus {
		var ch [16]int
		for k := range ch {
			ch[k] = 172 + 100*k
		}
		w := NewLine(sr)
		w.Idle(1e-3)
		for f := range 4 { // a frame every 7 ms
			t0 := w.T
			for _, v := range SBUSFrame(ch, []int{0x0c, 0x03, 0, 0x0c}[f]) {
				UARTChar(w, v, 100000, "even", 2)
			}
			w.Idle(7e-3 - (w.T - t0))
		}
		w.Invert()
		return Bus{Proto: protoUART, Name: "S.BUS 100000 8E2 inverted", Roles: []string{"tx"}, Lines: []*Line{w}, NaturalSR: 2e6}
	}},
	{ID: "arm_itm", Title: "ARM ITM over SWO", Bus: protoUART, make: func(sr float64) Bus {
		return uartLayer(sr, 1e6, [][]int{
			{0, 0, 0, 0, 0, 0x80, 0x01, 'h', 0x01, 'i', 0x01, '\n', 0x0b, 1, 2, 3, 4},
			{0x09, 'a', 0x02, 0x34, 0x12, 0x0e, 0x0f, 0x10, 0x17, 0x78, 0x56, 0x34, 0x12, 0x05, 0x21},
		})
	}},
	{ID: "arm_tpiu", Title: "ARM TPIU frames over SWO", Bus: protoUART, make: func(sr float64) Bus {
		tp := TPIUFrame([]TPIUItem{{ID: 1}, {B: 0x01}, {B: 'O'}, {B: 0x01}, {B: 'K'}, {B: 0x02}, {B: 0x34}, {B: 0x12}, {ID: 2}, {B: 0x08}, {B: 0x09}})
		return uartLayer(sr, 1e6, [][]int{append([]int{0xff, 0xff, 0xff, 0x7f}, tp...), append(slices.Clone(tp), tp...)})
	}},
	{ID: "pan1321", Title: "PAN1321 Bluetooth AT commands", Bus: protoUART, make: func(sr float64) Bus {
		s := "AT+JSLN=05,hello\r\nAT+JSEC=1,1,2,04,1234\r\nROK\r\nOK\r\nAT+JAAC=1\r\nAT+JSDA=03,abc\r\n"
		b := make([]int, len(s))
		for i := range s {
			b[i] = int(s[i])
		}
		return uartLayer(sr, 115200, [][]int{b})
	}},
	{ID: "ds28ea00", Title: "1-Wire temperature sensors", Bus: "onewire", make: func(sr float64) Bus {
		rom := OneWireROM(0x28, 0x2c, 0x1b, 0x6a, 0x05, 0x00, 0x00)
		sp := []int{0x91, 0x01, 0x4b, 0x46, 0x7f, 0xff, 0x0f, 0x10}
		sp = append(sp, CRC8Maxim(sp))
		return oneWireLayer(sr, [][]int{
			append(append([]int{0x55}, rom...), 0x44), append([]int{0xcc, 0xbe}, sp...),
			{0xcc, 0x4e, 0x4b, 0x46, 0x7f},
			{0xcc, 0x48},
		})
	}},
	{ID: "onewire_net", Title: "1-Wire ROM commands", Bus: "onewire", make: func(sr float64) Bus {
		return oneWireLayer(sr, [][]int{append([]int{0x33}, OneWireROM(0x01, 1, 2, 3, 4, 5, 6)...), {0xcc, 0xf0, 0x88, 0x00, 0xff, 0x00}, {0xcc, 0xf5, 0xff, 0xfe}})
	}},
	{ID: "cfp", Title: "CFP module over MDIO (clause 45)", Bus: protoMDIO, make: func(sr float64) Bus {
		per := 400e-9
		l := NewPair(sr)
		l.Add(1, 1, 2e-6)
		for _, f := range [][4]int{{0, 0, 1, 0x8000}, {3, 0, 1, 0x000e}, {0, 0, 1, 0x8010}, {2, 0, 1, 0x1234}, {3, 0, 1, 0x5678}, {1, 0, 1, 0xa000}} {
			MDIOFrame(l, per, 32, 0, f[0], f[1], f[2], f[3], 0)
		}
		return Bus{Proto: protoMDIO, Name: "CFP over MDIO", Roles: []string{"mdc", protoMDIO}, Lines: []*Line{l.A, l.B}, NaturalSR: 25e6}
	}},
	{ID: "usb_request", Title: "USB enumeration", Bus: "usbls", make: func(sr float64) Bus {
		setup := []int{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00}
		desc := []int{18, 1, 0x00, 0x02, 0, 0, 0, 8, 0x34, 0x12, 0x78, 0x56, 0, 1, 1, 2, 3, 1}
		p := NewPair(sr)
		const sof, setupPID, data0, data1, ack, in, out = 0x5, 0xd, 0x3, 0xb, 0x2, 0x9, 0x1
		USBWave(p, USBLowSpeed, 0, 0, [][]int{
			USBPacketBits(setupPID, USBTokenBits(0, 0)), USBPacketBits(data0, USBDataBits(setup, 0)), USBPacketBits(ack, nil),
			USBPacketBits(in, USBTokenBits(0, 0)), USBPacketBits(data1, USBDataBits(desc[:8], 0)), USBPacketBits(ack, nil),
			USBPacketBits(in, USBTokenBits(0, 0)), USBPacketBits(data0, USBDataBits(desc[8:16], 0)), USBPacketBits(ack, nil),
			USBPacketBits(out, USBTokenBits(0, 0)), USBPacketBits(data1, USBDataBits(nil, 0)), USBPacketBits(ack, nil),
		}, 40)
		_ = sof
		return Bus{Proto: "usbls", Name: "USB enumeration (low speed)", Roles: []string{"j", "k"}, Lines: []*Line{p.B, p.A}, NaturalSR: 30e6}
	}},
}

// Layers lists the layers MakeLayer writes.
// TRLC-LINKS: REQ-DSO-241
func Layers() []LayerSpec { return slices.Clone(layers) }

// MakeLayer writes layer id's session on its bus at sr samples a second
// (0: steps only); the bus's Truth names the layer.
// TRLC-LINKS: REQ-DSO-241
func MakeLayer(id string, sr float64) (Bus, error) {
	for _, l := range layers {
		if l.ID != id {
			continue
		}
		b := l.make(sr)
		b.SR = sr
		b.Truth.Layer = id
		for _, w := range b.Lines {
			b.Period = max(b.Period, w.T)
		}
		return b, nil
	}
	return Bus{}, fmt.Errorf("synth: no layer %q", id)
}

// spiLayer is an SPI bus (mode 0, 1 MHz) carrying packets, each a chip
// select's transaction: a pause of three byte times between them.
// TRLC-LINKS: REQ-DSO-018
func spiLayer(sr float64, packets [][]int) Bus {
	l := SPIPackets(SPIShape{Hz: 1e6, Duty: 0.5}, sr, packets, 24)
	return Bus{Proto: "spi", Name: "SPI 1 MHz mode 0", Roles: []string{"clk", roleData}, Lines: []*Line{l.A, l.B}, NaturalSR: 40e6}
}

// i2cLayer is an I2C bus at 100 kHz carrying transfers.
// TRLC-LINKS: REQ-DSO-018
func i2cLayer(sr float64, ts []I2CTransfer) Bus {
	l := I2CBus(100e3, sr, ts, nil)
	return Bus{Proto: "i2c", Name: "I2C 100 kHz", Roles: []string{"scl", "sda"}, Lines: []*Line{l.A, l.B}, NaturalSR: 5e6}
}

// uartLayer is a UART (8N1) carrying bursts of bytes, a pause of 20 bit
// times between them.
// TRLC-LINKS: REQ-DSO-018
func uartLayer(sr, baud float64, bursts [][]int) Bus {
	w := NewLine(sr)
	w.Idle(30 / baud)
	for _, b := range bursts {
		for _, v := range b {
			UARTChar(w, v, baud, "none", 1)
		}
		w.Idle(20 / baud)
	}
	w.Idle(30 / baud)
	return Bus{Proto: protoUART, Name: fmt.Sprintf("UART %g 8N1", baud), Roles: []string{"tx"}, Lines: []*Line{w}, NaturalSR: 32 * baud}
}

// oneWireLayer is a 1-Wire bus carrying transactions, each after its reset.
// TRLC-LINKS: REQ-DSO-018
func oneWireLayer(sr float64, txns [][]int) Bus {
	w := NewLine(sr)
	w.Idle(100e-6)
	for _, t := range txns {
		OneWireReset(w, false, true)
		for _, v := range t {
			OneWireByte(w, v, false)
		}
		w.Idle(200e-6)
	}
	return Bus{Proto: "onewire", Name: "1-Wire", Roles: []string{"dq"}, Lines: []*Line{w}, NaturalSR: 2e6}
}

// SPIPackets is an SPI bus of shape s carrying packets, gapBits clock
// periods of idle between them (a chip select's release on a bus probed
// without it), as a Pair (A the clock, B the data).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-242
func SPIPackets(s SPIShape, sr float64, packets [][]int, gapBits float64) Pair {
	l := NewPair(sr)
	idle := byte(0)
	if s.CPOL {
		idle = 1
	}
	act := 1 - idle
	T := 1 / s.Hz
	l.Add(idle, 0, 20*T)
	d := byte(0)
	for _, p := range packets {
		for _, v := range p {
			for i := 7; i >= 0; i-- {
				bit := byte(v >> i & 1)
				on := T * s.Duty
				if !s.CPHA {
					d = bit
					l.Add(idle, d, T-on)
					l.Add(act, d, on)
				} else {
					l.Add(idle, d, T-on)
					d = bit
					l.Add(act, d, on)
				}
			}
		}
		l.Add(idle, d, gapBits*T)
	}
	l.Add(idle, d, 20*T)
	return l
}

// Invert flips a logic line's levels (a UART idling low: S.BUS).
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Invert() {
	for i := range w.Bits {
		w.Bits[i] ^= 1
	}
	for i := range w.Steps {
		w.Steps[i].Level ^= 1
	}
}

// EDIDBlock is a valid 128-byte EDID base block of a 1920x1080 display
// named TESTMON.
// TRLC-LINKS: REQ-DSO-243
func EDIDBlock() []int {
	m := make([]int, 128)
	copy(m, []int{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
	m[8], m[9] = 0x10, 0xac
	m[10], m[11] = 0x34, 0x12
	m[18], m[19] = 1, 3
	pc := 14850 // 148.5 MHz
	m[54], m[55] = pc&0xff, pc>>8
	m[56], m[57], m[58] = 1920&0xff, 280&0xff, (1920>>8)<<4|(280>>8)
	m[59], m[60], m[61] = 1080&0xff, 45, (1080>>8)<<4
	d := 72
	m[d+3] = 0xfc
	for i, c := range "TESTMON\n     " {
		m[d+5+i] = int(c)
	}
	sum := 0
	for _, v := range m[:127] {
		sum += v
	}
	m[127] = (256 - sum%256) % 256
	return m
}

// XFPPage is an XFP module's lower memory and serial ID page (identifier 6).
// TRLC-LINKS: REQ-DSO-243
func XFPPage() []int {
	x := make([]int, 256)
	x[0], x[127], x[128] = 6, 1, 6
	x[96], x[97] = 0x23, 0x40
	x[100], x[101] = 0x0b, 0xb8
	x[102], x[103] = 0x13, 0x88
	x[104], x[105] = 0x0f, 0xa0
	put := func(at int, s string) {
		for i := range len(s) {
			x[at+i] = int(s[i])
		}
	}
	put(148, "ACME OPTICS     ")
	put(168, "XFP-10G-SR      ")
	x[186], x[187] = 0x66, 0x58
	put(196, "SN0001          ")
	return x
}

// SBUSFrame is an S.BUS frame: 0x0F, sixteen 11-bit channels, the flags and
// the end byte 0.
// TRLC-LINKS: REQ-DSO-244
func SBUSFrame(ch [16]int, flags int) []int {
	b := make([]int, 25)
	b[0] = 0x0f
	bit := 0
	for _, v := range ch {
		for j := range 11 {
			if v>>j&1 != 0 {
				b[1+bit>>3] |= 1 << (bit & 7)
			}
			bit++
		}
	}
	b[23] = flags
	return b
}

// TPIUItem is a stream change (ID > 0) or a data byte for TPIUFrame.
type TPIUItem struct {
	ID int
	B  int
}

// TPIUFrame packs stream changes and data into 16-byte TPIU frames: an ID
// takes an even byte (its low bit 1), data fills the rest, the auxiliary
// byte last.
// TRLC-LINKS: REQ-DSO-244
func TPIUFrame(items []TPIUItem) []int {
	var out []int
	for len(items) > 0 {
		f := make([]int, 16)
		aux := 0
		pos := 0
		for pos < 15 && len(items) > 0 {
			it := items[0]
			if pos%2 == 0 {
				if it.ID > 0 {
					f[pos] = it.ID<<1 | 1
				} else {
					f[pos] = it.B &^ 1
					aux |= (it.B & 1) << (pos / 2)
				}
			} else {
				if it.ID > 0 {
					break
				}
				f[pos] = it.B
			}
			items = items[1:]
			pos++
		}
		for ; pos < 15; pos++ {
			if pos%2 == 0 {
				f[pos] = 0x7f<<1 | 1
			}
		}
		f[15] = aux
		out = append(out, f...)
	}
	return out
}
