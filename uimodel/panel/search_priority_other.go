// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

//go:build !linux

package panel

// atSearchPriority runs fn as is: thread priorities are a Linux system
// call, and other platforms (the browser demo's WebAssembly) have no
// threads to prioritise.
// TRLC-LINKS: REQ-DSO-214
func atSearchPriority(fn func()) { fn() }
