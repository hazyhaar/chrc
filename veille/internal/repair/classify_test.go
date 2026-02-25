package repair

import "testing"

func TestClassify_Redirects(t *testing.T) {
	// WHAT: Redirects (301, 302) → follow_redirect action.
	// WHY: Auto-update URL prevents permanent fetch failures.
	for _, code := range []int{301, 302, 307, 308} {
		cls, act := Classify("web", code, "")
		if cls != ClassRedirect {
			t.Errorf("status %d: class = %s, want redirect", code, cls)
		}
		if act != ActionFollowRedirect {
			t.Errorf("status %d: action = %s, want follow_redirect", code, act)
		}
	}
}

func TestClassify_RateLimit(t *testing.T) {
	// WHAT: 429 → increase_rate action.
	// WHY: Slowing down prevents permanent bans.
	cls, act := Classify("api", 429, "")
	if cls != ClassRateLimit || act != ActionIncreaseRate {
		t.Errorf("429: got (%s, %s), want (rate_limit, increase_rate)", cls, act)
	}
}

func TestClassify_AuthBroken(t *testing.T) {
	// WHAT: 401 → mark_broken (expired API key).
	// WHY: Cannot auto-repair auth failures.
	cls, act := Classify("api", 401, "")
	if cls != ClassAuth || act != ActionMarkBroken {
		t.Errorf("401: got (%s, %s), want (auth, mark_broken)", cls, act)
	}
}

func TestClassify_ForbiddenWeb_RotateUA(t *testing.T) {
	// WHAT: 403 on web source → rotate_ua.
	// WHY: Many 403s are caused by bot-blocking User-Agent.
	cls, act := Classify("web", 403, "")
	if cls != ClassForbidden || act != ActionRotateUA {
		t.Errorf("403 web: got (%s, %s), want (forbidden, rotate_ua)", cls, act)
	}
}

func TestClassify_ForbiddenAPI_Broken(t *testing.T) {
	// WHAT: 403 on API source → mark_broken.
	// WHY: API 403 = revoked access, can't auto-fix.
	cls, act := Classify("api", 403, "")
	if cls != ClassForbidden || act != ActionMarkBroken {
		t.Errorf("403 api: got (%s, %s), want (forbidden, mark_broken)", cls, act)
	}
}

func TestClassify_NotFound(t *testing.T) {
	// WHAT: 404 and 410 → mark_broken.
	// WHY: Resource gone, no point retrying.
	for _, code := range []int{404, 410} {
		cls, act := Classify("web", code, "")
		if cls != ClassNotFound || act != ActionMarkBroken {
			t.Errorf("%d: got (%s, %s), want (not_found, mark_broken)", code, cls, act)
		}
	}
}

func TestClassify_ServerError_Backoff(t *testing.T) {
	// WHAT: 5xx → temporary + backoff.
	// WHY: Server-side issues are transient.
	for _, code := range []int{500, 502, 503} {
		cls, act := Classify("web", code, "")
		if cls != ClassTemporary || act != ActionBackoff {
			t.Errorf("%d: got (%s, %s), want (temporary, backoff)", code, cls, act)
		}
	}
}

func TestClassify_Timeout_Backoff(t *testing.T) {
	// WHAT: Timeout errors (no HTTP code) → temporary + backoff.
	// WHY: Network issues are usually transient.
	cls, act := Classify("web", 0, "http get: context deadline exceeded")
	if cls != ClassTemporary || act != ActionBackoff {
		t.Errorf("timeout: got (%s, %s), want (temporary, backoff)", cls, act)
	}
}

func TestClassify_DNS_Backoff(t *testing.T) {
	// WHAT: DNS resolution failure → temporary + backoff.
	// WHY: DNS failures can be transient (propagation, etc).
	cls, act := Classify("rss", 0, "http get: no such host example.com")
	if cls != ClassTemporary || act != ActionBackoff {
		t.Errorf("dns: got (%s, %s), want (temporary, backoff)", cls, act)
	}
}

func TestClassify_XMLParse_Broken(t *testing.T) {
	// WHAT: Parse error → mark_broken.
	// WHY: Content format changed, needs LLM investigation.
	cls, act := Classify("rss", 0, "xml parse error: unexpected EOF")
	if cls != ClassParse || act != ActionMarkBroken {
		t.Errorf("xml parse: got (%s, %s), want (parse, mark_broken)", cls, act)
	}
}

func TestClassify_Unknown(t *testing.T) {
	// WHAT: Unknown errors → no action.
	// WHY: fail_count increment is sufficient for unknown errors.
	cls, act := Classify("web", 0, "something weird happened")
	if cls != ClassUnknown || act != ActionNone {
		t.Errorf("unknown: got (%s, %s), want (unknown, none)", cls, act)
	}
}

func TestClassify_RSS403_RotateUA(t *testing.T) {
	// WHAT: 403 on RSS source → rotate_ua.
	// WHY: RSS feeds behind bot protection can be accessed with proper UA.
	cls, act := Classify("rss", 403, "")
	if cls != ClassForbidden || act != ActionRotateUA {
		t.Errorf("403 rss: got (%s, %s), want (forbidden, rotate_ua)", cls, act)
	}
}

func TestClassify_ConnectionRefused(t *testing.T) {
	// WHAT: Connection refused → temporary + backoff.
	// WHY: Server may be temporarily down.
	cls, act := Classify("web", 0, "http get: connection refused")
	if cls != ClassTemporary || act != ActionBackoff {
		t.Errorf("conn refused: got (%s, %s), want (temporary, backoff)", cls, act)
	}
}
