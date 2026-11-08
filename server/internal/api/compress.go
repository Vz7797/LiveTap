package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Compress gzips JSON API responses (/admin/api, /v1, /metrics) for clients that
// accept it. Over slow links the uncompressed 2.5 s snapshot alone could saturate
// the admin's bandwidth (docs/design.md, revision v0.18). WebSocket upgrades are left
// untouched (they negotiate permessage-deflate themselves); static assets are
// precompressed by the webui package and pass through unchanged.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !compressible(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

func compressible(r *http.Request) bool {
	if r.Header.Get("Upgrade") != "" || r.Header.Get("Range") != "" || r.Method == http.MethodHead {
		return false
	}
	p := r.URL.Path
	if !strings.HasPrefix(p, "/admin/api/") && !strings.HasPrefix(p, "/v1/") && p != "/metrics" {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if name, _, _ := strings.Cut(strings.TrimSpace(part), ";"); strings.EqualFold(name, "gzip") {
			return true
		}
	}
	return false
}

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(status int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		h.Add("Vary", "Accept-Encoding")
		// Bodiless or already-encoded responses are sent as is.
		if status != http.StatusNoContent && status != http.StatusNotModified && h.Get("Content-Encoding") == "" {
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			g.gz = gzipPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.gz == nil {
		return g.ResponseWriter.Write(p)
	}
	return g.gz.Write(p)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		g.gz.Reset(io.Discard)
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}
