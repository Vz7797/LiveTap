package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// asset is one embedded file prepared once at startup: its gzip form (when it is
// worth it) and a content-hash ETag. Embedded files have no modification time, so
// without an ETag browsers re-downloaded every script on each visit; over a slow
// cross-carrier link that cost several seconds (docs/design.md, revision v0.18).
type asset struct {
	body, gz    []byte
	etag, ctype string
}

var files = load()

func load() map[string]asset {
	out := map[string]asset{}
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		return out
	}
	_ = fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path.Base(name), ".") {
			return nil
		}
		body, err := fs.ReadFile(root, name)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(body)
		a := asset{body: body, etag: `"` + hex.EncodeToString(sum[:12]) + `"`, ctype: mime.TypeByExtension(path.Ext(name))}
		if a.ctype == "" {
			a.ctype = http.DetectContentType(body)
		}
		if compressibleType(a.ctype) && len(body) > 1024 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(body)
			_ = zw.Close()
			if buf.Len() < len(body) {
				a.gz = buf.Bytes()
			}
		}
		out["/"+name] = a
		return nil
	})
	return out
}

func compressibleType(ctype string) bool {
	return strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "javascript") || strings.Contains(ctype, "json") || strings.Contains(ctype, "svg")
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if name, _, _ := strings.Cut(strings.TrimSpace(part), ";"); strings.EqualFold(name, "gzip") {
			return true
		}
	}
	return false
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		a, ok := files[name]
		// Page paths (/overview, /rooms?id=…) are routed by the admin app itself; any
		// path without a file extension gets the app. Missing files still 404.
		if !ok && path.Ext(name) == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			name = "/index.html"
			a, ok = files[name]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", a.ctype)
		h.Set("ETag", a.etag)
		h.Set("Vary", "Accept-Encoding")
		if strings.HasPrefix(name, "/assets/") {
			// Vite names these by content hash; a new build gets new names.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, a.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := a.body
		if a.gz != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}
