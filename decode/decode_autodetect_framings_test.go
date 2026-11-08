// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import (
	"slices"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/decode/synth"
)

// synthBus is the bus s asks for at sr samples a second (0: its natural
// rate), with that rate.
func synthBus(t *testing.T, s synth.Spec, sr float64) (synth.Bus, float64) {
	t.Helper()
	b, err := synth.Make(s, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sr == 0 {
		sr = b.NaturalSR
	}
	if b, err = synth.Make(s, sr); err != nil {
		t.Fatal(err)
	}
	return b, sr
}

// repeated is a bus's lines repeated n times, as a signal source sends it.
func repeated(b synth.Bus, n int) (c1, c2 []uint8) {
	a := b.Lines[0].Codes()
	var o []uint8
	if len(b.Lines) > 1 {
		o = b.Lines[1].Codes()
	} else {
		o = idleLike(a)
	}
	for range n {
		c1, c2 = append(c1, a...), append(c2, o...)
	}
	return c1, c2
}

// A UART with a parity bit (8E1, 8O1) is named a UART with its parity,
// every character read: half its characters misframe as 8N1.
// TRLC-LINKS: REQ-DSO-018
func TestAutodetectUARTParity(t *testing.T) {
	for _, parity := range []string{"even", "odd"} {
		for _, baud := range []string{"9600", "115200", "1M"} {
			b, sr := synthBus(t, synth.Spec{Proto: "uart", Set: map[string]string{"parity": parity, "baud": baud}}, 0)
			c1, c2 := repeated(b, 1)
			r, _ := AutodetectFast(c1, c2, 1/sr, "hex")
			if r.Proto != protoUART || r.Parity != parity || !slices.Equal(r.Bytes, b.Truth.Bytes) {
				t.Errorf("%s: %s parity %q bytes % x, want uart %s % x", b.Name, r.Proto, r.Parity, r.Bytes, parity, b.Truth.Bytes)
			}
		}
	}
	// a plain 8N1 line keeps no parity
	b, sr := synthBus(t, synth.Spec{Proto: "uart"}, 0)
	c1, c2 := repeated(b, 1)
	if r, _ := AutodetectFast(c1, c2, 1/sr, "hex"); r.Proto != protoUART || r.Parity != "" {
		t.Errorf("8N1: %s parity %q", r.Proto, r.Parity)
	}
}

// USB packets two and three bit times apart (as a host sends a token and
// its data) are read apart at their EOPs, each whole with its CRC, on the
// line that idles at J; the other line hides the EOPs and is not the one
// named.
// TRLC-LINKS: REQ-DSO-018
func TestUSBPacketsCloseTogether(t *testing.T) {
	for _, speed := range []string{"low", "full"} {
		for _, gap := range []string{"2", "3", "6"} {
			b, sr := synthBus(t, synth.Spec{Proto: "usbls", Set: map[string]string{"speed": speed, "gap": gap}}, 0)
			j, k := b.Lines[0].Codes(), b.Lines[1].Codes()
			r := USBLS(j, 1/sr, USBLSCfg{})
			for _, w := range b.Truth.Contains {
				if !strings.Contains(r.Text, w) || strings.Contains(r.Text, "!") {
					t.Errorf("%s gap %s: %q lacks %q", b.Name, gap, r.Text, w)
				}
			}
			for _, order := range [][2][]uint8{{j, k}, {k, j}} {
				a, _ := AutodetectFast(order[0], order[1], 1/sr, "hex")
				want := 0
				if &order[0][0] != &j[0] {
					want = 1
				}
				if a.Proto != protoUSBLS || a.Roles.ChA != want || strings.Contains(a.Text, "!") {
					t.Errorf("%s gap %s: AUTO %s on C%d %q, want the J line C%d", b.Name, gap, a.Proto, a.Roles.ChA+1, a.Text, want+1)
				}
			}
		}
	}
}

// A CAN FD frame is read to its CRC-17 or CRC-21 (stuff count, fixed stuff
// bits) and named CAN by it; a bit flipped in its CRC field is flagged.
// TRLC-LINKS: REQ-DSO-018
func TestAutodetectCANFD(t *testing.T) {
	for _, baud := range []string{"500k", "1M"} {
		b, sr := synthBus(t, synth.Spec{Proto: "canfd", Set: map[string]string{"fd": "1", "baud": baud}}, 0)
		c1, c2 := repeated(b, 1)
		r, _ := AutodetectFast(c1, c2, 1/sr, "hex")
		if r.Proto != protoCANFD || !strings.Contains(r.Text, "FD") || strings.Contains(r.Text, "!") || !strings.Contains(r.Text, "CRC:") {
			t.Errorf("%s: %s %q", b.Name, r.Proto, r.Text)
		}
	}
	for _, n := range []int{8, 20} { // CRC-17, CRC-21
		data := make([]int, n)
		for i := range data {
			data[i] = (i*41 + 9) & 0xff
		}
		dlc := map[int]int{8: 8, 20: 11}[n]
		fd := canFDStdFrame(0x155, dlc, data)
		if r := CANFD(canRender(fd, 16, true, 160, 160), 1e-7, CANFDCfg{DominantLow: true}); !r.OK || strings.Contains(r.Text, "!") {
			t.Errorf("%d bytes: %q", n, r.Text)
		}
		at := len(fd) - 13 - 9 // within the CRC sequence
		bad := slices.Clone(fd)
		bad[at] ^= 1
		if r := CANFD(canRender(bad, 16, true, 160, 160), 1e-7, CANFDCfg{DominantLow: true}); !strings.Contains(r.Text, "CRC:!") {
			t.Errorf("%d bytes, CRC bit flipped: %q", n, r.Text)
		}
	}
}

// Overdrive 1-Wire is named 1-Wire, at overdrive's rate (the settings a
// decode reads it by), not SDQ, whose bit time it shares.
// TRLC-LINKS: REQ-DSO-018
func TestAutodetectOverdriveOneWire(t *testing.T) {
	b, sr := synthBus(t, synth.Spec{Proto: "onewire", Set: map[string]string{"overdrive": "1"}}, 0)
	c1, c2 := repeated(b, 1)
	r, _ := AutodetectFast(c1, c2, 1/sr, "hex")
	if r.Proto != protoOneWire || r.Baud < 100000 || !slices.Equal(r.Bytes, b.Truth.Bytes) {
		t.Errorf("overdrive 1-Wire: %s at %d: % x", r.Proto, r.Baud, r.Bytes)
	}
}

// An SD card in SPI mode is an SPI bus: its MOSI holds the SD bus's command
// tokens but no response, which an SD bus's CMD line carries.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func TestAutodetectSDCardInSPIMode(t *testing.T) {
	b, err := synth.MakeLayer("sdcard", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b, err = synth.MakeLayer("sdcard", b.NaturalSR); err != nil {
		t.Fatal(err)
	}
	r, _ := AutodetectFast(b.Lines[0].Codes(), b.Lines[1].Codes(), 1/b.NaturalSR, "hex")
	if r.Proto != protoSPI {
		t.Errorf("SD card in SPI mode: %s (%s)", r.Proto, r.Src)
	}
	// the SD bus itself, commands and responses, stays the SD bus
	sd, sr := synthBus(t, synth.Spec{Proto: "sdcard"}, 0)
	c1, c2 := repeated(sd, 1)
	if r, _ := AutodetectFast(c1, c2, 1/sr, "hex"); r.Proto != protoSDCard {
		t.Errorf("SD bus: %s", r.Proto)
	}
}

// A Manchester record that opens inside the preamble, at any of its bits,
// counts its words from the frame's end: the data reads right.
// TRLC-LINKS: REQ-DSO-018
func TestManchesterRecordOpensInPreamble(t *testing.T) {
	b, sr := synthBus(t, synth.Spec{Proto: "manchester"}, 40e6)
	c := b.Lines[0].Codes()
	idle, bit := 400, 40 // ten bit times of idle at 40 samples a bit
	for k := range 24 {
		cut := c[idle+bit*k+13:]
		r := Manchester(cut, 1/sr, ManchesterCfg{IEEE: true, MSB: true})
		want := b.Truth.Bytes[len(b.Truth.Bytes)-5:]
		if !r.OK || len(r.Bytes) < 5 || !slices.Equal(r.Bytes[len(r.Bytes)-5:], want) || strings.Contains(r.Text, "!") {
			t.Errorf("cut at bit %d: %q, want it to end % X", k, r.Text, want)
		}
	}
}

// Clocked buses framed by their quiet (a caliper's frames 30 ms apart,
// PS/2's characters) are named on a long record of a fast sample rate,
// which the detection cuts to its bursts: their bursts alone read as
// another clocked bus's.
// TRLC-LINKS: REQ-DSO-018
func TestAutodetectQuietFramedBusesOnLongRecords(t *testing.T) {
	for _, tc := range []struct {
		proto string
		sr    float64
		reps  int
	}{{"caliper", 25e6, 2}, {"caliper", 1e7, 3}, {"ps2", 250e6, 4}} {
		b, sr := synthBus(t, synth.Spec{Proto: tc.proto}, tc.sr)
		c1, c2 := repeated(b, tc.reps)
		if r, _ := AutodetectFast(c1, c2, 1/sr, "hex"); r.Proto != tc.proto {
			t.Errorf("%s at %g Sa/s, %d samples: %s (%s)", tc.proto, sr, len(c1), r.Proto, r.Src)
		}
	}
}

// A PWM output repeated by a source (whole periods) is named PWM on a
// record of many periods at any sample rate.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-256
func TestAutodetectRepeatedPWM(t *testing.T) {
	for _, sr := range []float64{1e9, 1e8} {
		b, _ := synthBus(t, synth.Spec{Proto: "pwm", Set: map[string]string{"period_us": "100"}}, sr)
		c1, c2 := repeated(b, 3)
		if r, _ := AutodetectFast(c1, c2, 1/sr, "hex"); r.Proto != protoPWM {
			t.Errorf("PWM at %g Sa/s: %s %q", sr, r.Proto, r.Error)
		}
	}
}

// A repeated I2C message on a decimated record (20 samples a bit) is I2C:
// its pair read as SWD frames one transfer, parity checking, again and
// again, but an SWD bus shows no acknowledged I2C transfers each ended by a
// STOP. A real SWD bus stays SWD.
// TRLC-LINKS: REQ-DSO-018
func TestAutodetectI2CIsNoSWD(t *testing.T) {
	for _, tc := range []struct {
		s  synth.Spec
		sr float64
	}{
		{synth.Spec{Proto: "i2c", Set: map[string]string{"rate": "100k"}}, 2e6},
		{synth.Spec{Proto: "i2c", Set: map[string]string{"rate": "100k"}}, 1e7},
		{synth.Spec{Proto: "swd", Set: map[string]string{"rate": "1M"}}, 1e8},
	} {
		b, sr := synthBus(t, tc.s, tc.sr)
		a, o := repeated(b, 4)
		for _, cut := range []int{0, len(a) / 8, len(a) / 3} {
			if r, _ := AutodetectFast(a[cut:], o[cut:], 1/sr, "hex"); r.Proto != b.Proto {
				t.Errorf("%s at %g Sa/s, cut %d: %s %q", tc.s, sr, cut, r.Proto, r.Text)
			}
		}
	}
}
