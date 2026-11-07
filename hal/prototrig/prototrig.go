// Package prototrig holds the typed protocol trigger configurations
// (hal.TrigStage.Proto): what a user asks a protocol trigger to match, in
// bus terms. They sit beside the HAL rather than with the decoders so a
// board programs its hardware matcher without importing a decoder, and the
// controllers (UI model, SCPI, web) build them without importing a board.
//
// A board takes the configurations its hardware matches and refuses the
// rest with the reason; nothing here is a register layout.
package prototrig

import "github.com/labeth/open-dso-core/hal"

// Compare is how a data value is compared.
type Compare int

// The comparisons.
const (
	Equal Compare = iota
	Greater
	Less
)

// Line is one bus line: its source and threshold.
type Line struct {
	Source hal.Source
	Level  float64 // volts at the tip
}

// I2CCondition is what an I2C trigger fires on.
type I2CCondition int

// The I2C conditions.
const (
	I2CStart I2CCondition = iota
	I2CStop
	I2CRestart
	I2CNoAck
	I2CEEPROM     // an EEPROM read: control byte 0xA0–0xAF, then a data byte compared
	I2CAddr7Data  // a 7-bit address, then up to two data bytes
	I2CAddr10Data // a 10-bit address, then up to two data bytes
	I2CDataLength // a frame of DataLength bytes
)

// RW is the I2C read/write bit to match.
type RW int

// The R/W choices.
const (
	Write RW = iota
	Read
	AnyRW
)

// I2C is an I2C trigger.
type I2C struct {
	hal.ProtoTriggerType
	SCL, SDA   Line
	Condition  I2CCondition
	Address    int     // -1: any address
	RW         RW      // address conditions
	Data       []uint8 // up to two bytes after the address; empty: any
	Compare    Compare // I2CEEPROM: how the data byte is compared
	DataLength int     // I2CDataLength
	Addr10     bool    // I2CDataLength: the frame has a 10-bit address
}

// Protocol is "i2c".
func (I2C) Protocol() string { return "i2c" }

// SPISelect is how an SPI frame is delimited.
type SPISelect int

// The SPI frame delimiters.
const (
	SelectActiveLow SPISelect = iota // a chip select that is low in the frame
	SelectActiveHigh
	SelectTimeout // no chip select: a clock idle time ends a frame
)

// SPI is an SPI trigger on one data line.
type SPI struct {
	hal.ProtoTriggerType
	Clock, Data  Line
	Select       Line // SelectActiveLow / SelectActiveHigh
	SelectMode   SPISelect
	IdleS        float64 // SelectTimeout: the idle time
	LatchFalling bool
	MSBFirst     bool
	// Bits is the pattern to match, first-sent bit first (up to 96):
	// hal.Low, hal.High or hal.DontCare.
	Bits []hal.PatternState
}

// Protocol is "spi".
func (SPI) Protocol() string { return "spi" }

// UARTCondition is what a UART trigger fires on.
type UARTCondition int

// The UART conditions.
const (
	UARTStart UARTCondition = iota
	UARTStop
	UARTData
	UARTError
)

// Parity is a UART frame's parity bit.
type Parity int

// The parities.
const (
	NoParity Parity = iota
	Odd
	Even
	Mark
	Space
)

// UART is a UART trigger on one line.
type UART struct {
	hal.ProtoTriggerType
	Line      Line
	Condition UARTCondition
	Baud      float64
	Bits      int // data bits, 5–9
	Parity    Parity
	StopBits  float64 // 1, 1.5 or 2
	IdleHigh  bool
	MSBFirst  bool
	Compare   Compare
	Value     int // UARTData: the data value; -1: any
}

// Protocol is "uart".
func (UART) Protocol() string { return "uart" }

// CANCondition is what a CAN trigger fires on.
type CANCondition int

// The CAN conditions.
const (
	CANStart CANCondition = iota
	CANRemote
	CANID
	CANIDData
	CANError
)

// CAN is a CAN trigger on one line (CAN_H or CAN_L).
type CAN struct {
	hal.ProtoTriggerType
	Line      Line
	Condition CANCondition
	Baud      float64
	ID        int // -1: any
	Extended  bool
	Data      []uint8 // CANIDData: up to two bytes
}

// Protocol is "can".
func (CAN) Protocol() string { return "can" }

// LINCondition is what a LIN trigger fires on.
type LINCondition int

// The LIN conditions.
const (
	LINBreak LINCondition = iota
	LINID
	LINIDData
	LINDataError
)

// LIN is a LIN trigger.
type LIN struct {
	hal.ProtoTriggerType
	Line      Line
	Condition LINCondition
	Baud      float64
	ID        int     // -1: any
	Data      []uint8 // LINIDData: up to two bytes
	// LINDataError: which errors fire, and the frame they are looked for in.
	ChecksumError, SyncError, ParityError bool
	Spec2                                 bool // LIN 2.x checksum
	ErrorID, ErrorLength                  int
}

// Protocol is "lin".
func (LIN) Protocol() string { return "lin" }
