package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheckStatusAndRedirect(t *testing.T) {
	for _, status := range []int{200, 503, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Error("unexpected path")
				}
				w.Header().Set("Location", "https://example.invalid/")
				w.WriteHeader(status)
			}))
			defer s.Close()
			if (healthcheck(strings.TrimPrefix(s.URL, "http://")) == nil) != (status == 200) {
				t.Fatal("health status mismatch")
			}
		})
	}
}
