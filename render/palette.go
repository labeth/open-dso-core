// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"strconv"

	"github.com/labeth/open-dso-core/design"
)

// The LCD palette: each colour is a token of the shared palette
// (open-dso-core/design), so the LCD and the web UI cannot diverge.
var (
	colAxis   = tok("axis")
	colBG     = tok("screen")
	colBoxBg  = tok("lcd-box")
	colC1     = tok("c1")
	colC2     = tok("c2")
	colCursor = tok("cursor")
	colDim    = tok("dim")
	colGrid   = tok("grid")
	colInfo   = tok("text")
	colMath   = tok("math")
	colMenuBg = tok("lcd-menu")
	colOK     = tok("run")
	colSR     = tok("sr")
	colStale  = tok("stop")
	colTrig   = tok("trigger")
	colWarn   = tok("warn")
)

// colChans are the channels' trace colours, C1 first: tokens c1, c2, ...
var colChans = func() (cs []Color) {
	for i := 1; ; i++ {
		c, ok := design.Tokens["c"+strconv.Itoa(i)]
		if !ok {
			return cs
		}
		cs = append(cs, rgb(c.R, c.G, c.B))
	}
}()

// chanColor is channel ch's trace colour; past the palette's channel
// tokens the colours repeat.
// TRLC-LINKS: REQ-SDS-021
func chanColor(ch int) Color { return colChans[ch%len(colChans)] }

// chanName is channel ch's name, C1 first.
// TRLC-LINKS: REQ-SDS-021
func chanName(ch int) string { return "C" + strconv.Itoa(ch+1) }

// tok is the shared palette's colour name in the LCD's RGB565. A name the
// palette lacks is a programming error, caught at start and by the tests.
// TRLC-LINKS: REQ-SDS-021
func tok(name string) Color {
	c, ok := design.Tokens[name]
	if !ok {
		panic("lcd: palette has no token " + name)
	}
	return rgb(c.R, c.G, c.B)
}
