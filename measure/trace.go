package measure

import "github.com/labeth/open-dso-core/sample"

// minAmplDiv is minAmplCodes in divisions, as the 8-bit kernels were tuned
// on a converter of 25 codes a division.
const minAmplDiv = minAmplCodes / 25.0

// ComputeTrace is ComputeAcquisition for samples [lo, hi) of a trace in any
// code format. voltsPerCode is volts per step of the 8-bit view
// (sample.CodeFormat.Unit8 words). 8-bit codes, with the trace's precision
// words when it has them, take exactly ComputeAcquisition's path; wider
// words are measured as Q8.8 on the 8-bit view, with the flat-record
// threshold kept at the same fraction of a division.
// TRLC-LINKS: REQ-SDS-017
func ComputeTrace(t *sample.Trace, lo, hi int, voltsPerCode, offV, sampleS float64, coupling, guard int) *Result {
	words := len(t.Codes8)
	if t.Format.Storage == sample.U16 {
		words = len(t.Codes16)
	}
	hi = min(hi, t.Valid, words)
	if lo < 0 || lo >= hi {
		return nil
	}
	if t.Format.Storage != sample.U16 {
		var q []uint16
		if len(t.Precision) >= hi {
			q = t.Precision[lo:hi]
		}
		return ComputeAcquisition(t.Codes8[lo:hi], q, voltsPerCode, offV, sampleS, coupling, guard)
	}
	ws := t.Codes16[lo:hi]
	q := make([]uint16, len(ws))
	for i, w := range ws {
		q[i] = t.Format.Q88(uint32(w))
	}
	if guard > 0 && 2*guard < len(q) {
		q = q[guard : len(q)-guard]
	}
	if coupling == 2 {
		return &Result{}
	}
	minAmpl := float64(minAmplCodes)
	if cpd := t.CodesPerDiv / t.Format.Unit8(); cpd > 0 {
		minAmpl = minAmplDiv * cpd
	}
	r := computeMin(q, voltsPerCode/256, offV, sampleS, 256, minAmpl)
	if coupling == 1 && r != nil {
		mean := r.Vmean
		r.Vmin -= mean
		r.Vmax -= mean
		r.Vtop -= mean
		r.Vbase -= mean
		r.Vmean = 0
		r.Vrms = r.VrmsAC
	}
	return r
}
