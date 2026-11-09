// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"time"

	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/streamwire"
)

// StreamSlot is a quarter-second counter while a decoded stream runs (0
// otherwise): the display's 4 Hz transcript refresh.
// TRLC-LINKS: REQ-DSO-018
func StreamSlot(hud render.HUD) int64 {
	if hud.StreamOn && (hud.Stream.State == streamwire.Streaming || hud.Stream.State == streamwire.Starting) {
		return time.Now().UnixMilli() / 250
	}
	return 0
}

// streamPageKey is what a cached transcript page depends on.
type streamPageKey struct {
	state      streamwire.State
	list       bool
	top, lines int
	trig       int
	slot       int64
}

// cachedStreamPage is a transcript page with what it was formatted for.
type cachedStreamPage struct {
	key  streamPageKey
	page streamwire.Page
}

// FillStreamHUD fills the stream page while BUS streams: the decoded
// stream's state and the transcript lines it shows, formatted again only
// when they can have changed. A stream that formats no page leaves it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-168
func (c *Controller) FillStreamHUD(hud *render.HUD) {
	if !c.StreamMode() {
		return
	}
	c.mu.Lock()
	sv, ok := c.stream.(interface {
		Snapshot() streamwire.View
		Window(rows, top int) streamwire.Page
	})
	c.mu.Unlock()
	if !ok {
		return
	}
	v := sv.Snapshot()
	list, top := c.StreamList()
	hud.StreamOn, hud.Stream = true, v
	hud.StreamList = list || v.State == streamwire.Streaming || v.State == streamwire.Failed || v.State == streamwire.Starting
	// Formatting the page holds the transcript's lock: redo it only when
	// what it shows can have changed (4 Hz while streaming).
	lines := v.Lines
	if v.State == streamwire.Streaming {
		lines = 0 // the slot paces a streaming page; its line count grows every tick
	}
	key := streamPageKey{v.State, hud.StreamList, top, lines, v.TriggerLine, StreamSlot(*hud)}
	if cached := c.streamPage.Load(); cached != nil && cached.key == key {
		hud.StreamPage = cached.page
		return
	}
	switch {
	case v.State == streamwire.Streaming:
		hud.StreamPage = sv.Window(streamwire.ListRows, top) // -1 follows the newest
	case hud.StreamList:
		hud.StreamPage = sv.Window(streamwire.ListRows, top)
	case v.TriggerLine >= 0: // waveform: a strip of the lines around the trigger
		hud.StreamPage = sv.Window(6, v.TriggerLine-2)
	default:
		hud.StreamPage = sv.Window(6, -1)
	}
	c.streamPage.Store(&cachedStreamPage{key, hud.StreamPage})
}
