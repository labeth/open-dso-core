// ENGMODEL-OWNER-UNIT: FU-OTA-DIAG

package diag

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readArchive returns the members of a written archive by name.
// TRLC-LINKS: REQ-SDS-286
func readArchive(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = data
	}
}

// The archive stores its members, lists every one in the manifest with its
// hash, notes what did not fit and the collectors' errors.
// TRLC-LINKS: REQ-SDS-286
func TestArchiveRoundTrip(t *testing.T) {
	var b bytes.Buffer
	a := NewArchive(&b, 64)
	a.AddText("one.txt", "hello")
	a.Add("big.bin", make([]byte, 100), "/dev/x", "")
	a.Errorf("a collector: %s", "failed")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	m := readArchive(t, b.Bytes())
	if string(m["one.txt"]) != "hello" || m["big.bin"] != nil {
		t.Fatalf("members: one %q, big stored %v", m["one.txt"], m["big.bin"] != nil)
	}
	var entries []Entry
	if err := json.Unmarshal(m["manifest.json"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "big.bin" || !strings.Contains(entries[0].Note, "not stored") || entries[1].SHA256 == "" {
		t.Fatalf("manifest %+v", entries)
	}
	if !strings.Contains(string(m["errors.txt"]), "a collector: failed") {
		t.Fatalf("errors %q", m["errors.txt"])
	}
}

// The firmware inventory hashes every file, stores the small ones whole and
// the head of a large one, lists links, and skips the stick's own mount.
// TRLC-LINKS: REQ-SDS-284
func TestFirmwareInventory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "version.txt"), []byte("1.0"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte{7}, wholeMax+10)
	if err := os.WriteFile(filepath.Join(root, "app.bin"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("version.txt", filepath.Join(root, "v")); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	a := NewArchive(&b, 64<<20)
	CollectFirmware(a, []string{root}, nil)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	m := readArchive(t, b.Bytes())
	var inv []FileInfo
	if err := json.Unmarshal(m["firmware/inventory.json"], &inv); err != nil {
		t.Fatal(err)
	}
	got := map[string]FileInfo{}
	for _, f := range inv {
		got[filepath.Base(f.Path)] = f
	}
	if got["version.txt"].Stored != "whole" || string(m["firmware/files"+filepath.Join(root, "version.txt")]) != "1.0" {
		t.Fatalf("small file: %+v", got["version.txt"])
	}
	if f := got["app.bin"]; f.Stored != "head" || len(m["firmware/files"+filepath.Join(root, "app.bin")]) != headMax || f.Size != int64(len(big)) || len(f.SHA256) != 64 {
		t.Fatalf("large file: %+v", f)
	}
	if got["v"].Link != "version.txt" {
		t.Fatalf("link: %+v", got["v"])
	}
}

// The framebuffer's geometry comes from sysfs (the visible mode inside a
// panned buffer), and its pixels convert at each depth.
// TRLC-LINKS: REQ-SDS-284
func TestFramebuffer(t *testing.T) {
	dir := t.TempDir()
	for name, v := range map[string]string{"virtual_size": "800,960\n", "bits_per_pixel": "16\n", "stride": "1600\n", "pan": "0,480\n", "mode": "\n", "modes": "U:800x480p-52\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g, err := readFBGeom(dir)
	if err != nil || g.W != 800 || g.H != 480 || g.BPP != 16 || g.Stride != 1600 || g.PanY != 480 {
		t.Fatalf("geometry %+v %v", g, err)
	}
	raw := make([]byte, 2*2)
	raw[0], raw[1] = 0x00, 0xf8 // pure red in RGB565
	im, err := fbImage(raw, fbGeom{W: 2, H: 1, BPP: 16, Stride: 4})
	if err != nil {
		t.Fatal(err)
	}
	if r, g, bl, _ := im.At(0, 0).RGBA(); r>>8 != 0xf8 || g != 0 || bl != 0 {
		t.Fatalf("red pixel: %x %x %x", r, g, bl)
	}
	if _, err := fbImage(raw[:2], fbGeom{W: 2, H: 1, BPP: 16, Stride: 4}); err == nil {
		t.Fatal("a short read converted")
	}
}

// A process's state comes from /proc: this test's own is running or
// sleeping, a pid no process has reads as gone.
// TRLC-LINKS: REQ-SDS-285
func TestProcState(t *testing.T) {
	if st := ProcState(os.Getpid()); st != "R" && st != "S" {
		t.Fatalf("own state %q", st)
	}
	if st := ProcState(1 << 30); st != "" {
		t.Fatalf("no such pid: %q", st)
	}
}

// TestBannerDrawsTheWord: the DONE screen's word is white on its colour.
// TRLC-LINKS: REQ-SDS-292
func TestBannerDrawsTheWord(t *testing.T) {
	b := newBanner(800, 480, doneGreen, "DONE", []string{"switch the scope off"})
	white, green := 0, 0
	for _, p := range b.px {
		switch p {
		case 0xffff:
			white++
		case doneGreen:
			green++
		}
	}
	if white < 4*12*12*10 || green < 800*480/2 || white+green != len(b.px) {
		t.Fatalf("white %d green %d of %d", white, green, len(b.px))
	}
	if _, ok := glyphs5x7['D']; !ok {
		t.Fatal("no D")
	}
}

// stageBoard records which stages ran.
type stageBoard struct {
	reach, activeOK, takeoverOK bool
	ran                         []string
}

func (b *stageBoard) Version() string { return "v-test" }
func (b *stageBoard) Child(name string, _ []string, _ io.Writer) (int, bool) {
	return 7, name == "child"
}

func (b *stageBoard) Passive(_ *Collection, a *Archive) bool {
	b.ran = append(b.ran, "passive")
	a.AddText("board/passive.txt", "x")
	return b.reach
}

func (b *stageBoard) ActiveGate(*Collection) []Check {
	return []Check{{Name: "scope", OK: b.activeOK, Detail: "known"}}
}
func (b *stageBoard) Active(*Collection, *Archive) { b.ran = append(b.ran, "active") }
func (b *stageBoard) TakeoverGate(*Collection) []Check {
	return []Check{{Name: "images", OK: b.takeoverOK, Detail: "on the stick"}}
}

func (b *stageBoard) Takeover(c *Collection, _ *Archive) {
	b.ran = append(b.ran, "takeover")
	c.TookOver = true
}
func (b *stageBoard) Close()          { b.ran = append(b.ran, "close") }
func (b *stageBoard) ScreenOn() error { return nil }

// Each stage runs only when the mode and the gates before it allow it, the
// gates' checks are archived, and the board's link closes once.
// TRLC-LINKS: REQ-SDS-284, REQ-SDS-285, REQ-SDS-287, REQ-SDS-288
func TestStagesFollowModeAndGates(t *testing.T) {
	for _, tc := range []struct {
		mode                        string
		reach, activeOK, takeoverOK bool
		want                        string
		gates                       []string
	}{
		{ModePassive, true, true, true, "passive close", nil},
		{ModeAuto, false, true, true, "passive close", nil},
		{ModeAuto, true, false, true, "passive close", []string{"gate.json"}},
		{ModeAuto, true, true, false, "passive active close", []string{"gate.json", "takeover/gate.json"}},
		{ModeAuto, true, true, true, "passive active takeover close", []string{"gate.json", "takeover/gate.json"}},
		{ModeActive, true, false, true, "passive active close", nil},
	} {
		b := &stageBoard{reach: tc.reach, activeOK: tc.activeOK, takeoverOK: tc.takeoverOK}
		c := &Collection{Mode: tc.mode, OutDir: t.TempDir(), board: b}
		c.status = filepath.Join(c.OutDir, "status.txt")
		var buf bytes.Buffer
		a := NewArchive(&buf, 1<<20)
		c.stages(a)
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(b.ran, " "); got != tc.want {
			t.Errorf("%s %+v: stages %q, want %q", tc.mode, tc, got, tc.want)
		}
		m := readArchive(t, buf.Bytes())
		for _, g := range []string{"gate.json", "takeover/gate.json"} {
			want := strings.Contains(strings.Join(tc.gates, " "), g)
			if _, got := m[g]; got != want {
				t.Errorf("%s %+v: %s archived %v, want %v", tc.mode, tc, g, got, want)
			}
		}
		if c.TookOver != strings.Contains(tc.want, "takeover") {
			t.Errorf("TookOver = %v", c.TookOver)
		}
	}
}

// A board's own child subcommand runs before any flag parsing.
// TRLC-LINKS: REQ-SDS-285
func TestRunHandsChildrenToTheBoard(t *testing.T) {
	if code := Run([]string{"child", "x"}, io.Discard, &stageBoard{}); code != 7 {
		t.Errorf("child exit %d, want the board's 7", code)
	}
	if code := Run([]string{"-nosuchflag"}, io.Discard, &stageBoard{}); code != 2 {
		t.Errorf("bad flag exit %d, want 2", code)
	}
}
