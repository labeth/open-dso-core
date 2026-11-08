// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import "github.com/labeth/open-dso-core/decode/synth"

// The tests write their captures with the bus generators of decode/synth
// (each protocol's frames as levels over time, at a sample rate, so
// non-integer samples per bit accumulate like a real capture); these are
// the tests' names for them.

type (
	timeline  = synth.Line
	twoLine   = synth.Pair
	irLine    = synth.IRLine
	swdLine   = synth.SWDLine
	rfidField = synth.RFIDField
	cjtagLine = synth.CJTAGLine
	mapleLine = synth.MapleLine
)

// TRLC-LINKS: REQ-DSO-018
func newTimeline(sr float64) *timeline { return synth.NewLine(sr) }

// TRLC-LINKS: REQ-DSO-018
func newTwoLine(sr float64) twoLine { return synth.NewPair(sr) }

const rfidFC = synth.RFIDFieldClock

var (
	tlUART        = synth.UARTChar
	linFrame      = synth.LINFrame
	dmxPacket     = synth.DMXPacket
	dmxPacketGap  = synth.DMXPacketGap
	modbusFrame   = synth.ModbusFrame
	owReset       = synth.OneWireReset
	owByte        = synth.OneWireByte
	owSearchROM   = synth.OneWireSearchROM
	owROM         = synth.OneWireROM
	wsFrame       = synth.WS281xFrame
	dhtReading    = synth.DHTReading
	cecMessage    = synth.CECMessage
	j1850Frame    = synth.J1850Frame
	daliFrame     = synth.DALIFrame
	pdMessage     = synth.PDMessage
	pdSOP         = synth.PDSOP
	pdSOPPrime    = synth.PDSOPPrime
	pdHardReset   = synth.PDHardReset
	spdifStream   = synth.SPDIFStream
	ps2Frame      = synth.PS2Frame
	mdioFrame     = synth.MDIOFrame
	wiegandFrame  = synth.WiegandFrame
	wiegandBits   = synth.WiegandBits
	necFrame      = synth.NECFrame
	necRepeat     = synth.NECRepeat
	sircFrame     = synth.SIRCFrame
	rc5Frame      = synth.RC5Frame
	rc6Frame      = synth.RC6Frame
	irPD          = synth.IRPulseDistance
	lsbBits       = synth.LSBBits
	msbBits       = synth.MSBBits
	irSamsung32   = synth.IRSamsung32
	irJVC         = synth.IRJVC
	irNEC16       = synth.IRNEC16
	irNEC42       = synth.IRNEC42
	irKaseikyo    = synth.IRKaseikyo
	irDenon       = synth.IRDenon
	irMatsushita  = synth.IRMatsushita
	irSamsung37   = synth.IRSamsung37
	quadMoves     = synth.QuadMoves
	stepperMoves  = synth.StepperMoves
	pwmWave       = synth.PWMWave
	rcFrame2262   = synth.RC2262Frame
	rcFrame1527   = synth.RC1527Frame
	oregonBurst   = synth.OregonBurst
	millerMessage = synth.MillerMessage
	morseText     = synth.MorseText
	dcf77Minute   = synth.DCF77Minute
	dcf77MinuteAt = synth.DCF77MinuteAt
	em4305Command = synth.EM4305Command
	em4305Bits    = synth.EM4305Bits
	t55xxCommand  = synth.T55xxCommand
	t55xxBits     = synth.T55xxBits
	em4100Frame   = synth.EM4100Frame
	swimFrame     = synth.SWIMFrame
	swimSync      = synth.SWIMSync
	sdqBreak      = synth.SDQBreak
	sdqByte       = synth.SDQByte
	pjonPacket    = synth.PJONPacket
	pjdlFrame     = synth.PJDLFrame
	dsiFrame      = synth.DSIFrame
	qiPacket      = synth.QiPacket
	clockBits     = synth.ClockBits
	sdToken       = synth.SDToken
	sdR2          = synth.SDR2
	sdIdle        = synth.SDIdle
	pdiChar       = synth.PDIChar
	mapleBytes    = synth.MapleBytes
	caliperFrame  = synth.CaliperFrame
)
