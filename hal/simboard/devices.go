package simboard

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
)

type platform struct{ b *Board }

// BringUp answers the configured identity.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) BringUp(ctx context.Context) (hal.Identity, error) {
	return p.b.profile.Identity, ctx.Err()
}

// Handover is empty: nothing supervises the simulated board.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) Handover() hal.Handover { return hal.Handover{FDs: map[string]int{}} }

// Service applies the front-end settings staged while nothing is armed.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) Service() {
	b := p.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if (b.state == hal.Idle || b.state == hal.Ready) && !slices.Equal(b.staged.channels, b.applied.channels) {
		b.applied.channels = slices.Clone(b.staged.channels) // a setting staged while nothing is armed applies now
	}
}

// Worker is none.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) Worker() func() int { return nil }

// Reboot is refused: there is no unit to restart.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) Reboot(string) error {
	return hal.Refuse("reboot", "the simulated board has no unit to restart: reload the page")
}

// Diag is the board's counters.
// TRLC-LINKS: REQ-DSO-1001
func (p platform) Diag() json.RawMessage {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	out, _ := json.Marshal(map[string]any{"records": p.b.waits, "forced": p.b.forced, "simulated": true})
	return out
}

// screen is the display's framebuffer, the picture last presented as RGBA
// and its number.
type screen struct {
	mu    sync.Mutex
	spec  hal.DisplaySpec
	back  []byte
	rgba  []byte
	seq   uint64
	ready chan struct{} // closed and replaced at every present
}

// init sizes the framebuffer for spec.
// TRLC-LINKS: REQ-DSO-1001
func (s *screen) init(spec hal.DisplaySpec) {
	s.spec = spec
	bpp := 4
	if spec.Format == hal.RGB565 {
		bpp = 2
	}
	s.back = make([]byte, spec.W*spec.H*bpp)
	s.rgba = make([]byte, spec.W*spec.H*4)
	s.ready = make(chan struct{})
}

// stride is the framebuffer's bytes a row.
// TRLC-LINKS: REQ-DSO-1001
func (s *screen) stride() int { return len(s.back) / max(s.spec.H, 1) }

// present converts the back buffer to RGBA (opaque: there is no plane
// under it) and wakes whoever waits for a picture.
// TRLC-LINKS: REQ-DSO-1001
func (s *screen) present(dim bool) {
	s.mu.Lock()
	px, out := s.back, s.rgba
	if s.spec.Format == hal.RGB565 {
		for i, j := 0, 0; i+1 < len(px); i, j = i+2, j+4 {
			v := uint16(px[i]) | uint16(px[i+1])<<8
			r, g, b := byte(v>>11)<<3, byte(v>>5&0x3f)<<2, byte(v&0x1f)<<3
			out[j], out[j+1], out[j+2], out[j+3] = r|r>>5, g|g>>6, b|b>>5, 255
		}
	} else {
		for i := 0; i+3 < len(px); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = px[i+2], px[i+1], px[i], 255
		}
	}
	if dim {
		clear(out)
		for i := 3; i < len(out); i += 4 {
			out[i] = 255
		}
	}
	s.seq++
	close(s.ready)
	s.ready = make(chan struct{})
	s.mu.Unlock()
}

// Screen is the picture last presented, as RGBA rows of the display's
// width, once its number is past after or wait has passed; the slice is a
// copy.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Screen(after uint64, wait time.Duration) (rgba []byte, w, h int, seq uint64) {
	s := &b.screen
	s.mu.Lock()
	if s.seq <= after && wait > 0 {
		ready := s.ready
		s.mu.Unlock()
		select {
		case <-ready:
		case <-time.After(wait):
		}
		s.mu.Lock()
	}
	defer s.mu.Unlock()
	return slices.Clone(s.rgba), s.spec.W, s.spec.H, s.seq
}

type display struct{ b *Board }

// Spec implements hal.Display.
// TRLC-LINKS: REQ-DSO-1001
func (d display) Spec() hal.DisplaySpec { return d.b.profile.Display }

// Framebuffer implements hal.Display.
// TRLC-LINKS: REQ-DSO-1001
func (d display) Framebuffer() hal.Framebuffer { return framebuffer(d) }

// Renderer implements hal.Display: the core draws the traces.
// TRLC-LINKS: REQ-DSO-1001
func (d display) Renderer() (hal.Renderer, bool) { return nil, false }

// Capture is the picture last presented.
// TRLC-LINKS: REQ-DSO-1001
func (d display) Capture(dst *image.RGBA) error {
	s := &d.b.screen
	if dst.Rect.Dx() < s.spec.W || dst.Rect.Dy() < s.spec.H {
		return fmt.Errorf("simboard: capture into %v, need %dx%d", dst.Rect, s.spec.W, s.spec.H)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for y := range s.spec.H {
		for x := range s.spec.W {
			i := (y*s.spec.W + x) * 4
			dst.SetRGBA(dst.Rect.Min.X+x, dst.Rect.Min.Y+y, color.RGBA{R: s.rgba[i], G: s.rgba[i+1], B: s.rgba[i+2], A: 255})
		}
	}
	return nil
}

// Backlight is on or off: a dark screen presents black.
// TRLC-LINKS: REQ-DSO-1001
func (d display) Backlight(level float64) error {
	if level < 0 || level > 1 {
		return hal.Refuse("backlight", "level is 0..1")
	}
	d.b.mu.Lock()
	d.b.backl = level
	d.b.mu.Unlock()
	return framebuffer(d).Present()
}

type framebuffer struct{ b *Board }

// Back implements hal.Framebuffer.
// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Back() []byte { return f.b.screen.back }

// Stride implements hal.Framebuffer.
// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Stride() int { return f.b.screen.stride() }

// Present shows the back buffer: the host hears it as RGBA.
// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Present() error {
	b := f.b
	b.mu.Lock()
	dark := b.backl == 0
	b.mu.Unlock()
	b.screen.present(dark)
	if b.cfg.OnPresent != nil {
		s := &b.screen
		s.mu.Lock()
		b.cfg.OnPresent(s.rgba, s.spec.W, s.spec.H)
		s.mu.Unlock()
	}
	return nil
}

type input struct{ b *Board }

// Events implements hal.Input: the bench's keys, knobs and touches
// (Inject).
// TRLC-LINKS: REQ-DSO-1001
func (in input) Events() <-chan hal.InputEvent { return in.b.events }

// Keys implements hal.Input.
// TRLC-LINKS: REQ-DSO-1001
func (in input) Keys() []string { return in.b.profile.Inputs.Keys }

// Knobs implements hal.Input.
// TRLC-LINKS: REQ-DSO-1001
func (in input) Knobs() []string { return in.b.profile.Inputs.Knobs }

// SetLamps keeps the lit lamps for the bench page, by core name: the page
// lights the ones its panel has.
// TRLC-LINKS: REQ-DSO-1001
func (in input) SetLamps(on []string) error {
	b := in.b
	lit := slices.Clone(on)
	b.mu.Lock()
	changed := !slices.Equal(lit, b.lamps)
	b.lamps = lit
	b.mu.Unlock()
	if changed && b.cfg.OnLamps != nil {
		b.cfg.OnLamps(slices.Clone(lit))
	}
	return nil
}

// Lamps are the lamps lit now.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Lamps() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.lamps)
}

// SetSound implements hal.Input.
// TRLC-LINKS: REQ-DSO-1001
func (in input) SetSound(on bool) error {
	in.b.mu.Lock()
	defer in.b.mu.Unlock()
	in.b.sound = on
	return nil
}

// Beep implements hal.Input: the bench page has no beeper.
// TRLC-LINKS: REQ-DSO-1001
func (in input) Beep(time.Duration) error { return nil }

// SetTouch implements hal.Input.
// TRLC-LINKS: REQ-DSO-1001
func (in input) SetTouch(bool) error {
	if !in.b.profile.Display.Touch {
		return hal.Refuse("touch", "this scope has no touch screen")
	}
	return nil
}

// Inject queues an input event from the bench's panel; false when the
// queue is full (the UI model is not keeping up).
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Inject(ev hal.InputEvent) bool {
	select {
	case b.events <- ev:
		return true
	default:
		return false
	}
}

type storage struct{ b *Board }

// Settings implements hal.Storage.
// TRLC-LINKS: REQ-DSO-1001
func (s storage) Settings() hal.KV { return s.b.settings }

// Setups implements hal.Storage.
// TRLC-LINKS: REQ-DSO-1001
func (s storage) Setups() (hal.KV, bool) { return s.b.setups, true }

// Captures is nowhere: the browser keeps no files.
// TRLC-LINKS: REQ-DSO-1001
func (s storage) Captures() (string, error) { return "", nil }

// Calibration implements hal.Storage.
// TRLC-LINKS: REQ-DSO-1001
func (s storage) Calibration() hal.CalSource { return frontEnd(s).Calibration() }

// KV is an in-memory store that tells its host every change, so the host
// can keep it (a browser's local storage).
type KV struct {
	mu      sync.Mutex
	name    string
	m       map[string][]byte
	budget  int
	persist func(store, key string, val []byte)
}

// newKV is a store named name holding init.
// TRLC-LINKS: REQ-DSO-1001
func newKV(name string, budget int, init map[string][]byte, persist func(string, string, []byte)) *KV {
	m := map[string][]byte{}
	for k, v := range init {
		m[k] = slices.Clone(v)
	}
	return &KV{name: name, m: m, budget: budget, persist: persist}
}

// Get implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Get(key string) ([]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return slices.Clone(v), ok
}

// Put implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Put(key string, val []byte) error {
	k.mu.Lock()
	if k.budget > 0 {
		used := len(val)
		for kk, v := range k.m {
			if kk != key {
				used += len(v)
			}
		}
		if used > k.budget {
			k.mu.Unlock()
			return hal.Refuse("store "+key, fmt.Sprintf("%d bytes past the %d-byte budget", used-k.budget, k.budget))
		}
	}
	k.m[key] = slices.Clone(val)
	persist := k.persist
	k.mu.Unlock()
	if persist != nil {
		persist(k.name, key, slices.Clone(val))
	}
	return nil
}

// Budget implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Budget() int { return k.budget }

// Name is where key lives, for the log.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Name(key string) string { return k.name + "/" + key }

// Keys are the store's keys, sorted.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Sorted(maps.Keys(k.m))
}

type census struct{}

// Run implements hal.Census.
// TRLC-LINKS: REQ-DSO-1001
func (census) Run(ctx context.Context, _ bool) ([]hal.Verdict, error) {
	return []hal.Verdict{{Part: "simulated board", Verdict: "present"}}, ctx.Err()
}

type counter struct{ b *Board }

// Source is the source whose comparator the simulated counter measures.
// TRLC-LINKS: REQ-DSO-1001
func (c counter) Source() hal.Source {
	c.b.mu.Lock()
	defer c.b.mu.Unlock()
	st := c.b.applied.trigger.Stages
	if len(st) == 0 {
		return hal.Source{Kind: hal.Analog, Index: -1}
	}
	return st[0].Source
}

// Read is the trigger source's repetition rate.
// TRLC-LINKS: REQ-DSO-1001
func (c counter) Read() (hal.Counters, error) {
	b := c.b
	b.mu.Lock()
	st := b.applied.trigger.Stages
	b.mu.Unlock()
	if len(st) == 0 || st[0].Source.Kind != hal.Analog {
		return hal.Counters{}, nil
	}
	s := b.sig.Signal(st[0].Source.Index)
	if s.Freq <= 0 || s.Kind == "dc" || s.Kind == "noise" {
		return hal.Counters{}, nil
	}
	return hal.Counters{FreqHz: s.Freq, Valid: true}, nil
}
