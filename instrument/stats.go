// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

// Stats is the instrument's state and its engine's counters as one
// snapshot: what every controller resyncs from (the UI model's knob
// shadows, SCPI's queries, the settings file, the web status) and what the
// health writer reports. The JSON names are the status document's. Fields a
// board has no use for stay zero: TrigCode, OffC1 and OffC2 are the board's
// staged level and offset words (0: untouched since boot), and the counters
// below Frames are the engine's diagnostics.
// TRLC-LINKS: REQ-DSO-009, REQ-DSO-127
type Stats struct {
	Frames        uint64  `json:"frames"`                  // FSM heartbeat: +1 per loop iteration, publish or not
	Coherent      uint64  `json:"coherent"`                // frames that latched+drained coherently
	Published     uint64  `json:"published"`               // frames handed to the arena
	Held          uint64  `json:"held"`                    // display-hold cycles
	Degraded      bool    `json:"degraded,omitempty"`      // last native-fast capture kept a dead tail through the retries
	DegradedRun   int     `json:"degraded_run,omitempty"`  // consecutive degraded captures
	StuckSuspect  bool    `json:"stuck_suspect,omitempty"` // the run crossed the stuck-FSM threshold: power-cycle likely needed
	HaltConfirm   uint64  `json:"halt_confirm"`            // halts with fill frozen across the double read
	BusErrors     uint64  `json:"bus_errors"`
	DeadRuns      int     `json:"dead_runs"` // consecutive fill-frozen + flat-drain frames
	Wedged        bool    `json:"wedged"`
	FPS           float64 `json:"fps"`
	Running       bool    `json:"running"`
	Norm          bool    `json:"norm"`
	Single        bool    `json:"single"`         // a single-shot is armed/waiting
	TrigPosFrac   float64 `json:"trig_pos_frac"`  // horizontal trigger position 0..1
	FalseTriggers uint64  `json:"false_triggers"` // hardware triggers with no calibrated edge at the index (dropped)
	// NoiseTriggers is the recent share of hardware edge triggers dropped
	// for no calibrated edge at the index (0..1): the level sits in a small
	// signal's noise.
	NoiseTriggers float64 `json:"noise_triggers"`
	TdivS         float64 `json:"tdiv_s"`
	TdivSetS      float64 `json:"tdiv_set_s"` // the timebase asked for: a new one waits for the next capture (TdivS until then)
	DisplayedS    float64 `json:"displayed_sdiv_s"`
	RecordS       float64 `json:"record_s"`  // planned SRAM record duration: how long one capture takes to fill
	TrigCode      uint16  `json:"trig_code"` // 0 = boot-inherited comparator untouched
	OffC1         uint16  `json:"off_c1"`    // 0 = boot-inherited offset untouched
	OffC2         uint16  `json:"off_c2"`
	TrigRising    bool    `json:"trig_rising"`
	TrigSource    int     `json:"trig_source"` // 0=C1, 1=C2
	LastPtp       int     `json:"last_ptp"`
	LastTrigPos   int     `json:"last_trigpos"`
	ArmToLatch    float64 `json:"arm_to_latch_ms"`
	DrainMs       float64 `json:"drain_ms"`
	LCDFrames     uint64  `json:"lcd_frames"`
	SRAMRecallMs  float64 `json:"sram_recall_ms"`
	// Envelope recall diagnostics: rejected commands and the last rejection.
	EnvelopeRejects int      `json:"envelope_rejects,omitempty"`
	EnvelopeLast    string   `json:"envelope_last,omitempty"`
	ConditionMs     float64  `json:"condition_ms"`
	LEDLagMs        float64  `json:"led_lag_ms"`             // last staged-lamp → latch delay
	LiveReadout     string   `json:"live_readout"`           // how the last frame was read (and why not reduced)
	Busy            string   `json:"busy,omitempty"`         // a frame is being read or processed (loading indicator)
	BusyPct         float64  `json:"busy_pct"`               // its progress 0..100, -1 when unknown
	BusyMs          float64  `json:"busy_ms"`                // how long it has been busy
	FrameStages     string   `json:"frame_stages,omitempty"` // last SRAM frame's stage times
	RollStream      bool     `json:"roll_stream"`            // roll uses the stream image (gap-free, image reload on entry)
	Roll            bool     `json:"roll"`                   // the live view is rolling (slow timebase, AUTO)
	ViewPending     bool     `json:"view_pending"`           // stopped: the held record is being re-shown at a new view
	HoldoffS        float64  `json:"holdoff_s"`              // trigger holdoff (0 = off)
	Seq             uint64   `json:"seq"`
	MmapDrain       bool     `json:"mmap_drain"` // the fast (EDMA) BURST drain is active; json name kept for the UI
	ETS             bool     `json:"ets"`
	BandKind        string   `json:"band"`      // native-fast | decimated | envelope | roll
	HaltMode        string   `json:"halt_mode"` // capture-halt | latch-no-halt
	TrigType        int      `json:"trig_type"` // 0=edge 1=pulse 2=slope 3=video
	TrigQual        TrigQual `json:"trig_qual"` // the pulse/slope qualifier parameters in effect
	AcqMode         int      `json:"acq_mode"`  // 0=normal 1=average 2=eres 3=peak
	AvgCount        int      `json:"avg_count"`
	PrecisionRateHz float64  `json:"precision_rate_hz"`
	EresLen         int      `json:"eres_len"`
	WinColStd       float64  `json:"wincol_std"`       // centred cross-frame uniformity
	WinColRaw       float64  `json:"wincol_std_raw"`   // fixed-position variant
	WinColMax       float64  `json:"wincol_max"`       // worst centred column
	ValidDepth      int      `json:"valid_depth"`      // real-signal samples in the drain
	WinCols         int      `json:"win_cols"`         // display-window width in raw samples
	MemDepth        int      `json:"mem_depth"`        // configured decimated drain depth
	Stream          bool     `json:"stream"`           // stitched streaming decode mode on
	GapMs           float64  `json:"gap_ms"`           // stream: blackout between windows
	ShortDrains     uint64   `json:"short_drains"`     // drains where BURST_REMAIN held fewer words than the frame asked for
	StreamOverflows uint64   `json:"stream_overflows"` // roll: STATUS_A.OVERFLOW seen (the drain fell behind the ring)

	// A web page's decoded capture holds the acquisition (decoded_capture.go).
	DecodedWeb bool `json:"decoded_web,omitempty"`
	// DecodedStreamMode says how the decoded stream runs: gap-free from the
	// FPGA's events or the continuous stream, or captures (soft_stream.go).
	DecodedStreamMode string `json:"decoded_stream_mode,omitempty"`

	// TdivMaxS is the slowest timebase the loaded image shows whole (0: any):
	// an image recording raw samples only holds 2.1 ms (the shipping images
	// all decimate).
	TdivMaxS float64 `json:"tdiv_max_s,omitempty"`

	// Zone trigger + mask testing (docs/zonemask-plan.md)
	ZoneMode    int   `json:"zone_mode,omitempty"`  // 0 off, 1 trigger
	ZoneCount   int   `json:"zone_count,omitempty"` // installed zones
	ZoneSkip    int64 `json:"zone_skip,omitempty"`  // zone-armed publishes that were untestable (env/roll)
	MaskMode    int   `json:"mask_mode,omitempty"`  // 0 off, 1 test, 2 stop-on-fail
	MaskPass    int64 `json:"mask_pass,omitempty"`
	MaskFail    int64 `json:"mask_fail,omitempty"`
	MaskSkip    int64 `json:"mask_skip,omitempty"`    // frames not comparable (scale/mode changed)
	MaskRing    int   `json:"mask_ring,omitempty"`    // captured failures available
	MaskStopped bool  `json:"mask_stopped,omitempty"` // stop-on-fail latched
	MaskSet     bool  `json:"mask_set,omitempty"`     // an envelope mask is installed
	// MaskGen counts the mask's installs and clears: a client that installed
	// one knows whether it is still the one installed
	MaskGen uint64 `json:"mask_gen,omitempty"`

	// Serial / protocol trigger (serialtrig.go)
	SerialMode    int    `json:"serial_mode,omitempty"` // 0 off, 1 armed
	SerialBackend string `json:"serial_backend,omitempty"`
	// TriggerBackend is a pulse, slope or video trigger's decider:
	// "hardware-pulse", or "none: <why>" while the FPGA cannot take it.
	TriggerBackend string `json:"trigger_backend,omitempty"`
	// ZoneBackend is the zone trigger's decider: "hardware", or "none: <why>".
	ZoneBackend string `json:"zone_backend,omitempty"`
	// Image names the loaded acquisition image: general or packet.
	Image         string `json:"image,omitempty"`
	SerialMatches int64  `json:"serial_matches,omitempty"` // frames that matched the pattern
	// SymbolMatches is the symbol trigger's matches during the last record
	// (from its arm to its freeze; -1 unknown, absent without the block).
	SymbolMatches int64   `json:"symbol_matches,omitempty"`
	SymbolArmedS  float64 `json:"symbol_armed_s,omitempty"` // that record's arm to freeze
	SerialSet     bool    `json:"serial_set,omitempty"`     // a match pattern is configured

	// Converter health (adc_health.go): ADCFault names the converters that
	// stay stuck after the recoveries (empty while every core converts);
	// ADCRecoveries counts stuck cores brought back and ADCRecovery says how
	// the last were (an encode restart or an image reload) and after what;
	// ADCCheckMs is the last check's duration.
	ADCFault      string  `json:"adc_fault,omitempty"`
	ADCRecoveries int64   `json:"adc_recoveries,omitempty"`
	ADCRecovery   string  `json:"adc_recovery,omitempty"`
	ADCCheckMs    float64 `json:"adc_check_ms,omitempty"`

	// FRA / Bode plot (bode.go)
	BodeMode     int     `json:"bode_mode,omitempty"`      // 0 off, 1 armed
	BodeRef      int     `json:"bode_ref"`                 // the reference (DUT input) channel, 0 = C1
	BodeDut      int     `json:"bode_dut"`                 // the DUT output channel
	BodePoints   int     `json:"bode_points,omitempty"`    // accumulated curve size
	BodeValid    bool    `json:"bode_valid,omitempty"`     // the live point is valid
	BodeFreqHz   float64 `json:"bode_freq_hz,omitempty"`   // live point frequency
	BodeGainDB   float64 `json:"bode_gain_db,omitempty"`   // live point magnitude (dB)
	BodePhaseDeg float64 `json:"bode_phase_deg,omitempty"` // live point phase (deg)
	BodeWhy      string  `json:"bode_why,omitempty"`       // why the last capture gave no point
}

// TdivSet is the timebase setting: the one asked for (TdivSetS), or TdivS
// from a snapshot that does not name one.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-212
func (s Stats) TdivSet() float64 {
	if s.TdivSetS > 0 {
		return s.TdivSetS
	}
	return s.TdivS
}

// TrigQual reports the pulse and slope qualifiers (levels as fractions of the
// span, widths and times in ns, conditions any/<min/>max/in), so every
// surface shows and edits the values the engine uses.
type TrigQual struct {
	VideoStd   int     `json:"video_std"`
	VideoLine  int     `json:"video_line"`
	VideoNeg   bool    `json:"video_neg"`
	PulseLvl   float64 `json:"pulse_lvl"`
	PulseMinNs float64 `json:"pulse_min_ns"`
	PulseMaxNs float64 `json:"pulse_max_ns"`
	PulseCond  int     `json:"pulse_cond"`
	SlopeLo    float64 `json:"slope_lo"`
	SlopeHi    float64 `json:"slope_hi"`
	SlopeMinNs float64 `json:"slope_min_ns"`
	SlopeMaxNs float64 `json:"slope_max_ns"`
	SlopeCond  int     `json:"slope_cond"`
}
