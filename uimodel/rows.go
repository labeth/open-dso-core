// ENGMODEL-OWNER-UNIT: FU-DSO-UIMODEL

package uimodel

import (
	"fmt"
	"slices"
)

// The row kinds (RowModel.Kind).
const (
	KindEnum   = "enum"
	KindNumber = "number"
	KindAction = "action"
	KindBytes  = "bytes"
)

// RowSpec is a settings row the shared page or the phone remote reads or
// sets by its id: the id, its kind (enum, number, action, bytes) and the
// unit a number row is in ("" for none).
type RowSpec struct {
	ID, Kind, Unit string
}

// PageRows are the rows the shared page and the phone remote drive by id.
// Every scope's sheets list each of them: in use, or present with
// Available false and the reason in Why, so a page never meets a missing
// row and the same id means the same setting on every scope.
var PageRows = []RowSpec{
	{"trig.edge", KindEnum, ""},
	{"trig.level", KindNumber, "V"},
	{"trig.mode", KindEnum, ""},
	{"trig.type", KindEnum, ""},
	{"trig.holdoff", KindNumber, "s"},
	{"ch1.vdiv", KindNumber, "V"},
	{"ch1.offset", KindNumber, "V"},
	{"ch1.bwl", KindEnum, ""},
	{"ch1.invert", KindEnum, ""},
	{"ch2.vdiv", KindNumber, "V"},
	{"ch2.offset", KindNumber, "V"},
	{"ch2.bwl", KindEnum, ""},
	{"ch2.invert", KindEnum, ""},
	{"time.tdiv", KindNumber, "s"},
	{"time.trig_pos", KindNumber, "%"},
	{"time.zoom", KindEnum, ""},
	{"time.zoom_pos", KindNumber, "%"},
	{"acq.mode", KindEnum, ""},
	{"acq.sr_target", KindNumber, ""},
	{"acq.run", KindEnum, ""},
	{"view.view", KindEnum, ""},
	{"view.fft_source", KindEnum, ""},
	{"curs.mode", KindEnum, ""},
	{"curs.a", KindNumber, "s"},
	{"curs.b", KindNumber, "s"},
	{"cmp.math", KindEnum, ""},
	{"cmp.store_a", KindAction, ""},
	{"cmp.mask", KindEnum, ""},
	{"cmp.master_depth", KindEnum, ""},
	{"cmp.stack_master", KindAction, ""},
	{"bus.protocol", KindEnum, ""},
	{"bus.stream_trigger", KindEnum, ""},
	{"bus.stream_value", KindNumber, ""},
	{"bus.baud", KindEnum, ""},
	{"bus.bit_rate", KindNumber, "b/s"},
	{"bus.parity", KindEnum, ""},
	{"bus.uart_line", KindEnum, ""},
	{"bus.source", KindEnum, ""},
	{"bus.scl", KindEnum, ""},
	{"bus.sda", KindEnum, ""},
	{"bus.clk", KindEnum, ""},
	{"bus.data", KindEnum, ""},
	{"bus.spi_mode", KindEnum, ""},
	{"bus.bit_order", KindEnum, ""},
	{"bus.spi_clock", KindNumber, "Hz"},
	{"bus.layer", KindEnum, ""},
	{"bus.layer_address", KindNumber, ""},
	{"bus.match", KindBytes, ""},
	{"bus.trigger_on", KindEnum, ""},
	{"bus.i2c_address", KindNumber, ""},
	{"sys.help", KindAction, ""},
	{"sys.autoset", KindAction, ""},
	{"sys.default", KindAction, ""},
}

// CheckRows reports how sheets m break the row contract: a row of
// PageRows that is missing, listed twice, of another kind or unit, or out
// of use without saying why. No problems is nil.
// TRLC-LINKS: REQ-DSO-219, REQ-DSO-220
func CheckRows(m SheetsModel) []string {
	seen := map[string][]RowModel{}
	for _, t := range m.Tabs {
		for _, r := range t.Rows {
			seen[r.ID] = append(seen[r.ID], r)
		}
	}
	var out []string
	for _, spec := range PageRows {
		rs := seen[spec.ID]
		switch {
		case len(rs) == 0:
			out = append(out, spec.ID+": missing")
			continue
		case len(rs) > 1:
			out = append(out, fmt.Sprintf("%s: listed %d times", spec.ID, len(rs)))
		}
		r := rs[0]
		if r.Kind != spec.Kind {
			out = append(out, fmt.Sprintf("%s: kind %q, want %q", spec.ID, r.Kind, spec.Kind))
		}
		if r.Unit != spec.Unit {
			out = append(out, fmt.Sprintf("%s: unit %q, want %q", spec.ID, r.Unit, spec.Unit))
		}
		if !r.Available && r.Why == "" {
			out = append(out, spec.ID+": out of use without a reason")
		}
	}
	return out
}

// PageRow is the spec of the page row id, ok false when the page does not
// drive it.
// TRLC-LINKS: REQ-DSO-219, REQ-DSO-220
func PageRow(id string) (RowSpec, bool) {
	i := slices.IndexFunc(PageRows, func(r RowSpec) bool { return r.ID == id })
	if i < 0 {
		return RowSpec{}, false
	}
	return PageRows[i], true
}
