// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// The check sequences and tables the generators write, each from its
// protocol's specification.

// reflected8 is a reflected CRC-8 of poly (the reversed polynomial) from 0.
// TRLC-LINKS: REQ-DSO-018
func reflected8(b []int, poly int) int {
	c := 0
	for _, v := range b {
		c ^= v & 0xff
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ poly
			} else {
				c >>= 1
			}
		}
	}
	return c
}

// CRC8Maxim is the 1-Wire CRC-8 (x^8+x^5+x^4+1, reflected, from 0).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func CRC8Maxim(b []int) int { return reflected8(b, 0x8C) }

// CRC8PJON is PJON's CRC-8 (polynomial 0x97 reflected, from 0).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-253
func CRC8PJON(b []int) int { return reflected8(b, 0x97) }

// CRC7 is the SD card's CRC-7 (x^7+x^3+1) over bits, most significant
// first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-254
func CRC7(bits []int) int {
	c := 0
	for _, b := range bits {
		fb := (c>>6 ^ b) & 1
		c = c << 1 & 0x7f
		if fb != 0 {
			c ^= 0x09
		}
	}
	return c
}

// CRC32IEEE is the IEEE 802.3 CRC-32 (USB PD's) over bytes.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-235
func CRC32IEEE(b []int) uint32 {
	c := uint32(0xFFFFFFFF)
	for _, v := range b {
		c ^= uint32(v & 0xff)
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xEDB88320
			} else {
				c >>= 1
			}
		}
	}
	return c ^ 0xFFFFFFFF
}

// J1850CRC is SAE J1850's CRC-8 (x^8+x^4+x^3+x^2+1, from 0xFF, inverted).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-233
func J1850CRC(b []int) int {
	c := 0xff
	for _, v := range b {
		c ^= v & 0xff
		for range 8 {
			if c&0x80 != 0 {
				c = (c<<1 ^ 0x1D) & 0xff
			} else {
				c = c << 1 & 0xff
			}
		}
	}
	return c ^ 0xff
}

// LINPID is a LIN frame identifier with its two parity bits.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func LINPID(id int) int {
	b := func(k int) int { return (id >> k) & 1 }
	p0 := b(0) ^ b(1) ^ b(2) ^ b(4)
	p1 := 1 - (b(1) ^ b(3) ^ b(4) ^ b(5))
	return id&0x3f | p0<<6 | p1<<7
}

// LINChecksum is a LIN frame's checksum: the inverted sum with carry of
// the data, and the protected identifier when enhanced.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func LINChecksum(pid int, data []int, enhanced bool) int {
	s := 0
	if enhanced {
		s = pid
	}
	for _, d := range data {
		s += d
		if s > 0xff {
			s -= 0xff
		}
	}
	return 0xff - s
}

// ModbusCRC is Modbus RTU's CRC-16 (0xA001 reflected, from 0xFFFF), sent
// low byte first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func ModbusCRC(b []int) int {
	c := 0xFFFF
	for _, v := range b {
		c ^= v & 0xff
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
	}
	return c
}

// PJDLModes are PJDL's four modes' data and pad times (us), by mode number.
var PJDLModes = [5][2]float64{{}, {44, 116}, {40, 92}, {28, 88}, {26, 60}}

// morseTable is International Morse code by character.
var morseTable = map[byte]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.", 'H': "....", 'I': "..",
	'J': ".---", 'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--", 'Z': "--..",
	'1': ".----", '2': "..---", '3': "...--", '4': "....-", '5': ".....", '6': "-....", '7': "--...",
	'8': "---..", '9': "----.", '0': "-----", '.': ".-.-.-", ',': "--..--", ':': "---...", '?': "..--..",
	'-': "-....-", '/': "-..-.", '(': "-.--.", ')': "-.--.-", '=': "-...-", '+': ".-.-.", '@': ".--.-.",
}

// MorseOf is a character's Morse code ("" for none).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-251
func MorseOf(c byte) string { return morseTable[c] }
