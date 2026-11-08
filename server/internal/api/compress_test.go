package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompressGzipsAPIResponsesOnly(t *testing.T) {
	body := strings.Repeat(`{"liveId":"123456789","status":"collecting"}`, 200)
	h := Compress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "999")
		_, _ = io.WriteString(w, body)
	}))
	get := func(path, enc string, upgrade bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		if upgrade {
			r.Header.Set("Upgrade", "websocket")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := get("/admin/api/snapshot", "gzip, deflate, br", false)
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Content-Length") != "" {
		t.Fatalf("API response not gzipped: %v", w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != body {
		t.Fatal("gzip body differs")
	}
	if w.Body.Len() >= len(body)/4 {
		t.Fatalf("compression too weak: %d of %d", w.Body.Len(), len(body))
	}

	for _, c := range []struct {
		path, enc string
		upgrade   bool
	}{
		{"/admin/api/snapshot", "", false},  // client does not accept gzip
		{"/admin/api/stream", "gzip", true}, // WebSocket upgrade negotiates its own compression
		{"/assets/index.js", "gzip", false}, // static files are precompressed by webui
		{"/v1/stream", "gzip", true},
	} {
		if w := get(c.path, c.enc, c.upgrade); w.Header().Get("Content-Encoding") != "" || w.Body.String() != body {
			t.Fatalf("%s should pass through unchanged", c.path)
		}
	}
	if w := get("/v1/node", "gzip", false); w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("northbound JSON should be gzipped")
	}
}
