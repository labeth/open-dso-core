// Package streamwire is the decoded stream's transcript as the web server
// serves it: the formatted window the display's list writes and the lines
// as data for a reader following the stream.
// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE
package streamwire

// ListRows is how many transcript lines the full-screen list shows; the
// panel centres the trigger with it.
const ListRows = 40

// Page is a window of formatted transcript lines.
type Page struct {
	Lines       []string
	Top, Total  int // first line shown, lines held
	TriggerLine int // -1 when unknown
	Header      string
}

// LineData is one transcript line as data: its absolute index since the
// stream started, the first unit's sample ordinal, the units, and which of
// them are decode errors or (I2C) not acknowledged.
type LineData struct {
	Index  int      `json:"i"`
	Sample uint64   `json:"s"`
	Units  []uint32 `json:"v"`
	Errs   uint16   `json:"e,omitempty"`
	Naks   uint16   `json:"k,omitempty"`
	Note   string   `json:"n,omitempty"`
}

// LinesPage answers Lines: Base is the oldest line still held, Next the index
// to ask for next; a reader that falls behind Base has missed lines.
type LinesPage struct {
	State string     `json:"state"`
	Base  int        `json:"base"`
	Next  int        `json:"next"`
	Lines []LineData `json:"lines"`
}
