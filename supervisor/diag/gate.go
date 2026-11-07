// ENGMODEL-OWNER-UNIT: FU-OTA-DIAG

package diag

// Check is one condition a stage needs, as the stages before it found it.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Passed reports whether every check passed, and the first that did not.
// TRLC-LINKS: REQ-SDS-287
func Passed(cs []Check) (bool, string) {
	for _, c := range cs {
		if !c.OK {
			return false, c.Name + ": " + c.Detail
		}
	}
	return true, ""
}

// Verdict is what a census says about one part of the board.
type Verdict struct {
	Part    string `json:"part"`
	Verdict string `json:"verdict"` // present, absent, partial, other
	Detail  string `json:"detail"`
}
