// ENGMODEL-OWNER-UNIT: FU-DSO-FSGUARD

package fsguard

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Mount is one line of /proc/mounts.
type Mount struct {
	Source, Dir, FS string
	RW              bool
}

// ParseMounts reads a /proc/mounts listing.
// TRLC-LINKS: REQ-DSO-296
func ParseMounts(r io.Reader) ([]Mount, error) {
	var ms []Mount
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		rw := strings.Contains(","+f[3]+",", ",rw,")
		ms = append(ms, Mount{Source: unescape(f[0]), Dir: unescape(f[1]), FS: f[2], RW: rw})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("fsguard: mounts: %w", err)
	}
	return ms, nil
}

// unitSource reports whether a /proc/mounts source is the unit's own
// storage.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) unitSource(src string) bool {
	for _, p := range g.p.UnitSources {
		if strings.HasPrefix(src, p) {
			return true
		}
	}
	return false
}

// CheckInstallMounts is an installer's check after it mounted the root's
// partition on dir: dir holds the policy's Device as DeviceFS, and
// no other partition of the unit is mounted writable. Only then may the
// install write (with dir as the root).
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) CheckInstallMounts(mounts io.Reader, dir string) error {
	ms, err := ParseMounts(mounts)
	if err != nil {
		return err
	}
	dev, fs := g.p.Device, g.p.DeviceFS
	if dev == "" {
		return refuse("mount", dir, "the board named no installable partition")
	}
	found := false
	for _, m := range ms {
		switch {
		case m.Dir == dir && m.Source == dev && m.FS == fs:
			found = true
		case m.Dir == dir:
			return refuse("mount", dir, fmt.Sprintf("holds %s (%s), not %s as %s", m.Source, m.FS, dev, fs))
		case g.unitSource(m.Source) && m.RW:
			return refuse("mount", m.Dir, m.Source+" is mounted writable; the installer writes only "+dev)
		}
	}
	if !found {
		return refuse("mount", dir, dev+" is not mounted there")
	}
	return nil
}

// CheckOffUnit returns nil when path may be written by a program that must
// not write the unit's own storage at all (a census, captures to a stick):
// the mount that holds it is not the unit's, it lies in no vendor area and
// not under the root, and it has no forbidden name. mounts is a
// /proc/mounts listing.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) CheckOffUnit(path string, mounts io.Reader) error {
	clean, err := abs(path)
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
		if isPseudo(p) {
			return refuse("write", path, "a device node or kernel attribute is not a file")
		}
		if g.p.Root != "" && within(p, g.p.Root) {
			return refuse("write", path, "on the unit's own storage")
		}
		for _, v := range g.p.Vendor {
			if within(p, v.Dir) {
				return refuse("write", path, "on the unit's own storage")
			}
		}
	}
	ms, err := ParseMounts(mounts)
	if err != nil {
		return err
	}
	var best Mount
	for _, m := range ms {
		if within(rp, m.Dir) && len(m.Dir) >= len(best.Dir) {
			best = m
		}
	}
	if g.unitSource(best.Source) {
		return refuse("write", path, "on the unit's own storage ("+best.Source+" at "+best.Dir+")")
	}
	return nil
}

// unescape undoes the octal escapes of /proc/mounts (\040 is a space).
// TRLC-LINKS: REQ-DSO-296
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
