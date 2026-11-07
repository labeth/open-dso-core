// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"testing"
)

// The edge after the trigger edge, the other way: past the trigger edge's
// own bucket, the first bucket that comes back from the trigger edge's new
// level; none on a flat envelope or before it settles.
// TRLC-LINKS: REQ-SDS-021
func TestOppositeEdgeBucket(t *testing.T) {
	// A square of 20-bucket periods: high from bucket 10 to 19, low 0-9.
	pairs := make([]uint8, 0, 200)
	for b := range 100 {
		lo, hi := uint8(50), uint8(50)
		switch {
		case b%20 == 10: // a rising edge inside the bucket
			hi = 200
		case b%20 == 0 && b > 0: // a falling edge inside the bucket
			hi = 200
		case b%20 > 10:
			lo, hi = 200, 200
		}
		pairs = append(pairs, lo, hi)
	}
	if got := OppositeEdgeBucket(pairs, 10, true); got != 20 {
		t.Fatalf("falling edge after the rising trigger at bucket 10: %d", got)
	}
	if got := OppositeEdgeBucket(pairs, 20, false); got != 30 {
		t.Fatalf("rising edge after the falling trigger at bucket 20: %d", got)
	}
	flat := make([]uint8, 200)
	for i := range flat {
		flat[i] = 128
	}
	if got := OppositeEdgeBucket(flat, 10, true); got != -1 {
		t.Fatalf("flat envelope: %d", got)
	}
	if got := OppositeEdgeBucket(pairs, 90, true); got != -1 {
		t.Fatalf("no edge after the last one: %d", got)
	}
}

// A serial match ends in its stop bit, no edge: the edge windows go to the
// edge nearest the trigger point, either way, and the next one the other way
// (the owner's UART trigger read no rise, ever: the trigger window held the
// flat stop bit and the opposite one was sought in the edge trigger's sense).
// TRLC-LINKS: REQ-SDS-021
func TestNearestEdgeBucket(t *testing.T) {
	pairs := make([]uint8, 0, 200)
	for b := range 100 { // low 0-29, a rise inside 30, high 31-59, a fall between 59 and 60
		lo, hi := uint8(50), uint8(50)
		switch {
		case b == 30:
			hi = 200
		case b > 30 && b < 60:
			lo, hi = 200, 200
		}
		pairs = append(pairs, lo, hi)
	}
	if b, rising := NearestEdgeBucket(pairs, 50); b != 60 || rising {
		t.Fatalf("from 50: edge %d rising %v, want the fall at 60", b, rising)
	}
	if b, rising := NearestEdgeBucket(pairs, 36); b != 30 || !rising {
		t.Fatalf("from 36: edge %d rising %v, want the rise at 30", b, rising)
	}
	if b, _ := NearestEdgeBucket(pairs, 50); OppositeEdgeBucket(pairs, b, false) != -1 {
		t.Fatal("no rise after the fall at 60 in this record")
	}
	if b, rising := NearestEdgeBucket(pairs, 25); b != 30 || !rising {
		t.Fatalf("from 25: edge %d rising %v", b, rising)
	}
	flat := make([]uint8, 200)
	if b, _ := NearestEdgeBucket(flat, 10); b != -1 {
		t.Fatalf("a flat record has an edge at %d", b)
	}
}
