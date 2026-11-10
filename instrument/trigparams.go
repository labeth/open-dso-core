// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
)

// ParamKind is how a trigger setting's value is given.
type ParamKind int

// The kinds of trigger settings.
const (
	ParamNumber ParamKind = iota // Num, in the setting's unit
	ParamInt                     // Num, a whole number
	ParamChoice                  // Text, one of Choices (Num: its index)
	ParamBool                    // Num 0 or 1 (Text "on"/"off")
	ParamSource                  // Text, a source name: C1 …, LINE, EXT, D0 … (SourceName)
	ParamStates                  // Text, H, L or X per input or bit, comma separated
)

// TrigParam is one setting of a trigger type, as every surface edits it:
// the UI model's rows, the web's controls and a SCPI dialect's headers
// all name it by ID and pass it a ParamValue. Settings are structural:
// whether a value is in the board's range is the board's to refuse when
// the trigger is programmed.
type TrigParam struct {
	ID         string    `json:"id"`
	Kind       ParamKind `json:"kind"`
	Unit       string    `json:"unit,omitempty"`
	Choices    []string  `json:"choices,omitempty"`
	PerChannel bool      `json:"per_channel,omitempty"` // the value is per analog channel (ParamValue.Ch)
}

// ParamValue is a trigger setting's value. Num carries numbers, booleans
// and a choice's index; Text carries choices, sources and state lists;
// Ch is the channel of a per-channel setting.
type ParamValue struct {
	Num  float64
	Text string
	Ch   int
}

// paramDef is a setting with its access to a configuration.
type paramDef struct {
	TrigParam
	get func(c *TrigConfig, ch int) ParamValue
	set func(c *TrigConfig, v ParamValue, channels int) error
}

// The choices settings name.
var (
	limitChoices    = []string{"less", "greater", "inside", "outside"}
	limitOps        = []hal.QualOp{hal.Shorter, hal.Longer, hal.Inside, hal.Outside}
	slopeChoices    = []string{"rising", "falling", "either"}
	couplingChoices = []string{"dc", "ac", "lf_reject", "hf_reject"}
	couplings       = []hal.TrigCoupling{hal.TrigDC, hal.TrigAC, hal.TrigLFReject, hal.TrigHFReject}
	videoStandards  = []string{"ntsc", "pal", "720p50", "720p60", "1080p50", "1080p60", "1080i50", "1080i60", "custom"}
	logicChoices    = []string{"and", "or", "nand", "nor"}
	compareChoices  = []string{"equal", "greater", "less"}
	orderChoices    = []string{"lsb", "msb"}
)

// errNoParam is a setting a type does not have.
var errNoParam = errors.New("no such trigger setting")

// TrigParams are type t's settings, in the order a surface lists them.
// TRLC-LINKS: REQ-DSO-011
func TrigParams(t TrigType) []TrigParam {
	defs := trigParamDefs(t)
	out := make([]TrigParam, len(defs))
	for i, d := range defs {
		out[i] = d.TrigParam
	}
	return out
}

// TrigParam reads setting id of type t (channel ch for a per-channel one).
// TRLC-LINKS: REQ-DSO-011
func (s State) TrigParam(t TrigType, id string, ch int) (ParamValue, error) {
	return s.Trigger.param(t, id, ch)
}

// SetTrigParam sets setting id of type t, whether t runs or not: each type
// keeps its settings. A value that is not one of the setting's (a choice it
// lacks, a source the scope does not have) is refused with the reason.
// TRLC-LINKS: REQ-DSO-011
func (s *State) SetTrigParam(t TrigType, id string, v ParamValue) error {
	return s.Trigger.setParam(t, id, v, len(s.Channels))
}

// param reads setting id of type t.
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) param(tt TrigType, id string, ch int) (ParamValue, error) {
	d, c, err := t.def(tt, id)
	if err != nil {
		return ParamValue{}, err
	}
	// A getter may fill in a configuration it reads (a time condition, a
	// pattern term): it reads a copy, so a read never writes the state.
	cp := *c
	cp.Stages = make([]hal.TrigStage, len(c.Stages))
	for i, st := range c.Stages {
		cp.Stages[i] = cloneStage(st)
	}
	if len(cp.Stages) == 0 {
		cp = DefaultTrigConfig(tt, max(ch+1, 1))
	}
	return d.get(&cp, ch), nil
}

// setParam sets setting id of type tt.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) setParam(tt TrigType, id string, v ParamValue, channels int) error {
	d, c, err := t.def(tt, id)
	if err != nil {
		return err
	}
	if d.PerChannel && (v.Ch < 0 || v.Ch >= channels) {
		return fmt.Errorf("%s %s: no channel C%d", tt, id, v.Ch+1)
	}
	if d.Kind == ParamChoice {
		if v.Text != "" {
			i := slices.IndexFunc(d.Choices, func(c string) bool { return strings.EqualFold(c, v.Text) })
			if i < 0 {
				return fmt.Errorf("%s %s: %q is not one of %s", tt, id, v.Text, strings.Join(d.Choices, ", "))
			}
			v.Num = float64(i)
		}
		if v.Num != math.Trunc(v.Num) || v.Num < 0 || int(v.Num) >= len(d.Choices) {
			return fmt.Errorf("%s %s: choice %g is not one of %s", tt, id, v.Num, strings.Join(d.Choices, ", "))
		}
		v.Text = d.Choices[int(v.Num)]
	}
	if (d.Kind == ParamNumber || d.Kind == ParamInt) && (math.IsNaN(v.Num) || math.IsInf(v.Num, 0)) {
		return fmt.Errorf("%s %s: not a number", tt, id)
	}
	if d.Kind == ParamInt && v.Num != math.Trunc(v.Num) {
		return fmt.Errorf("%s %s: %g is not a whole number", tt, id, v.Num)
	}
	if d.Kind == ParamBool && v.Text != "" {
		on, ok := map[string]bool{"on": true, "off": false, "true": true, "false": false, "1": true, "0": false}[strings.ToLower(v.Text)]
		if !ok {
			return fmt.Errorf("%s %s: %q is not on or off", tt, id, v.Text)
		}
		v.Num = b2f(on)
	}
	if len(c.Stages) == 0 {
		*c = DefaultTrigConfig(tt, channels)
	}
	switch id {
	case "source", "slope", "coupling", "noise_reject", "holdoff":
	default:
		c.Placed = false // the stage's own levels and limits now run
	}
	return d.set(c, v, channels)
}

// def is setting id of type tt and the configuration it edits.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) def(tt TrigType, id string) (paramDef, *TrigConfig, error) {
	if tt < 0 || tt >= NumTrigTypes {
		return paramDef{}, nil, fmt.Errorf("trigger type %d: %w", int(tt), errNoParam)
	}
	for _, d := range trigParamDefs(tt) {
		if d.ID == id {
			return d, &t.Configs[tt], nil
		}
	}
	return paramDef{}, nil, fmt.Errorf("the %s trigger has no %q setting: %w", tt, id, errNoParam)
}

// b2f is 1 for true.
// TRLC-LINKS: REQ-DSO-011
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// SourceName spells a trigger source: C1 …, D0 …, EXT, LINE.
// TRLC-LINKS: REQ-DSO-011
func SourceName(s hal.Source) string {
	switch s.Kind {
	case hal.Digital:
		return "D" + strconv.Itoa(s.Index)
	case hal.External:
		return "EXT"
	case hal.ACLine:
		return "LINE"
	case hal.Analog:
	}
	return "C" + strconv.Itoa(s.Index+1)
}

// ParseSource reads a source name; channels bounds the analog channels
// (negative: any). Case is ignored.
// TRLC-LINKS: REQ-DSO-011
func ParseSource(name string, channels int) (hal.Source, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	switch {
	case n == "LINE":
		return hal.Source{Kind: hal.ACLine}, nil
	case n == "EXT":
		return hal.Source{Kind: hal.External}, nil
	case strings.HasPrefix(n, "C"):
		if i, err := strconv.Atoi(n[1:]); err == nil && i >= 1 && (channels < 0 || i <= channels) {
			return hal.Source{Kind: hal.Analog, Index: i - 1}, nil
		}
	case strings.HasPrefix(n, "D"):
		if i, err := strconv.Atoi(n[1:]); err == nil && i >= 0 {
			return hal.Source{Kind: hal.Digital, Index: i}, nil
		}
	}
	return hal.Source{}, fmt.Errorf("%q is not a source of this scope", name)
}

// StatesText spells pattern states: H, L or X each, comma separated.
// TRLC-LINKS: REQ-DSO-011
func StatesText(st []hal.PatternState) string {
	var b strings.Builder
	for i, s := range st {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte("XLH"[min(max(int(s), 0), 2)])
	}
	return b.String()
}

// ParseStates reads H, L, X (or 1, 0, X) per input, comma separated.
// TRLC-LINKS: REQ-DSO-011
func ParseStates(text string) ([]hal.PatternState, error) {
	var out []hal.PatternState
	for f := range strings.SplitSeq(text, ",") {
		switch strings.ToUpper(strings.TrimSpace(f)) {
		case "H", "1":
			out = append(out, hal.High)
		case "L", "0":
			out = append(out, hal.Low)
		case "X", "":
			out = append(out, hal.DontCare)
		default:
			return nil, fmt.Errorf("%q is not H, L or X", f)
		}
	}
	return out, nil
}

// ---- the settings, per type ----

// mainStage is c's firing stage (the last).
// TRLC-LINKS: REQ-DSO-011
func mainStage(c *TrigConfig) *hal.TrigStage { return &c.Stages[len(c.Stages)-1] }

// firstStage is c's first stage (a delay's pattern).
// TRLC-LINKS: REQ-DSO-011
func firstStage(c *TrigConfig) *hal.TrigStage { return &c.Stages[0] }

// stageQual is a stage's time condition, made when it has none (an Op of
// zero is no condition, its limits kept).
// TRLC-LINKS: REQ-DSO-011
func stageQual(st *hal.TrigStage) *hal.Qualifier {
	if st.Qual == nil {
		st.Qual = &hal.Qualifier{}
	}
	return st.Qual
}

// srcParam is a source setting.
// TRLC-LINKS: REQ-DSO-011
func srcParam(id string, sel func(c *TrigConfig) *hal.Source, after func(c *TrigConfig)) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamSource},
		get:       func(c *TrigConfig, _ int) ParamValue { return ParamValue{Text: SourceName(*sel(c))} },
		set: func(c *TrigConfig, v ParamValue, channels int) error {
			src, err := ParseSource(v.Text, channels)
			if err != nil {
				return err
			}
			*sel(c) = src
			if after != nil {
				after(c)
			}
			return nil
		},
	}
}

// numParam is a number setting.
// TRLC-LINKS: REQ-DSO-011
func numParam(id, unit string, sel func(c *TrigConfig) *float64, after func(c *TrigConfig)) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamNumber, Unit: unit},
		get:       func(c *TrigConfig, _ int) ParamValue { return ParamValue{Num: *sel(c)} },
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			*sel(c) = v.Num
			if after != nil {
				after(c)
			}
			return nil
		},
	}
}

// intParam is a whole-number setting.
// TRLC-LINKS: REQ-DSO-011
func intParam(id string, sel func(c *TrigConfig) *int) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamInt},
		get:       func(c *TrigConfig, _ int) ParamValue { return ParamValue{Num: float64(*sel(c))} },
		set:       func(c *TrigConfig, v ParamValue, _ int) error { *sel(c) = int(v.Num); return nil },
	}
}

// boolParam is an on/off setting.
// TRLC-LINKS: REQ-DSO-011
func boolParam(id string, sel func(c *TrigConfig) *bool) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamBool},
		get: func(c *TrigConfig, _ int) ParamValue {
			on := *sel(c)
			return ParamValue{Num: b2f(on), Text: map[bool]string{true: "on", false: "off"}[on]}
		},
		set: func(c *TrigConfig, v ParamValue, _ int) error { *sel(c) = v.Num != 0; return nil },
	}
}

// choiceParam is a choice setting read and written by index.
// TRLC-LINKS: REQ-DSO-011
func choiceParam(id string, choices []string, get func(c *TrigConfig) int, set func(c *TrigConfig, i int)) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamChoice, Choices: choices},
		get: func(c *TrigConfig, _ int) ParamValue {
			i := min(max(get(c), 0), len(choices)-1)
			return ParamValue{Num: float64(i), Text: choices[i]}
		},
		set: func(c *TrigConfig, v ParamValue, _ int) error { set(c, int(v.Num)); return nil },
	}
}

// slopeParam is a stage's slope among choices (rising, falling[, either]).
// TRLC-LINKS: REQ-DSO-011
func slopeParam(id string, either bool, sel func(c *TrigConfig) *hal.TrigStage) paramDef {
	ch := slopeChoices[:2]
	if either {
		ch = slopeChoices
	}
	return choiceParam(id, ch, func(c *TrigConfig) int { return max(int(sel(c).Levels.Slope)-1, 0) },
		func(c *TrigConfig, i int) { sel(c).Levels.Slope = hal.Slope(i + 1) })
}

// limitParams are a stage's time condition and its limits.
// TRLC-LINKS: REQ-DSO-011
func limitParams(sel func(c *TrigConfig) *hal.TrigStage) []paramDef {
	return []paramDef{
		choiceParam("limit", limitChoices,
			func(c *TrigConfig) int { return max(slices.Index(limitOps, stageQual(sel(c)).Op), 0) },
			func(c *TrigConfig, i int) { stageQual(sel(c)).Op = limitOps[i] }),
		numParam("lower", "s", func(c *TrigConfig) *float64 { return &stageQual(sel(c)).MinS }, nil),
		numParam("upper", "s", func(c *TrigConfig) *float64 { return &stageQual(sel(c)).MaxS }, nil),
	}
}

// commonParams are every type's trigger-wide settings.
// TRLC-LINKS: REQ-DSO-011
func commonParams() []paramDef {
	return []paramDef{
		choiceParam("coupling", couplingChoices,
			func(c *TrigConfig) int { return max(slices.Index(couplings, c.Coupling), 0) },
			func(c *TrigConfig, i int) { c.Coupling = couplings[i] }),
		boolParam("noise_reject", func(c *TrigConfig) *bool { return &c.NoiseReject }),
		numParam("holdoff", "s", func(c *TrigConfig) *float64 { return &c.HoldoffS }, nil),
	}
}

// stageParams are a single-input stage's source, slope and level.
// TRLC-LINKS: REQ-DSO-011
func stageParams(sel func(c *TrigConfig) *hal.TrigStage, slope, either bool) []paramDef {
	out := []paramDef{srcParam("source", func(c *TrigConfig) *hal.Source { return &sel(c).Source }, nil)}
	if slope {
		out = append(out, slopeParam("slope", either, sel))
	}
	return append(out, numParam("level", "V", func(c *TrigConfig) *float64 { return &sel(c).Levels.High }, nil))
}

// lowLevel is a two-level stage's low level.
// TRLC-LINKS: REQ-DSO-011
func lowLevel(sel func(c *TrigConfig) *hal.TrigStage) paramDef {
	return numParam("level_low", "V", func(c *TrigConfig) *float64 { return &sel(c).Levels.Low }, nil)
}

// patternParams are a pattern stage's states, levels and logic.
// TRLC-LINKS: REQ-DSO-011
func patternParams(sel func(c *TrigConfig) *hal.TrigStage, logic bool) []paramDef {
	pat := func(c *TrigConfig) *hal.Pattern {
		st := sel(c)
		if st.Pattern == nil {
			st.Pattern = &hal.Pattern{}
		}
		return st.Pattern
	}
	term := func(c *TrigConfig, ch int) *hal.PatternTerm {
		p := pat(c)
		for i := range p.Terms {
			if t := &p.Terms[i]; t.Source.Kind == hal.Analog && t.Source.Index == ch {
				return t
			}
		}
		p.Terms = append(p.Terms, hal.PatternTerm{Source: hal.Source{Kind: hal.Analog, Index: ch}})
		return &p.Terms[len(p.Terms)-1]
	}
	out := []paramDef{
		{
			TrigParam: TrigParam{ID: "states", Kind: ParamStates},
			get: func(c *TrigConfig, _ int) ParamValue {
				var st []hal.PatternState
				for _, t := range pat(c).Terms {
					if t.Source.Kind != hal.Analog {
						continue
					}
					for len(st) <= t.Source.Index {
						st = append(st, hal.DontCare)
					}
					st[t.Source.Index] = t.State
				}
				return ParamValue{Text: StatesText(st)}
			},
			set: func(c *TrigConfig, v ParamValue, channels int) error {
				st, err := ParseStates(v.Text)
				if err != nil {
					return err
				}
				for ch := range channels {
					s := hal.DontCare
					if ch < len(st) {
						s = st[ch]
					}
					term(c, ch).State = s
				}
				return nil
			},
		},
		{
			TrigParam: TrigParam{ID: "pattern_level", Kind: ParamNumber, Unit: "V", PerChannel: true},
			get:       func(c *TrigConfig, ch int) ParamValue { return ParamValue{Num: term(c, ch).Level, Ch: ch} },
			set:       func(c *TrigConfig, v ParamValue, _ int) error { term(c, v.Ch).Level = v.Num; return nil },
		},
	}
	if logic {
		out = append(out, choiceParam("logic", logicChoices,
			func(c *TrigConfig) int { return int(pat(c).Logic) },
			func(c *TrigConfig, i int) { pat(c).Logic = hal.PatternLogic(i) }))
	}
	return out
}

// protoOf is configuration c's protocol trigger as type P, its zero value
// when it holds another.
// TRLC-LINKS: REQ-DSO-013
func protoOf[P hal.ProtoTrigger](c *TrigConfig) P {
	p, _ := firstStage(c).Proto.(P)
	return p
}

// protoEdit applies fn to c's protocol configuration of type P and keeps
// the stage's source and level on its data line.
// TRLC-LINKS: REQ-DSO-013
func protoEdit[P hal.ProtoTrigger](c *TrigConfig, line func(p *P) *prototrig.Line, fn func(p *P)) {
	p := protoOf[P](c)
	fn(&p)
	st := firstStage(c)
	st.Proto = p
	if l := line(&p); l != nil {
		st.Source, st.Levels.High = l.Source, l.Level
	}
}

// pSrc, pNum, pInt, pBool and pChoice are a protocol configuration's
// settings of each kind.
// TRLC-LINKS: REQ-DSO-013
func pSrc[P hal.ProtoTrigger](id string, line, sel func(p *P) *prototrig.Line) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamSource},
		get: func(c *TrigConfig, _ int) ParamValue {
			p := protoOf[P](c)
			return ParamValue{Text: SourceName(sel(&p).Source)}
		},
		set: func(c *TrigConfig, v ParamValue, channels int) error {
			src, err := ParseSource(v.Text, channels)
			if err == nil {
				protoEdit(c, line, func(p *P) { sel(p).Source = src })
			}
			return err
		},
	}
}

// TRLC-LINKS: REQ-DSO-013
func pNum[P hal.ProtoTrigger](id, unit string, line func(p *P) *prototrig.Line, sel func(p *P) *float64) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamNumber, Unit: unit},
		get:       func(c *TrigConfig, _ int) ParamValue { p := protoOf[P](c); return ParamValue{Num: *sel(&p)} },
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			protoEdit(c, line, func(p *P) { *sel(p) = v.Num })
			return nil
		},
	}
}

// TRLC-LINKS: REQ-DSO-013
func pInt[P hal.ProtoTrigger](id string, line func(p *P) *prototrig.Line, sel func(p *P) *int) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamInt},
		get:       func(c *TrigConfig, _ int) ParamValue { p := protoOf[P](c); return ParamValue{Num: float64(*sel(&p))} },
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			protoEdit(c, line, func(p *P) { *sel(p) = int(v.Num) })
			return nil
		},
	}
}

// TRLC-LINKS: REQ-DSO-013
func pBool[P hal.ProtoTrigger](id string, line func(p *P) *prototrig.Line, sel func(p *P) *bool) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamBool},
		get: func(c *TrigConfig, _ int) ParamValue {
			p := protoOf[P](c)
			on := *sel(&p)
			return ParamValue{Num: b2f(on), Text: map[bool]string{true: "on", false: "off"}[on]}
		},
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			protoEdit(c, line, func(p *P) { *sel(p) = v.Num != 0 })
			return nil
		},
	}
}

// TRLC-LINKS: REQ-DSO-013
func pChoice[P hal.ProtoTrigger](id string, choices []string, line func(p *P) *prototrig.Line, get func(p *P) int, set func(p *P, i int)) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamChoice, Choices: choices},
		get: func(c *TrigConfig, _ int) ParamValue {
			p := protoOf[P](c)
			i := min(max(get(&p), 0), len(choices)-1)
			return ParamValue{Num: float64(i), Text: choices[i]}
		},
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			protoEdit(c, line, func(p *P) { set(p, int(v.Num)) })
			return nil
		},
	}
}

// pData is a protocol's data byte n (1 or 2) over its byte list: -1 is no
// byte; the second byte needs the first.
// TRLC-LINKS: REQ-DSO-013
func pData[P hal.ProtoTrigger](id string, n int, line func(p *P) *prototrig.Line, sel func(p *P) *[]uint8) paramDef {
	return paramDef{
		TrigParam: TrigParam{ID: id, Kind: ParamInt},
		get: func(c *TrigConfig, _ int) ParamValue {
			p := protoOf[P](c)
			if d := *sel(&p); len(d) >= n {
				return ParamValue{Num: float64(d[n-1])}
			}
			return ParamValue{Num: -1}
		},
		set: func(c *TrigConfig, v ParamValue, _ int) error {
			if v.Num < -1 || v.Num > 255 {
				return fmt.Errorf("%s: a byte is 0 … 255 (-1: none)", id)
			}
			protoEdit(c, line, func(p *P) {
				d := slices.Clone(*sel(p)) // replaced, never edited in place
				switch {
				case v.Num < 0:
					d = d[:min(len(d), n-1)]
				case len(d) >= n:
					d[n-1] = uint8(v.Num)
				default:
					for len(d) < n-1 {
						d = append(d, 0)
					}
					d = append(d, uint8(v.Num))
				}
				*sel(p) = d
			})
			return nil
		},
	}
}

// paramDefsOf holds each type's settings, built once: a snapshot reads
// the trigger's source through them many times a second.
var paramDefsOf = sync.OnceValue(func() (all [NumTrigTypes][]paramDef) {
	for t := range NumTrigTypes {
		all[t] = buildTrigParamDefs(t)
	}
	return all
})

// trigParamDefs are type t's settings (shared: never written).
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func trigParamDefs(t TrigType) []paramDef {
	if t < 0 || t >= NumTrigTypes {
		return nil
	}
	return paramDefsOf()[t]
}

// buildTrigParamDefs builds type t's settings.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func buildTrigParamDefs(t TrigType) []paramDef {
	m := mainStage
	var out []paramDef
	switch t { //nolint:exhaustive // default: NumTrigTypes and out of range have no settings
	case TrigEdge:
		out = stageParams(m, true, true)
	case TrigPulse:
		out = append(stageParams(m, true, false), limitParams(m)...)
	case TrigSlope, TrigRunt:
		out = append(stageParams(m, true, false), lowLevel(m))
		out = append(out, limitParams(m)...)
	case TrigWindow:
		out = append(stageParams(m, false, false), lowLevel(m))
	case TrigInterval:
		out = append(stageParams(m, true, false), limitParams(m)...)
	case TrigVideo:
		vid := func(c *TrigConfig) *hal.Video {
			st := m(c)
			if st.Video == nil {
				st.Video = &hal.Video{Standard: "ntsc", Line: 1, Field: 1}
			}
			return st.Video
		}
		out = append(stageParams(m, false, false),
			choiceParam("standard", videoStandards,
				func(c *TrigConfig) int { return max(slices.Index(videoStandards, vid(c).Standard), 0) },
				func(c *TrigConfig, i int) { vid(c).Standard = videoStandards[i] }),
			intParam("line", func(c *TrigConfig) *int { return &vid(c).Line }),
			intParam("field", func(c *TrigConfig) *int { return &vid(c).Field }),
			boolParam("any_line", func(c *TrigConfig) *bool { return &vid(c).AnyLine }),
			boolParam("positive", func(c *TrigConfig) *bool { return &vid(c).Positive }),
			intParam("lines", func(c *TrigConfig) *int { return &vid(c).Lines }),
			numParam("frame_hz", "Hz", func(c *TrigConfig) *float64 { return &vid(c).FrameHz }, nil),
			intParam("interlace", func(c *TrigConfig) *int { return &vid(c).Interlace }),
			intParam("fields", func(c *TrigConfig) *int { return &vid(c).Fields }),
		)
	case TrigDropout:
		out = append(stageParams(m, true, false),
			numParam("timeout", "s", func(c *TrigConfig) *float64 { return &stageQual(m(c)).MinS },
				func(c *TrigConfig) { stageQual(m(c)).Op = hal.Longer }),
			choiceParam("form", []string{"edge", "state"},
				func(c *TrigConfig) int { return int(m(c).Timeout) },
				func(c *TrigConfig, i int) { m(c).Timeout = hal.TimeoutForm(i) }),
		)
	case TrigPattern:
		out = append(patternParams(m, true), limitParams(m)...)
	case TrigQualified:
		gate := func(c *TrigConfig) *hal.Gate {
			st := m(c)
			if st.Gate == nil {
				st.Gate = &hal.Gate{}
			}
			return st.Gate
		}
		out = append(stageParams(m, true, false),
			srcParam("gate_source", func(c *TrigConfig) *hal.Source { return &gate(c).Source }, nil),
			numParam("gate_level", "V", func(c *TrigConfig) *float64 { return &gate(c).Level }, nil),
			// The form: the gate's state or an edge of it, and whether the
			// trigger waits a time after it (the delayed forms run the time
			// condition, the others keep it unused: NoTime).
			choiceParam("gate_form", []string{"state", "state_delay", "edge", "edge_delay"},
				func(c *TrigConfig) int { return 2*int(b2f(gate(c).Edge)) + int(b2f(!c.NoTime)) },
				func(c *TrigConfig, i int) { gate(c).Edge, c.NoTime = i >= 2, i%2 == 0 }),
			boolParam("gate_high", func(c *TrigConfig) *bool { return &gate(c).High }),
		)
		out = append(out, limitParams(m)...)
	case TrigNthEdge:
		out = append(stageParams(m, true, true),
			intParam("count", func(c *TrigConfig) *int { return &m(c).Count }),
			// The idle time before counting: 0 is none.
			paramDef{
				TrigParam: TrigParam{ID: "idle", Kind: ParamNumber, Unit: "s"},
				get: func(c *TrigConfig, _ int) ParamValue {
					if q := m(c).Qual; q != nil && q.Op == hal.Longer {
						return ParamValue{Num: q.MinS}
					}
					return ParamValue{}
				},
				set: func(c *TrigConfig, v ParamValue, _ int) error {
					q := stageQual(m(c))
					q.Op, q.MinS = hal.Longer, v.Num
					if v.Num <= 0 {
						q.Op, q.MinS = 0, 0
					}
					return nil
				},
			})
	case TrigDelay:
		out = append(patternParams(firstStage, false),
			slopeParam("slope_a", false, firstStage))
		out = append(out, stageParams(m, true, false)...)
		out = append(out, limitParams(m)...)
	case TrigSetupHold:
		gate := func(c *TrigConfig) *hal.Gate {
			st := m(c)
			if st.Gate == nil {
				st.Gate = &hal.Gate{}
			}
			return st.Gate
		}
		out = append(stageParams(m, true, false),
			srcParam("data_source", func(c *TrigConfig) *hal.Source { return &gate(c).Source }, nil),
			numParam("data_level", "V", func(c *TrigConfig) *float64 { return &gate(c).Level }, nil),
			boolParam("data_high", func(c *TrigConfig) *bool { return &gate(c).High }),
			choiceParam("form", []string{"setup", "hold"},
				func(c *TrigConfig) int { return int(b2f(gate(c).Hold)) },
				func(c *TrigConfig, i int) { gate(c).Hold = i == 1 }),
		)
		out = append(out, limitParams(m)...)
	case TrigI2C:
		out = i2cParams()
	case TrigSPI:
		out = spiParams()
	case TrigUART:
		out = uartParams()
	case TrigCAN:
		out = canParams()
	case TrigLIN:
		out = linParams()
	default:
		return nil
	}
	return append(out, commonParams()...)
}

// lineParams are a protocol line's source and level settings.
// TRLC-LINKS: REQ-DSO-013
func lineParams[P hal.ProtoTrigger](prefix string, main, sel func(p *P) *prototrig.Line) []paramDef {
	src, lvl := "source", "level"
	if prefix != "" {
		src, lvl = prefix+"_source", prefix+"_level"
	}
	return []paramDef{
		pSrc(src, main, sel),
		pNum(lvl, "V", main, func(p *P) *float64 { return &sel(p).Level }),
	}
}

// TRLC-LINKS: REQ-DSO-013
func i2cParams() []paramDef {
	type P = prototrig.I2C
	sda := func(p *P) *prototrig.Line { return &p.SDA }
	out := lineParams("scl", sda, func(p *P) *prototrig.Line { return &p.SCL })
	out = append(out, lineParams("sda", sda, sda)...)
	return append(out,
		pChoice("condition", []string{"start", "stop", "restart", "no_ack", "eeprom", "addr7_data", "addr10_data", "data_length"}, sda,
			func(p *P) int { return int(p.Condition) }, func(p *P, i int) { p.Condition = prototrig.I2CCondition(i) }),
		pInt("address", sda, func(p *P) *int { return &p.Address }),
		pChoice("rw", []string{"write", "read", "any"}, sda,
			func(p *P) int { return int(p.RW) }, func(p *P, i int) { p.RW = prototrig.RW(i) }),
		pData("data1", 1, sda, func(p *P) *[]uint8 { return &p.Data }),
		pData("data2", 2, sda, func(p *P) *[]uint8 { return &p.Data }),
		pChoice("compare", compareChoices, sda,
			func(p *P) int { return int(p.Compare) }, func(p *P, i int) { p.Compare = prototrig.Compare(i) }),
		pInt("length", sda, func(p *P) *int { return &p.DataLength }),
		pBool("addr10", sda, func(p *P) *bool { return &p.Addr10 }),
	)
}

// TRLC-LINKS: REQ-DSO-013
func spiParams() []paramDef {
	type P = prototrig.SPI
	data := func(p *P) *prototrig.Line { return &p.Data }
	out := lineParams("clk", data, func(p *P) *prototrig.Line { return &p.Clock })
	out = append(out, pChoice("edge", slopeChoices[:2], data,
		func(p *P) int { return int(b2f(p.LatchFalling)) }, func(p *P, i int) { p.LatchFalling = i == 1 }))
	out = append(out, lineParams("data", data, data)...)
	out = append(out, pChoice("cs_mode", []string{"active_low", "active_high", "timeout"}, data,
		func(p *P) int { return int(p.SelectMode) }, func(p *P, i int) { p.SelectMode = prototrig.SPISelect(i) }))
	out = append(out, lineParams("cs", data, func(p *P) *prototrig.Line { return &p.Select })...)
	return append(out,
		pNum("idle", "s", data, func(p *P) *float64 { return &p.IdleS }),
		paramDef{
			TrigParam: TrigParam{ID: "bits", Kind: ParamStates},
			get:       func(c *TrigConfig, _ int) ParamValue { return ParamValue{Text: StatesText(protoOf[P](c).Bits)} },
			set: func(c *TrigConfig, v ParamValue, _ int) error {
				st, err := ParseStates(v.Text)
				if err == nil {
					protoEdit(c, data, func(p *P) { p.Bits = st })
				}
				return err
			},
		},
		pChoice("bit_order", orderChoices, data,
			func(p *P) int { return int(b2f(p.MSBFirst)) }, func(p *P, i int) { p.MSBFirst = i == 1 }),
	)
}

// TRLC-LINKS: REQ-DSO-013
func uartParams() []paramDef {
	type P = prototrig.UART
	line := func(p *P) *prototrig.Line { return &p.Line }
	return append(lineParams("", line, line),
		pChoice("condition", []string{"start", "stop", "data", "error"}, line,
			func(p *P) int { return int(p.Condition) }, func(p *P, i int) { p.Condition = prototrig.UARTCondition(i) }),
		pNum("baud", "bit/s", line, func(p *P) *float64 { return &p.Baud }),
		pInt("bits", line, func(p *P) *int { return &p.Bits }),
		pChoice("parity", []string{"none", "odd", "even", "mark", "space"}, line,
			func(p *P) int { return int(p.Parity) }, func(p *P, i int) { p.Parity = prototrig.Parity(i) }),
		pNum("stop", "bit", line, func(p *P) *float64 { return &p.StopBits }),
		pChoice("idle", []string{"low", "high"}, line,
			func(p *P) int { return int(b2f(p.IdleHigh)) }, func(p *P, i int) { p.IdleHigh = i == 1 }),
		pChoice("bit_order", orderChoices, line,
			func(p *P) int { return int(b2f(p.MSBFirst)) }, func(p *P, i int) { p.MSBFirst = i == 1 }),
		pChoice("compare", compareChoices, line,
			func(p *P) int { return int(p.Compare) }, func(p *P, i int) { p.Compare = prototrig.Compare(i) }),
		pInt("value", line, func(p *P) *int { return &p.Value }),
	)
}

// TRLC-LINKS: REQ-DSO-013
func canParams() []paramDef {
	type P = prototrig.CAN
	line := func(p *P) *prototrig.Line { return &p.Line }
	return append(lineParams("", line, line),
		pChoice("condition", []string{"start", "remote", "id", "id_data", "error"}, line,
			func(p *P) int { return int(p.Condition) }, func(p *P, i int) { p.Condition = prototrig.CANCondition(i) }),
		pNum("baud", "bit/s", line, func(p *P) *float64 { return &p.Baud }),
		pInt("id", line, func(p *P) *int { return &p.ID }),
		pBool("extended", line, func(p *P) *bool { return &p.Extended }),
		pData("data1", 1, line, func(p *P) *[]uint8 { return &p.Data }),
		pData("data2", 2, line, func(p *P) *[]uint8 { return &p.Data }),
	)
}

// TRLC-LINKS: REQ-DSO-013
func linParams() []paramDef {
	type P = prototrig.LIN
	line := func(p *P) *prototrig.Line { return &p.Line }
	return append(lineParams("", line, line),
		pChoice("condition", []string{"break", "id", "id_data", "data_error"}, line,
			func(p *P) int { return int(p.Condition) }, func(p *P, i int) { p.Condition = prototrig.LINCondition(i) }),
		pNum("baud", "bit/s", line, func(p *P) *float64 { return &p.Baud }),
		pInt("id", line, func(p *P) *int { return &p.ID }),
		pData("data1", 1, line, func(p *P) *[]uint8 { return &p.Data }),
		pData("data2", 2, line, func(p *P) *[]uint8 { return &p.Data }),
		pBool("checksum_error", line, func(p *P) *bool { return &p.ChecksumError }),
		pBool("sync_error", line, func(p *P) *bool { return &p.SyncError }),
		pBool("parity_error", line, func(p *P) *bool { return &p.ParityError }),
		pBool("spec2", line, func(p *P) *bool { return &p.Spec2 }),
		pInt("error_id", line, func(p *P) *int { return &p.ErrorID }),
		pInt("error_length", line, func(p *P) *int { return &p.ErrorLength }),
	)
}
