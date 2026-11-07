// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

// ExpireMeasurements makes the next reply measure afresh even within the
// throttle window.
func (s *Scope) ExpireMeasurements() { s.meas.Expire() }
