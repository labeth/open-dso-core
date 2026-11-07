// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

// SetupSlot is one setup slot as the scope's FILE tab shows it.
type SetupSlot struct {
	Slot    int    `json:"slot"`
	Used    bool   `json:"used"`
	Summary string `json:"summary,omitempty"`
}
