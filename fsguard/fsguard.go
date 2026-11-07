// ENGMODEL-OWNER-UNIT: FU-DSO-FSGUARD

// Package fsguard is the write guard of a scope's firmware: every file its
// programs write, rename or delete on the unit goes through one, and so does
// every write to a device node or kernel attribute and every writable
// /dev/mem window.
//
// A scope stays recoverable as long as nothing of ours touches what its
// vendor boot and recovery path depend on. Which paths those are is the
// board's knowledge; the guard is the mechanism. The board hands it a
// Policy: the one writable root (after cleaning the path and resolving
// symlinks), the vendor areas that are never written, the directories below
// the root the vendor boot flashes or runs, file names never written
// anywhere, device nodes and kernel attributes never opened for writing,
// physical register windows never mapped writable, and the block device a
// partition installer may mount. Every refusal wraps ErrRefused and says
// why.
//
// The writable root is the policy's unless the process says otherwise once
// at start (SetRoot): an installer that mounts the root's partition
// elsewhere, and tests. SetRoot itself refuses a root inside a vendor area.
package fsguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Policy is a board's rules. Directories and paths are clean and absolute.
type Policy struct {
	// Root is the writable root at run time: the only tree files are
	// written under. RootName names it in refusals ("p12").
	Root, RootName string
	// Vendor are trees no file is ever written in, even when Root lies
	// inside one of them (the vendor partitions). A root never contains
	// one of them either, unless it is listed in Enclosable.
	Vendor []Area
	// Enclosable are the Vendor trees a root may contain: a mount point
	// above them is a place the root's partition is mounted at (an
	// installer's /mnt above a /mnt/logo).
	Enclosable []string
	// System are the directories of the vendor system and the kernel's
	// pseudo file systems: a root never lies inside one, unless inside
	// Root.
	System []string
	// RootForbidden are directories below the root, relative to it, the
	// vendor boot turns into writes of other partitions.
	RootForbidden []Area
	// Names are file base names, lower case, never written anywhere (with
	// or without a ".new" suffix); NamesWhy says why.
	Names    []string
	NamesWhy string
	// DeviceWords are parts of device and attribute paths, lower case,
	// that reach one-time programmable or secure-boot storage.
	DeviceWords []string
	// BlockPrefixes are the block-device nodes and block-layer attribute
	// paths of the unit, lower case.
	BlockPrefixes []string
	// Phys are the physical register blocks no writable /dev/mem window
	// may cover.
	Phys []Window
	// Device is the root partition's block device, the one an installer
	// mounts for writing, and DeviceFS its file system (CheckInstallMounts).
	Device, DeviceFS string
	// UnitSources are /proc/mounts sources that are the unit's own
	// storage, by prefix (CheckInstallMounts, CheckOffUnit).
	UnitSources []string
}

// Area is a directory and why it is never written.
type Area struct {
	Dir, Why string
}

// Window is a physical register block and what it is.
type Window struct {
	Base, Size uint64
	What       string
}

// ErrRefused is what every refusal of the guard wraps.
var ErrRefused = errors.New("refused by the write guard")

// RefusedError is a refused write: the path and the rule it breaks.
type RefusedError struct {
	Op, Path, Reason string
}

// TRLC-LINKS: REQ-DSO-296
func (e *RefusedError) Error() string {
	return fmt.Sprintf("%s %s: %v: %s", e.Op, e.Path, ErrRefused, e.Reason)
}

// Unwrap makes errors.Is(err, ErrRefused) hold.
// TRLC-LINKS: REQ-DSO-296
func (e *RefusedError) Unwrap() error { return ErrRefused }

// TRLC-LINKS: REQ-DSO-296
func refuse(op, path, reason string) error { return &RefusedError{Op: op, Path: path, Reason: reason} }

// Guard checks and performs writes under one policy. A process has one
// guard per policy; its root is process-wide.
type Guard struct {
	p    Policy
	mu   sync.RWMutex
	root string
}

// New is the guard of policy p, rooted at p.Root. A policy without a root
// refuses every file write.
// TRLC-LINKS: REQ-DSO-296
func New(p Policy) *Guard {
	return &Guard{p: p, root: p.Root}
}

// Policy is the guard's policy.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) Policy() Policy { return g.p }

// Root is the directory writes may go under.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) Root() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.root
}

// SetRoot makes dir the writable root and returns the function that puts
// the previous one back. It refuses a dir that is, contains or lies inside
// a vendor area: only the policy's root, a directory below it, or a
// directory outside every vendor area (an installer's mount point of the
// root's partition, a test's temporary directory) can be a root.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) SetRoot(dir string) (restore func(), err error) {
	clean, err := abs(dir)
	if err != nil {
		return nil, err
	}
	if g.p.Root == "" || !within(clean, g.p.Root) {
		for _, v := range g.p.Vendor {
			if within(v.Dir, clean) && !slices.Contains(g.p.Enclosable, v.Dir) {
				return nil, refuse("root", dir, "contains "+v.Dir+": "+v.Why)
			}
			if within(clean, v.Dir) {
				return nil, refuse("root", dir, "lies in "+v.Dir+": "+v.Why)
			}
		}
		for _, s := range g.p.System {
			if within(clean, s) {
				return nil, refuse("root", dir, "a system area ("+s+") cannot be the writable root")
			}
		}
	}
	g.mu.Lock()
	prev := g.root
	g.root = clean
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		g.root = prev
		g.mu.Unlock()
	}, nil
}

// TRLC-LINKS: REQ-DSO-296
func (g *Guard) rootName() string {
	if g.p.RootName != "" {
		return g.p.RootName
	}
	return "the writable"
}

// Check returns nil when path may be written, created, renamed or deleted:
// it lies inside the writable root once cleaned and with every symlink of
// its existing part resolved, in no vendor area, not in a directory below
// the root the vendor boot flashes or runs, and has no forbidden name.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) Check(path string) error {
	clean, err := abs(path)
	if err != nil {
		return err
	}
	if err := g.checkName(clean); err != nil {
		return err
	}
	if isPseudo(clean) {
		return refuse("write", path, "a device node or kernel attribute is not a file (CheckDevice guards those)")
	}
	r := g.Root()
	if r == "" {
		return refuse("write", path, "the board named no writable root")
	}
	rr, err := resolve(r)
	if err != nil {
		return err
	}
	rp, err := resolve(clean)
	if err != nil {
		return err
	}
	if err := g.checkName(rp); err != nil {
		return err
	}
	for _, p := range []string{clean, rp} {
		for _, v := range g.p.Vendor {
			if within(p, v.Dir) && (g.p.Root == "" || !within(p, g.p.Root)) {
				return refuse("write", path, v.Why)
			}
		}
	}
	name := g.rootName()
	if !within(clean, r) {
		return refuse("write", path, "outside "+name+"'s root "+r)
	}
	if !within(rp, rr) {
		return refuse("write", path, "resolves to "+rp+", outside "+name+"'s root "+r)
	}
	rel, _ := filepath.Rel(rr, rp)
	for _, d := range g.p.RootForbidden {
		if within(filepath.Join("/", rel), filepath.Join("/", d.Dir)) {
			return refuse("write", path, name+"/"+d.Dir+": "+d.Why)
		}
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-296
func (g *Guard) checkName(p string) error {
	b := strings.ToLower(filepath.Base(p))
	for _, n := range g.p.Names {
		if b == n || b == n+".new" {
			why := g.p.NamesWhy
			if why == "" {
				why = "a file of this name is never written"
			}
			return refuse("write", p, why)
		}
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-296
func isPseudo(p string) bool {
	return within(p, "/dev") || within(p, "/sys") || within(p, "/proc")
}

// abs is the cleaned absolute path.
// TRLC-LINKS: REQ-DSO-296
func abs(p string) (string, error) {
	if p == "" {
		return "", refuse("write", p, "empty path")
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("fsguard: %w", err)
	}
	return a, nil
}

// resolve resolves every symlink of the longest existing prefix of p and
// appends the rest, so a path that does not exist yet is judged by where it
// would be created. A dangling symlink is followed to where its target
// would be created.
// TRLC-LINKS: REQ-DSO-296
func resolve(p string) (string, error) {
	return resolveDepth(p, 0)
}

// maxLinks bounds the symlinks followed, as the kernel's ELOOP does.
const maxLinks = 40

// TRLC-LINKS: REQ-DSO-296
func resolveDepth(p string, depth int) (string, error) {
	if depth > maxLinks {
		return "", refuse("write", p, "too many levels of symbolic links")
	}
	rest := ""
	cur := p
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(r, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("fsguard: resolve %s: %w", p, err)
		}
		if st, lerr := os.Lstat(cur); lerr == nil && st.Mode()&os.ModeSymlink != 0 {
			target, rerr := os.Readlink(cur)
			if rerr != nil {
				return "", fmt.Errorf("fsguard: resolve %s: %w", p, rerr)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			return resolveDepth(filepath.Join(target, rest), depth+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// within reports whether p is dir or below it (both clean and absolute).
// TRLC-LINKS: REQ-DSO-296
func within(p, dir string) bool {
	if dir == "/" {
		return true
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}
