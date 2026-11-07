// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"image"
	"time"

	"github.com/labeth/open-dso-core/render"
)

// shotWait bounds how long a screen capture waits for the display loop
// (the board's read-back waits for whole frames of its own).
const shotWait = 3 * time.Second

// screenPNG is the device screen as a PNG at the display's native size.
// Over a hardware renderer it is the picture the panel shows: the board's
// read-back of the waveform plane with the UI layer composited over it, on
// the display goroutine; otherwise, or when that fails, the picture drawn
// headless.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-168
func (d *display) screenPNG() []byte {
	if d.capturing.Load() {
		reply := make(chan []byte, 1)
		t := time.NewTimer(shotWait)
		defer t.Stop()
		select {
		case d.shots <- reply:
			select {
			case b := <-reply:
				if b != nil {
					return b
				}
			case <-t.C:
			}
		case <-t.C:
		}
	}
	c := d.canvas()
	d.Render(c)
	return render.EncodePNG(c)
}

// capturePNG reads the board's composed screen back and draws ui, the UI
// layer the loop last presented, over it (display goroutine); nil when the
// board cannot.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-168
func (d *display) capturePNG(ui render.Canvas) []byte {
	disp := d.env.Board.Display()
	if disp == nil {
		return nil
	}
	m := ui.Metrics()
	under := image.NewRGBA(image.Rect(0, 0, m.W, m.H))
	if err := disp.Capture(under); err != nil {
		d.env.Logf("display: screen capture: %v", err)
		return nil
	}
	return render.EncodePNGOver(ui, under)
}
