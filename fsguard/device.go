// ENGMODEL-OWNER-UNIT: FU-DSO-FSGUARD

package fsguard

import (
	"fmt"
	"os"
	"strings"
)

// CheckDevice returns nil when the device node or kernel attribute at path
// may be opened for writing: a scope's hardware control (GPIO, SPI, the
// FPGA manager) passes; the policy's block devices and its one-time
// programmable or secure-boot storage do not, by name and, for an existing
// node, by type.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) CheckDevice(path string) error {
	clean, err := abs(path)
	if err != nil {
		return err
	}
	rp, err := resolve(clean)
	if err != nil {
		return err
	}
	for _, p := range []string{clean, rp} {
		lp := strings.ToLower(p) + "/"
		for _, w := range g.p.DeviceWords {
			if strings.Contains(lp, w) {
				return refuse("write", path, "one-time programmable or secure-boot storage is never written")
			}
		}
		for _, b := range g.p.BlockPrefixes {
			if strings.HasPrefix(lp, b) {
				return refuse("write", path, "a block device (the unit's raw storage) is never written")
			}
		}
		if within(p, "/dev") {
			if err := g.checkName(p); err != nil {
				return err
			}
		}
	}
	if st, err := os.Stat(rp); err == nil && st.Mode()&os.ModeDevice != 0 && st.Mode()&os.ModeCharDevice == 0 {
		return refuse("write", path, "a block device (the unit's raw storage) is never written")
	}
	return nil
}

// CheckPhys returns nil when a writable /dev/mem window of size bytes at
// phys covers none of the policy's forbidden register blocks. Read-only
// windows need no check.
// TRLC-LINKS: REQ-DSO-296
func (g *Guard) CheckPhys(phys, size uint64) error {
	for _, f := range g.p.Phys {
		if phys < f.Base+f.Size && f.Base < phys+size {
			return refuse("map", fmt.Sprintf("/dev/mem %#x+%#x", phys, size), "covers the "+f.What)
		}
	}
	return nil
}
