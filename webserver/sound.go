// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// SOUND's web surface (ADR-SOUND): /api/sound.bin hands the audio stream to
// the browser (the page and the phone play it and analyse their own copy),
// /api/sound reads and changes the settings and the scope's shared
// readouts (V/oct table, drift log, envelope capture). Every request keeps
// the stream leased for a few seconds; when the last page goes, the stream
// stops and the scope captures again. The stream takes the whole
// acquisition, so the last one to act wins: a key on the display takes it
// back and the pages' requests are refused (the page paused) until one
// takes the scope again (the action "take_scope").

// leaseSound is a page's SOUND request holding the stream; "" when held,
// else the paused page's state. who=phone names the phone.
// TRLC-LINKS: REQ-DSO-277, REQ-DSO-265
func leaseSound(src Sound, r *http.Request) string {
	who := ""
	if r.URL.Query().Get("who") == "phone" {
		who = "phone"
	}
	return src.Lease(who)
}

// hSoundBin serves the stream from ordinal from (or the newest stretch),
// parking up to wait ms for samples not yet there (or for min of them): a
// binary header (magic, flags, the JSON length) and JSON, then N
// little-endian Q8.8 codes of C1 and N of C2.
// TRLC-LINKS: REQ-DSO-265
func (s *Server) hSoundBin(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.Sound()
	if !ok {
		http.Error(w, "no audio stream", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	if paused := leaseSound(src, r); paused != "" {
		// Paused: no samples; the reply waits as a parked one would, so a
		// paused page does not poll the scope in a tight loop.
		if wait, err := strconv.Atoi(q.Get("wait")); err == nil && wait > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Duration(min(wait, 600)) * time.Millisecond):
			}
		}
		info := src.Info()
		h := wire.SoundBinHeader{
			First: info.Next, Next: info.Next, Rate: info.RateHz, State: paused,
			Paused: true, ServerMS: time.Now().UnixMilli(),
		}
		writeSoundBin(w, h, wire.SoundBlock{}, 5*time.Second)
		return
	}
	if sp, ok := s.sc.SoundControls(); ok {
		sp.SoundWant(false, q.Get("marks") == "1")
	}
	maxN := 8192
	if v, err := strconv.Atoi(q.Get("max")); err == nil && v > 0 {
		maxN = min(v, 1<<16)
	}
	info := src.Info()
	var from uint64
	if f, err := strconv.ParseUint(q.Get("from"), 10, 64); err == nil {
		from = f
	} else if info.Next > 4096 {
		from = info.Next - 4096 // a new listener starts near now
	}
	// wait parks the reply until samples come; min until that many have:
	// a listener's request a seal period long, not one per bank, keeps the
	// replies (and the scope's work for them) to about fifty a second.
	minN := 1
	if v, err := strconv.Atoi(q.Get("min")); err == nil && v > 1 {
		minN = min(v, maxN)
	}
	if wait, err := strconv.Atoi(q.Get("wait")); err == nil && wait > 0 && from+uint64(minN) > info.Next {
		src.Wait(from+uint64(minN)-1, time.Duration(min(wait, 1500))*time.Millisecond)
		info = src.Info()
	}
	b := src.Read(from, maxN)
	h := wire.SoundBinHeader{
		First: b.First, N: b.N, Next: b.Next, Rate: b.RateHz, Gaps: b.Gaps, Marks: b.Marks,
		State: info.State, Active: info.Active, Losses: info.Losses, MarksOn: info.Marks, MarkCh: info.MarkCh,
		ServerMS: time.Now().UnixMilli(), Err: info.Err,
	}
	h.Scales = b.Scales
	h.MarkV = wire.LevelVolts(h.Scales, info.MarkCh, info.MarkLevel)
	writeSoundBin(w, h, b, 5*time.Second)
}

// writeSoundBin writes a header and block b as /api/sound.bin's format
// (wire.EncodeSoundBin), within deadline.
// TRLC-LINKS: REQ-DSO-265, REQ-DSO-275
func writeSoundBin(w http.ResponseWriter, h any, b wire.SoundBlock, deadline time.Duration) {
	buf := wire.EncodeSoundBin(h, b.Q1, b.Q2, b.N)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(deadline))
	_, _ = w.Write(buf)
}

// hSoundTakeBin serves the take shown (STOP, or a NORMAL or SINGLE
// trigger's) in /api/sound.bin's format with the take's header; 404 with
// none. A page fetches it once per take (its id in /api/sound's info).
// TRLC-LINKS: REQ-DSO-275
func (s *Server) hSoundTakeBin(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.Sound()
	var tk SoundTakes
	if ok {
		tk, ok = src.(SoundTakes)
	}
	if !ok {
		http.Error(w, "no audio stream", http.StatusNotFound)
		return
	}
	leaseSound(src, r) // paused, the take shown (if any) still comes
	t := tk.Take()
	if t == nil {
		http.Error(w, "no take", http.StatusNotFound)
		return
	}
	h := wire.SoundTakeHeader{
		ID: t.ID, Why: t.Why, First: t.First, N: t.N, Rate: t.RateHz, Gaps: t.Gaps, Marks: t.Marks,
		HasTrig: t.HasTrig, Trig: t.Trig, TrigCh: t.TrigCh, TrigRising: t.TrigRising,
	}
	h.Scales = t.Scales
	h.TrigV = wire.LevelVolts(h.Scales, t.TrigCh, t.TrigLevel)
	writeSoundBin(w, h, t.SoundBlock, 20*time.Second)
}

// soundPost is a /api/sound change: settings (only the fields sent change)
// and an action.
type soundPost struct {
	Settings json.RawMessage `json:"settings"`
	Action   string          `json:"action"`
}

// hSound reads (GET) or changes (POST) SOUND: the settings, the shared
// readouts and the stream's state. GET with marks=1 keeps the FPGA
// marking the tuned channel's edges for a page's cycle view.
// TRLC-LINKS: REQ-DSO-274
func (s *Server) hSound(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.sc.SoundControls()
	if !ok {
		http.Error(w, "no SOUND here", http.StatusNotFound)
		return
	}
	var errText string
	var req soundPost
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
			WriteBadJSON(w)
			return
		}
		if req.Action == "take_scope" {
			// Before the lease: the page takes the scope back from the display.
			if err := sp.SoundAction(req.Action); err != nil {
				errText = err.Error()
			}
			req.Action = ""
		}
	}
	if src, ok := s.sc.Sound(); ok && r.URL.Query().Get("lease") != "0" {
		leaseSound(src, r)
	}
	if r.Method == http.MethodPost { //nolint:nestif // a POST's settings and action, applied in order
		if len(req.Settings) > 0 {
			cur := sp.SoundSettings()
			if err := json.Unmarshal(req.Settings, &cur); err != nil {
				WriteJSON(w, map[string]any{"ok": false, "err": "bad settings"})
				return
			}
			sp.SetSoundSettings(cur)
		}
		if req.Action != "" {
			if err := sp.SoundAction(req.Action); err != nil {
				errText = err.Error()
			}
		}
	}
	// api=1 asks the scope to keep its own tuner and meters current (a page
	// that does not analyse the stream itself); the web page and the phone
	// analyse their copy and leave the scope's CPU to the stream.
	rep := sp.SoundReport(r.URL.Query().Get("api") == "1", r.URL.Query().Get("marks") == "1")
	out := map[string]any{"ok": errText == "", "report": wire.JSONSafe(reflect.ValueOf(rep))}
	if errText != "" {
		out["err"] = errText
	}
	WriteJSON(w, out)
}
