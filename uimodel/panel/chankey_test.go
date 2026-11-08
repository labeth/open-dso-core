// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// eventPanel is a board's front panel as the panel loop reads it: its
// events come through Events, its lamps are the names SetLamps lit last.
type eventPanel struct {
	ev chan hal.InputEvent
	mu sync.Mutex
	on []string
}

// TRLC-LINKS: REQ-DSO-135
func (p *eventPanel) Events() <-chan hal.InputEvent { return p.ev }

// TRLC-LINKS: REQ-DSO-135
func (p *eventPanel) SetLamps(on []string) error {
	p.mu.Lock()
	p.on = slices.Clone(on)
	p.mu.Unlock()
	return nil
}

// lit reports whether the lamp named name is lit.
func (p *eventPanel) lit(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.on, name)
}

// press is a key pressed and released on the panel, as a board reports it
// (and as the simulated board's bench injects a key): the press, then the
// release. It returns once the loop has run both.
func (p *eventPanel) press(name string) {
	p.ev <- hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: name, Down: true}
	p.ev <- hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: name}
	// The channel is unbuffered and the loop runs one event at a time: this
	// event taken means the release before it has run.
	p.ev <- hal.InputEvent{Source: hal.FromPanel, Kind: hal.Overload}
}

// A channel's key on the scope's panel loop: on a channel switched off it
// switches the channel on, lights its lamp and opens its tab; on the tab
// it opened it closes the sheet and the channel stays on; another
// channel's key moves the sheet to that channel's tab; the key's release
// does nothing. A channel is switched off on its tab (Channel = Off), its
// lamp then dark, and its key switches it on again. Two-channel and
// four-channel panels alike.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-212
func TestChannelKeysOnThePanelLoop(t *testing.T) {
	for _, chans := range []int{2, 4} {
		t.Run(fmt.Sprintf("%dch", chans), func(t *testing.T) {
			c, m, _ := halRig(t, chans)
			p := &eventPanel{ev: make(chan hal.InputEvent)}
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() { c.Run(stop, p); close(done) }()
			t.Cleanup(func() { close(stop); <-done })

			sheet := func() (bool, string) {
				st := c.PanelStatus()
				return st.Sheet.Open, st.Sheet.Tab
			}
			on := func(ch int) bool { return m.State().Channels[ch].On }
			last := fmt.Sprintf("ch%d", chans)

			if r := c.SheetSet(last+".on", "Off", ""); !r.OK || on(chans-1) {
				t.Fatalf("%s.on Off: %+v", last, r)
			}
			p.press("ch1") // settle: the lamps of the state above
			p.press("ch1")
			if p.lit(last) {
				t.Fatalf("%s off, its lamp lit", last)
			}
			for ch := range chans - 1 {
				if !p.lit(fmt.Sprintf("ch%d", ch+1)) {
					t.Fatalf("CH%d on, its lamp dark", ch+1)
				}
			}

			// off → on
			p.press(last)
			if !on(chans-1) || !p.lit(last) {
				t.Fatalf("%s key on a channel off: on %v, lamp %v", last, on(chans-1), p.lit(last))
			}
			if open, tab := sheet(); !open || tab != last {
				t.Fatalf("%s key on a channel off: sheet open %v tab %q", last, open, tab)
			}

			// on its own tab: the sheet closes, the channel stays on
			p.press(last)
			if open, _ := sheet(); open || !on(chans-1) || !p.lit(last) {
				t.Fatalf("%s key on its tab: sheet open %v, on %v, lamp %v", last, open, on(chans-1), p.lit(last))
			}

			// select: each channel's key opens that channel's tab
			for ch := range chans {
				name := fmt.Sprintf("ch%d", ch+1)
				p.press(name)
				if open, tab := sheet(); !open || tab != name {
					t.Fatalf("%s key: sheet open %v tab %q", name, open, tab)
				}
				if !on(ch) || !p.lit(name) {
					t.Fatalf("%s key on a channel on: on %v, lamp %v", name, on(ch), p.lit(name))
				}
			}

			// on → off on the channel's tab, then the key closes the sheet
			// and the next press switches it on again
			p.press("ch1")
			if r := c.SheetSet("ch1.on", "Off", ""); !r.OK || on(0) {
				t.Fatalf("ch1.on Off: %+v", r)
			}
			p.press("ch1")
			if open, _ := sheet(); open || p.lit("ch1") || on(0) {
				t.Fatalf("CH1 key on its tab after Channel = Off: sheet open %v, lamp %v, on %v", open, p.lit("ch1"), on(0))
			}
			p.press("ch1")
			if !on(0) || !p.lit("ch1") {
				t.Fatalf("CH1 key on CH1 off: on %v, lamp %v", on(0), p.lit("ch1"))
			}
			if open, tab := sheet(); !open || tab != "ch1" {
				t.Fatalf("CH1 key on CH1 off: sheet open %v tab %q", open, tab)
			}
		})
	}
}
