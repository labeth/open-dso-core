// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import "math/rand"

// Parametric UART, SPI and I2C buses with random content: the AUTO fuzzing
// campaign's buses, and a bus of any rate and shape for a bench. Each
// returns the bytes it wrote.

// UARTText is a UART bus of 40 printable characters at baud (8N1), a few
// idle gaps between them, sampled at sr.
// TRLC-LINKS: REQ-DSO-018
func UARTText(rng *rand.Rand, baud, sr float64) (*Line, []int) {
	w := NewLine(sr)
	w.Idle(30 / baud)
	var sent []int
	for range 40 {
		v := 0x20 + rng.Intn(0x5f)
		sent = append(sent, v)
		UARTChar(w, v, baud, "none", 1)
		if rng.Intn(6) == 0 {
			w.Idle(float64(5+rng.Intn(30)) / baud)
		}
	}
	w.Idle(30 / baud)
	return w, sent
}

// SPIShape is an SPI bus's timing: its clock rate and duty (the share of a
// period the clock rests away from its idle level), whether its bytes come
// back to back or bit-banged (each bit's period stretched by up to Jitter,
// a gap of ByteGap periods after each byte), MSB first, in mode CPOL/CPHA.
type SPIShape struct {
	Hz, Duty, Jitter, ByteGap float64
	CPOL, CPHA                bool
}

// SPIBytes is an SPI bus of shape s carrying n random bytes, as a Pair (A
// the clock, B the data).
// TRLC-LINKS: REQ-DSO-018
func SPIBytes(rng *rand.Rand, s SPIShape, sr float64, n int) (Pair, []int) {
	vals := make([]int, 0, n)
	l := SPIBus(s, sr, func() int {
		v := rng.Intn(256)
		vals = append(vals, v)
		return v
	}, n, rng)
	return l, vals
}

// SPIBus is an SPI bus of shape s carrying n bytes from next, as a Pair (A
// the clock, B the data); rng times a bit-banging host (nil: none).
// TRLC-LINKS: REQ-DSO-018
func SPIBus(s SPIShape, sr float64, next func() int, n int, rng *rand.Rand) Pair {
	l := NewPair(sr)
	idle := byte(0)
	if s.CPOL {
		idle = 1
	}
	act := 1 - idle
	T := 1 / s.Hz
	l.Add(idle, 0, 20*T)
	d := byte(0)
	for range n {
		v := next()
		for i := 7; i >= 0; i-- {
			bit := byte(v >> i & 1)
			// a bit-banging loop: each bit within 5% of its period, one in
			// ten held up (an interrupt) by up to jitter periods
			per := T
			if rng != nil {
				per = T * (1 + 0.05*(rng.Float64()*2-1))
				if s.Jitter > 0 && rng.Intn(10) == 0 {
					per += T * s.Jitter * rng.Float64()
				}
			}
			on := per * s.Duty
			if !s.CPHA {
				// data set before the leading edge, which samples it
				d = bit
				l.Add(idle, d, per-on)
				l.Add(act, d, on)
			} else {
				// data set on the leading edge, sampled on the trailing one
				l.Add(idle, d, per-on)
				d = bit
				l.Add(act, d, on)
			}
		}
		if s.ByteGap > 0 {
			g := s.ByteGap * T
			if rng != nil {
				g *= 0.5 + rng.Float64()
			}
			l.Add(idle, d, g)
		}
	}
	l.Add(idle, d, 20*T)
	return l
}

// I2CTransfer is one I2C transfer: a 7-bit address, read or write, and its
// data bytes.
type I2CTransfer struct {
	Addr int
	Read bool
	Data []int
}

// I2CRandom is an I2C bus at hz of transfers random transfers of a few
// bytes, as a Pair (A SCL, B SDA), and the transfers.
// TRLC-LINKS: REQ-DSO-018
func I2CRandom(rng *rand.Rand, hz, sr float64, transfers int) (Pair, []I2CTransfer) {
	var ts []I2CTransfer
	var gaps []int
	for range transfers {
		addr := 0x08 + rng.Intn(0x70)
		t := I2CTransfer{Addr: addr, Read: rng.Intn(2) == 1}
		for n := 1 + rng.Intn(4); n > 0; n-- {
			t.Data = append(t.Data, rng.Intn(256))
		}
		ts = append(ts, t)
		gaps = append(gaps, 10+rng.Intn(40))
	}
	return I2CBus(hz, sr, ts, gaps), ts
}

// I2CBus is an I2C bus at hz carrying the transfers, each acknowledged but
// a read's last byte (the controller NAKs it), with START and STOP, gaps[i]
// clock periods of idle after transfer i (nil: 20), as a Pair (A SCL, B
// SDA).
// TRLC-LINKS: REQ-DSO-018
func I2CBus(hz, sr float64, ts []I2CTransfer, gaps []int) Pair {
	l := NewPair(sr)
	T := 1 / hz
	l.Add(1, 1, 20*T)
	// SDA moves a quarter period into SCL low, never with SCL's fall: a
	// device holds it past the fall (300 ns at least) so that no data
	// change reads as a START or a STOP
	sda := byte(0)
	bitOut := func(b byte) {
		l.Add(0, sda, T/4)
		l.Add(0, b, T/4)
		l.Add(1, b, T/2)
		sda = b
	}
	for k, t := range ts {
		l.Add(1, 0, T/2) // START
		l.Add(0, 0, T/2)
		sda = 0
		rw := 0
		if t.Read {
			rw = 1
		}
		frame := append([]int{t.Addr<<1 | rw}, t.Data...)
		for j, v := range frame {
			for i := 7; i >= 0; i-- {
				bitOut(byte(v >> i & 1))
			}
			if t.Read && j > 0 && j == len(frame)-1 {
				bitOut(1) // a read's last byte: the controller's NAK
			} else {
				bitOut(0) // ACK
			}
		}
		l.Add(0, 0, T/2) // STOP
		l.Add(1, 0, T/2)
		g := 20
		if gaps != nil {
			g = gaps[k]
		}
		l.Add(1, 1, float64(g)*T)
	}
	return l
}
