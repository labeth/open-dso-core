// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

// AcqSample is one per-frame acquisition record captured by the realtime
// acquisition checker (pure instrumentation — it never influences the FSM). It
// snapshots the hardware/wait state of a single halt+drain frame so a HALF
// record (valid_depth ≈ half of cols on a native-fast band) can be correlated
// with the arm/wait/halt outcome that produced it. Recorded for every frame
// that reaches halt+drain, published or held.
// TRLC-LINKS: REQ-DSO-127
type AcqSample struct {
	Seq          uint64  `json:"seq"`          // FSM heartbeat (stats.Frames) at record time
	Band         string  `json:"band"`         // native-fast | decimated | envelope | roll
	ValidDepth   int     `json:"valid_depth"`  // live samples before the dead tail
	Cols         int     `json:"cols"`         // drained record width (effDrainCols)
	FillAtHalt   int     `json:"fill_at_halt"` // 0x46 & fillMask, final read inside halt()
	HaltOK       bool    `json:"halt_ok"`      // halt() confirmed the fill froze
	SawTrig      bool    `json:"saw_trig"`     // status bit1 asserted during the wait
	Anchored     bool    `json:"anchored"`     // wait anchored on done/valid
	Filled       bool    `json:"filled"`       // post-trigger record filled (0x46 ≥ LatchAt)
	ArmToLatchMs float64 `json:"arm_to_latch_ms"`
	TrigPos      int     `json:"trig_pos"`
	Half         bool    `json:"half"`       // published ValidDepth < 0.6*Cols (after re-capture)
	FirstHalf    bool    `json:"first_half"` // the FIRST drain was half (raw HW rate, before re-capture)
	Published    bool    `json:"published"`  // this capture became user-visible (vs held/discarded)
	Norm         bool    `json:"norm"`       // NORM (vs AUTO) at capture time
	TdivS        float64 `json:"tdiv_s"`     // band timebase at capture time
}
