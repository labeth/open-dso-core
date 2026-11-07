package decode

import "github.com/labeth/open-dso-core/sample"

// Codes8 is samples [0, Valid) of trace t on the 8-bit view the decoders
// read: 8-bit codes as they are (no copy), wider words mapped onto it
// (sample.CodeFormat.Code8). The decoders' level thresholds are in 8-bit
// codes, so a 12-bit scope decodes what an 8-bit one would of the same
// screen.
// TRLC-LINKS: REQ-SDS-018
func Codes8(t *sample.Trace) []uint8 {
	if t == nil {
		return nil
	}
	return t.View8()
}

// AutodetectTraces is Autodetect over two traces of any code format.
// TRLC-LINKS: REQ-SDS-018
func AutodetectTraces(a, b *sample.Trace, colTimeS float64, format string) Result {
	return Autodetect(Codes8(a), Codes8(b), colTimeS, format)
}

// AutodetectFastTraces is AutodetectFast over two traces of any code format.
// TRLC-LINKS: REQ-SDS-018
func AutodetectFastTraces(a, b *sample.Trace, colTimeS float64, format string) (Result, float64) {
	return AutodetectFast(Codes8(a), Codes8(b), colTimeS, format)
}
