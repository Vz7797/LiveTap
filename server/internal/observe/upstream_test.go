package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v2.3.3", "v2.3.2", true},
		{"v2.4.0", "v2.3.9", true},
		{"v3.0.0", "v2.9.9", true},
		{"v2.3.2", "v2.3.2", false},
		{"v2.3.1", "v2.3.2", false},
		{"v2.10.0", "v2.9.0", true},
		{"v2.3.3-rc1", "v2.3.2", true},
		{"latest", "v2.3.2", false},
		{"", "v2.3.2", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestUpstreamCheckReportsNewerReleaseAndKeepsItOnFailure(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub requires a User-Agent")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://github.com/jwwsjlm/douyinLive/releases/tag/v9.9.9","published_at":"2026-10-23T09:16:53Z"}`))
	}))
	defer srv.Close()
	u := NewUpstreamCheck(true)
	u.url = srv.URL
	u.check(context.Background())
	s := u.Status()
	if !s.Newer || s.Latest != "v9.9.9" || s.Version != buildinfo.EngineVersion || s.PublishedAt == 0 || s.Error != "" {
		t.Fatalf("status = %+v", s)
	}
	fail = true
	u.check(context.Background())
	s = u.Status()
	if !s.Newer || s.Latest != "v9.9.9" || s.Error != "GitHub 返回 HTTP 403" {
		t.Fatalf("a failed check must keep the last answer and report the error: %+v", s)
	}
}

func TestUpstreamCheckDisabledOrMissing(t *testing.T) {
	var none *UpstreamCheck
	if s := none.Status(); s.Enabled || s.Version != buildinfo.EngineVersion {
		t.Fatalf("nil status = %+v", s)
	}
	u := NewUpstreamCheck(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u.Run(ctx) // returns at once without a request
	if s := u.Status(); s.Enabled || s.CheckedAt != 0 {
		t.Fatalf("disabled status = %+v", s)
	}
}
