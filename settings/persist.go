// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/labeth/open-dso-core/hal"
)

// FileKey is the persisted setup's key in a scope's settings store.
const FileKey = "scope-settings.json"

// MaxFileSize caps what the loader parses: a real setup is a few KB, and
// anything huge is not ours (and must not balloon the heap on a small
// board).
const MaxFileSize = 64 << 10

// Parse decodes and validates one setup. It never panics on hostile input;
// any structural problem is an error (the caller falls back to defaults).
// Value ranges are not checked here: Apply routes every value through the
// owning setter, which clamps.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func Parse(raw []byte) (Settings, error) {
	if len(raw) > MaxFileSize {
		return Settings{}, fmt.Errorf("settings: file too large (%d bytes)", len(raw))
	}
	var s Settings
	if err := json.Unmarshal(bytes.TrimSpace(raw), &s); err != nil {
		return Settings{}, fmt.Errorf("settings: %w", err)
	}
	if s.Version != Version {
		// Future versions add migrations here, keyed on s.Version.
		return Settings{}, fmt.Errorf("settings: unsupported schema version %d", s.Version)
	}
	return s, nil
}

// Encode is s as the file keeps it: indented JSON with its schema version
// and a final newline, readable by the owner who copies it off the scope.
// TRLC-LINKS: REQ-DSO-089
func Encode(s Settings) ([]byte, error) {
	s.Version = Version
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// reader is a store that says why a value could not be read (a FileKV):
// a missing value is quiet, anything else is logged.
type reader interface {
	Read(key string) ([]byte, error)
}

// namer is a store that names where a key lives (a FileKV: its path), for
// the log.
type namer interface {
	Name(key string) string
}

// nameOf is where key lives in kv, for the log.
func nameOf(kv hal.KV, key string) string {
	if n, ok := kv.(namer); ok {
		return n.Name(key)
	}
	return key
}

// Load reads the setup under key. ok=false means no restore (missing,
// unreadable, corrupt, another version): the scope starts with its
// defaults. Never fatal, never a panic.
// TRLC-LINKS: REQ-DSO-089
func Load(kv hal.KV, key string, logf func(string, ...any)) (Settings, bool) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if kv == nil {
		return Settings{}, false
	}
	raw, ok := read(kv, key, logf)
	if !ok {
		return Settings{}, false
	}
	s, err := Parse(raw)
	if err != nil {
		logf("settings: %s: %v — using defaults", nameOf(kv, key), err)
		return Settings{}, false
	}
	return s, true
}

// read is key's value in kv; a reason it could not be read other than its
// absence is logged.
// TRLC-LINKS: REQ-DSO-089
func read(kv hal.KV, key string, logf func(string, ...any)) ([]byte, bool) {
	r, ok := kv.(reader)
	if !ok {
		return kv.Get(key)
	}
	b, err := r.Read(key)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logf("settings: read %s: %v — using defaults", nameOf(kv, key), err)
	}
	return b, err == nil
}

// Save stores s under key. A store that already holds these bytes is left
// as it is: a write and its sync cost milliseconds and wear the medium.
// TRLC-LINKS: REQ-DSO-089
func Save(kv hal.KV, key string, s Settings) error {
	if kv == nil {
		return errors.New("settings: no store")
	}
	b, err := Encode(s)
	if err != nil {
		return err
	}
	if old, ok := kv.Get(key); ok && bytes.Equal(old, b) {
		return nil
	}
	return kv.Put(key, b)
}

// LoadFile is Load of the file at path.
// TRLC-LINKS: REQ-DSO-089
func LoadFile(path string, logf func(string, ...any)) (Settings, bool) {
	return Load(FileKV{Dir: filepath.Dir(path)}, filepath.Base(path), logf)
}

// SaveFile is Save to the file at path.
// TRLC-LINKS: REQ-DSO-089
func SaveFile(path string, s Settings) error {
	return Save(FileKV{Dir: filepath.Dir(path)}, filepath.Base(path), s)
}

// FileKV is a store of one file a key in Dir, written atomically: a
// temporary file beside it, fsync, rename. A power cut mid-save leaves the
// old file or the new one, never a torn one. It has no budget.
type FileKV struct{ Dir string }

var _ hal.KV = FileKV{}

// Name is the key's file.
// TRLC-LINKS: REQ-DSO-089
func (kv FileKV) Name(key string) string { return filepath.Join(kv.Dir, key) }

// Read is the key's file, with why it could not be read.
// TRLC-LINKS: REQ-DSO-089
func (kv FileKV) Read(key string) ([]byte, error) {
	if key != filepath.Base(key) {
		return nil, fs.ErrNotExist
	}
	return os.ReadFile(kv.Name(key))
}

// Get is the key's file.
// TRLC-LINKS: REQ-DSO-089
func (kv FileKV) Get(key string) ([]byte, bool) {
	b, err := kv.Read(key)
	return b, err == nil
}

// Put writes the key's file atomically.
// TRLC-LINKS: REQ-DSO-089
func (kv FileKV) Put(key string, val []byte) error {
	if key == "" || key != filepath.Base(key) {
		return hal.Refuse("settings file "+key, "a key is a plain file name")
	}
	path := kv.Name(key)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // G302: the setup file is the owner's to copy off the scope
	if err != nil {
		return err
	}
	// On a failure the temporary file is cleaned up as far as it can be;
	// the error returned is the one that stopped the save.
	if _, err := f.Write(val); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Budget is 0: unbounded.
// TRLC-LINKS: REQ-DSO-089
func (FileKV) Budget() int { return 0 }
