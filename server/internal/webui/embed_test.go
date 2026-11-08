package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticCachingAndPrecompression(t *testing.T) {
	files = map[string]asset{}
	files["/index.html"] = asset{body: []byte("<html>"), etag: `"idx"`, ctype: "text/html; charset=utf-8"}
	files["/assets/index-abc.js"] = asset{body: []byte("console.log(1)"), gz: []byte("gz-bytes"), etag: `"js"`, ctype: "text/javascript; charset=utf-8"}
	h := Handler()
	do := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	js := do("/assets/index-abc.js", map[string]string{"Accept-Encoding": "gzip"})
	if js.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || js.Header().Get("Content-Encoding") != "gzip" || js.Body.String() != "gz-bytes" {
		t.Fatalf("hashed asset: %v %q", js.Header(), js.Body.String())
	}
	if plain := do("/assets/index-abc.js", nil); plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != "console.log(1)" {
		t.Fatal("client without gzip must get the plain body")
	}
	index := do("/", nil)
	if index.Header().Get("Cache-Control") != "no-cache" || index.Header().Get("ETag") != `"idx"` || index.Body.String() != "<html>" {
		t.Fatalf("index: %v", index.Header())
	}
	if again := do("/", map[string]string{"If-None-Match": `"idx"`}); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("unchanged index should be 304, got %d", again.Code)
	}
	for _, page := range []string{"/overview", "/rooms?id=123", "/settings/"} {
		if got := do(page, nil); got.Code != http.StatusOK || got.Body.String() != "<html>" || got.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("page path %s should serve the app, got %d", page, got.Code)
		}
	}
	if missing := do("/assets/gone-123.js", nil); missing.Code != http.StatusNotFound {
		t.Fatal("a missing asset must 404 instead of returning the app")
	}
	if missing := do("/nope.js", nil); missing.Code != http.StatusNotFound {
		t.Fatal("unknown file should 404")
	}
	if api := do("/admin/api/snapshot", nil); api.Code != http.StatusNotFound {
		t.Fatal("API paths are not served by webui")
	}
}
