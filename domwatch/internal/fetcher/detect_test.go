package fetcher

import (
	"testing"
)

func TestIsSufficient_StaticPage(t *testing.T) {
	html := []byte(`<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<main>
<article>
<h1>Article Title</h1>
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.</p>
</article>
</main>
</body>
</html>`)
	if !IsSufficient(html) {
		t.Error("expected sufficient for static page with content")
	}
}

func TestIsSufficient_SPAShell(t *testing.T) {
	html := []byte(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>App</title></head>
<body>
<div id="root"></div>
<script src="/static/js/main.chunk.js"></script>
</body>
</html>`)
	if IsSufficient(html) {
		t.Error("expected insufficient for SPA shell")
	}
}

func TestIsSufficient_TooShort(t *testing.T) {
	html := []byte(`<html><body>hi</body></html>`)
	if IsSufficient(html) {
		t.Error("expected insufficient for very short content")
	}
}

func TestIsSufficient_EmptyBody(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head></head><body></body></html>`)
	if IsSufficient(html) {
		t.Error("expected insufficient for empty body")
	}
}

func TestTextMarkupRatio(t *testing.T) {
	html := []byte(`<div>Hello World</div>`)
	text, markup := textMarkupRatio(html)
	if text == 0 {
		t.Error("expected non-zero text count")
	}
	if markup == 0 {
		t.Error("expected non-zero markup count")
	}
	if text >= markup+text {
		t.Error("text should be less than total")
	}
}
