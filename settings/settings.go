// Package settings is the persisted instrument setup: its shape, the file
// a scope restores its last setup from at power-on (Load, Save, Saver), the
// setup memories (Slots), the slice of it the UI model owns (ViewState),
// and the paths that collect it from and apply it to the instrument's
// controls (Collect, Apply). Every field is comparable, so a saver detects
// a change with plain ==. The file's bytes are the SDS1000CML+'s as they
// have always been (ADR-HAL-SETTINGS-FORMAT-KEPT): what a scope with more
// channels or a level in volts adds is left out when it is zero.
// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS
package settings

import "github.com/labeth/open-dso-core/instrument"

// MaxChannels is the most analog channels a setup keeps.
const MaxChannels = 4

// Version is the schema version written to (and required from) the file.
// Bump it and add a migration in Parse when the shape changes.
const Version = 1

// Channel is one vertical channel's persisted setup.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Channel struct {
	VdivV     float64 `json:"vdiv_v"`        // volts/div (must be a ladder detent to restore)
	OffsetV   float64 `json:"offset_v"`      // input-referred offset volts
	OffsetSet bool    `json:"offset_set"`    // false = boot-inherited offset untouched (never restore 0 V over it)
	Coupling  int     `json:"coupling"`      // 0=DC 1=AC 2=GND (dsp.Couple*)
	Probe     float64 `json:"probe"`         // probe attenuation: 1, 10 or 100
	BWL       bool    `json:"bwl,omitempty"` // 20 MHz bandwidth limit
}

// Trigger is the persisted trigger setup.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Trigger struct {
	Qualifiers instrument.TrigQual `json:"qualifiers"`
	LevelCode  int                 `json:"level_code"` // the board's trigger-level word (the SDS1000CML+'s comparator DAC code); 0 = untouched
	// LevelV is the level in volts at the probe tip, for a board whose
	// level is volts in the instrument state (LevelSet marks it saved: 0 V
	// is a level).
	LevelV   float64 `json:"level_v,omitempty"`
	LevelSet bool    `json:"level_set,omitempty"`
	Rising   bool    `json:"rising"`             // edge slope
	Source   int     `json:"source"`             // 0=C1 1=C2
	Type     int     `json:"type"`               // 0=edge 1=pulse 2=slope 3=video
	Norm     bool    `json:"norm"`               // trigger mode: true=NORM false=AUTO
	HoldoffS float64 `json:"holdoff_s"`          // 0 = off
	PosFrac  float64 `json:"pos_frac,omitempty"` // trigger position on screen; 0 = not saved
}

// Acq is the persisted acquisition mode.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Acq struct {
	PrecisionRateHz float64 `json:"precision_rate_hz,omitempty"`
	Mode            int     `json:"mode"` // 0=normal 1=average 2=eres 3=peak
	AvgCount        int     `json:"avg_count"`
	EresLen         int     `json:"eres_len"`
	RollStream      bool    `json:"roll_stream,omitempty"` // roll on the stream image (gap-free)
	MemDepth        int     `json:"mem_depth,omitempty"`   // requested depth (samples); 0 = not saved
}

// Decode is the device protocol-decode setup (controller-owned; historically
// reset to Off on every app restart).
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Decode struct {
	Proto  int  `json:"proto"` // 0=off 2=uart 3=i2c 4=spi 5..53 the other decoders (framecalc's numbers); 1, Auto, loads as a detection
	Baud   int  `json:"baud"`
	ChA    int  `json:"cha"` // UART source; I2C SCL; SPI CLK (0=C1 1=C2)
	ChB    int  `json:"chb"` // I2C SDA; SPI DATA
	CPOL   bool `json:"cpol"`
	CPHA   bool `json:"cpha"`
	LSB    bool `json:"lsb,omitempty"` // SPI least significant bit first
	Format int  `json:"format"`        // 0=hex 1=ascii 2=both
	// SPI's data delay after the sampling edge (ns), or Auto (the data's eye
	// measured); Fine: the FPGA's receivers on the samples at any rate.
	DelayNs   float64 `json:"delay_ns,omitempty"`
	DelayAuto bool    `json:"delay_auto,omitempty"`
	Fine      bool    `json:"fine,omitempty"`
	// Decoded-stream trigger (BUS ▸ Stream): 0=error 1=value 2=manual, and
	// the value it waits for.
	StreamTrig  int `json:"stream_trig,omitempty"`
	StreamValue int `json:"stream_value,omitempty"`
	// Parity is Modbus RTU's framing ("even", "odd", "none"; "" found by the
	// decoder); Inverted, an IR or DALI line's marks at its high level.
	Parity   string `json:"parity,omitempty"`
	Inverted bool   `json:"inverted,omitempty"`
	// FrameBits is SIRC's, Wiegand's or a T55xx command's frame length, or
	// the variant detected (an RC code's trits or bytes, Oregon's version,
	// the PJON mode, the IR multi protocol); 0: not known.
	FrameBits int `json:"frame_bits,omitempty"`
	// Layer is the bus's application layer (analysis.Layers' ID); the I2C
	// filter layer's address is LayerAddr less one (0: any) and its
	// direction LayerDir (0 both, 1 read, 2 write).
	Layer     string `json:"layer,omitempty"`
	LayerAddr int    `json:"layer_addr,omitempty"`
	LayerDir  int    `json:"layer_dir,omitempty"`
}

// Settings is the whole persisted setup. Every field is comparable, so the
// saver detects change with plain ==.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Settings struct {
	Version int        `json:"version"`
	TdivS   float64    `json:"tdiv_s"`
	Ch      [2]Channel `json:"ch"`
	// ChMore are the channels past C2 (C3, C4) of a scope that has them;
	// a channel never saved has VdivV 0.
	ChMore   [MaxChannels - 2]Channel `json:"ch_more,omitzero"`
	VertSet  bool                     `json:"vert_set"` // vertical front end was driven; false = keep the seed-don't-emit boot rule
	Trigger  Trigger                  `json:"trigger"`
	Acq      Acq                      `json:"acq"`
	Decode   Decode                   `json:"decode"`
	ViewMode int                      `json:"view_mode"` // 0=Y-T 1=X-Y 2=FFT 3=Bode 4=Spgm
	Display  Display                  `json:"display"`
	UI       UI                       `json:"ui"`
	Serial   Serial                   `json:"serial"`
	Bode     Bode                     `json:"bode,omitzero"`
	Sound    Sound                    `json:"sound,omitzero"`
	Screen   Screen                   `json:"screen,omitzero"` // the instrument's, never in a saved setup
}

// Bode is the frequency response's reference (DUT input) and DUT output
// channels, 0 = C1 (the VIEW tab's Bode rows). Set marks a saved block: an
// older file has none and keeps C1 / C2.
// TRLC-LINKS: REQ-DSO-066, REQ-DSO-089
type Bode struct {
	Set bool `json:"set"`
	Ref int  `json:"ref"`
	Dut int  `json:"dut"`
}

// Sound is SOUND's settings (ADR-SOUND): the tuner's reference, channel,
// range and smoothing, the meters' 0 dB, the LCD's readout page, the cycles
// drawn, A/B's input, the envelope's trigger, the V/oct and drift jobs and a
// triggered take's pre- and post-trigger times and trigger.
// Set marks a saved block (an older file has none and keeps the power-on
// settings).
// TRLC-LINKS: REQ-DSO-273, REQ-DSO-089
type Sound struct {
	Set      bool    `json:"set"`
	A4       float64 `json:"a4"`
	Ch       int     `json:"ch"`
	Ref      string  `json:"ref"`
	Range    string  `json:"range,omitempty"`
	Slow     bool    `json:"slow,omitempty"`
	Page     int     `json:"page"`
	Cycles   int     `json:"cycles"`
	ABIn     int     `json:"ab_in"`
	EnvCV    bool    `json:"env_cv,omitempty"`
	Gate     int     `json:"gate"`
	GateV    float64 `json:"gate_v"`
	VOctAuto bool    `json:"voct_auto,omitempty"`
	Drift    bool    `json:"drift,omitempty"`
	PreS     float64 `json:"pre_s,omitempty"`
	PostS    float64 `json:"post_s,omitempty"`
	TrigGate bool    `json:"trig_gate,omitempty"`
}

// Screen is the screen saver (ADR-SCREEN-SAVER): minutes without input
// before the saver and before the screen goes off, 0 for never. It belongs
// to the instrument, so the setup memories leave it out and their recall
// leaves it as it is. Set marks a saved block (an older file has none and
// keeps the factory times).
// TRLC-LINKS: REQ-DSO-221, REQ-DSO-089
type Screen struct {
	Set      bool `json:"set"`
	SaverMin int  `json:"saver_min"`
	OffMin   int  `json:"off_min"`
}

// Serial is the serial trigger: its match as the panel writes it ("55 AA",
// "A? ??"; empty = any byte), the I2C address and direction it waits for, and
// whether it is armed. Set marks a saved block: an older file has none and
// boots with the trigger disarmed.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-089
type Serial struct {
	Set   bool   `json:"set"`
	Armed bool   `json:"armed"`
	Match string `json:"match,omitempty"`
	Addr  int    `json:"addr"` // -1 = any address
	RW    int    `json:"rw"`   // 0 write, 1 read, 2 either
}

// UI is where the panel's task workspaces were (ADR-TASK-WORKSPACES): the
// workspace, each one's view, and the panel-held choices beside them, so a
// restart or a recalled setup returns there. Set marks a saved block (an
// older file has none and keeps the power-on workspaces).
// TRLC-LINKS: REQ-DSO-211, REQ-DSO-089
type UI struct {
	Set       bool   `json:"set"`
	Workspace int    `json:"workspace"`            // 0 SCOPE, 1 BUS, 2 SPECTRUM, 3 MEASURE, 4 COMPARE
	Views     [5]int `json:"views"`                // each workspace's display view
	BusEye    bool   `json:"bus_eye,omitempty"`    // BUS on the eye diagram
	BusStream bool   `json:"bus_stream,omitempty"` // BUS on its Stream view (the stream starts again)
	BusVisual bool   `json:"bus_visual,omitempty"` // BUS on its Visual view
	MeasAll   bool   `json:"meas_all,omitempty"`   // MEASURE on all values
	// Meas holds the chosen readouts in order: channel*32 + parameter + 1
	// (0 = none).
	Meas          [12]int `json:"meas"`
	FFTSource     int     `json:"fft_source,omitempty"`
	MasterDepth   int     `json:"master_depth,omitempty"`
	RemoteNotices bool    `json:"remote_notices"`
	// The mask build's frames and tolerance, and super-res's channel, grid and
	// stop, kept over a restart. Zero is the factory value (an older file);
	// the indexes are stored plus one.
	MaskFrames int     `json:"mask_frames,omitempty"`
	MaskTol    int     `json:"mask_tol,omitempty"` // preset index + 1
	SrCh       int     `json:"sr_ch,omitempty"`    // channel + 1
	SrGrid     int     `json:"sr_grid,omitempty"`
	SrStop     int     `json:"sr_stop,omitempty"` // stop mode + 1
	SrTarget   float64 `json:"sr_target,omitempty"`
}

// Display is the LCD's display setup beyond the view mode. Zero values are the
// power-on state (both channels shown), so an older file reads as defaults.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Display struct {
	MathMode int     `json:"math_mode"` // 0 off, 1 C1+C2, 2 C1-C2, 3 C2-C1, 4 C1xC2
	ShowMeas bool    `json:"show_meas"`
	Persist  bool    `json:"persist"`
	Hidden   [2]bool `json:"hidden"` // channel trace off (DISPLAY CH1/CH2)
	// HiddenMore are the channels past C2 drawn off (C3, C4). MathCh are
	// math's operands and XYCh X-Y's axes, each channel stored plus one;
	// zero is C1 and C2, the only pair a two-channel scope has, so its file
	// keeps its bytes.
	HiddenMore [MaxChannels - 2]bool `json:"hidden_more,omitzero"`
	MathCh     [2]int                `json:"math_ch,omitzero"`
	XYCh       [2]int                `json:"xy_ch,omitzero"`
	// Set marks the blocks below (an older file has none: invert off, no
	// zoom, the cursors where they were). A recalled setup brings them back.
	Set    bool    `json:"set,omitempty"`
	Invert [2]bool `json:"invert"` // the channel drawn upside down (the CH tabs' Invert)
	// InvertMore are the channels past C2 drawn upside down (C3, C4).
	InvertMore [MaxChannels - 2]bool `json:"invert_more,omitzero"`
	CurMode    int                   `json:"cur_mode"` // the cursors: 0 off, 1 time, 2 volts
	CurX       [2]float64            `json:"cur_x"`    // A and B, screen fractions across
	CurY       [2]float64            `json:"cur_y"`    // A and B, screen fractions down
	// The cursors anchored to the signal (CurAt; an older file has only
	// their screen places): times from the trigger (s), volts at the tip of
	// channel CurVCh.
	CurAt   bool       `json:"cur_at,omitempty"`
	CurT    [2]float64 `json:"cur_t"`
	CurV    [2]float64 `json:"cur_v"`
	CurVCh  int        `json:"cur_v_ch"`
	CurSel  int        `json:"cur_sel"`  // the cursor Adjust moves: 0 A, 1 B
	Zoom    int        `json:"zoom"`     // horizontal magnification (1 none)
	ZoomOff float64    `json:"zoom_off"` // the zoom window's pan
}

// ViewState is the controller-owned slice of the setup: the device decode
// config and the display view mode.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type ViewState struct {
	ViewMode int
	Decode   Decode
	Display  Display
	UI       UI
	Serial   Serial
	Screen   Screen
	Sound    Sound
}
