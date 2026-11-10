// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
)

// The status document's v2 keys (WEB-API-DESIGN §2.4), beside the flat
// fields the page has always read.

// RunStatus is the acquisition's run state.
type RunStatus struct {
	Running bool    `json:"running"`
	Single  bool    `json:"single"`
	Mode    string  `json:"mode"` // auto, normal, single
	State   string  `json:"state"`
	FPS     float64 `json:"fps"`
	Seq     uint64  `json:"seq"`
}

// TimeStatus is the timebase.
type TimeStatus struct {
	TdivS      float64 `json:"tdiv_s"`
	DisplayedS float64 `json:"displayed_s"`
	PosFrac    float64 `json:"pos_frac"`
	DelayS     float64 `json:"delay_s"`
	HoldoffS   float64 `json:"holdoff_s"`
	Depth      int     `json:"depth"`
}

// TrigStatus is the trigger: the running type, its source, slope and
// level, and every setting of the type by id (TrigParams; a per-channel
// setting as one value a channel). Refused is the board's last refusal of
// a commit, with its count: a setting the board did not take.
type TrigStatus struct {
	Type       string         `json:"type"`
	Source     int            `json:"source"`
	Slope      string         `json:"slope"`
	LevelV     float64        `json:"level_v"`
	Params     map[string]any `json:"params,omitempty"`
	Refused    string         `json:"refused,omitempty"`
	RefusedSeq uint64         `json:"refused_seq,omitempty"`
}

// AcqStatus is how the frames are acquired.
type AcqStatus struct {
	Mode    string `json:"mode"`
	Content string `json:"content,omitempty"`
}

// ChannelStatus is one channel's front end, at the probe tip.
type ChannelStatus struct {
	Ch       int     `json:"ch"`
	On       bool    `json:"on"`
	Vdiv     float64 `json:"vdiv"`
	Probe    float64 `json:"probe"`
	Coupling string  `json:"coupling"`
	OffsetV  float64 `json:"offset_v"`
	BWLimit  float64 `json:"bw_limit_hz"`
	Invert   bool    `json:"invert"`
}

// fpsMeter is the published frame rate, from the fan-out's seq between
// status polls.
type fpsMeter struct {
	mu   sync.Mutex
	seq  uint64
	at   time.Time
	rate float64
}

// sample updates the rate with the newest seq.
// TRLC-LINKS: REQ-DSO-164
func (m *fpsMeter) sample(seq uint64, now time.Time) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dt := now.Sub(m.at).Seconds(); !m.at.IsZero() && dt >= 0.25 && seq >= m.seq {
		r := float64(seq-m.seq) / dt
		if m.rate == 0 {
			m.rate = r
		} else {
			m.rate = 0.5*m.rate + 0.5*r
		}
		m.seq, m.at = seq, now
	} else if m.at.IsZero() || seq < m.seq {
		m.seq, m.at = seq, now
	}
	return m.rate
}

// status is the shared status document: the flat fields the page reads
// (channel by number: vdiv1, probe3, …) and the v2 keys.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) status() any {
	st := s.state()
	p := s.profile()
	var seq uint64
	var f0 struct {
		tdiv, displayed float64
		content, band   string
		winCols, depth  int
	}
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f == nil {
			return
		}
		seq = f.Seq
		f0.tdiv, f0.displayed, f0.winCols, f0.depth = f.Snapshot.TdivS, f.Snapshot.DisplayedS, f.WinPoints, f.Len()
		if f.Content != 0 {
			f0.content = f.Content.String()
			f0.band = bandOf(f)
		}
	})
	fps := s.fps.sample(seq, time.Now())
	tdiv := f0.tdiv
	if !(tdiv > 0) {
		tdiv = st.TimeDivS
	}
	displayed := f0.displayed
	if !(displayed > 0) {
		displayed = tdiv
	}
	run := "Stop"
	if s.cfg.Instrument != nil {
		run = s.cfg.Instrument.Acquisition().String()
	}
	pos := posFrac(st, s.divsH())
	out := map[string]any{
		"version": s.cfg.Version, "seq": seq, "fps": fps, "frames": seq, "published": seq,
		"running": st.Running, sweepSingle: st.Running && st.Single, "norm": st.Sweep == hal.NormalSweep,
		"tdiv_s": tdiv, "tdiv_set_s": st.TimeDivS, "displayed_sdiv_s": displayed,
		"tdivs": p.Timebase.TdivS, "full_rate_tdiv_s": p.Timebase.FullRateTdivS,
		"trig_pos_frac": pos, "holdoff_s": st.Trigger.Active().HoldoffS,
		"trig_source": st.Trigger.Source().Index, "trig_rising": st.Trigger.Slope() != hal.Falling,
		"trig_volts": st.Trigger.LevelV(), "trig_type": int(st.Trigger.Type), "trig_qual": st.Qual,
		"acq_mode": acqModeNumber(st), "mem_depth": st.Depth, "win_cols": f0.winCols,
		"band": f0.band, "live_readout": f0.content, "vdiv_live": true, "run_state": run,
		// the screen's record takes its span to fill: a page waits that long
		// for a frame at a slow timebase that does not roll
		"record_s": st.TimeDivS * float64(s.divsH()),
	}
	// what decides a pulse, slope, video or serial trigger, or why nothing
	// can yet (the page says so beside the trigger)
	if s.cfg.Instrument != nil {
		snap := s.cfg.Instrument.Snapshot()
		if snap.Counter != nil {
			out["counter"], out["counter_text"] = snap.Counter, snap.Counter.Text()
		}
		out["trigger_backend"], out["serial_backend"] = snap.TriggerBackend, snap.SerialBackend
		out["serial_mode"], out["serial_set"] = snap.SerialMode, snap.SerialSet
	}
	// the UI model's panel snapshot: what the display shows, and whether an
	// Autoset sweep runs, which the page waits out before its next command
	if ps, ok := s.cfg.UI.(webserver.UIStatus); ok {
		v := ps.PanelStatus()
		out["panel"] = &v
	}
	if s.cfg.Board != nil {
		out["cal_source"] = s.cfg.Board.FrontEnd().Calibration().Source
		if s.cfg.Board.Platform() != nil {
			if d := s.cfg.Board.Platform().Diag(); len(d) > 0 && json.Valid(d) {
				out["diag"] = d
			}
		}
		if len(st.Channels) > 0 {
			var vdivs []float64
			for _, step := range s.cfg.Board.FrontEnd().Ladder(0) {
				vdivs = append(vdivs, step.VoltsDiv)
			}
			out["vdivs"] = vdivs
		}
	}
	chans := make([]ChannelStatus, len(st.Channels))
	for i, ch := range st.Channels {
		k := strconv.Itoa(i + 1)
		probe := ch.Probe
		if !(probe > 0) {
			probe = 1
		}
		out["vdiv"+k], out["probe"+k], out["cpl"+k], out["off"+k+"_v"] = ch.VoltsDiv, probe, int(ch.Coupling), ch.OffsetV
		out["zoom"+k], out["bwl"+k], out["inv"+k], out["on"+k] = 1, boolInt(ch.BWLimit > 0), ch.Invert, ch.On
		chans[i] = ChannelStatus{
			Ch: i, On: ch.On, Vdiv: ch.VoltsDiv, Probe: probe, Coupling: couplingName(int(ch.Coupling)),
			OffsetV: ch.OffsetV, BWLimit: ch.BWLimit, Invert: ch.Invert,
		}
	}
	slope := "rising"
	switch st.Trigger.Slope() { //nolint:exhaustive // rising is the default
	case hal.Falling:
		slope = "falling"
	case hal.Either:
		slope = "either"
	}
	out["run"] = RunStatus{
		Running: st.Running, Single: st.Running && st.Single, Mode: sweepName(st.BoardSweep()),
		State: run, FPS: fps, Seq: seq,
	}
	out["time"] = TimeStatus{TdivS: tdiv, DisplayedS: displayed, PosFrac: pos, DelayS: st.DelayS, HoldoffS: st.Trigger.Active().HoldoffS, Depth: st.Depth}
	trig := TrigStatus{
		Type: trigTypeName(st), Source: st.Trigger.Source().Index, Slope: slope, LevelV: st.Trigger.LevelV(),
		Params: trigParamValues(st),
	}
	trig.Refused, trig.RefusedSeq = s.refusals.last()
	out["trig"] = trig
	out["acq"] = AcqStatus{Mode: acqModeName(st.Mode), Content: f0.content}
	out["channels"] = chans
	if mr, ok := s.cfg.Parts.Masks.(MaskRunner); ok {
		k := mr.Counts()
		out["mask_mode"], out["mask_pass"], out["mask_fail"], out["mask_skip"] = k.Mode, k.Pass, k.Fail, k.Skip
		out["mask_ring"], out["mask_stopped"], out["mask_set"], out["mask_gen"] = k.Ring, k.Stopped, k.Set, k.Gen
		out["features"] = map[string]any{"mask": map[string]any{"mode": k.Mode, "pass": k.Pass, "fail": k.Fail, "gen": k.Gen}}
	}
	return out
}

// bandOf is the newest frame's band as the page names what a frame holds:
// "native-fast" every converter sample, "decimated" evenly sampled, below the
// full rate, "envelope" min/max pairs. The page's eye and super-resolution
// take the first two (they fold sampled records).
// TRLC-LINKS: REQ-DSO-164
func bandOf(f *sample.Frame) string {
	switch {
	case f.Content == sample.PeakPairs || f.Flags.Has(sample.Envelope):
		return "envelope"
	case f.Content == sample.RawFullRate:
		return "native-fast"
	case f.Content == sample.RawDecimated:
		return "decimated"
	}
	return f.Content.String()
}

// trigTypeName is the trigger state s runs, by name.
// TRLC-LINKS: REQ-DSO-164
func trigTypeName(s instrument.State) string {
	if s.SerialMode != 0 {
		return "serial"
	}
	return s.Trigger.Type.String()
}

// sweepSingle is the single-shot sweep's name.
const sweepSingle = "single"

// TRLC-LINKS: REQ-DSO-164
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// sweepName is a sweep as the status names it.
// TRLC-LINKS: REQ-DSO-164
func sweepName(s hal.Sweep) string {
	switch s { //nolint:exhaustive // auto is the default
	case hal.NormalSweep:
		return "normal"
	case hal.Single:
		return sweepSingle
	}
	return "auto"
}

// acqModeNumber is state st's mode as v1's acq_mode numbers it
// (acqModes; 4 is precision, high resolution from the precision words).
// TRLC-LINKS: REQ-DSO-164
func acqModeNumber(st instrument.State) int {
	if st.Mode == hal.HighRes && st.Precision {
		return 4
	}
	for n, v := range acqModes {
		if v == st.Mode {
			return n
		}
	}
	return 0
}

// acqModeName is a mode as the status and caps name it.
// TRLC-LINKS: REQ-DSO-164
func acqModeName(m hal.AcqMode) string {
	switch m { //nolint:exhaustive // normal is the default
	case hal.PeakDetect:
		return "peak"
	case hal.Average:
		return "average"
	case hal.HighRes:
		return "eres"
	case hal.RollMode:
		return "roll"
	case hal.Stream:
		return "stream"
	}
	return "normal"
}

// trigParamValues is every setting of the running trigger type by id, a
// per-channel one as a list over the channels.
// TRLC-LINKS: REQ-DSO-164, REQ-DSO-011
func trigParamValues(st instrument.State) map[string]any {
	t := st.Trigger.Type
	out := map[string]any{}
	for _, p := range instrument.TrigParams(t) {
		if p.PerChannel {
			vals := make([]any, len(st.Channels))
			for ch := range st.Channels {
				v, _ := st.TrigParam(t, p.ID, ch)
				vals[ch] = paramJSON(t, p.ID, v)
			}
			out[p.ID] = vals
			continue
		}
		v, _ := st.TrigParam(t, p.ID, 0)
		out[p.ID] = paramJSON(t, p.ID, v)
	}
	return out
}

// refusals keeps the board's last refusal of a commit for the status.
type refusals struct {
	mu   sync.Mutex
	text string
	n    uint64
}

// note records a refusal.
// TRLC-LINKS: REQ-DSO-164
func (r *refusals) note(err error) {
	r.mu.Lock()
	r.text, r.n = refusalText(err), r.n+1
	r.mu.Unlock()
}

// last is the last refusal and how many there were.
// TRLC-LINKS: REQ-DSO-164
func (r *refusals) last() (string, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text, r.n
}
