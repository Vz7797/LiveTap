package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/observe"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestLabDisabledHidesRoutesAndReportsFeature(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	db, err := store.Open(t.TempDir(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	password := auth.Token()
	a, err := auth.New(db, key, password)
	if err != nil {
		t.Fatal(err)
	}
	m, err := room.New(context.Background(), db, engine.NewBudget(100), nil, "offline")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	snap := observe.New(context.Background(), m)
	defer snap.Close()
	token, err := a.Login("127.0.0.1", "admin", password)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.ChangePassword("127.0.0.1", token, password, auth.Token()); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		s := New(m, a, snap, http.NotFoundHandler())
		s.LabEnabled = enabled
		handler := s.Handler()
		call := func(path string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("GET", path, nil)
			r.AddCookie(&http.Cookie{Name: "livetap_session", Value: token})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		var session struct {
			Features map[string]bool `json:"features"`
		}
		if err = json.Unmarshal(call("/admin/api/session").Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		if session.Features["lab"] != enabled {
			t.Fatalf("enabled=%v: session features = %v", enabled, session.Features)
		}
		want := 404
		if enabled {
			want = 200
		}
		if code := call("/admin/api/lab/runs").Code; code != want {
			t.Fatalf("enabled=%v: GET lab/runs = %d, want %d", enabled, code, want)
		}
	}
}
