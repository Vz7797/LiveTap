package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListenerSeparation(t *testing.T) {
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })
	north := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	for _, separate := range []bool{false, true} {
		public, management := Listeners(admin, north, separate)
		for _, tc := range []struct {
			h    http.Handler
			path string
			want int
		}{{public, "/v1/node", 202}, {management, "/v1/node", 404}, {public, "/healthz", 201}, {management, "/admin/api/clients", 201}} {
			w := httptest.NewRecorder()
			tc.h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.want {
				t.Fatal("listener route boundary failed")
			}
		}
		w := httptest.NewRecorder()
		public.ServeHTTP(w, httptest.NewRequest("GET", "/admin/api/clients", nil))
		want := 201
		if separate {
			want = 404
		}
		if w.Code != want {
			t.Fatal("management API exposed on public port")
		}
	}
}
