package engine

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

type replayBody struct {
	io.Reader
	io.Closer
}

// Inspect a bounded prefix and replay it. Never expose upstream bodies/headers.
func RestrictedResponse(r *http.Response) bool { return RestrictedReason(r) != "" }

// RestrictedReason explains why a response counts as restricted, or returns "".
// It names only the rule, status and host+path; never query strings, headers or bodies.
func RestrictedReason(r *http.Response) string {
	if r == nil || r.StatusCode == http.StatusSwitchingProtocols {
		return ""
	}
	where := ""
	if r.Request != nil && r.Request.URL != nil {
		where = " " + r.Request.URL.Host + r.Request.URL.Path
	}
	if r.StatusCode == http.StatusForbidden || r.StatusCode == http.StatusTooManyRequests {
		return "status_" + http.StatusText(r.StatusCode) + where
	}
	if r.Body == nil {
		return ""
	}
	body := r.Body
	prefix, err := io.ReadAll(io.LimitReader(body, 64<<10))
	r.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(prefix), body), Closer: body}
	if err != nil {
		return ""
	}
	text := string(prefix)
	for _, marker := range []string{"验证码中间页", "<title>验证码", "<title>安全验证", "id=\"captcha_container\"", "id=\"verify-center\""} {
		if strings.Contains(text, marker) {
			return "captcha_page" + where
		}
	}
	// An empty web/enter body alone is NOT a restriction: the engine routinely falls
	// back to the live page (third_party/douyinLive room_info.go). Treating it as one
	// tripped the node cooldown on a clean server IP (2026-10-16 server test). When both
	// the live page and web/enter fail, the engine reports ErrUpstreamAccessRestricted.
	return ""
}
