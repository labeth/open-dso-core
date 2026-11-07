// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"io"
	"net/http"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// setupSlotCount is the slot range the web offers (the SCPI *SAV range).
const setupSlotCount = 20

// hSetup saves or recalls a setup slot (the web top bar's Setups):
// POST {"op":"save"|"recall","slot":1..20} → {"ok":bool,"err":string}.
// GET lists what the slots hold, as the scope's FILE tab shows them:
// {"ok":true,"slots":[{"slot":1,"used":true,"summary":"BUS  500us/div ..."}]}
// (slots is null when the store cannot say).
// TRLC-LINKS: REQ-SDS-219
func (s *Server) hSetup(w http.ResponseWriter, r *http.Request) {
	setups, have := s.sc.Setups()
	if r.Method == http.MethodGet {
		if !have {
			WriteJSON(w, map[string]any{"ok": false, "err": "setup memory unavailable"})
			return
		}
		var slots []wire.SetupSlot
		if l, ok := setups.(SetupLister); ok {
			slots = l.SetupSlots()
		}
		WriteJSON(w, map[string]any{"ok": true, "slots": slots})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Op   string `json:"op"`
		Slot int    `json:"slot"`
	}
	if err := DecodeSmall(io.LimitReader(r.Body, 1<<10), &req); err != nil {
		WriteBadJSON(w)
		return
	}
	if req.Slot < 1 || req.Slot > setupSlotCount {
		WriteJSON(w, map[string]any{"ok": false, "err": "slot must be 1-20"})
		return
	}
	if !have {
		WriteJSON(w, map[string]any{"ok": false, "err": "setup memory unavailable"})
		return
	}
	var err error
	switch req.Op {
	case "save":
		err = setups.SaveSetup(req.Slot)
	case "recall":
		err = setups.RecallSetup(req.Slot)
	default:
		WriteJSON(w, map[string]any{"ok": false, "err": "op must be save or recall"})
		return
	}
	if err != nil {
		WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
		return
	}
	WriteJSON(w, map[string]any{"ok": true})
}
