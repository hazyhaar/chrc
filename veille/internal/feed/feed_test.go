package feed

import "testing"

const rss20Sample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Tech News</title>
    <link>https://technews.example.com</link>
    <item>
      <guid>item-001</guid>
      <title>Go 1.24 Released</title>
      <link>https://technews.example.com/go-124</link>
      <description>Go 1.24 brings major improvements.</description>
      <pubDate>Mon, 24 Feb 2026 10:00:00 GMT</pubDate>
      <author>alice@example.com</author>
    </item>
    <item>
      <guid>item-002</guid>
      <title>Rust 2.0 Preview</title>
      <link>https://technews.example.com/rust-20</link>
      <description>A look at Rust 2.0.</description>
      <pubDate>Sun, 23 Feb 2026 09:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

const atom10Sample = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Science Blog</title>
  <link href="https://science.example.com" rel="alternate"/>
  <entry>
    <id>urn:uuid:abc-001</id>
    <title>Quantum Computing Advances</title>
    <link href="https://science.example.com/quantum" rel="alternate"/>
    <summary>New breakthroughs in quantum computing.</summary>
    <published>2026-02-24T08:00:00Z</published>
    <author><name>Bob</name></author>
  </entry>
  <entry>
    <id>urn:uuid:abc-002</id>
    <title>Mars Mission Update</title>
    <link href="https://science.example.com/mars"/>
    <summary>Latest from the Mars mission.</summary>
    <updated>2026-02-23T12:00:00Z</updated>
  </entry>
</feed>`

func TestParseRSS20(t *testing.T) {
	// WHAT: Parse a standard RSS 2.0 feed.
	// WHY: RSS 2.0 is the most common feed format.
	f, err := Parse([]byte(rss20Sample))
	if err != nil {
		t.Fatalf("parse rss: %v", err)
	}
	if f.Title != "Tech News" {
		t.Errorf("title: got %q", f.Title)
	}
	if f.Link != "https://technews.example.com" {
		t.Errorf("link: got %q", f.Link)
	}
	if len(f.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2", len(f.Entries))
	}

	e := f.Entries[0]
	if e.GUID != "item-001" {
		t.Errorf("guid: got %q", e.GUID)
	}
	if e.Title != "Go 1.24 Released" {
		t.Errorf("title: got %q", e.Title)
	}
	if e.Link != "https://technews.example.com/go-124" {
		t.Errorf("link: got %q", e.Link)
	}
	if e.Author != "alice@example.com" {
		t.Errorf("author: got %q", e.Author)
	}
}

func TestParseAtom10(t *testing.T) {
	// WHAT: Parse a standard Atom 1.0 feed.
	// WHY: Atom 1.0 is used by many blogs and services.
	f, err := Parse([]byte(atom10Sample))
	if err != nil {
		t.Fatalf("parse atom: %v", err)
	}
	if f.Title != "Science Blog" {
		t.Errorf("title: got %q", f.Title)
	}
	if f.Link != "https://science.example.com" {
		t.Errorf("link: got %q", f.Link)
	}
	if len(f.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2", len(f.Entries))
	}

	e := f.Entries[0]
	if e.GUID != "urn:uuid:abc-001" {
		t.Errorf("guid: got %q", e.GUID)
	}
	if e.Title != "Quantum Computing Advances" {
		t.Errorf("title: got %q", e.Title)
	}
	if e.Link != "https://science.example.com/quantum" {
		t.Errorf("link: got %q", e.Link)
	}
	if e.Author != "Bob" {
		t.Errorf("author: got %q", e.Author)
	}

	// Second entry uses Updated as Published fallback.
	e2 := f.Entries[1]
	if e2.Published != "2026-02-23T12:00:00Z" {
		t.Errorf("published (from updated): got %q", e2.Published)
	}
}

func TestParse_Empty(t *testing.T) {
	// WHAT: Empty data returns an error.
	// WHY: Guard against nil/empty input.
	_, err := Parse([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestParse_Malformed(t *testing.T) {
	// WHAT: Malformed XML returns an error.
	// WHY: Garbage input should not panic.
	_, err := Parse([]byte(`<html><body>not a feed</body></html>`))
	if err == nil {
		t.Error("expected error for malformed feed")
	}
}

func TestParse_GUIDFallbackToLink(t *testing.T) {
	// WHAT: When GUID is missing, Link is used as GUID.
	// WHY: Many feeds omit <guid>.
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>T</title>
	<item><title>No GUID</title><link>https://example.com/no-guid</link></item>
	</channel></rss>`
	f, err := Parse([]byte(rss))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Entries) != 1 {
		t.Fatalf("entries: %d", len(f.Entries))
	}
	if f.Entries[0].GUID != "https://example.com/no-guid" {
		t.Errorf("guid should fallback to link, got %q", f.Entries[0].GUID)
	}
}

func TestParse_EmptyFeed(t *testing.T) {
	// WHAT: A valid feed with zero entries returns empty entries.
	// WHY: Some feeds may temporarily have no items.
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>Empty</title></channel></rss>`
	f, err := Parse([]byte(rss))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(f.Entries))
	}
}
