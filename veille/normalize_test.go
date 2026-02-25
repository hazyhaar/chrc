package veille

import "testing"

func TestNormalizeSourceURL_LowercaseScheme(t *testing.T) {
	// WHAT: Scheme is lowercased during normalization.
	// WHY: HTTP/HTTPS should match regardless of case.
	got, err := NormalizeSourceURL("HTTPS://example.com/feed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/feed" {
		t.Errorf("got %q, want %q", got, "https://example.com/feed")
	}
}

func TestNormalizeSourceURL_LowercaseHost(t *testing.T) {
	// WHAT: Host is lowercased during normalization.
	// WHY: DNS is case-insensitive; Example.COM = example.com.
	got, err := NormalizeSourceURL("https://Example.COM/feed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/feed" {
		t.Errorf("got %q, want %q", got, "https://example.com/feed")
	}
}

func TestNormalizeSourceURL_RemoveTrailingSlash(t *testing.T) {
	// WHAT: Trailing slash is removed from non-root paths.
	// WHY: /feed/ and /feed are the same resource.
	got, err := NormalizeSourceURL("https://example.com/feed/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/feed" {
		t.Errorf("got %q, want %q", got, "https://example.com/feed")
	}
}

func TestNormalizeSourceURL_KeepRootSlash(t *testing.T) {
	// WHAT: Root path / is preserved, but trailing slash on bare host is removed.
	// WHY: https://example.com/ and https://example.com are the same resource.
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com/", "https://example.com"},
		{"https://example.com", "https://example.com"},
	}
	for _, tc := range cases {
		got, err := NormalizeSourceURL(tc.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeSourceURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeSourceURL_RemoveFragment(t *testing.T) {
	// WHAT: Fragment (#section) is removed.
	// WHY: Fragments are client-side only and don't affect the resource fetched.
	got, err := NormalizeSourceURL("http://example.com/rss.xml#latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://example.com/rss.xml" {
		t.Errorf("got %q, want %q", got, "http://example.com/rss.xml")
	}
}

func TestNormalizeSourceURL_SortQueryParams(t *testing.T) {
	// WHAT: Query parameters are sorted by key.
	// WHY: ?a=1&b=2 and ?b=2&a=1 fetch the same resource.
	got, err := NormalizeSourceURL("https://example.com/api?z=3&a=1&m=2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/api?a=1&m=2&z=3" {
		t.Errorf("got %q, want %q", got, "https://example.com/api?a=1&m=2&z=3")
	}
}

func TestNormalizeSourceURL_PreserveDeepPath(t *testing.T) {
	// WHAT: Deep paths like /feed, /rss.xml, /blog/feed are preserved.
	// WHY: RSS feed URLs ARE deep URLs — normalization must not strip them.
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com/feed", "https://example.com/feed"},
		{"https://example.com/rss.xml", "https://example.com/rss.xml"},
		{"https://example.com/blog/feed", "https://example.com/blog/feed"},
		{"https://example.com/path/to/resource.json", "https://example.com/path/to/resource.json"},
	}
	for _, tc := range cases {
		got, err := NormalizeSourceURL(tc.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeSourceURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeSourceURL_InvalidURL(t *testing.T) {
	// WHAT: Invalid URLs are rejected.
	// WHY: Malformed URLs should not be stored.
	cases := []string{
		"",
		"://missing-scheme",
		"not a url at all",
	}
	for _, input := range cases {
		_, err := NormalizeSourceURL(input)
		if err == nil {
			t.Errorf("NormalizeSourceURL(%q) should return error", input)
		}
	}
}

func TestNormalizeSourceURL_PreserveScheme(t *testing.T) {
	// WHAT: http and https are treated as different schemes.
	// WHY: Some servers don't support HTTPS; we must not silently upgrade.
	http, err := NormalizeSourceURL("http://example.com/feed")
	if err != nil {
		t.Fatal(err)
	}
	https, err := NormalizeSourceURL("https://example.com/feed")
	if err != nil {
		t.Fatal(err)
	}
	if http == https {
		t.Errorf("http and https should produce different normalized URLs: %q vs %q", http, https)
	}
}

func TestNormalizeSourceURL_InternalSchemes(t *testing.T) {
	// WHAT: Internal schemes (question://, document paths) are returned as-is.
	// WHY: Only http/https URLs get full normalization; internal schemes are synthetic.
	cases := []struct {
		input string
		want  string
	}{
		{"question://abc123", "question://abc123"},
		{"reports/q1.pdf", "reports/q1.pdf"},
	}
	for _, tc := range cases {
		got, err := NormalizeSourceURL(tc.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeSourceURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
