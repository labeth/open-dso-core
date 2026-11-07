package settings

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// refsKV is a store in memory that counts its writes.
type refsKV struct {
	mu     sync.Mutex
	m      map[string][]byte
	writes int
}

func (k *refsKV) Get(key string) ([]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, ok
}

func (k *refsKV) Put(key string, v []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string][]byte{}
	}
	k.m[key] = append([]byte(nil), v...)
	k.writes++
	return nil
}

func (*refsKV) Budget() int { return 0 }

// refsSrc is references that change when told.
type refsSrc struct {
	gen  atomic.Uint64
	refs Refs
}

func (s *refsSrc) RefsState() Refs { return s.refs }
func (s *refsSrc) RefsGen() uint64 { return s.gen.Load() }

// References are their own store: stored only on a change, read back with
// every channel, and a file of another version is no references.
// TRLC-LINKS: REQ-DSO-138
func TestRefsStore(t *testing.T) {
	kv := &refsKV{}
	src := &refsSrc{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { WatchRefs(kv, src, time.Millisecond, stop, t.Logf); close(done) }()
	time.Sleep(20 * time.Millisecond)
	if kv.writes != 0 {
		t.Fatal("references never touched were written")
	}
	src.refs.Slots[1] = RefSlot{Has: true, Show: true, Cols: [][]byte{nil, nil, {1, 2, 3}, {4}}, VdivV: []float64{1, 1, 0.5, 2}}
	src.gen.Add(1)
	deadline := time.Now().Add(2 * time.Second)
	for {
		kv.mu.Lock()
		w := kv.writes
		kv.mu.Unlock()
		if w > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(stop)
	<-done
	r, ok := LoadRefs(kv, t.Logf)
	if !ok || !r.Slots[1].Has || string(r.Slots[1].Cols[2]) != "\x01\x02\x03" || r.Slots[1].VdivV[3] != 2 {
		t.Fatalf("read back %+v (ok %v)", r, ok)
	}
	if _, err := ParseRefs([]byte(`{"version":7}`)); err == nil {
		t.Fatal("another version was read")
	}
}
