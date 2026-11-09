// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"maps"
	"sync"
)

// SoftOneWire is the decode package's name of 1-Wire, whose overdrive
// speed a stream plans apart (a tenth of the standard speed's slots).
const SoftOneWire = "onewire"

// softStreamNames are the decode package's names of the protocols a scope
// decodes from a stream of samples in software (ADR-STREAM-SOFT-DECODE):
// those no FPGA receiver decodes.
var softStreamNames = map[int]string{
	SerLIN: "lin", SerDMX: "dmx512", SerModbus: "modbus", SerOneWire: SoftOneWire,
	SerWS281x: "ws281x", SerDHT: "dht", SerCEC: "cec", SerJ1850: "j1850", SerIRNEC: "irnec", SerIRSIRC: "irsirc",
	SerIRRC5: "irrc5", SerIRRC6: "irrc6", SerDALI: "dali", SerUSBPD: "usbpd", SerSPDIF: "spdif", SerPS2: "ps2",
	SerSWD: "swd", SerMDIO: "mdio", SerWiegand: "wiegand", SerQuad: "quad", SerStepper: "stepper", SerPWM: "pwm",
	SerRCEncode: "rcencode", SerOOK: "ook", SerMiller: "miller", SerMorse: "morse", SerDCF77: "dcf77", SerEM4100: "em4100",
	SerEM4305: "em4305", SerT55xx: "t55xx", SerSWIM: "swim", SerSDQ: "sdq", SerPJDL: "pjdl", SerDSI: "dsi", SerQi: "qi",
	SerSDCard: "sdcard", SerPDI: "pdi", SerCJTAG: "cjtag", SerMaple: "maple", SerCaliper: "caliper", SerIRMP: "irmp",
}

// SoftStreamNames is a copy of the protocols decoded in software from a
// stream and their decode package names.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func SoftStreamNames() map[int]string { return maps.Clone(softStreamNames) }

// SoftStreamName is protocol proto's decode package name when a scope
// decodes its stream in software, from a table by id built once (the map
// is hashed on every stream view's frame otherwise).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func SoftStreamName(proto int) (string, bool) {
	t := softNameTable()
	if proto < 0 || proto >= len(t) || t[proto] == "" {
		return "", false
	}
	return t[proto], true
}

// softNameTable is softStreamNames indexed by protocol id.
var softNameTable = sync.OnceValue(func() []string {
	n := 0
	for id := range softStreamNames {
		n = max(n, id+1)
	}
	t := make([]string, n)
	for id, name := range softStreamNames {
		if id >= 0 {
			t[id] = name
		}
	}
	return t
})
