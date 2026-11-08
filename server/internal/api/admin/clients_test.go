package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/api/northbound"
	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/observe"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestClientManagementRequiresSessionAndShowsKeyOnlyOnce(t *testing.T) {
	key := bytes.Repeat([]byte{8}, 32)
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
	north, err := northbound.New(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	defer north.Close()
	snap := observe.New(context.Background(), m)
	defer snap.Close()
	s := New(m, a, snap, http.NotFoundHandler())
	s.Northbound = north
	handler := s.Handler()
	token, err := a.Login("127.0.0.1", "admin", password)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.ChangePassword("127.0.0.1", token, password, auth.Token()); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, apiKey string, session bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if session {
			r.AddCookie(&http.Cookie{Name: "livetap_session", Value: token})
			r.Header.Set("X-CSRF-Token", a.CSRF(token))
		}
		if apiKey != "" {
			r.Header.Set("Authorization", "Bearer "+apiKey)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/admin/api/clients", `{"name":"fixture","env":"测试","quota":2,"enabled":true,"whitelist":[]}`, "", true)
	if w.Code != 201 {
		t.Fatal("create failed")
	}
	var created struct {
		APIKey string `json:"api_key"`
		Client struct {
			ID string `json:"id"`
		} `json:"client"`
	}
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || len(created.APIKey) != 51 {
		t.Fatal("one-time key missing")
	}
	if call("GET", "/admin/api/clients", "", created.APIKey, false).Code != 401 {
		t.Fatal("API key authorized management")
	}
	for _, path := range []string{"/admin/api/clients", "/admin/api/snapshot", "/admin/api/audit"} {
		w = call("GET", path, "", "", true)
		if w.Code != 200 || strings.Contains(w.Body.String(), created.APIKey) || strings.Contains(w.Body.String(), auth.Digest(created.APIKey)) {
			t.Fatal("credential appeared after creation")
		}
	}
	if call("DELETE", "/admin/api/clients/"+created.Client.ID, "", "", true).Code != 200 {
		t.Fatal("delete failed")
	}
}
