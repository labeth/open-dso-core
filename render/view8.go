// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import "github.com/labeth/open-dso-core/sample"

// view8Cache keeps the last wide frame's 8-bit view: the display redraws the
// same frame as the HUD changes around it.
var view8Cache sample.View8Cache

// view8 is frame f on the 8-bit view the renderer draws (SPLIT-LEARNINGS:
// the 8-bit view plus the trace's own codes a division): an 8-bit frame as
// it is; a frame of wider words on the view with its words kept as Q8.8
// precision (sample.View8Frame), which the trace drawing reads for rows
// finer than the view's codes.
// TRLC-LINKS: REQ-DSO-021
func view8(f *sample.Frame) *sample.Frame { return view8Cache.Of(f) }
