// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package wire

import (
	"encoding/json"
	"testing"
)

// The words header reads as a struct embedding the stream's description:
// its fields first, then the read's.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func TestWordsHeaderFlattensInfo(t *testing.T) {
	type info struct {
		Session uint32 `json:"session"`
		Proto   string `json:"proto"`
		Why     string `json:"why,omitempty"`
	}
	type embedded struct {
		info
		Next     uint64 `json:"next"`
		Dropped  uint64 `json:"dropped"`
		Chunks   int    `json:"chunks"`
		Lag      uint64 `json:"lag"`
		ServerMS int64  `json:"serverMS"`
	}
	in := info{Session: 3, Proto: "lin<>&"}
	want, _ := json.Marshal(embedded{info: in, Next: 9, Dropped: 1, Chunks: 2, Lag: 4096, ServerMS: 1700000000000})
	got, err := json.Marshal(WordsHeader{Info: in, Next: 9, Dropped: 1, Chunks: 2, Lag: 4096, ServerMS: 1700000000000})
	if err != nil || string(got) != string(want) {
		t.Fatalf("got %s (%v)\nwant %s", got, err, want)
	}
	if b, _ := json.Marshal(WordsHeader{Next: 1}); string(b) != `{"next":1,"dropped":0,"chunks":0,"lag":0,"serverMS":0}` {
		t.Fatalf("no description: %s", b)
	}
	if _, err := json.Marshal(WordsHeader{Info: 7}); err == nil {
		t.Fatal("a description that is no object was taken")
	}
}
