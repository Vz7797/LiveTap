package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/observe"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

type fakeEngine struct{ runs *atomic.Int32 }

func (f *fakeEngine) Name() string { return "test" }
func (f *fakeEngine) Close()       {}
func (f *fakeEngine) Probe(ctx context.Context, s engine.RoomSpec) (engine.LiveStatus, error) {
	return engine.LiveStatus{Code: "online", LiveID: s.LiveID, RoomID: s.LiveID + "01", Name: "测试直播间"}, ctx.Err()
}
func (f *fakeEngine) Run(ctx context.Context, s engine.RoomSpec, sink engine.Sink) error {
	f.runs.Add(1)
	defer f.runs.Add(-1)
	sink.State(engine.EngineState{State: "collecting", Reason: "测试连接已就绪"})
	sink.Event(engine.RawEvent{Method: "WebcastChatMessage", ReceivedAt: time.Now(), Bytes: 20, Decode: func() model.Event {
		return model.Event{ID: s.LiveID, LiveID: s.LiveID, Type: "chat", Content: "test body never persisted", ReceivedAt: time.Now()}
	}})
	<-ctx.Done()
	return ctx.Err()
}
func TestAdminSecurityAndRoomLifecycle(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	s, e := store.Open(t.TempDir(), key)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cfg := model.DefaultSettings()
	cfg.MaxRooms = 2
	cfg.StartsPerSecond = 20
	if e = s.SaveSettings(cfg); e != nil {
		t.Fatal(e)
	}
	a, e := auth.New(s, key, "initial-password-for-tests")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	b := engine.NewBudget(100)
	m, e := room.New(ctx, s, b, func() engine.Engine { return &fakeEngine{&runs} }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	snapshot := observe.New(ctx, m)
	defer snapshot.Close()
	server := New(m, a, snapshot, http.NotFoundHandler())
	server.CookieSecure = true
	handler := server.Handler()
	cookie, csrf := "", ""
	request := func(method, path string, body any, sendCSRF bool) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		r := httptest.NewRequest(method, path, &buf)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		if sendCSRF {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/admin/api/rooms", nil, false); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	w := request("POST", "/admin/api/login", map[string]string{"username": "admin", "password": "initial-password-for-tests"}, false)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	cookie = cookies[0].Name + "=" + cookies[0].Value
	var session map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &session)
	csrf = session["csrf"].(string)
	if w = request("GET", "/admin/api/rooms", nil, false); w.Code != 403 {
		t.Fatal("password change not enforced")
	}
	body := map[string]string{"current_password": "initial-password-for-tests", "new_password": "changed-password-for-tests"}
	if w = request("POST", "/admin/api/password", body, false); w.Code != 403 {
		t.Fatal("CSRF not enforced")
	}
	if w = request("POST", "/admin/api/password", body, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request("PUT", "/admin/api/settings", map[string]any{"settings": map[string]any{"share_guest_identity": true}, "apply": "new"}, true)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"share_guest_identity":true`)) {
		t.Fatal("sharing setting missing from response")
	}
	stored, err := s.Settings()
	if err != nil || !stored.ShareGuestIdentity {
		t.Fatal("sharing setting not persisted")
	}
	w = request("GET", "/admin/api/settings", nil, false)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"share_guest_identity":true`)) {
		t.Fatal("sharing setting missing after reload")
	}
	r := httptest.NewRequest("POST", "/admin/api/rooms", bytes.NewBufferString(`{"live_ids":["123456789"]}`))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Origin", "https://attacker.invalid")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site mutation allowed")
	}
	w = request("POST", "/admin/api/rooms", map[string]any{"live_ids": []string{"123456789", "223456789", "323456789"}}, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var added struct {
		Results []struct {
			OK bool `json:"ok"`
		}
	}
	_ = json.Unmarshal(w.Body.Bytes(), &added)
	if len(added.Results) != 3 || !added.Results[0].OK || !added.Results[1].OK || added.Results[2].OK {
		t.Fatal("capacity bound incorrect")
	}
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 2 {
		t.Fatal("tasks failed to start")
	}
	w = request("PATCH", "/admin/api/rooms/123456789", map[string]any{"note": "persisted note", "overrides": map[string]any{"protocol_mode": "pc", "cookie": "test_cookie=encrypted_value"}}, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request("GET", "/admin/api/rooms", nil, false)
	if bytes.Contains(w.Body.Bytes(), []byte("encrypted_value")) {
		t.Fatal("secret leaked")
	}
	var options string
	if e = s.DB.QueryRow("SELECT options FROM rooms WHERE live_id='123456789'").Scan(&options); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains([]byte(options), []byte("encrypted_value")) {
		t.Fatal("plaintext persisted")
	}
	w = request("POST", "/admin/api/rooms/123456789/actions", map[string]string{"action": "pause"}, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	deadline = time.Now().Add(3 * time.Second)
	for runs.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 1 {
		t.Fatal("pause affected wrong number of rooms")
	}
	if w = request("DELETE", "/admin/api/rooms/123456789", nil, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	records, e := s.Rooms()
	if e != nil || len(records) != 1 {
		t.Fatal("delete not persisted")
	}
	if runs.Load() != 1 {
		t.Fatal("unrelated task stopped")
	}
	if w = request("GET", "/metrics", nil, false); w.Code != 200 {
		t.Fatal("metrics auth failed")
	}
	if w = request("POST", "/admin/api/logout", nil, true); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w = request("GET", "/admin/api/rooms", nil, false); w.Code != 401 {
		t.Fatal("logout did not revoke session")
	}
}
