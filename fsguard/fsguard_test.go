package fsguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPolicy is a board's policy in the shape of a unit whose vendor system
// lives under /vendor with the writable partition at /vendor/data.
var testPolicy = Policy{
	Root: "/vendor/data", RootName: "data",
	Vendor:        []Area{{"/vendor", "a vendor partition"}, {"/mnt/logo", "a vendor partition"}},
	Enclosable:    []string{"/mnt/logo"},
	System:        []string{"/bin", "/dev", "/etc", "/lib", "/mnt/logo", "/proc", "/sys", "/usr"},
	RootForbidden: []Area{{"update", "is flashed by the vendor boot"}, {"usr/upgrade", "is run by the vendor boot"}},
	Names:         []string{"uboot.env", "boot.bin", "uenv.txt"},
	NamesWhy:      "a boot file is never written",
	DeviceWords:   []string{"efuse", "nvmem"},
	BlockPrefixes: []string{"/dev/mmcblk", "/dev/sd", "/sys/block/"},
	Phys:          []Window{{0xFF16_0000, 0x1_0000, "SD host"}, {0xFFCC_0000, 0x1_0000, "eFuse controller"}},
	Device:        "/dev/mmcblk0p12", DeviceFS: "ext4",
	UnitSources: []string{"/dev/mmcblk", "/dev/root"},
}

// guardAt is a guard of the test policy rooted at a fresh temporary
// directory.
func guardAt(t *testing.T) (*Guard, string) {
	t.Helper()
	g := New(testPolicy)
	dir := t.TempDir()
	if _, err := g.SetRoot(dir); err != nil {
		t.Fatal(err)
	}
	return g, dir
}

func refused(err error) bool { return errors.Is(err, ErrRefused) }

// TRLC-LINKS: REQ-DSO-296
func TestCheckInsideOutside(t *testing.T) {
	g, root := guardAt(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "open"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, l := range [][2]string{
		{outside, filepath.Join(root, "open", "away")},
		{filepath.Join(outside, "f"), filepath.Join(root, "open", "away-file")},
		{filepath.Join(root, "open"), filepath.Join(root, "inner")},
	} {
		if err := os.Symlink(l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{
		root,
		filepath.Join(root, "open", "a", "app"),
		filepath.Join(root, "open", "not", "yet", "there"),
		filepath.Join(root, "open", "..", "open", "current"),
		filepath.Join(root, "inner", "current"), // a symlink that stays inside
	} {
		if err := g.Check(p); err != nil {
			t.Errorf("%s refused: %v", p, err)
		}
	}
	for _, p := range []string{
		"", "/", filepath.Dir(root), filepath.Join(root, ".."), filepath.Join(root, "open", "..", "..", "x"),
		outside,
		filepath.Join(root, "open", "away", "x"), // a symlinked directory leading out
		filepath.Join(root, "open", "away", "new", "y"), // ... with parts that do not exist yet
		filepath.Join(root, "open", "away-file"),        // a symlinked file leading out
		filepath.Join(root, "update", "BOOT.bin"),
		filepath.Join(root, "usr", "upgrade", "update.sh"),
		filepath.Join(root, "uboot.env"),
		filepath.Join(root, "open", "UENV.TXT"),
		filepath.Join(root, "open", "boot.bin.new"),
		"/dev/mmcblk0", "/sys/bus/nvmem/x", "/vendor/main.app", "/mnt/logo/logo.bmp", "/lib/firmware/x.bin",
	} {
		if !refused(g.Check(p)) {
			t.Errorf("%q not refused", p)
		}
	}
	var re *RefusedError
	if err := g.Check(filepath.Join(outside, "x")); !errors.As(err, &re) || !strings.Contains(err.Error(), "outside data's root") {
		t.Errorf("refusal %v", err)
	}
}

// TRLC-LINKS: REQ-DSO-296
func TestDefaultRootIsThePolicys(t *testing.T) {
	g := New(testPolicy)
	if g.Root() != "/vendor/data" {
		t.Fatalf("default root %s", g.Root())
	}
	if err := g.Check("/vendor/data/open/current"); err != nil {
		t.Error(err)
	}
	for _, p := range []string{"/vendor/data/../x", "/vendor/config/x", "/tmp/x", "/vendor/data/update/BOOT.bin"} {
		if !refused(g.Check(p)) {
			t.Errorf("%s not refused", p)
		}
	}
	if !refused(New(Policy{}).Check("/tmp/x")) {
		t.Error("a policy without a root wrote")
	}
}

// TRLC-LINKS: REQ-DSO-296
func TestSetRootRefusesVendorAreas(t *testing.T) {
	g := New(testPolicy)
	for _, d := range []string{"/", "/vendor", "/vendor/firmdata", "/dev", "/sys/class", "/mnt/logo", "/etc", "/usr/x", ""} {
		if r, err := g.SetRoot(d); err == nil {
			r()
			t.Errorf("root %q accepted", d)
		}
	}
	for _, d := range []string{"/vendor/data", "/vendor/data/open", "/mnt", t.TempDir()} {
		r, err := g.SetRoot(d)
		if err != nil {
			t.Errorf("root %q: %v", d, err)
			continue
		}
		if g.Root() != d {
			t.Errorf("root %q is %q", d, g.Root())
		}
		r()
	}
	if g.Root() != "/vendor/data" {
		t.Fatalf("restore left %s", g.Root())
	}
}

// TRLC-LINKS: REQ-DSO-296
func TestCheckDeviceAndPhys(t *testing.T) {
	g := New(testPolicy)
	for _, p := range []string{"/sys/class/gpio/export", "/dev/spidev1.0", "/sys/class/fpga_manager/fpga0/firmware"} {
		if err := g.CheckDevice(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"/dev/mmcblk0p1", "/dev/sda", "/sys/block/mmcblk0/force_ro", "/dev/xilinx_efuse", "/sys/class/nvmem/x", "/dev/uboot.env", "/dev/../dev/mmcblk0"} {
		if !refused(g.CheckDevice(p)) {
			t.Errorf("%s not refused", p)
		}
	}
	if err := g.CheckPhys(0x8000_0000, 0x1_0000); err != nil {
		t.Error(err)
	}
	if !refused(g.CheckPhys(0xFFCC_8000, 0x10)) || !refused(g.CheckPhys(0xFF00_0000, 0x100_0000)) {
		t.Error("a window over a forbidden block not refused")
	}
}

// TRLC-LINKS: REQ-DSO-296
func TestWritesThroughTheGuard(t *testing.T) {
	g, root := guardAt(t)
	outside := t.TempDir()
	in := filepath.Join(root, "open", "current")
	if err := g.MkdirAll(filepath.Dir(in)); err != nil {
		t.Fatal(err)
	}
	if err := g.WriteFile(in, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(in); string(b) != "a\n" {
		t.Fatalf("content %q", b)
	}
	out := filepath.Join(outside, "x")
	if !refused(g.WriteFile(out, []byte("x"), 0o644)) {
		t.Error("write outside the root not refused")
	}
	for _, p := range []string{out, out + ".new"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("a refused write left %s", p)
		}
	}
	if !refused(g.Rename(in, out)) || !refused(g.Rename(out, in)) {
		t.Error("rename across the root not refused")
	}
	if !refused(g.Remove(out)) || !refused(g.MkdirAll(filepath.Join(outside, "d"))) || !refused(g.Mkdir(outside)) {
		t.Error("remove or mkdir outside the root not refused")
	}
	if _, err := g.OpenFile(filepath.Join(root, "update", "BOOT.bin"), os.O_WRONLY|os.O_CREATE, 0o644); !refused(err) {
		t.Errorf("open in update/: %v", err)
	}
	if err := g.Rename(in, in+".old"); err != nil {
		t.Fatal(err)
	}
	if err := g.Remove(in + ".old"); err != nil {
		t.Fatal(err)
	}
	if err := g.Mkdir(filepath.Join(root, "open")); err != nil {
		t.Errorf("an existing directory: %v", err)
	}
}

const testMounts = `/dev/root / ext4 ro,relatime 0 0
devtmpfs /dev devtmpfs rw,relatime 0 0
tmpfs /tmp tmpfs rw,relatime 0 0
/dev/mmcblk0p10 /vendor ext4 ro,relatime 0 0
/dev/mmcblk0p12 /vendor/data ext4 rw,relatime 0 0
/dev/mmcblk0p8 /mnt/logo vfat rw,relatime 0 0
/dev/sda1 /mnt/usb\040stick vfat rw,relatime 0 0
`

// TRLC-LINKS: REQ-DSO-296
func TestMounts(t *testing.T) {
	g := New(testPolicy)
	good := "rootfs / rootfs rw 0 0\n/dev/mmcblk0p12 /mnt ext4 rw,relatime 0 0\n"
	if err := g.CheckInstallMounts(strings.NewReader(good), "/mnt"); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]string{
		"nothing mounted": "rootfs / rootfs rw 0 0\n",
		"wrong partition": "/dev/mmcblk0p9 /mnt ext4 rw 0 0\n",
		"wrong fs":        "/dev/mmcblk0p12 /mnt vfat rw 0 0\n",
		"another rw":      good + "/dev/mmcblk0p5 /boot vfat rw 0 0\n",
	} {
		if !refused(g.CheckInstallMounts(strings.NewReader(m), "/mnt")) {
			t.Errorf("%s not refused", name)
		}
	}
	if !refused(New(Policy{}).CheckInstallMounts(strings.NewReader(good), "/mnt")) {
		t.Error("a policy without a device installed")
	}
	for _, p := range []string{"/tmp/census.json", "/mnt/usb stick/census.json"} {
		if err := g.CheckOffUnit(p, strings.NewReader(testMounts)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"/vendor/data/x", "/vendor/x", "/mnt/logo/x", "/etc/x", "/dev/mmcblk0p9", "/mnt/usb stick/uboot.env"} {
		if !refused(g.CheckOffUnit(p, strings.NewReader(testMounts))) {
			t.Errorf("%s not refused", p)
		}
	}
}
