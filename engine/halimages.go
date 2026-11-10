// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"slices"
	"sync/atomic"

	"github.com/labeth/open-dso-core/hal"
)

// The FPGA image a trigger runs on is the board's to name (hal.ImageChooser,
// ADR-HAL-IMAGE-CHOICE): while the committed trigger asks for an image other
// than the current one, the cycle says why the trigger waits ("loading the
// qualifier FPGA image"), runs the trigger it has free, and loads the image
// at the next pass, between acquisitions; then it commits the whole state
// again on the new image. A load that fails goes back to the base image and
// is not tried again until the choice changes.

// halImages is the cycle's image switching.
type halImages struct {
	keep   atomic.Bool // hold the current image while it serves the trigger (HoldImage)
	again  atomic.Bool // the hold changed: the next pass commits and chooses again
	due    []string    // the features of the image to load at the next pass; nil: none
	failed []string    // the image whose load failed, not tried again until the choice changes
	chosen []string    // the image the last commit chose
}

// HoldImage asks the board to stay on the current image while it serves
// the trigger (AUTO in BUS measuring edge-triggered between the
// qualifier's idle triggers, without two loads of seconds); false lets the
// trigger choose again. ANY.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-223
func (c *HALCycle) HoldImage(on bool) {
	if c.img.keep.Swap(on) != on {
		c.img.again.Store(true) // the choice is made again at the next pass
	}
}

// images is the board's images with its chooser, when it has both.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) images() (hal.Images, hal.ImageChooser, bool) {
	im, ok := c.b.Images()
	if !ok {
		return nil, nil, false
	}
	ch, ok := im.(hal.ImageChooser)
	return im, ch, ok
}

// imageDue reports trigger t needs an image other than the current one,
// and names it (keep: stay on the current one while it serves t, beside
// the hold); the load is due at the next pass. A choice whose load
// failed is not due until the choice changes.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) imageDue(t hal.TriggerSpec, keep bool) (name string, due bool) {
	im, ch, ok := c.images()
	if !ok {
		return "", false
	}
	want := choose(ch, t, keep, c.img.keep.Load())
	c.img.chosen = want
	if c.img.failed != nil && !sameFeatures(want, c.img.failed) {
		c.img.failed = nil
	}
	cur := im.Current()
	if sameFeatures(want, cur.Features) || sameFeatures(want, c.img.failed) {
		return "", false
	}
	c.img.due = want
	return imageCalled(im, want), true
}

// onImageFor reports the board is on the image trigger t runs on (a board
// without images always is).
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func (c *HALCycle) onImageFor(t hal.TriggerSpec) bool {
	im, ch, ok := c.images()
	return !ok || sameFeatures(choose(ch, t, false, c.img.keep.Load()), im.Current().Features)
}

// choose is the image ch names for trigger t: kept for the owner's own
// measuring, or held by the user (HoldImage); a board that tells the two
// apart (hal.HeldChooser) says which images each keeps.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-223
func choose(ch hal.ImageChooser, t hal.TriggerSpec, measuring, held bool) []string {
	if hc, ok := ch.(hal.HeldChooser); ok {
		return hc.ChooseHeld(t, measuring, held)
	}
	return ch.Choose(t, measuring || held)
}

// loadDueImage loads the image a commit found due; false when none was
// due. A failed load goes back to the base image. OWNER, LONG.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-231
func (c *HALCycle) loadDueImage(ctx context.Context) bool {
	want := c.img.due
	c.img.due = nil
	im, _, ok := c.images()
	if want == nil || !ok || sameFeatures(want, im.Current().Features) {
		return false // none due, or the board is on it already (a stream loaded it)
	}
	name := imageName(im, want)
	c.stopArmed()
	c.inst.SetBusy("loading " + name + " image")
	defer c.inst.SetBusy("")
	c.cfg.Logf("engine: loading the %s image", name)
	if err := im.Load(ctx, want); err != nil {
		c.cfg.Logf("engine: %s image: %v", name, err)
		c.img.failed = want
		if err := im.Load(ctx, nil); err != nil {
			c.cfg.Logf("engine: base image: %v", err)
		}
	}
	c.inst.SetImage(im.Current().Name)
	c.stopped = stoppedRecord{content: c.stopped.content} // the load lost the stored record
	c.gen = 0
	return true
}

// imageName is the image with features f as the board names it, else its
// first feature.
// TRLC-LINKS: REQ-DSO-011
func imageName(im hal.Images, f []string) string {
	return imageNamed(im, f, func(s hal.ImageSet) string { return s.Name })
}

// imageCalled is the image with features f as a trigger waiting for it
// says it (ImageSet.Called), else as imageName names it.
// TRLC-LINKS: REQ-DSO-011
func imageCalled(im hal.Images, f []string) string {
	return imageNamed(im, f, func(s hal.ImageSet) string {
		if s.Called != "" {
			return s.Called
		}
		return s.Name
	})
}

// imageNamed is the image with features f by name, else its first feature.
// TRLC-LINKS: REQ-DSO-011
func imageNamed(im hal.Images, f []string, name func(hal.ImageSet) string) string {
	for _, s := range im.Provides() {
		if n := name(s); sameFeatures(s.Features, f) && n != "" {
			return n
		}
	}
	if len(f) == 0 {
		return "base"
	}
	return f[0]
}

// sameFeatures reports a and b name the same features.
// TRLC-LINKS: REQ-DSO-011
func sameFeatures(a, b []string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return len(a) == len(b) && !slices.ContainsFunc(a, func(f string) bool { return !slices.Contains(b, f) })
}

// checkConverters runs the board's periodic converter check when it is
// due, between acquisitions while the run captures (never in a stream or a
// roll, which return before it); true when a recovery loaded the image
// again, so the state is committed again and the stored record is gone.
// OWNER, LONG.
// TRLC-LINKS: REQ-DSO-231
func (c *HALCycle) checkConverters(ctx context.Context) bool {
	cc, ok := c.b.Platform().(hal.ConverterChecker)
	if !ok || !cc.CheckDue() {
		return false
	}
	c.stopArmed()
	if !cc.CheckConverters(ctx) {
		return false
	}
	c.stopped = stoppedRecord{content: c.stopped.content}
	c.gen = 0
	return true
}
