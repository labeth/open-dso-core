package session

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Value is one field of a snapshot: its value, the number it stands for,
// or why the target could not produce it.
type Value struct {
	V   any      `json:"v,omitempty"`
	Num *float64 `json:"num,omitempty"`
	NA  string   `json:"na,omitempty"`
}

// Snapshot is a checkpoint's fields by name.
type Snapshot map[string]Value

// text is v as text, for comparisons and tables.
// TRLC-LINKS: REQ-DSO-295
func (v Value) text() string {
	switch x := v.V.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var parts []string
		for _, e := range x {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, " | ")
	case []string:
		return strings.Join(x, " | ")
	case float64:
		return strconv.FormatFloat(x, 'g', 6, 64)
	default:
		return fmt.Sprint(x)
	}
}

// na is a field the target cannot produce, with the reason.
// TRLC-LINKS: REQ-DSO-295
func na(why string) Value { return Value{NA: why} }

// num is a number field.
// TRLC-LINKS: REQ-DSO-295
func num(x float64) Value { return Value{V: x, Num: &x} }

// DefaultFields are the fields a checkpoint compares when it names none:
// the settings that do not depend on the signal.
var DefaultFields = []string{
	"workspace", "view", "mode", "sheet.open", "sheet.tab", "sheet.row", "fkeys", "lamps",
	"run.running", "run.single", "shown.",
	"row.time.tdiv", "row.trig.type", "row.trig.edge", "row.trig.mode",
	"row.ch1.on", "row.ch1.vdiv", "row.ch1.coupling", "row.ch1.probe",
	"row.ch2.on", "row.ch2.vdiv", "row.ch2.coupling", "row.ch2.probe",
	"row.ch3.on", "row.ch3.vdiv", "row.ch4.on", "row.ch4.vdiv",
}

// statusDoc is what the snapshot reads of /api/status: the panel, which
// every scope serving the core UI model has, and the run flags.
type statusDoc struct {
	Panel    *uimodel.Status `json:"panel"`
	Running  *bool           `json:"running"`
	Single   *bool           `json:"single"`
	Norm     *bool           `json:"norm"`
	TdivS    *float64        `json:"tdiv_s"`
	RunState *string         `json:"run_state"`
	Seq      *uint64         `json:"seq"`
	Frames   *uint64         `json:"frames"`
}

// readStatus reads the status document.
// TRLC-LINKS: REQ-DSO-295
func readStatus(ctx context.Context, c *Client) (statusDoc, error) {
	var st statusDoc
	err := c.JSON(ctx, http.MethodGet, "/api/status", nil, &st)
	return st, err
}

// capsDoc is what the snapshot reads of /api/caps.
type capsDoc struct {
	Features map[string]struct {
		OK  bool   `json:"ok"`
		Why string `json:"why"`
	} `json:"features"`
}

// Capture reads one snapshot of the scope at c: the panel status, every
// sheet row, the capabilities, the frame's measurements and, while a
// decoder is set, the packets decoded from the served record.
// TRLC-LINKS: REQ-DSO-295
func Capture(ctx context.Context, c *Client) (Snapshot, error) {
	s := Snapshot{}
	st, err := readStatus(ctx, c)
	if err != nil {
		return nil, err
	}
	s.status(st)
	var sheets uimodel.SheetsModel
	if err := c.JSON(ctx, http.MethodGet, "/api/sheets", nil, &sheets); err != nil {
		s["row."] = na("no sheet model: " + err.Error())
	} else {
		s.rows(sheets)
	}
	var caps capsDoc
	if err := c.JSON(ctx, http.MethodGet, "/api/caps", nil, &caps); err != nil {
		s["caps."] = na("no capabilities document: " + err.Error())
	} else {
		for name, f := range caps.Features {
			if f.OK {
				s["caps."+name] = Value{V: "ok"}
			} else {
				s["caps."+name] = Value{V: "refused: " + f.Why}
			}
		}
	}
	since := uint64(0)
	if st.Seq != nil {
		since = *st.Seq
	}
	s.measures(ctx, c, since)
	if st.Panel != nil {
		s.packets(ctx, c, since, st.Panel.Display)
	}
	return s, nil
}

// status adds the panel's and the run's fields.
// TRLC-LINKS: REQ-DSO-295
func (s Snapshot) status(st statusDoc) {
	opt := func(name string, p any) {
		switch v := p.(type) {
		case *bool:
			if v != nil {
				s[name] = Value{V: *v}
				return
			}
		case *float64:
			if v != nil {
				s[name] = num(*v)
				return
			}
		case *string:
			if v != nil {
				s[name] = Value{V: *v}
				return
			}
		}
		s[name] = na("the status document has no " + name)
	}
	opt("run.running", st.Running)
	opt("run.single", st.Single)
	opt("run.norm", st.Norm)
	opt("run.state", st.RunState)
	opt("tdiv_s", st.TdivS)
	p := st.Panel
	if p == nil {
		s["panel."] = na("the status document has no panel")
		return
	}
	s["workspace"] = Value{V: p.WorkspaceID}
	s["view"] = Value{V: p.View}
	s["mode"] = Value{V: p.Mode}
	s["sheet.open"] = Value{V: p.Sheet.Open}
	s["sheet.tab"] = Value{V: p.Sheet.Tab}
	row := ""
	if p.Sheet.Row != nil {
		row = p.Sheet.Row.ID
	}
	s["sheet.row"] = Value{V: row}
	s["sheet.edit"] = Value{V: p.Sheet.Edit}
	var keys []string
	for _, k := range p.FKeys {
		l := k.Label
		if k.Value != "" {
			l += " " + k.Value
		}
		if k.Sel {
			l = "*" + l
		}
		keys = append(keys, l)
	}
	s["fkeys"] = Value{V: keys}
	lamps := slices.Clone(p.LampsLit)
	slices.Sort(lamps)
	s["lamps"] = Value{V: lamps}
	s["notice"] = Value{V: p.Notice}
	s["again"] = Value{V: p.AgainNext}
	s["autoset.busy"] = Value{V: p.AutosetBusy}
	d := p.Display
	shown := d.ShowCh
	if len(shown) == 0 {
		shown = []bool{d.ShowC1, d.ShowC2}
	}
	for i, on := range shown {
		s[fmt.Sprintf("shown.ch%d", i+1)] = Value{V: on}
	}
	s["decode.detecting"] = Value{V: d.DecDetecting}
	s["decode.auto"] = Value{V: d.DecAuto}
	s["decode.note"] = Value{V: d.DecNote}
	s["meas.shown"] = Value{V: d.ShowMeas}
}

// rows adds every sheet row as its text, with the number it stands for,
// and each MEAS row's live readings per channel.
// TRLC-LINKS: REQ-DSO-295
func (s Snapshot) rows(m uimodel.SheetsModel) {
	for _, tab := range m.Tabs {
		for _, r := range tab.Rows {
			if r.Kind == "action" {
				continue
			}
			v := Value{V: r.Text}
			if x, ok := r.Value.(float64); ok && r.Kind == "number" {
				v.Num = &x
			}
			s["row."+r.ID] = v
			for i, live := range r.Live {
				if live == "" {
					continue
				}
				lv := Value{V: live}
				if x, ok := parseSI(live); ok {
					lv.Num = &x
				}
				s[fmt.Sprintf("meas.%s.ch%d", strings.TrimPrefix(r.ID, "meas."), i+1)] = lv
			}
		}
	}
}

// parseSI reads a reading such as "1.000kHz", "-2.5 mV" or "50 %" as a
// number in its base unit.
// TRLC-LINKS: REQ-DSO-295
func parseSI(t string) (float64, bool) {
	t = strings.TrimSpace(t)
	end := 0
	for end < len(t) && (unicode.IsDigit(rune(t[end])) || strings.ContainsRune("+-.eE", rune(t[end]))) {
		if (t[end] == 'e' || t[end] == 'E') && (end+1 >= len(t) || !strings.ContainsRune("+-0123456789", rune(t[end+1]))) {
			break
		}
		end++
	}
	x, err := strconv.ParseFloat(t[:end], 64)
	if err != nil {
		return 0, false
	}
	rest := strings.TrimSpace(t[end:])
	scale := map[string]float64{"p": 1e-12, "n": 1e-9, "u": 1e-6, "µ": 1e-6, "m": 1e-3, "k": 1e3, "M": 1e6, "G": 1e9}
	for p, f := range scale {
		if strings.HasPrefix(rest, p) && len(rest) > len(p) {
			return x * f, true
		}
	}
	return x, true
}

// measFields are the frame measurements a snapshot keeps.
var measFields = []string{"vpp", "vmax", "vmin", "vmean", "vrms", "freq", "period", "duty"}

// measures adds the served frame's measurements of each channel, from a v2
// header's traces or a v1 header's m1 and m2.
// TRLC-LINKS: REQ-DSO-295
func (s Snapshot) measures(ctx context.Context, c *Client, since uint64) {
	b, _, err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/frame.bin?v=2&cols=1024&since=%d&waitms=%d", since, frameWaitMs), nil)
	if err != nil {
		s["fmeas."] = na("no frame: " + err.Error())
		return
	}
	s.measuresOf(b)
}

// measuresOf adds the measurements of each channel a served frame's
// header carries: a v2 header's traces' or a v1 header's m1 and m2.
// TRLC-LINKS: REQ-DSO-295
func (s Snapshot) measuresOf(b []byte) {
	per := map[int]map[string]any{}
	switch {
	case len(b) >= 8 && string(b[:4]) == string(wire.FrameMagic2[:]):
		n := int(binary.LittleEndian.Uint32(b[4:8]))
		var h struct {
			Traces []struct {
				Ch   int            `json:"ch"`
				Meas map[string]any `json:"meas"`
			} `json:"traces"`
		}
		if 8+n > len(b) || json.Unmarshal(b[8:8+n], &h) != nil {
			s["fmeas."] = na("an unreadable v2 frame header")
			return
		}
		for _, t := range h.Traces {
			if t.Meas != nil {
				per[t.Ch] = t.Meas
			}
		}
	case len(b) >= 8 && b[0] == wire.BinMagic:
		n := int(binary.LittleEndian.Uint32(b[4:8]))
		var h struct {
			M1 map[string]any `json:"m1"`
			M2 map[string]any `json:"m2"`
		}
		if 8+n > len(b) || json.Unmarshal(b[8:8+n], &h) != nil {
			s["fmeas."] = na("an unreadable v1 frame header")
			return
		}
		if h.M1 != nil {
			per[0] = h.M1
		}
		if h.M2 != nil {
			per[1] = h.M2
		}
	default:
		s["fmeas."] = na("an unknown frame format")
		return
	}
	for _, ch := range slices.Sorted(maps.Keys(per)) {
		for _, f := range measFields {
			if x, ok := per[ch][f].(float64); ok && !math.IsNaN(x) {
				s[fmt.Sprintf("fmeas.ch%d.%s", ch+1, f)] = num(x)
			}
		}
	}
}

// packets adds the packets decoded from the served record with the
// panel's decode settings, while a decoder is set.
// TRLC-LINKS: REQ-DSO-295
func (s Snapshot) packets(ctx context.Context, c *Client, since uint64, d uimodel.Display) {
	if d.DecProto == framecalc.ProtoOff || d.DecProto == framecalc.ProtoAuto {
		s["packets"] = Value{V: []string{}}
		s["packets.count"] = num(0)
		s["packets.text"] = Value{V: ""}
		return
	}
	f, err := rawFrame(ctx, c, since)
	if err != nil {
		for _, k := range []string{"packets", "packets.count", "packets.text"} {
			s[k] = na("no raw record to decode: " + err.Error())
		}
		return
	}
	ps := decodePackets(f, d)
	if ps == nil {
		ps = []string{}
	}
	s["packets"] = Value{V: ps}
	s["packets.count"] = num(float64(len(ps)))
	s["packets.text"] = Value{V: packetBytesText(f, d)}
}
