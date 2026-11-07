// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/uimodel"
)

// The phone remote's surface (REQ-DSO-211, REQ-DSO-212): the panel snapshot
// with a long-poll, the whole settings-sheet model, and setting a row or
// running an action by id. A remote change never opens, moves or closes
// anything on the display; the scope shows a short REMOTE note instead.

// hPanel injects a front-panel button or knob event (spec 08 §6), or types
// into the display's current context. Body: {"button":"F1"},
// {"knob":"adjust","dir":1,"steps":1}, {"type":{"text":"55 AA"}} (the
// highlighted sheet row, the byte picker or the match editor),
// {"set":{"row":"trig.level","text":"1.25V"}} (a row by id, the display left
// as it is), {"workspace":"bus"} or {"again":true} (the workspace keys for
// the phone remote, an open sheet left as it is; {"again":true,
// "workspace":"bus"} runs BUS's again only while BUS is active). A key or
// knob with "remote":true is answered once it has run, with the rows it
// changed and the notice it raised; it waits while Autoset sweeps, and with
// "nomenu":true while a menu is open on the display. {"button":"auto","for":"bus"}
// runs the AUTO of the workspace the page shows, whichever the display shows.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-220, REQ-DSO-226
func (s *Server) hPanel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Button    string `json:"button"`
		Knob      string `json:"knob"`
		Dir       int    `json:"dir"`
		Steps     int    `json:"steps"`
		Remote    bool   `json:"remote"`
		NoMenu    bool   `json:"nomenu"`
		Workspace string `json:"workspace"`
		Again     bool   `json:"again"`
		For       string `json:"for"` // AUTO: the workspace it frames for
		Type      *struct {
			Text string `json:"text"`
		} `json:"type"`
		Set *struct {
			Row  string `json:"row"`
			Text string `json:"text"`
		} `json:"set"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		WriteBadJSON(w)
		return
	}
	ui, ok := s.sc.UI()
	if !ok {
		WriteJSON(w, map[string]any{"ok": false, "err": "no panel"})
		return
	}
	if req.Workspace != "" || req.Again {
		s.hPanelWorkspace(w, ui, req.Workspace, req.Again)
		return
	}
	if req.Type != nil || req.Set != nil {
		pr, ok := ui.(UIRemote)
		if !ok {
			WriteJSON(w, map[string]any{"ok": false, "err": "no panel"})
			return
		}
		var res uimodel.SetResult
		if req.Type != nil {
			res = pr.TypeText(req.Type.Text)
		} else {
			res = pr.SheetSet(req.Set.Row, nil, req.Set.Text)
		}
		WriteJSON(w, map[string]any{"ok": res.OK, "applied": res.Text, "row": res.Row, "value": res.Value, "error": res.Error}) //nolint:goconst // JSON field names: the literal shows the reply's shape
		return
	}
	if pk, isKeys := ui.(UIKeys); isKeys && req.Remote && (req.Button != "" || req.Knob != "") {
		if req.Button != "" {
			WriteJSON(w, pk.RemoteKey(req.Button, req.NoMenu))
		} else {
			WriteJSON(w, pk.RemoteKnob(req.Knob, req.Dir, req.Steps, req.NoMenu))
		}
		return
	}
	ok = false
	if pa, isAuto := ui.(UIAutoFor); isAuto && req.For != "" && strings.EqualFold(req.Button, "auto") {
		ok = pa.InjectAutoFor(req.For)
	} else if req.Button != "" {
		ok = ui.InjectButton(req.Button)
	} else if req.Knob != "" {
		if req.Steps == 0 {
			req.Steps = 1
		}
		ok = ui.InjectKnob(req.Knob, req.Dir, req.Steps)
	}
	WriteJSON(w, map[string]any{"ok": ok})
}

// hPanelWorkspace switches the workspace ({"workspace":"bus"}) or runs the
// active one's "again" ({"again":true}; with "workspace" it is refused
// unless that one is active) for the phone remote. Unlike F1..F5
// it works while the display has a sheet open and leaves that sheet as it is
// (REQ-DSO-220). The answer carries the workspace, its view, what again
// shows next, the notice the switch itself raised on the display (not one
// showing from before), the rows it changed and the sheet values' count.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-220
func (s *Server) hPanelWorkspace(w http.ResponseWriter, ui UIModel, name string, again bool) {
	pw, ok := ui.(UIWorkspaces)
	if !ok {
		WriteJSON(w, map[string]any{"ok": false, "error": "no panel"})
		return
	}
	var res uimodel.SetResult
	if again {
		res = pw.RemoteAgain(name)
	} else {
		res = pw.RemoteWorkspace(name)
	}
	out := map[string]any{"ok": res.OK, "error": res.Error, "notice": res.Notice, "changed": res.Changed, "sheetsSeq": res.SheetsSeq}
	if ps, ok := ui.(UIStatus); ok {
		st := ps.PanelStatus()
		out["seq"], out["workspace"], out["name"], out["view"], out["next"] = st.Seq, st.WorkspaceID, st.Workspace, st.View, st.AgainNext
	}
	WriteJSON(w, out)
}

// hRefs serves the scope's REF A and REF B (the master is REF A): the web
// app shows the scope's references, not its own. POST {"slot":"A",
// "op":"store"} (store, show, hide, clear) changes one on the scope, as the
// phone's COMPARE rows do, and answers with both slots after it.
// TRLC-LINKS: REQ-DSO-138, REQ-DSO-218, REQ-DSO-227
func (s *Server) hRefs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		return
	}
	pr, ok := s.sc.Refs()
	if !ok {
		WriteJSON(w, map[string]any{"ok": false, "error": "no panel on this server"})
		return
	}
	if r.Method == http.MethodGet {
		WriteJSON(w, map[string]any{"ok": true, "refs": pr.RefsExport()})
		return
	}
	pa, ok := pr.(RefActions)
	if !ok {
		WriteJSON(w, map[string]any{"ok": false, "error": "this scope's references cannot be changed from here"})
		return
	}
	var req struct {
		Slot string `json:"slot"`
		Op   string `json:"op"`
	}
	if err := DecodeSmall(r.Body, &req); err != nil || (req.Slot != "A" && req.Slot != "B") {
		WriteJSON(w, map[string]any{"ok": false, "error": "bad json: want {\"slot\":\"A\"|\"B\",\"op\":store|show|hide|clear}"})
		return
	}
	res := pa.RefAction(int(req.Slot[0]-'A'), req.Op)
	WriteJSON(w, map[string]any{"ok": res.OK, "error": res.Error, "notice": res.Notice, "refs": pr.RefsExport()})
}

// remote returns the UI's remote surface or answers 404.
// TRLC-LINKS: REQ-DSO-162
func (s *Server) remote(w http.ResponseWriter) (UIRemote, bool) {
	var pr UIRemote
	ui, ok := s.sc.UI()
	if ok {
		pr, ok = ui.(UIRemote)
	}
	if !ok {
		http.Error(w, "no panel", http.StatusNotFound)
	}
	return pr, ok
}

// hPanelWait parks until the panel snapshot changes: ?seq=N&timeout_ms=T
// (at most 10 s) returns it as soon as its Seq differs from N, else at the
// timeout.
// TRLC-LINKS: REQ-DSO-162
func (s *Server) hPanelWait(w http.ResponseWriter, r *http.Request) {
	pr, ok := s.remote(w)
	if !ok {
		return
	}
	q := r.URL.Query() // parsed once for both values
	seq, _ := strconv.ParseUint(q.Get("seq"), 10, 64)
	ms, err := strconv.Atoi(q.Get("timeout_ms"))
	if err != nil || ms < 0 {
		ms = 5000
	}
	if ms > 10000 {
		ms = 10000
	}
	WriteJSON(w, pr.WaitPanel(seq, time.Duration(ms)*time.Millisecond))
}

// hSheets returns the whole sheet model: every tab and row with its id,
// kind, options or range, value and whether it is in use, and the Seq.
// TRLC-LINKS: REQ-DSO-162
func (s *Server) hSheets(w http.ResponseWriter, _ *http.Request) {
	pr, ok := s.remote(w)
	if !ok {
		return
	}
	WriteJSON(w, pr.Sheets())
}

// hSheetsSet sets one row: {"row":"trig.level","value":1.25} or
// {"row":"trig.level","text":"1.25V"}.
// TRLC-LINKS: REQ-DSO-162
func (s *Server) hSheetsSet(w http.ResponseWriter, r *http.Request) {
	pr, ok := s.remote(w)
	if !ok {
		return
	}
	var req struct {
		Row   string `json:"row"`
		Value any    `json:"value"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil || req.Row == "" {
		WriteJSON(w, uimodel.SetResult{Error: "bad json: want {\"row\":id,\"value\":v} or {\"row\":id,\"text\":t}"})
		return
	}
	WriteJSON(w, pr.SheetSet(req.Row, req.Value, req.Text))
}

// hStreamPage starts ({"on":true}) or ends the scope's stream for this page,
// which then reads it: the scope ends a stream its page stopped reading.
// {"leave":true} is the page going: only a stream it still holds ends.
// TRLC-LINKS: REQ-DSO-228, REQ-DSO-278
func (s *Server) hStreamPage(w http.ResponseWriter, r *http.Request) {
	pr, ok := s.remote(w)
	if !ok {
		return
	}
	var req struct {
		On    bool `json:"on"`
		Leave bool `json:"leave"` // the page going: only a stream it still holds ends
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req); err != nil {
		WriteJSON(w, uimodel.SetResult{Error: "bad json: want {\"on\":bool}"})
		return
	}
	if pl, ok := pr.(UIPageLeave); ok && req.Leave {
		WriteJSON(w, pl.PageLeave())
		return
	}
	if ps, ok := pr.(UIPageStream); ok {
		WriteJSON(w, ps.PageStream(req.On))
		return
	}
	v := 0.0 // as a sheet set's JSON number
	if req.On {
		v = 1
	}
	WriteJSON(w, pr.SheetSet("bus.stream", v, ""))
}

// hSheetsAction runs an action row: {"row":"file.setup3","action":"save"}
// (the action may be left out on a row with one).
// TRLC-LINKS: REQ-DSO-162
func (s *Server) hSheetsAction(w http.ResponseWriter, r *http.Request) {
	pr, ok := s.remote(w)
	if !ok {
		return
	}
	var req struct {
		Row    string `json:"row"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil || req.Row == "" {
		WriteJSON(w, uimodel.SetResult{Error: "bad json: want {\"row\":id,\"action\":name}"})
		return
	}
	WriteJSON(w, pr.SheetAction(req.Row, req.Action))
}
