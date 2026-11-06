// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/labeth/open-dso-core/webserver/shape"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// hSet applies one scalar control: {"control":name,"value":v} and the
// qualifiers' parameters. The reply is {"ok","applied","err"}.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req Control
	// 4 KB cap: a scalar-verb body is tens of bytes; an unbounded decoder
	// would buffer an attacker-sized body on a 64 MB-class ARM.
	if err := DecodeSmall(r.Body, &req); err != nil {
		WriteBadJSON(w)
		return
	}
	for _, v := range []float64{req.Value, req.Lvl, req.Lo, req.Hi, req.Min, req.Max} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			WriteJSON(w, map[string]any{"ok": false, "err": "bad value"})
			return
		}
	}
	a := s.sc.Settings().Set(r.Context(), req)
	out := map[string]any{"ok": a.OK}
	if a.Value != nil {
		out["applied"] = a.Value
	}
	if !a.Brief {
		out["err"] = a.Err
	}
	WriteJSON(w, out)
}

// hZones installs the zone-trigger rectangles (POST JSON array of zones in
// edge-anchored seconds x display codes). Empty array clears.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	tr, ok := s.sc.Triggers()
	if !ok {
		http.Error(w, "no zone trigger", http.StatusNotFound)
		return
	}
	var zs []struct {
		DtLoS  float64 `json:"dt_lo_s"`
		DtHiS  float64 `json:"dt_hi_s"`
		CodeLo int     `json:"code_lo"`
		CodeHi int     `json:"code_hi"`
		Avoid  bool    `json:"avoid"`
		Ch     int     `json:"ch"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&zs); err != nil {
		WriteBadJSON(w)
		return
	}
	if len(zs) > 4 {
		zs = zs[:4]
	}
	out := make([]wire.Zone, 0, len(zs))
	for _, z := range zs {
		if math.IsNaN(z.DtLoS) || math.IsNaN(z.DtHiS) || math.IsInf(z.DtLoS, 0) || math.IsInf(z.DtHiS, 0) {
			continue
		}
		out = append(out, wire.Zone{
			DtLoS: z.DtLoS, DtHiS: z.DtHiS,
			CodeLo: clampI(z.CodeLo, 0, 255), CodeHi: clampI(z.CodeHi, 0, 255),
			Avoid: z.Avoid, Ch: z.Ch & 1,
		})
	}
	tr.SetZones(out)
	WriteJSON(w, map[string]any{"ok": true, "zones": len(out)})
}

// hSerial installs the serial/protocol-trigger configuration (POST, the
// scope's configuration as JSON); arm and disarm are separate, via
// /api/set {control:"serialmode"}. GET answers the trigger as armed (on
// the display, a phone or a page): a page shows it rather than replace it
// with its own empty match.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hSerial(w http.ResponseWriter, r *http.Request) {
	tr, ok := s.sc.Triggers()
	if r.Method == http.MethodGet {
		cfg, mode, readable := any(nil), 0, false
		if ok {
			cfg, mode, readable = tr.SerialConfig()
		}
		if !readable {
			WriteJSON(w, map[string]any{"ok": false, "err": "no serial trigger"})
			return
		}
		WriteJSON(w, map[string]any{"ok": true, "params": cfg, "mode": mode})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		return
	}
	if !ok {
		http.Error(w, "no serial trigger", http.StatusNotFound)
		return
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	if err := tr.SetSerial(dec.Decode); err != nil {
		WriteBadJSON(w)
		return
	}
	WriteJSON(w, map[string]any{"ok": true})
}

// hSerialProtocols lists the protocols the trigger takes.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hSerialProtocols(w http.ResponseWriter, _ *http.Request) {
	tr, ok := s.sc.Triggers()
	if !ok {
		http.Error(w, "no serial trigger", http.StatusNotFound)
		return
	}
	WriteJSON(w, tr.SerialProtocols())
}

// TRLC-LINKS: REQ-SDS-162
func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// hMask uploads the envelope mask (POST JSON {lo:[],hi:[],win,ch}; empty lo
// clears). Full masks use compact base64 lo_b64/hi_b64 byte arrays.
// The envelopes are display-window columns (win = the scope's window
// columns at build time); the client builds and dilates.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hMask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	mk, ok := s.sc.Masks()
	if !ok {
		http.Error(w, "no mask test", http.StatusNotFound)
		return
	}
	var m struct {
		Lo         []int   `json:"lo"`
		LoBytes    []uint8 `json:"lo_b64"`
		HiBytes    []uint8 `json:"hi_b64"`
		Hi         []int   `json:"hi"`
		Win        int     `json:"win"`
		Ch         int     `json:"ch"`
		TdivS      float64 `json:"tdiv_s"`
		SampleS    float64 `json:"sample_s"`
		PeakDetect bool    `json:"peak_detect"`
		IfGen      *uint64 `json:"if_gen"` // a re-install: the install it replaces (the status's mask_gen)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&m); err != nil {
		WriteBadJSON(w)
		return
	}
	var lo, hi []uint8
	if len(m.LoBytes) > 0 {
		if len(m.Lo) > 0 || len(m.Hi) > 0 || len(m.LoBytes) != m.Win || len(m.HiBytes) != m.Win || m.Win <= 0 || m.Win > 1<<20 {
			WriteJSON(w, map[string]any{"ok": false, "err": "length mismatch"})
			return
		}
		lo, hi = m.LoBytes, m.HiBytes
	} else {
		if len(m.Lo) == 0 {
			mk.ClearMask()
			WriteJSON(w, map[string]any{"ok": true, "cleared": true, "gen": mk.MaskGen()})
			return
		}
		// Preserve the original bounded numeric upload for older clients.
		if len(m.Lo) != m.Win || len(m.Hi) != m.Win || m.Win <= 0 || m.Win > 1<<16 {
			WriteJSON(w, map[string]any{"ok": false, "err": "length mismatch"})
			return
		}
		lo, hi = make([]uint8, m.Win), make([]uint8, m.Win)
		for i := range m.Win {
			lo[i] = uint8(clampI(m.Lo[i], 0, 255) & 0xff)
			hi[i] = uint8(clampI(m.Hi[i], 0, 255) & 0xff)
		}
	}
	// a page re-scaling its mask to a new V/div names the install it replaces:
	// refused once the scope's mask changed since (cleared or replaced on the
	// display, by a phone or another page), so a cleared mask does not come back
	if m.IfGen != nil {
		if cur := mk.MaskGen(); cur != *m.IfGen {
			WriteJSON(w, map[string]any{"ok": false, "stale": true, "gen": cur, "err": "the scope's mask changed since this page installed it"})
			return
		}
	}
	mk.SetMask(wire.Mask{Lo: lo, Hi: hi, WinCols: m.Win, Ch: m.Ch & 1, TdivS: m.TdivS, SampleS: m.SampleS, PeakDetect: m.PeakDetect})
	// gen names this install: the page keeps re-scaling its mask only while
	// the scope still holds it (the status's mask_gen)
	WriteJSON(w, map[string]any{"ok": true, "win": m.Win, "gen": mk.MaskGen()})
}

// hBode serves the accumulated Frequency-Response (Bode) curve as parallel
// arrays (compact for the plot): frequency (Hz), magnitude (dB), phase (deg),
// sorted ascending by frequency.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hBode(w http.ResponseWriter, _ *http.Request) {
	bd, ok := s.sc.Bode()
	if !ok {
		http.Error(w, "no Bode", http.StatusNotFound)
		return
	}
	pts := bd.BodePoints()
	f := make([]float64, len(pts))
	g := make([]float64, len(pts))
	p := make([]float64, len(pts))
	for i, pt := range pts {
		f[i], g[i], p[i] = pt.FreqHz, pt.GainDB, pt.PhaseDeg
	}
	WriteJSON(w, map[string]any{"ok": true, "n": len(pts), "freq": f, "gain_db": g, "phase_deg": p})
}

// hMaskFail serves one captured failing frame from the ring as JSON (gallery
// click — not a hot path). ?i=k indexes the snapshot, most recent last.
// TRLC-LINKS: REQ-SDS-162
func (s *Server) hMaskFail(w http.ResponseWriter, r *http.Request) {
	mk, ok := s.sc.Masks()
	if !ok {
		http.Error(w, "no mask test", http.StatusNotFound)
		return
	}
	ring := mk.MaskFails()
	i := 0
	_, _ = fmt.Sscanf(r.URL.Query().Get("i"), "%d", &i) // bad: 0, the newest
	if i < 0 || i >= len(ring) {
		WriteJSON(w, map[string]any{"ok": false, "err": "no such failure", "count": len(ring)})
		return
	}
	f := ring[i]
	// Current calibrated vertical scales, so the replay view reads real volts.
	// The ring stores raw codes only, so these are the scales NOW — exact
	// unless V/div, offset, or probe changed since the failure was captured,
	// and far better than a hard-coded 1 V/div ×1 (CSV/sigrok exports were
	// wrong off that scale).
	off, vpc := mk.VertScales()
	WriteJSON(w, map[string]any{
		"ok": true, "i": i, "count": len(ring),
		"seq": f.Seq, "at_ms": f.AtNs / 1e6,
		"fail_col": f.FailCol, "fail_code": f.FailCode, "fail_sample": f.FailSample,
		"edge_x": f.EdgeX, "sample_s": f.SampleS, "valid": f.Valid, "win_cols": f.WinCols,
		"vpc1": vpc[0], "vpc2": vpc[1], "off1_v": off[0], "off2_v": off[1],
		"c1": shape.RawInt16(f.C1), "c2": shape.RawInt16(f.C2),
	})
}
