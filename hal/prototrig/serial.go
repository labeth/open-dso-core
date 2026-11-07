package prototrig

import "slices"

// SerialParams is the self-contained serial-trigger config: the protocol +
// channel roles + decode params to use, and the byte/address pattern to match.
// It is the core decoder registry's request (the protocol ids are package
// engine's Ser* constants), and the Bus trigger's: a board whose receivers
// match decoded units takes the request whole. Copied by value; Bytes is
// replaced wholesale by a setter (never mutated in place).
// TRLC-LINKS: REQ-DSO-013
type SerialParams struct {
	SPIClockHz int `json:"spiClockHz"` // explicit clock for acquisition-time SPI gap framing; zero uses software inference
	// I2CClockHz is the I2C clock rate (0: measured on SCL); past the general
	// image's receiver it runs on the fast image's.
	I2CClockHz int `json:"i2cClockHz,omitempty"`
	// Fine asks for the receivers on the samples (the fast image) for SPI,
	// UART and I2C at any rate they take: sub-nanosecond edge timing.
	Fine bool `json:"fine,omitempty"`
	// SPIDelayNs is where SPI's data line is read after the clock's sampling
	// edge (a device whose data settles late), beyond the channels' phase.
	SPIDelayNs float64 `json:"spiDelayNs,omitempty"`
	Inverted   bool    `json:"inverted"` // UART/I2C/SPI/SENT input polarity; independent of display inversion
	// Decode settings — mirrored from the operator's live decode config so the
	// trigger decodes IDENTICALLY to the decode strip (the UI does not re-enter
	// these; it sends whatever decode is set to).
	Proto    int     `json:"proto"`  // 0 off, 1 uart, 2 i2c, 3 spi ... 52 IR multi (the ser* ids)
	ChA      int     `json:"chA"`    // primary channel role: 0=C1 1=C2 (uart line / i2c SCL / spi CLK)
	ChB      int     `json:"chB"`    // secondary channel role (i2c SDA / spi DATA)
	Baud     int     `json:"baud"`   // uart: 0 = auto-infer
	Bits     int     `json:"bits"`   // uart: data bits (0 = decode default 8)
	Parity   string  `json:"parity"` // uart: "none"|"even"|"odd" ("" = none)
	CPOL     bool    `json:"cpol"`
	CPHA     bool    `json:"cpha"`
	MSB      bool    `json:"msb"`      // spi bit order (true = MSB-first) / manchester bit order
	IEEE     bool    `json:"ieee"`     // manchester: true = IEEE 802.3, false = Thomas/G.E.
	TickNs   float64 `json:"tickNs"`   // sent: tick override (ns); 0 = auto from the 56-tick SYNC
	Nibbles  int     `json:"nibbles"`  // sent: nibbles per frame (0 = default 8)
	DataBaud int     `json:"dataBaud"` // can-fd: data-phase baud (0 = same as nominal Baud)
	// Overdrive is 1-Wire's overdrive speed (its resets and slots a tenth
	// as long).
	Overdrive bool    `json:"overdrive,omitempty"`
	Threshold float64 `json:"threshold"` // slice threshold code, used only if HaveThr
	HaveThr   bool    `json:"haveThr"`   // false = decode auto-threshold
	// ThresholdB slices the second line (I2C's SDA, SPI's data) in the
	// hardware when HaveThrB; otherwise that line's learned level when the
	// first line's is learned too, else Threshold.
	ThresholdB float64 `json:"thresholdB,omitempty"`
	HaveThrB   bool    `json:"haveThrB,omitempty"`
	// match spec:
	Addr  int   `json:"addr"`  // i2c 7-bit address; <0 = any
	RW    int   `json:"rw"`    // i2c: 0=write 1=read 2=any
	Bytes []int `json:"bytes"` // data byte sequence to find (empty = any transaction/byte)
	// Pattern, when set, replaces Bytes with tokens that may be wildcards or
	// errors and run up to 32 long (ADR-PROTOCOL-SEQUENCE-TRIGGER).
	Pattern []PatternElem `json:"pattern,omitempty"`
}

// Equal reports that p and o ask the same: every field, Bytes and Pattern
// element by element (nil apart from empty).
// TRLC-LINKS: REQ-DSO-013
func (p *SerialParams) Equal(o *SerialParams) bool {
	return p.SPIClockHz == o.SPIClockHz && p.I2CClockHz == o.I2CClockHz && p.Fine == o.Fine &&
		p.SPIDelayNs == o.SPIDelayNs && p.Inverted == o.Inverted && p.Proto == o.Proto &&
		p.ChA == o.ChA && p.ChB == o.ChB && p.Baud == o.Baud && p.Bits == o.Bits && p.Parity == o.Parity &&
		p.CPOL == o.CPOL && p.CPHA == o.CPHA && p.MSB == o.MSB && p.IEEE == o.IEEE && p.TickNs == o.TickNs &&
		p.Nibbles == o.Nibbles && p.DataBaud == o.DataBaud && p.Overdrive == o.Overdrive &&
		p.Threshold == o.Threshold && p.HaveThr == o.HaveThr && p.ThresholdB == o.ThresholdB &&
		p.HaveThrB == o.HaveThrB && p.Addr == o.Addr && p.RW == o.RW &&
		sliceEqual(p.Bytes, o.Bytes) && sliceEqual(p.Pattern, o.Pattern)
}

// sliceEqual reports that a and b are both nil or hold equal elements.
// TRLC-LINKS: REQ-DSO-013
func sliceEqual[E comparable](a, b []E) bool {
	return (a == nil) == (b == nil) && slices.Equal(a, b)
}

// Empty reports that no protocol is set.
// TRLC-LINKS: REQ-DSO-013
func (p SerialParams) Empty() bool { return p.Proto == 0 }

// PatternElem is one trigger pattern token (ADR-PROTOCOL-SEQUENCE-TRIGGER):
// Kind 2 is a decoded unit (byte, word or ARINC data field) equal to Value
// under Mask, in the decoder's own units; Mask 0 accepts any value. Kind 4 is
// a decoder-reported error (CRC, parity, framing, stuffing or coding). Kind 1
// is the start of a frame as the hardware decoder reports it (a START, SOF,
// SYNC or the idle gap before an SPI word), so [start, any unit] fires on a
// frame's first unit. A MIL-1553 unit may also require the command sync
// (MILCommand in Value and Mask).
// TRLC-LINKS: REQ-DSO-013
type PatternElem struct {
	Kind  int `json:"kind"`
	Value int `json:"value"`
	Mask  int `json:"mask"`
}
