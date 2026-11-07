// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the pulse-coded single-wire protocols (proto_gen_test.go).

// OneWireReset appends a 1-Wire reset (standard or overdrive) and, if presence,
// a device's presence pulse, then the rest of the reset's high time.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func OneWireReset(w *Line, od, presence bool) {
	k := 1.0
	if od {
		k = 0.1
	}
	w.Add(0, 500e-6*k)
	if presence {
		w.Add(1, 30e-6*k)
		w.Add(0, 120e-6*k)
		w.Add(1, 350e-6*k)
	} else {
		w.Add(1, 500e-6*k)
	}
}

// OneWireSlot appends one 1-Wire time slot.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func OneWireSlot(w *Line, b int, od bool) {
	if od {
		if b == 1 {
			w.Add(0, 1e-6)
			w.Add(1, 9e-6)
		} else {
			w.Add(0, 7.5e-6)
			w.Add(1, 2.5e-6)
		}
		return
	}
	if b == 1 {
		w.Add(0, 6e-6)
		w.Add(1, 64e-6)
	} else {
		w.Add(0, 60e-6)
		w.Add(1, 10e-6)
	}
}

// OneWireByte appends a byte least significant bit first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func OneWireByte(w *Line, v int, od bool) {
	for i := range 8 {
		OneWireSlot(w, v>>i&1, od)
	}
}

// OneWireSearchROM appends a Search ROM's slots for one device's ROM: per bit
// the bit, its complement and the master's direction.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func OneWireSearchROM(w *Line, rom []int) {
	for _, v := range rom {
		for i := range 8 {
			b := v >> i & 1
			OneWireSlot(w, b, false)
			OneWireSlot(w, 1-b, false)
			OneWireSlot(w, b, false)
		}
	}
}

// OneWireROM is a ROM id with its CRC-8.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func OneWireROM(family int, serial ...int) []int {
	rom := append([]int{family}, serial...)
	return append(rom, CRC8Maxim(rom))
}

// WS281xFrame appends a WS281x frame at rate (800e3 or 400e3) and an 80 us latch.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func WS281xFrame(w *Line, rate float64, b []int) {
	per := 1 / rate
	for _, v := range b {
		for i := 7; i >= 0; i-- {
			hi := 0.32 * per
			if v>>i&1 == 1 {
				hi = 0.68 * per
			}
			w.Add(1, hi)
			w.Add(0, per-hi)
		}
	}
	w.Add(0, 80e-6)
}

// DHTReading appends the host's start (startUs low), the sensor's response
// and its five bytes, the checksum computed unless ck >= 0 replaces it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func DHTReading(w *Line, startUs float64, b [4]int, ck int) {
	if ck < 0 {
		ck = (b[0] + b[1] + b[2] + b[3]) & 0xff
	}
	w.Add(0, startUs*1e-6)
	w.Add(1, 30e-6)
	w.Add(0, 80e-6)
	w.Add(1, 80e-6)
	for _, v := range append(b[:], ck) {
		for i := 7; i >= 0; i-- {
			w.Add(0, 50e-6)
			if v>>i&1 == 1 {
				w.Add(1, 70e-6)
			} else {
				w.Add(1, 27e-6)
			}
		}
	}
	w.Add(0, 50e-6)
	w.Add(1, 5e-3)
}

// CECBit appends one CEC data bit.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func CECBit(w *Line, b int) {
	if b == 1 {
		w.Add(0, 0.6e-3)
		w.Add(1, 1.8e-3)
	} else {
		w.Add(0, 1.5e-3)
		w.Add(1, 0.9e-3)
	}
}

// CECMessage appends a CEC message: the start bit and blocks of the bytes
// with EOM on the last; acks gives each block's acknowledge bit as the
// follower leaves it (0: held low).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func CECMessage(w *Line, b, acks []int) {
	w.Add(0, 3.7e-3)
	w.Add(1, 0.8e-3)
	for i, v := range b {
		for k := 7; k >= 0; k-- {
			CECBit(w, v>>k&1)
		}
		eom := 0
		if i == len(b)-1 {
			eom = 1
		}
		CECBit(w, eom)
		CECBit(w, acks[i])
	}
	w.Add(1, 10e-3)
}

// J1850Frame appends a J1850 VPW frame (idle low): SOF, the bytes with
// their CRC (spoiled when badCRC), EOD and the end of frame. The bits
// alternate between active and passive runs, so a whole number of bytes
// ends on an active one and the EOD is passive.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func J1850Frame(w *Line, b []int, badCRC bool) {
	crc := J1850CRC(b)
	if badCRC {
		crc ^= 0x5A
	}
	w.Add(1, 200e-6)
	lvl := byte(0)
	for _, v := range append(append([]int{}, b...), crc) {
		for i := 7; i >= 0; i-- {
			bit := v >> i & 1
			d := 64e-6
			if (lvl == 1) == (bit == 0) {
				d = 128e-6
			}
			w.Add(lvl, d)
			lvl ^= 1
		}
	}
	w.Add(0, 600e-6)
}
