package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestSourceIPTrustBoundary(t *testing.T) {
	s := &Server{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8:1::/48")}}
	for _, tc := range []struct {
		name, peer string
		xff, real  []string
		want       string
	}{
		{"untrusted-spoof", "192.0.2.1:1000", []string{"198.51.100.1"}, []string{"198.51.100.2"}, "192.0.2.1"},
		{"trusted-single", "10.0.0.1:1000", []string{"198.51.100.1"}, nil, "198.51.100.1"},
		{"trusted-chain", "10.0.0.1:1000", []string{"192.0.2.9, 198.51.100.1, 10.2.0.1"}, nil, "198.51.100.1"},
		{"separate-header-lines", "10.0.0.1:1000", []string{"192.0.2.9, 198.51.100.1", "10.2.0.1"}, nil, "198.51.100.1"},
		{"untrusted-middle", "10.0.0.1:1000", []string{"198.51.100.1, 192.0.2.3"}, nil, "192.0.2.3"},
		{"real-ip", "10.0.0.1:1000", nil, []string{"198.51.100.2"}, "198.51.100.2"},
		{"xff-precedes-real", "10.0.0.1:1000", []string{"198.51.100.1"}, []string{"198.51.100.2"}, "198.51.100.1"},
		{"ipv6", "[2001:db8:1::1]:1000", []string{"2001:db8:2::1, 2001:db8:1::2"}, nil, "2001:db8:2::1"},
		{"mapped-ipv4", "[::ffff:10.0.0.1]:1000", []string{"::ffff:198.51.100.1"}, nil, "198.51.100.1"},
		{"invalid-xff", "10.0.0.1:1000", []string{"spoof, 198.51.100.1"}, []string{"198.51.100.2"}, "10.0.0.1"},
		{"empty-xff", "10.0.0.1:1000", []string{""}, []string{"198.51.100.2"}, "10.0.0.1"},
		{"invalid-real", "10.0.0.1:1000", nil, []string{"198.51.100.1:1234"}, "10.0.0.1"},
		{"multiple-real", "10.0.0.1:1000", nil, []string{"198.51.100.1", "198.51.100.2"}, "10.0.0.1"},
		{"zone-rejected", "10.0.0.1:1000", []string{"fe80::1%eth0"}, nil, "10.0.0.1"},
		{"excessive-chain", "10.0.0.1:1000", []string{strings.Repeat("10.0.0.1,", 64) + "198.51.100.1"}, nil, "10.0.0.1"},
		{"all-trusted", "10.0.0.1:1000", []string{"10.0.0.3, 10.0.0.2"}, nil, "10.0.0.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			for _, v := range tc.real {
				r.Header.Add("X-Real-IP", v)
			}
			if got := s.sourceIP(r); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := (&Server{}).sourceIP(r); got != "10.0.0.1" {
		t.Fatal("forwarding trusted by default")
	}
}

func TestForwardedLoginAndPasswordFailuresStayPerClient(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	db, err := store.Open(t.TempDir(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial := auth.Token()
	a, err := auth.New(db, key, initial)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Auth: a, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}}
	call := func(handler http.HandlerFunc, ip string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/admin/api/login", &buf)
		r.RemoteAddr = "10.0.0.1:1000"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", ip)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	for range 5 {
		w := call(s.login, "198.51.100.1", map[string]string{"username": "admin", "password": "wrong"}, nil)
		if w.Code != 401 {
			t.Fatal("expected failed login")
		}
	}
	if call(s.login, "198.51.100.1", map[string]string{"username": "admin", "password": initial}, nil).Code != 429 {
		t.Fatal("first client was not locked")
	}
	w := call(s.login, "198.51.100.2", map[string]string{"username": "admin", "password": initial}, nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("second client inherited proxy lock")
	}
	cookie := w.Result().Cookies()[0]
	badChange := map[string]string{"current_password": "wrong", "new_password": auth.Token()}
	for range 5 {
		if call(s.password, "198.51.100.3", badChange, cookie).Code != 400 {
			t.Fatal("expected old password failure")
		}
	}
	if call(s.password, "198.51.100.3", badChange, cookie).Code != 429 {
		t.Fatal("password failures did not lock forwarded client")
	}
	if call(s.password, "198.51.100.2", map[string]string{"current_password": initial, "new_password": auth.Token()}, cookie).Code != 200 {
		t.Fatal("password lock leaked to second client")
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM auth_failures WHERE ip='10.0.0.1'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failure recorded against trusted proxy")
	}
}
