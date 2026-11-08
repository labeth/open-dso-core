package session

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// CaptureFormat names a capture file's format.
const CaptureFormat = "open-dso capture 1"

// CaptureFile is one record a scope held, as it served it, with what it
// reported of it: the raw record (a v2 raw frame, its traces' codes, code
// format and scale), the sheet rows that set it up, the decoder the panel
// had set, and the measurements its frame carried and the packets its
// record decodes to. Replayed through the demo (simboard's replay), the
// same record must report the same.
type CaptureFile struct {
	Format string `json:"format"`
	// About says what was on the inputs.
	About string       `json:"about,omitempty"`
	Scope CaptureScope `json:"scope"`
	// Rows are the sheet rows that set the record up, by row id
	// ("time.tdiv": "1us"), as their text.
	Rows map[string]string `json:"rows"`
	// Decode is the panel's decoder (nil: none set).
	Decode *CaptureDecode `json:"decode,omitempty"`
	// Frame is the raw record: /api/frame.bin?v=2&raw=1&full=1.
	Frame []byte `json:"frame"`
	// Reported are the fields the scope reported of the record
	// (ReplayFields): every measurement of its frame's header
	// (fmeas.chN.name) and the packets of its record.
	Reported Snapshot `json:"reported"`
}

// CaptureScope is the scope that held the record, as its capabilities name
// it.
type CaptureScope struct {
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Product  string `json:"product,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}

// CaptureDecode is the panel's decoder as the packets are decoded with it.
type CaptureDecode struct {
	Proto    int     `json:"proto"`
	Baud     int     `json:"baud,omitempty"`
	ChA      int     `json:"ch_a"`
	ChB      int     `json:"ch_b,omitempty"`
	CPOL     bool    `json:"cpol,omitempty"`
	CPHA     bool    `json:"cpha,omitempty"`
	LSB      bool    `json:"lsb,omitempty"`
	Format   int     `json:"format,omitempty"`
	DelayNs  float64 `json:"delay_ns,omitempty"`
	Parity   string  `json:"parity,omitempty"`
	Inverted bool    `json:"inverted,omitempty"`
}

// display is the decoder as the panel's display state.
// TRLC-LINKS: REQ-DSO-295
func (d *CaptureDecode) display() uimodel.Display {
	if d == nil {
		return uimodel.Display{DecProto: framecalc.ProtoOff}
	}
	return uimodel.Display{
		DecProto: d.Proto, DecBaud: d.Baud, DecChA: d.ChA, DecChB: d.ChB, DecCPOL: d.CPOL, DecCPHA: d.CPHA,
		DecLSB: d.LSB, DecFormat: d.Format, DecDelayNs: d.DelayNs, DecParity: d.Parity, DecInverted: d.Inverted,
	}
}

// decodeOf is the display's decoder, nil when none is set.
// TRLC-LINKS: REQ-DSO-295
func decodeOf(d uimodel.Display) *CaptureDecode {
	if d.DecProto == framecalc.ProtoOff || d.DecProto == framecalc.ProtoAuto {
		return nil
	}
	return &CaptureDecode{
		Proto: d.DecProto, Baud: d.DecBaud, ChA: d.DecChA, ChB: d.DecChB, CPOL: d.DecCPOL, CPHA: d.DecCPHA,
		LSB: d.DecLSB, Format: d.DecFormat, DelayNs: d.DecDelayNs, Parity: d.DecParity, Inverted: d.DecInverted,
	}
}

// ReplayRows are the sheet rows a capture keeps and a replay sets, in the
// order they are set: the timebase, then each channel's front end, then
// the trigger.
var ReplayRows = []string{
	"time.tdiv", "time.pos",
	"ch1.on", "ch1.probe", "ch1.coupling", "ch1.vdiv", "ch1.offset",
	"ch2.on", "ch2.probe", "ch2.coupling", "ch2.vdiv", "ch2.offset",
	"ch3.on", "ch3.probe", "ch3.coupling", "ch3.vdiv", "ch3.offset",
	"ch4.on", "ch4.probe", "ch4.coupling", "ch4.vdiv", "ch4.offset",
	"trig.edge", "trig.level",
}

// StopForCapture takes one record with SINGLE when the scope at c runs (or
// always, with single), and waits up to limit for it to stop holding it.
// TRLC-LINKS: REQ-DSO-295
func StopForCapture(ctx context.Context, c *Client, single bool, limit time.Duration) error {
	st, err := readStatus(ctx, c)
	if err != nil {
		return err
	}
	if !single && (st.Running == nil || !*st.Running) {
		return nil
	}
	if err := c.Post(ctx, "/api/panel", map[string]any{"button": "single"}); err != nil {
		return err
	}
	end := time.Now().Add(limit)
	for {
		st, err := readStatus(ctx, c)
		if err != nil {
			return err
		}
		if st.Running != nil && !*st.Running {
			return nil
		}
		if time.Now().After(end) {
			return errors.New("the scope did not take its single record (is its trigger met?)")
		}
		sleep(ctx, 200*time.Millisecond)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// RecordCapture reads the record the scope at c holds, stopped, and what
// it reports of it. A running scope is refused: its record would move
// between the reads.
// TRLC-LINKS: REQ-DSO-295
func RecordCapture(ctx context.Context, c *Client, about string) (*CaptureFile, error) {
	st, err := readStatus(ctx, c)
	if err != nil {
		return nil, err
	}
	if st.Running != nil && *st.Running {
		return nil, errors.New("the scope is running: stop it (RUN/STOP or SINGLE) so the record holds still")
	}
	if st.Panel == nil {
		return nil, errors.New("the status document has no panel")
	}
	f := &CaptureFile{Format: CaptureFormat, About: about, Rows: map[string]string{}, Decode: decodeOf(st.Panel.Display)}
	var caps struct {
		Identity CaptureScope `json:"identity"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/api/caps", nil, &caps); err != nil {
		return nil, err
	}
	f.Scope = caps.Identity
	var sheets uimodel.SheetsModel
	if err := c.JSON(ctx, http.MethodGet, "/api/sheets", nil, &sheets); err != nil {
		return nil, err
	}
	for _, tab := range sheets.Tabs {
		for _, r := range tab.Rows {
			if slices.Contains(ReplayRows, r.ID) && r.Kind != "action" {
				f.Rows[r.ID] = r.Text
			}
		}
	}
	f.Frame, f.Reported, err = recordOf(ctx, c, f.Decode)
	return f, err
}

// recordOf reads the raw record the scope at c holds and what it reports
// of it with decoder dec.
// TRLC-LINKS: REQ-DSO-295
func recordOf(ctx context.Context, c *Client, dec *CaptureDecode) ([]byte, Snapshot, error) {
	raw, _, err := c.Do(ctx, http.MethodGet, "/api/frame.bin?v=2&raw=1&full=1", nil)
	if err != nil {
		return nil, nil, err
	}
	if raw, err = rawAsV2(raw); err != nil {
		return nil, nil, err
	}
	shown, _, err := c.Do(ctx, http.MethodGet, "/api/frame.bin?v=2&cols=1024", nil)
	if err != nil {
		return nil, nil, err
	}
	rec, err := frameOfV2(raw)
	if err != nil {
		return nil, nil, err
	}
	rawSeq, shownSeq := headerSeq(raw), headerSeq(shown)
	if rawSeq != shownSeq {
		return nil, nil, fmt.Errorf("the raw record (frame %d) and the shown frame (%d) are not one record", rawSeq, shownSeq)
	}
	s := Snapshot{}
	allMeasures(s, shown)
	d := dec.display()
	if dec != nil {
		ps := decodePackets(rec, d)
		if ps == nil {
			ps = []string{}
		}
		s["packets"] = Value{V: ps}
		s["packets.count"] = num(float64(len(ps)))
		s["packets.text"] = Value{V: packetBytesText(rec, d)}
	}
	return raw, s, nil
}

// rawAsV2 is a served raw record in wire v2: as served, or a v1 scope's
// raw shape (a scope that serves frame wire v1 alone) as a v1 scope serves
// it in v2 (wire.RawV2), so a capture holds one format whichever wire the
// scope serves.
// TRLC-LINKS: REQ-DSO-295
func rawAsV2(raw []byte) ([]byte, error) {
	if len(raw) >= 4 && string(raw[:4]) == string(wire.FrameMagic2[:]) {
		return raw, nil
	}
	rf, err := wire.DecodeRawFrame(raw)
	if err != nil {
		return nil, fmt.Errorf("the scope serves no raw record: %w", err)
	}
	if rf.Unchanged || rf.Empty {
		return nil, errors.New("the scope holds no record")
	}
	return wire.RawV2(rf)
}

// headerSeq is a frame's sequence number, v2 or v1 (both carry it in the
// JSON header after an 8-byte prefix).
// TRLC-LINKS: REQ-DSO-295
func headerSeq(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	var h struct {
		Seq uint64 `json:"seq"`
	}
	if 8+n > len(b) || json.Unmarshal(b[8:8+n], &h) != nil {
		return 0
	}
	return h.Seq
}

// allMeasures adds every measurement a frame header carries: a v2
// header's traces' or a v1 header's m1 and m2.
// TRLC-LINKS: REQ-DSO-295
func allMeasures(s Snapshot, b []byte) {
	var h struct {
		Traces []struct {
			Ch   int            `json:"ch"`
			Meas map[string]any `json:"meas"`
		} `json:"traces"`
		M1 map[string]any `json:"m1"`
		M2 map[string]any `json:"m2"`
	}
	v2 := len(b) >= 8 && string(b[:4]) == string(wire.FrameMagic2[:])
	if !v2 && (len(b) < 8 || b[0] != wire.BinMagic) {
		s["fmeas."] = na("an unknown frame format")
		return
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	if 8+n > len(b) || json.Unmarshal(b[8:8+n], &h) != nil {
		s["fmeas."] = na("an unreadable frame header")
		return
	}
	if !v2 {
		for ch, m := range []map[string]any{h.M1, h.M2} {
			if m != nil {
				h.Traces = append(h.Traces, struct {
					Ch   int            `json:"ch"`
					Meas map[string]any `json:"meas"`
				}{ch, m})
			}
		}
	}
	for _, t := range h.Traces {
		for _, k := range slices.Sorted(maps.Keys(t.Meas)) {
			switch x := t.Meas[k].(type) {
			case float64:
				if !math.IsNaN(x) {
					s[fmt.Sprintf("fmeas.ch%d.%s", t.Ch+1, k)] = num(x)
				}
			case string, bool:
				s[fmt.Sprintf("fmeas.ch%d.%s", t.Ch+1, k)] = Value{V: x}
			}
		}
	}
}

// ReplayReport is what the scope at c reports of the record it holds now,
// with the capture's decoder: the fields CaptureFile.Reported holds.
// TRLC-LINKS: REQ-DSO-295
func (f *CaptureFile) ReplayReport(ctx context.Context, c *Client) (Snapshot, *sample.Frame, error) {
	raw, s, err := recordOf(ctx, c, f.Decode)
	if err != nil {
		return nil, nil, err
	}
	rec, err := frameOfV2(raw)
	return s, rec, err
}

// Mismatches lists the reported fields got differs from the capture's in,
// exactly: the same record through the same code reports the same.
// TRLC-LINKS: REQ-DSO-295
func (f *CaptureFile) Mismatches(got Snapshot) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(f.Reported)) {
		w, g := f.Reported[k], got[k]
		switch {
		case w.Num != nil || g.Num != nil:
			if w.Num == nil || g.Num == nil || *w.Num != *g.Num {
				out = append(out, fmt.Sprintf("%s: reported %s, replayed %s", k, w.text(), g.text()))
			}
		case w.text() != g.text() || w.NA != g.NA:
			out = append(out, fmt.Sprintf("%s: reported %q, replayed %q", k, w.text(), g.text()))
		}
	}
	for k := range got {
		if _, ok := f.Reported[k]; !ok && strings.HasPrefix(k, "fmeas.") {
			out = append(out, fmt.Sprintf("%s: not reported, replayed %s", k, got[k].text()))
		}
	}
	slices.Sort(out)
	return out
}

// ParseCapture reads a capture file.
// TRLC-LINKS: REQ-DSO-295
func ParseCapture(data []byte) (*CaptureFile, error) {
	var f CaptureFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Format != CaptureFormat {
		return nil, fmt.Errorf("a capture file of format %q, want %q", f.Format, CaptureFormat)
	}
	if _, err := frameOfV2(f.Frame); err != nil {
		return nil, fmt.Errorf("the capture's record: %w", err)
	}
	return &f, nil
}

// replayWait bounds how long a replay waits for the demo to hold the
// replayed record.
const replayWait = 20 * time.Second

// Replay plays the capture back on the simulated board of the demo at c
// (its /api/sim/replay), sets the capture's rows, takes one record and
// returns what the demo reports of it with the capture's decoder, and the
// fields that differ from what the scope reported.
// TRLC-LINKS: REQ-DSO-295
func (f *CaptureFile) Replay(ctx context.Context, c *Client) (Snapshot, []string, error) {
	var r okReply
	if b, _, err := c.Do(ctx, http.MethodPost, "/api/sim/replay", rawBody(f.Frame)); err != nil {
		return nil, nil, fmt.Errorf("replay: %w", err)
	} else if json.Unmarshal(b, &r) != nil || !r.OK {
		return nil, nil, fmt.Errorf("replay: %s", r.Err)
	}
	t := &Target{Client: c}
	for _, id := range ReplayRows {
		if text, ok := f.Rows[id]; ok {
			if err := t.setRow(ctx, id, text, nil); err != nil {
				return nil, nil, err
			}
		}
	}
	want, err := frameOfV2(f.Frame)
	if err != nil {
		return nil, nil, err
	}
	if err := c.Post(ctx, "/api/panel", map[string]any{"button": "single"}); err != nil {
		return nil, nil, err
	}
	end := time.Now().Add(replayWait)
	for {
		raw, _, err := c.Do(ctx, http.MethodGet, "/api/frame.bin?v=2&raw=1&full=1", nil)
		if err == nil {
			if got, err := frameOfV2(raw); err == nil && sameRecord(got, want) && headerSeq(raw) > 0 {
				var st statusDoc
				if st, err = readStatus(ctx, c); err == nil && st.Running != nil && !*st.Running {
					break
				}
			}
		}
		if time.Now().After(end) {
			got := "none"
			if raw, _, err := c.Do(ctx, http.MethodGet, "/api/frame.bin?v=2&raw=1&full=1", nil); err == nil {
				if g, err := frameOfV2(raw); err == nil {
					got = fmt.Sprintf("%g s a sample, trigger at %g", g.SampleS, g.TriggerAt)
				}
			}
			return nil, nil, fmt.Errorf("replay: the demo did not take the replayed record (%g s a sample, trigger at %g); its record: %s",
				want.SampleS, want.TriggerAt, got)
		}
		sleep(ctx, 200*time.Millisecond)
	}
	got, _, err := f.ReplayReport(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	return got, f.Mismatches(got), nil
}

// rawBody marks a request body sent as it is.
type rawBody []byte

// sameRecord reports whether a holds b's samples on every channel b has.
// TRLC-LINKS: REQ-DSO-295
func sameRecord(a, b *sample.Frame) bool {
	if a.SampleS != b.SampleS || a.TriggerAt != b.TriggerAt {
		return false
	}
	for _, tb := range b.Traces {
		i := slices.IndexFunc(a.Traces, func(t sample.Trace) bool { return t.Ch == tb.Ch })
		if i < 0 || !slices.Equal(a.Traces[i].Codes8, tb.Codes8) || !slices.Equal(a.Traces[i].Codes16, tb.Codes16) {
			return false
		}
	}
	return true
}

// WithDecode is the capture with packets reported as decoder dec decodes
// its record (a capture made on a scope whose panel had no decoder set).
// TRLC-LINKS: REQ-DSO-295
func (f *CaptureFile) WithDecode(dec *CaptureDecode) error {
	rec, err := frameOfV2(f.Frame)
	if err != nil {
		return err
	}
	f.Decode = dec
	for _, k := range []string{"packets", "packets.count", "packets.text"} {
		delete(f.Reported, k)
	}
	if dec == nil {
		return nil
	}
	d := dec.display()
	ps := decodePackets(rec, d)
	if ps == nil {
		ps = []string{}
	}
	f.Reported["packets"] = Value{V: ps}
	f.Reported["packets.count"] = num(float64(len(ps)))
	f.Reported["packets.text"] = Value{V: packetBytesText(rec, d)}
	return nil
}

// SetRow sets a sheet row of the scope at c by its text, as the web
// remote does.
// TRLC-LINKS: REQ-DSO-295
func SetRow(ctx context.Context, c *Client, id, text string) error {
	return (&Target{Client: c}).setRow(ctx, id, text, nil)
}
