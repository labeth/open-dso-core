// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webtest

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/shape"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Fake is a scope for the server's own tests and a starting point for a new
// scope's adapter: a two-channel 8-bit scope with one published frame (a sine
// on C1, a square on C2), the settings it echoes, and, unless Bare, every
// optional part with canned answers.
type Fake struct {
	// Bare leaves out every optional part.
	Bare bool

	mu      sync.Mutex
	seq     uint64
	running bool
	tdivS   float64
	maskGen uint64
	mask    *wire.Mask
	zones   []wire.Zone
	serial  map[string]any
	sound   uimodel.SoundSettings
	saved   map[int]bool
	leases  map[string]time.Duration
}

// NewFake is a running fake scope with its first frame published.
// TRLC-LINKS: REQ-DSO-161
func NewFake(bare bool) *Fake {
	return &Fake{Bare: bare, seq: 1, running: true, tdivS: 1e-6, saved: map[int]bool{}, leases: map[string]time.Duration{}}
}

// Publish publishes the next frame.
// TRLC-LINKS: REQ-DSO-163
func (f *Fake) Publish() {
	f.mu.Lock()
	f.seq++
	f.mu.Unlock()
}

// Lease is the hold of a need the server last asked for (0 when given back).
// TRLC-LINKS: REQ-DSO-163
func (f *Fake) Lease(kind string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leases[kind]
}

// fakeN is the samples of the fake's record.
const fakeN = 2000

// record is the fake's record: a sine on C1, a square on C2.
// TRLC-LINKS: REQ-DSO-163
func record() (c1, c2 []uint8) {
	c1, c2 = make([]uint8, fakeN), make([]uint8, fakeN)
	for i := range fakeN {
		c1[i] = uint8(128 + math.Round(60*math.Sin(2*math.Pi*float64(i)/200)))
		c2[i] = 70
		if i%250 < 125 {
			c2[i] = 190
		}
	}
	return c1, c2
}

// Status is a small status document.
// TRLC-LINKS: REQ-DSO-164
func (f *Fake) Status() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[string]any{"running": f.running, "seq": f.seq, "tdiv_s": f.tdivS, "mask_gen": f.maskGen}
}

// Frames is the fake itself.
// TRLC-LINKS: REQ-DSO-163
func (f *Fake) Frames() webserver.Frames { return fakeFrames{f} }

// Settings is the fake itself.
// TRLC-LINKS: REQ-DSO-162
func (f *Fake) Settings() webserver.Settings { return fakeSettings{f} }

// UI is the fake's panel unless Bare.
// TRLC-LINKS: REQ-DSO-162
func (f *Fake) UI() (webserver.UIModel, bool) { return &fakeUI{f: f}, !f.Bare }

// Refs is the fake's references unless Bare.
// TRLC-LINKS: REQ-DSO-138
func (f *Fake) Refs() (webserver.Refs, bool) { return &fakeUI{f: f}, !f.Bare }

// Stream is the fake's transcript unless Bare.
// TRLC-LINKS: REQ-DSO-018
func (f *Fake) Stream() (webserver.Stream, bool) { return fakeStream{}, !f.Bare }

// Records takes the fake's record unless Bare.
// TRLC-LINKS: REQ-DSO-280
func (f *Fake) Records() (webserver.Records, bool) { return fakeRecords{f}, !f.Bare }

// Triggers is the fake's zone and serial triggers unless Bare.
// TRLC-LINKS: REQ-DSO-162
func (f *Fake) Triggers() (webserver.Triggers, bool) { return fakeTriggers{f}, !f.Bare }

// Masks is the fake's mask test unless Bare.
// TRLC-LINKS: REQ-DSO-162
func (f *Fake) Masks() (webserver.Masks, bool) { return fakeMasks{f}, !f.Bare }

// Bode is a two-point curve unless Bare.
// TRLC-LINKS: REQ-DSO-162
func (f *Fake) Bode() (webserver.Bode, bool) { return fakeBode{}, !f.Bare }

// Sound is the fake's audio stream unless Bare.
// TRLC-LINKS: REQ-DSO-265
func (f *Fake) Sound() (webserver.Sound, bool) { return fakeSound{f}, !f.Bare }

// SoundControls is SOUND's settings unless Bare.
// TRLC-LINKS: REQ-DSO-274
func (f *Fake) SoundControls() (webserver.SoundControls, bool) { return fakeSound{f}, !f.Bare }

// ClientDecode is never the fake's: it decodes no digital stream.
// TRLC-LINKS: REQ-DSO-240
func (f *Fake) ClientDecode() (webserver.ClientDecode, bool) { return nil, false }

// Setups is the fake's setup memory unless Bare.
// TRLC-LINKS: REQ-DSO-219
func (f *Fake) Setups() (webserver.Setups, bool) { return fakeSetups{f}, !f.Bare }

// Screen is a fixed PNG signature unless Bare.
// TRLC-LINKS: REQ-DSO-161
func (f *Fake) Screen() (webserver.Screen, bool) { return fakeScreen{}, !f.Bare }

// Extensions is one scope route unless Bare.
// TRLC-LINKS: REQ-DSO-161
func (f *Fake) Extensions() []webserver.Route {
	if f.Bare {
		return nil
	}
	return []webserver.Route{{Pattern: "GET /api/fake/hello", Feature: "fake", Handler: helloHandler{}}}
}

// fakeFrames serves the fake's frame.
type fakeFrames struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-163
func (fr fakeFrames) Wait(since uint64, timeout time.Duration) {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		fr.f.mu.Lock()
		seq := fr.f.seq
		fr.f.mu.Unlock()
		if seq != since {
			return
		}
	}
}

// TRLC-LINKS: REQ-DSO-163
func (fr fakeFrames) Frame(q webserver.FrameQuery) wire.FrameReply {
	fr.f.mu.Lock()
	seq, tdiv := fr.f.seq, fr.f.tdivS
	fr.f.mu.Unlock()
	if seq == q.Since {
		return wire.FrameReply{Seq: seq, Unchanged: true, EdgeX: -1}
	}
	c1, c2 := record()
	return wire.FrameReply{
		Seq: seq, Cols: q.Cols, EdgeX: 1000, TdivS: tdiv, DisplayedS: tdiv, ColSpanS: 10 * tdiv, Interp: true,
		C1: shape.Window(c1, fakeN, 1000, 1000, true, q.Cols, 0.5), C2: shape.Window(c2, fakeN, 1000, 1000, true, q.Cols, 0.5),
		EdgeFrac: 0.5, WinFrac: 1, Vpc1: 0.04, Vpc2: 0.04,
	}
}

// TRLC-LINKS: REQ-DSO-163
func (fr fakeFrames) RawFrame(since uint64) wire.RawFrame {
	fr.f.mu.Lock()
	seq := fr.f.seq
	fr.f.mu.Unlock()
	if seq == since {
		return wire.RawFrame{Header: wire.FrameReply{Seq: seq, Unchanged: true, EdgeX: -1}, Unchanged: true}
	}
	c1, c2 := record()
	return wire.RawFrame{
		Header:  wire.FrameReply{Seq: seq, Cols: fakeN, EdgeX: 1000, SampleS: 1e-8, EdgeFrac: -1, WinFrac: 1},
		Payload: append(c1, c2...),
	}
}

// TRLC-LINKS: REQ-DSO-163
func (fr fakeFrames) Lease(n webserver.Need) bool {
	fr.f.mu.Lock()
	defer fr.f.mu.Unlock()
	fr.f.leases[n.Kind] = n.Hold
	return true
}

// fakeSettings applies the controls the fake has.
type fakeSettings struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-162
func (s fakeSettings) Set(_ context.Context, c webserver.Control) webserver.Applied {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	switch c.Name {
	case "run":
		s.f.running = c.Value != 0
		return webserver.Applied{OK: true, Value: c.Value}
	case "tdiv":
		if !(c.Value >= 1e-9 && c.Value <= 50) {
			return webserver.Applied{Value: c.Value, Err: "tdiv not in ladder"}
		}
		s.f.tdivS = c.Value
		return webserver.Applied{OK: true, Value: c.Value}
	case "memdepth":
		return webserver.Applied{OK: true, Value: int(c.Value), Brief: true}
	}
	return webserver.Applied{Value: c.Value, Err: "unknown control"}
}

// fakeUI is the fake's panel: keys, the remote surface and the references.
type fakeUI struct {
	f *Fake
}

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) InjectButton(name string) bool { return name != "" }

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) InjectKnob(name string, _, _ int) bool { return name != "" }

// TRLC-LINKS: REQ-DSO-164
func (u *fakeUI) PanelStatus() uimodel.Status {
	return uimodel.Status{Seq: 3, Workspace: "SCOPE", WorkspaceID: "scope", View: "Y-T", Mode: "workspace"}
}

// TRLC-LINKS: REQ-DSO-164
func (u *fakeUI) WaitPanel(uint64, time.Duration) uimodel.Status { return u.PanelStatus() }

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) Sheets() uimodel.SheetsModel {
	rows := []uimodel.RowModel{{ID: "trig.level", Label: "Level", Kind: "number", Available: true, Value: 0.2, Text: "200mV", Unit: "V"}}
	for _, r := range uimodel.PageRows {
		if r.ID != "trig.level" {
			rows = append(rows, uimodel.RowModel{ID: r.ID, Label: r.ID, Kind: r.Kind, Unit: r.Unit, Why: "the fake has no " + r.ID})
		}
	}
	return uimodel.SheetsModel{Seq: 3, Tabs: []uimodel.TabModel{{ID: "trig", Name: "TRIG", Rows: rows}}}
}

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) SheetSet(row string, value any, text string) uimodel.SetResult {
	if row != "trig.level" {
		return uimodel.SetResult{Row: row, Error: "no row " + row}
	}
	return uimodel.SetResult{OK: true, Row: row, Value: value, Text: text}
}

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) SheetAction(row, _ string) uimodel.SetResult {
	return uimodel.SetResult{Row: row, Error: "no action row " + row}
}

// TRLC-LINKS: REQ-DSO-162
func (u *fakeUI) TypeText(text string) uimodel.SetResult {
	return uimodel.SetResult{OK: true, Row: "trig.level", Text: text}
}

// TRLC-LINKS: REQ-DSO-220
func (u *fakeUI) RemoteWorkspace(name string) uimodel.SetResult {
	return uimodel.SetResult{OK: name == "scope", Row: "workspace", Value: name}
}

// TRLC-LINKS: REQ-DSO-220
func (u *fakeUI) RemoteAgain(string) uimodel.SetResult {
	return uimodel.SetResult{OK: true, Row: "again", Value: "X-Y"}
}

// TRLC-LINKS: REQ-DSO-138
func (u *fakeUI) RefsExport() [2]uimodel.RefExport {
	return [2]uimodel.RefExport{{Has: true, Show: true, Gen: 1, Cols: 2, C1: []int{100, 120}}, {}}
}

// TRLC-LINKS: REQ-DSO-138
func (u *fakeUI) RefAction(slot int, op string) uimodel.SetResult {
	if op != "store" && op != "clear" {
		return uimodel.SetResult{Error: "no " + op}
	}
	return uimodel.SetResult{OK: true, Notice: string(rune('A'+slot)) + " " + op} //nolint:gosec // G115: a reference slot, 0 or 1
}

// fakeStream is a two-line transcript.
type fakeStream struct{}

// TRLC-LINKS: REQ-DSO-018
func (fakeStream) StreamLines(from, limit int) (streamwire.LinesPage, bool) {
	p := streamwire.LinesPage{State: "STREAM", Next: from}
	for i := from; i < 2 && len(p.Lines) < limit; i++ {
		p.Lines = append(p.Lines, streamwire.LineData{Index: i, Units: []uint32{0x55, 0xAA}})
		p.Next = i + 1
	}
	return p, true
}

// TRLC-LINKS: REQ-DSO-018
func (fakeStream) StreamLinesBinary(_ int, dst []byte) (int, bool) { return copy(dst, "55 AA\n"), true }

// TRLC-LINKS: REQ-DSO-018
func (fakeStream) StreamWindow(rows int) (streamwire.Page, bool) {
	return streamwire.Page{Lines: []string{"55 AA"}, Total: 1, Top: 0, TriggerLine: -1, Header: "hex"}, rows > 0
}

// fakeRecords takes the fake's record.
type fakeRecords struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-280
func (r fakeRecords) Take(_ context.Context, sampleS float64, _ time.Duration) (wire.Record, error) {
	r.f.mu.Lock()
	seq, running := r.f.seq, r.f.running
	r.f.mu.Unlock()
	if !running {
		return wire.Record{}, errors.New("the scope is stopped: it captures no record (Run it)")
	}
	if sampleS < 1e-8 {
		return wire.Record{}, errors.New("no record sampled that finely")
	}
	c1, c2 := record()
	return wire.Record{Seq: seq, SampleS: 1e-8, TdivS: 1e-6, Vpc: [2]float64{0.04, 0.04}, C1: c1, C2: c2}, nil
}

// fakeTriggers keeps what it is given.
type fakeTriggers struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-162
func (t fakeTriggers) SetZones(z []wire.Zone) {
	t.f.mu.Lock()
	t.f.zones = z
	t.f.mu.Unlock()
}

// TRLC-LINKS: REQ-DSO-162
func (t fakeTriggers) SerialConfig() (any, int, bool) {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	return t.f.serial, 0, true
}

// TRLC-LINKS: REQ-DSO-162
func (t fakeTriggers) SetSerial(decode func(v any) error) error {
	var p map[string]any
	if err := decode(&p); err != nil {
		return err
	}
	t.f.mu.Lock()
	t.f.serial = p
	t.f.mu.Unlock()
	return nil
}

// TRLC-LINKS: REQ-DSO-162
func (t fakeTriggers) SerialProtocols() any {
	return []map[string]any{{"id": 1, "name": "uart"}}
}

// fakeMasks keeps the mask it is given.
type fakeMasks struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-162
func (m fakeMasks) SetMask(mk wire.Mask) {
	m.f.mu.Lock()
	m.f.mask = &mk
	m.f.maskGen++
	m.f.mu.Unlock()
}

// TRLC-LINKS: REQ-DSO-014
func (m fakeMasks) ClearMask() {
	m.f.mu.Lock()
	m.f.mask = nil
	m.f.maskGen++
	m.f.mu.Unlock()
}

// TRLC-LINKS: REQ-DSO-162
func (m fakeMasks) MaskGen() uint64 {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	return m.f.maskGen
}

// TRLC-LINKS: REQ-DSO-162
func (m fakeMasks) MaskFails() []wire.MaskFail {
	return []wire.MaskFail{{C1: []uint8{1, 2}, C2: []uint8{3, 4}, Valid: 2, Seq: 1, WinCols: 2, FailCol: 1, FailCode: 2}}
}

// TRLC-LINKS: REQ-DSO-164
func (m fakeMasks) VertScales() (off, vpc [2]float64) { return off, [2]float64{0.04, 0.04} }

// fakeBode is a two-point curve.
type fakeBode struct{}

// TRLC-LINKS: REQ-DSO-066
func (fakeBode) BodePoints() []wire.BodePoint {
	return []wire.BodePoint{{FreqHz: 1e3, GainDB: 0, PhaseDeg: 0}, {FreqHz: 1e4, GainDB: -3, PhaseDeg: -45}}
}

// fakeSound is a stream of 64 samples and SOUND's settings.
type fakeSound struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-277
func (fakeSound) Lease(string) string { return "" }

// TRLC-LINKS: REQ-DSO-265
func (fakeSound) Info() wire.SoundInfo {
	return wire.SoundInfo{State: "streaming", Active: true, RateHz: 48000, Next: 64}
}

// TRLC-LINKS: REQ-DSO-265
func (fakeSound) Read(from uint64, limit int) wire.SoundBlock {
	from = min(from, 64)
	n := int(min(uint64(limit), 64-from)) //nolint:gosec // G115: limit is a block count the server passes, never negative
	b := wire.SoundBlock{
		First: from, N: n, Next: 64, RateHz: 48000, Q1: make([]uint16, n), Q2: make([]uint16, n),
		Scales: []wire.SoundScale{{VPC: [2]float64{0.04, 0.04}}},
	}
	for i := range n {
		b.Q1[i], b.Q2[i] = 32768, 32768
	}
	return b
}

// TRLC-LINKS: REQ-DSO-265
func (fakeSound) Wait(uint64, time.Duration) {}

// TRLC-LINKS: REQ-DSO-275
func (s fakeSound) Take() *wire.SoundTake {
	return &wire.SoundTake{SoundBlock: s.Read(0, 16), ID: 1, Why: "stop"}
}

// TRLC-LINKS: REQ-DSO-274
func (s fakeSound) SoundSettings() uimodel.SoundSettings {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return s.f.sound
}

// TRLC-LINKS: REQ-DSO-274
func (s fakeSound) SetSoundSettings(v uimodel.SoundSettings) uimodel.SoundSettings {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.sound = v
	return v
}

// TRLC-LINKS: REQ-DSO-274
func (fakeSound) SoundAction(name string) error {
	if name != "take_scope" {
		return errors.New("no action " + name)
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-274
func (s fakeSound) SoundReport(bool, bool) any {
	return struct {
		Settings uimodel.SoundSettings `json:"settings"`
		Hz       float64               `json:"hz"` // no reading yet: null on the wire
	}{s.SoundSettings(), math.NaN()}
}

// TRLC-LINKS: REQ-DSO-274
func (fakeSound) SoundWant(bool, bool) {}

// fakeSetups remembers the slots saved.
type fakeSetups struct{ f *Fake }

// TRLC-LINKS: REQ-DSO-219
func (s fakeSetups) SaveSetup(slot int) error {
	s.f.mu.Lock()
	s.f.saved[slot] = true
	s.f.mu.Unlock()
	return nil
}

// TRLC-LINKS: REQ-DSO-219
func (s fakeSetups) RecallSetup(slot int) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if !s.f.saved[slot] {
		return errors.New("the slot is empty")
	}
	return nil
}

// fakeScreen is a PNG signature.
type fakeScreen struct{}

// TRLC-LINKS: REQ-DSO-161
func (fakeScreen) PNG() []byte { return []byte("\x89PNG\r\n\x1a\n") }
