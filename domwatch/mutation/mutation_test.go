package mutation

import (
	"encoding/json"
	"testing"
)

func TestBatchMarshalRoundtrip(t *testing.T) {
	b := &Batch{
		ID:      "01234567-89ab-cdef-0123-456789abcdef",
		PageURL: "https://example.com",
		PageID:  "page-1",
		Seq:     42,
		Records: []Record{
			{Op: OpInsert, XPath: "/html/body/div", NodeType: 1, Tag: "div", HTML: "<div>hello</div>"},
			{Op: OpAttr, XPath: "/html/body/div", Name: "class", Value: "new", OldValue: "old"},
			{Op: OpText, XPath: "/html/body/div/text()", Value: "world", OldValue: "hello"},
			{Op: OpRemove, XPath: "/html/body/div/span"},
		},
		Timestamp:   1708700000000,
		SnapshotRef: "snap-1",
	}

	data, err := MarshalBatch(b)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalBatch(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != b.ID {
		t.Errorf("ID: got %q, want %q", got.ID, b.ID)
	}
	if got.Seq != b.Seq {
		t.Errorf("Seq: got %d, want %d", got.Seq, b.Seq)
	}
	if len(got.Records) != len(b.Records) {
		t.Fatalf("Records: got %d, want %d", len(got.Records), len(b.Records))
	}
	for i, r := range got.Records {
		if r.Op != b.Records[i].Op {
			t.Errorf("Record[%d].Op: got %q, want %q", i, r.Op, b.Records[i].Op)
		}
	}
}

func TestSnapshotMarshalRoundtrip(t *testing.T) {
	s := &Snapshot{
		ID:        "snap-1",
		PageURL:   "https://example.com",
		PageID:    "page-1",
		HTML:      []byte("<html><body>hello</body></html>"),
		HTMLHash:  HashHTML([]byte("<html><body>hello</body></html>")),
		Timestamp: 1708700000000,
	}

	data, err := MarshalSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.HTMLHash != s.HTMLHash {
		t.Errorf("HTMLHash: got %q, want %q", got.HTMLHash, s.HTMLHash)
	}
}

func TestProfileMarshalRoundtrip(t *testing.T) {
	p := &Profile{
		PageURL: "https://example.com",
		Landmarks: []Landmark{
			{Tag: "main", XPath: "/html/body/main"},
			{Tag: "nav", XPath: "/html/body/nav", Role: "navigation"},
		},
		DynamicZones: []Zone{
			{XPath: "/html/body/main/div", Selector: "main > div", MutationRate: 2.5},
		},
		StaticZones: []Zone{
			{XPath: "/html/body/footer", Selector: "footer", MutationRate: 0},
		},
		ContentSelectors: []string{"main", "article"},
		Fingerprint:      "abc123",
		TextDensityMap:   map[string]float64{"/html/body/main": 0.75},
	}

	data, err := MarshalProfile(p)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalProfile(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Landmarks) != len(p.Landmarks) {
		t.Fatalf("Landmarks: got %d, want %d", len(got.Landmarks), len(p.Landmarks))
	}
	if got.Fingerprint != p.Fingerprint {
		t.Errorf("Fingerprint: got %q, want %q", got.Fingerprint, p.Fingerprint)
	}
}

func TestHashHTML(t *testing.T) {
	html := []byte("<html><body>test</body></html>")
	h1 := HashHTML(html)
	h2 := HashHTML(html)
	if h1 != h2 {
		t.Errorf("HashHTML not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("HashHTML length: got %d, want 64", len(h1))
	}
}

func TestOpJSONValues(t *testing.T) {
	ops := []Op{OpInsert, OpRemove, OpText, OpAttr, OpAttrDel, OpDocReset}
	for _, op := range ops {
		data, _ := json.Marshal(op)
		var got Op
		json.Unmarshal(data, &got)
		if got != op {
			t.Errorf("Op roundtrip: got %q, want %q", got, op)
		}
	}
}
