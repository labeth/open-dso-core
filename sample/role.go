package sample

// Role is what a window of a record is for: the screen's main window, or a
// detail window read beside it.
type Role int

// The roles. RoleView(n) numbers the magnified views' detail windows.
const (
	Main    Role = iota // the screen's window
	Zoom                // a zoom window beside the main one
	Edge                // full-rate window around the trigger edge
	EdgeOpp             // full-rate window around the first opposite edge after it
	view0               // first View role
)

// RoleView is the n-th magnified view's detail window. (It is not called
// View: that is the hardware renderer's view.)
// TRLC-LINKS: REQ-DSO-1003
func RoleView(n int) Role { return view0 + Role(n) }

// ViewIndex returns n for RoleView(n), or false for another role.
// TRLC-LINKS: REQ-DSO-1003
func (r Role) ViewIndex() (int, bool) { return int(r - view0), r >= view0 }
