package northbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/Vz7797/LiveTap/sdk/go"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

type fakeSource struct {
	mu      sync.Mutex
	sinks   map[string]engine.Sink
	starts  atomic.Int64
	offline bool
}
type fakeEngine struct{ source *fakeSource }

func (f *fakeEngine) Name() string { return "offline-fixture" }
func (f *fakeEngine) Close()       {}
func (f *fakeEngine) Probe(ctx context.Context, s engine.RoomSpec) (engine.LiveStatus, error) {
	code := "online"
	if f.source.offline {
		code = "offline"
	}
	return engine.LiveStatus{Code: code, LiveID: s.LiveID, Name: "fake room"}, ctx.Err()
}
func (f *fakeEngine) Run(ctx context.Context, s engine.RoomSpec, sink engine.Sink) error {
	f.source.starts.Add(1)
	f.source.mu.Lock()
	f.source.sinks[s.LiveID] = sink
	f.source.mu.Unlock()
	defer func() { f.source.mu.Lock(); delete(f.source.sinks, s.LiveID); f.source.mu.Unlock() }()
	sink.State(engine.EngineState{State: "collecting", Reason: "fake ready"})
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeSource) emit(id, msg string) {
	f.mu.Lock()
	sink := f.sinks[id]
	f.mu.Unlock()
	if sink == nil {
		return
	}
	at := time.Now()
	sink.Event(engine.RawEvent{Method: "WebcastChatMessage", ReceivedAt: at, Bytes: 100, Decode: func() model.Event {
		return model.Event{ID: msg, LiveID: id, Type: "chat", TS: at, ReceivedAt: at, Content: "synthetic", Extra: map[string]any{}}
	}})
}

type fixture struct {
	s      *Service
	m      *room.Manager
	db     *store.Store
	source *fakeSource
	http   *httptest.Server
	ctx    context.Context
}

func setup(t *testing.T, offline bool, options ...Options) *fixture {
	t.Helper()
	db, err := store.Open(t.TempDir(), bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.DefaultSettings()
	cfg.StartsPerSecond = 20
	cfg.RequestsPerSecond = 100
	if err = db.SaveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeSource{sinks: map[string]engine.Sink{}, offline: offline}
	m, err := room.New(ctx, db, engine.NewBudget(100), func() engine.Engine { return &fakeEngine{source} }, "fake-node")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, m, options...)
	if err != nil {
		t.Fatal(err)
	}
	// A fake resolver covers short aliases without accessing any upstream host.
	s.Resolve = func(ctx context.Context, input string) (string, error) {
		id, err := room.ParseID(input)
		if id == "shop_alias" {
			return "123456789", nil
		}
		return id, err
	}
	server := httptest.NewServer((&Server{Service: s, PingInterval: time.Second}).Handler())
	f := &fixture{s, m, db, source, server, ctx}
	t.Cleanup(func() {
		cancel()
		server.CloseClientConnections()
		s.Close()
		server.Close()
		m.Close()
		db.Close()
	})
	return f
}
func (f *fixture) client(t *testing.T) (model.Client, *sdk.Client, string) {
	t.Helper()
	c, key, err := f.s.CreateClient(model.Client{Name: "fixture", Env: "测试", Enabled: true, Quota: 200})
	if err != nil {
		t.Fatal(err)
	}
	client, err := sdk.New(f.http.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	return c, client, key
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("local condition timeout")
}

func TestRestartRestoresValidLeaseAndReleasesExpiredAndOrphanRooms(t *testing.T) {
	db, err := store.Open(t.TempDir(), bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeSource{sinks: map[string]engine.Sink{}}
	start := func() (*room.Manager, *Service) {
		t.Helper()
		m, err := room.New(ctx, db, engine.NewBudget(100), func() engine.Engine { return &fakeEngine{source} }, "restart-fixture")
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(ctx, m)
		if err != nil {
			m.Close()
			t.Fatal(err)
		}
		return m, s
	}
	m, s := start()
	c, key, err := s.CreateClient(model.Client{Name: "restart", Env: "测试", Enabled: true, Quota: 10})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := s.Subscribe(ctx, c.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.Subscribe(ctx, c.ID, SubscribeRequest{LiveID: "234567890"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.EnsureSubscription(ctx, "345678901"); err != nil {
		t.Fatal(err)
	}
	boot := m.BootID
	s.Close()
	m.Close()
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if err = db.SaveSubscription(expired); err != nil {
		t.Fatal(err)
	}
	m, s = start()
	defer m.Close()
	defer s.Close()
	if m.BootID == boot {
		t.Fatal("node restart did not change boot ID")
	}
	if id, err := s.Authenticate(key, netip.MustParseAddr("127.0.0.1")); err != nil || id != c.ID {
		t.Fatal("stored key hash did not authenticate after restart")
	}
	subs := s.List(c.ID)
	if len(subs) != 1 || subs[0].ID != valid.ID || subs[0].StartSeq != 0 {
		t.Fatal("valid persisted subscription was not restored")
	}
	if status, ok := m.Status(valid.LiveID); !ok || !status.Enabled {
		t.Fatal("valid subscription room was not resumed")
	}
	for _, id := range []string{expired.LiveID, "345678901"} {
		if _, ok := m.Status(id); ok {
			t.Fatal("unreferenced subscription room survived restart")
		}
	}
	if m.Budget.Count() != 0 {
		t.Fatal("restart fixture contacted upstream")
	}
}

func TestLeaseReferencesNormalizationQuotaAndManualOwnership(t *testing.T) {
	f := setup(t, false)
	a, _, _ := f.client(t)
	b, _, _ := f.client(t)
	first, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "https://live.douyin.com/123456789/?from=fixture"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "shop_alias"})
	if err != nil || same.ID != first.ID {
		t.Fatal("alias/id idempotency failed")
	}
	other, err := f.s.Subscribe(f.ctx, b.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.source.starts.Load() == 1 })
	if used, _ := f.m.Capacity(); used != 1 {
		t.Fatal("shared room allocated twice")
	}
	if err = f.s.Unsubscribe(a.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.m.Status(first.LiveID); !ok {
		t.Fatal("room released while another subscriber remained")
	}
	if _, _, err = f.s.Renew(a.ID, []string{other.ID}); err == nil {
		t.Fatal("cross-client renew allowed")
	}
	if _, err = f.s.Get(a.ID, other.ID); err == nil {
		t.Fatal("cross-client detail allowed")
	}
	if err = f.s.Unsubscribe(a.ID, other.ID); err == nil {
		t.Fatal("cross-client delete allowed")
	}
	if _, _, err = f.m.Add(f.ctx, "123456789", nil); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Unsubscribe(b.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	st, ok := f.m.Status("123456789")
	if !ok || st.Source != "manual" {
		t.Fatal("manual ownership lost after final subscription")
	}
	a.Quota = 1
	if err = f.s.UpdateClient(a.ID, a); err != nil {
		t.Fatal(err)
	}
	sub, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "223456789"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "323456789"}); err == nil {
		t.Fatal("client quota exceeded")
	}
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return time.Now().Add(121 * time.Second) }
	f.s.mu.Unlock()
	f.s.Sweep()
	if _, ok = f.m.Status(sub.LiveID); ok {
		t.Fatal("expired unowned room retained")
	}
	if f.m.Budget.Count() != 0 {
		t.Fatal("fake test issued upstream request")
	}
}
func TestIdleReleaseNotResetByProbesAndManualRoomPreserved(t *testing.T) {
	f := setup(t, true)
	a, _, _ := f.client(t)
	sub, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "123456789", Options: model.SubscriptionOptions{LeaseSeconds: 3600, IdleReleaseSeconds: 1}})
	if err != nil {
		t.Fatal(err)
	}
	q, _, err := f.s.Open(a.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(q)
	waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.State == "waiting" })
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return time.Now().Add(2 * time.Second) }
	f.s.mu.Unlock()
	f.s.Sweep()
	if _, ok := f.m.Status(sub.LiveID); ok {
		t.Fatal("idle room not released")
	}
	p, _, ok := q.Next()
	if !ok || p.Event.Extra["reason"] != "idle_release" {
		t.Fatal("idle release status missing")
	}
}
func TestAPIKeyHashIPPolicyIsolationAndRevocation(t *testing.T) {
	f := setup(t, false)
	a, c, key := f.client(t)
	_, other, _ := f.client(t)
	var hash, data string
	if err := f.db.DB.QueryRow("SELECT key_hash,data FROM clients WHERE id=?", a.ID).Scan(&hash, &data); err != nil {
		t.Fatal(err)
	}
	if hash == key || strings.Contains(data, key) || len(hash) != 64 {
		t.Fatal("API key stored in plaintext")
	}
	views, _, _ := f.s.Metrics()
	encoded, _ := json.Marshal(views)
	if bytes.Contains(encoded, []byte(key)) || bytes.Contains(encoded, []byte(hash)) {
		t.Fatal("client listing leaked credentials")
	}
	sub, err := c.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Subscription(f.ctx, sub.ID); err == nil {
		t.Fatal("API detail leaked")
	}
	if _, err = other.StatusBatch(f.ctx, []string{sub.LiveID}); err == nil {
		t.Fatal("batch status leaked")
	}
	request := httptest.NewRequest("GET", "/v1/node", nil)
	request.AddCookie(&http.Cookie{Name: "livetap_session", Value: "fixture"})
	w := httptest.NewRecorder()
	(&Server{Service: f.s}).Handler().ServeHTTP(w, request)
	if w.Code != 401 {
		t.Fatal("management cookie authorized northbound")
	}
	var envelope map[string]any
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope["request_id"] == nil || envelope["error"].(map[string]any)["suggestion"] == nil {
		t.Fatal("uniform error missing")
	}
	a.Whitelist = []string{"192.0.2.0/24"}
	if err = f.s.UpdateClient(a.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Node(f.ctx); err == nil {
		t.Fatal("IP restriction ignored")
	}
	if _, err = f.s.Authenticate(key, netip.MustParseAddr("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	a.Whitelist = nil
	a.Enabled = false
	if err = f.s.UpdateClient(a.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Node(f.ctx); err == nil {
		t.Fatal("disabled key accepted")
	}
	if _, ok := f.m.Status(sub.LiveID); ok {
		t.Fatal("disabled client kept room alive")
	}
	if err = f.s.DeleteClient(a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestHealthyStreamRenewsOnlyItsScopeAndDoesNotReviveExpiredLease(t *testing.T) {
	f := setup(t, false)
	owner, _, _ := f.client(t)
	first, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "234567890"})
	if err != nil {
		t.Fatal(err)
	}
	q, _, err := f.s.Open(owner.ID, first.ID, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(q)
	now := time.Now().Add(65 * time.Second)
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return now }
	f.s.mu.Unlock()
	if err = f.s.KeepAlive(q); err != nil {
		t.Fatal(err)
	}
	renewed, err := f.s.Get(owner.ID, first.ID)
	untouched, otherErr := f.s.Get(owner.ID, second.ID)
	if err != nil || otherErr != nil || !renewed.ExpiresAt.After(first.ExpiresAt) || !untouched.ExpiresAt.Equal(second.ExpiresAt) {
		t.Fatal("single-subscription stream renewed the wrong scope")
	}
	all, _, err := f.s.Open(owner.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(all)
	if err = f.s.KeepAlive(all); err != nil {
		t.Fatal(err)
	}
	untouched, err = f.s.Get(owner.ID, second.ID)
	if err != nil || !untouched.ExpiresAt.After(second.ExpiresAt) {
		t.Fatal("all-subscription stream failed to renew remaining lease")
	}
	f.s.mu.Lock()
	now = now.Add(121 * time.Second)
	f.s.mu.Unlock()
	if err = f.s.KeepAlive(all); err != nil {
		t.Fatal(err)
	}
	if len(f.s.List(owner.ID)) != 0 {
		t.Fatal("healthy stream revived expired leases")
	}
	f.s.Sweep()
	if used, _ := f.m.Capacity(); used != 0 {
		t.Fatal("expired stream leases retained upstream capacity")
	}
}
func TestSDKEndToEndReconnectReplayRenewAndExpiry(t *testing.T) {
	f := setup(t, false)
	owner, c, _ := f.client(t)
	sub, err := c.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: "123456789", ExternalRef: "merchant-session", Options: sdk.Options{LeaseSeconds: 3}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.source.starts.Load() == 1 })
	renewCtx, stopRenew := context.WithCancel(f.ctx)
	defer stopRenew()
	renewDone := make(chan error, 1)
	go func() { renewDone <- c.AutoRenew(renewCtx, []sdk.Subscription{sub}, nil) }()
	ctx, stop := context.WithCancel(f.ctx)
	defer stop()
	events := make(chan sdk.Event, 20)
	var connects atomic.Int64
	done := make(chan error, 1)
	go func() {
		done <- c.Stream(ctx, sdk.StreamOptions{OnConnect: func() { connects.Add(1) }, OnEvent: func(e sdk.Event) error {
			if e.Type == "chat" {
				events <- e
			}
			return nil
		}, OnGap: func(sdk.Event) { t.Error("unexpected local replay gap") }})
	}()
	waitFor(t, func() bool { return connects.Load() == 1 })
	f.source.emit(sub.LiveID, "fixture-1")
	select {
	case e := <-events:
		if e.ID != "fixture-1" || e.ExternalRef != "merchant-session" || e.Seq == 0 {
			t.Fatal("event contract mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event missing")
	}
	f.s.mu.Lock()
	f.s.closeClient(owner.ID)
	f.s.mu.Unlock()
	f.source.emit(sub.LiveID, "fixture-2")
	waitFor(t, func() bool { return connects.Load() >= 2 })
	select {
	case e := <-events:
		if e.ID != "fixture-2" {
			t.Fatal("replayed event mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconnect replay missing")
	}
	f.source.emit(sub.LiveID, "fixture-2")
	time.Sleep(100 * time.Millisecond)
	select {
	case <-events:
		t.Fatal("duplicate message delivered")
	default:
	}
	// Stop streaming while automatic HTTP renewal keeps the short lease alive.
	stop()
	<-done
	time.Sleep(3500 * time.Millisecond)
	if _, err = c.Subscription(f.ctx, sub.ID); err != nil {
		t.Fatal("automatic renewal failed")
	}
	stopRenew()
	<-renewDone
	waitFor(t, func() bool { _, ok := f.m.Status(sub.LiveID); return !ok })
	if f.m.Budget.Count() != 0 {
		t.Fatal("upstream request in fake E2E")
	}
}

func TestMalformedRequestsAreStructuredAndNeverEchoKey(t *testing.T) {
	f := setup(t, false)
	_, _, key := f.client(t)
	for _, path := range []string{"/v1/subscriptions", "/v1/rooms/status:batch", "/v1/subscriptions:renew", "/v1/stream"} {
		method := "POST"
		if path == "/v1/stream" {
			method = "GET"
		}
		r, _ := http.NewRequest(method, f.http.URL+path, strings.NewReader(`{"unknown":true}`))
		r.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal("local request failed")
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 || bytes.Contains(body, []byte(key)) || !bytes.Contains(body, []byte(`"suggestion"`)) {
			t.Fatal("unsafe error response")
		}
	}
}

func TestLastSeenWritesAtMostOncePerThirtySeconds(t *testing.T) {
	f := setup(t, false)
	_, _, key := f.client(t)
	if _, err := f.db.DB.Exec(`CREATE TABLE auth_write_count(n INTEGER); INSERT INTO auth_write_count VALUES(0);
 CREATE TRIGGER count_auth_write AFTER UPDATE ON clients BEGIN UPDATE auth_write_count SET n=n+1; END;`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return now }
	f.s.mu.Unlock()
	for i := 0; i < 60; i++ {
		if _, err := f.s.Authenticate(key, netip.MustParseAddr("127.0.0.1")); err != nil {
			t.Fatal("authentication failed")
		}
	}
	var writes int
	if err := f.db.DB.QueryRow(`SELECT n FROM auth_write_count`).Scan(&writes); err != nil || writes != 1 {
		t.Fatal("frequent authentication wrote last-used repeatedly")
	}
	f.s.mu.Lock()
	now = now.Add(30 * time.Second)
	f.s.mu.Unlock()
	if _, err := f.s.Authenticate(key, netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Fatal("authentication failed")
	}
	if err := f.db.DB.QueryRow(`SELECT n FROM auth_write_count`).Scan(&writes); err != nil || writes != 2 {
		t.Fatal("last-used did not persist after thirty seconds")
	}
	if f.s.Node()["api_version"] != sdk.APIVersion {
		t.Fatal("SDK and node API version mismatch")
	}
}

func TestProductionAndConfiguredPingIntervals(t *testing.T) {
	if (&Server{}).pingInterval() != 15*time.Second || (&Server{PingInterval: time.Second}).pingInterval() != time.Second {
		t.Fatal("ping interval does not respect production default or test override")
	}
}

func TestDormantManualRoomReleasesSubscriptionAndStays(t *testing.T) {
	old := room.NeverLiveLimit
	room.NeverLiveLimit = 0
	defer func() { room.NeverLiveLimit = old }()
	f := setup(t, true)
	a, _, _ := f.client(t)
	if _, _, err := f.m.Add(f.ctx, "123456789", nil); err != nil {
		t.Fatal(err)
	}
	sub, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "123456789", ExternalRef: "merchant:1", Options: model.SubscriptionOptions{LeaseSeconds: 3600, IdleReleaseSeconds: 3600}})
	if err != nil {
		t.Fatal(err)
	}
	if refs := f.s.SubscriberRefs()["123456789"]; len(refs) != 1 || refs[0].ExternalRef != "merchant:1" {
		t.Fatal("subscriber refs missing", refs)
	}
	waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.Dormant })
	f.s.Sweep()
	if _, err = f.s.Get(a.ID, sub.ID); err == nil {
		t.Fatal("subscription kept on a room that stopped probing")
	}
	if st, ok := f.m.Status(sub.LiveID); !ok || st.Source != "manual" || !st.Dormant {
		t.Fatal("manual room should stay, dormant")
	}
}

func TestClientSubscriptionsListsOnlyThatClient(t *testing.T) {
	f := setup(t, true)
	a, _, _ := f.client(t)
	b, _, _ := f.client(t)
	if _, err := f.s.Subscribe(f.ctx, a.ID, SubscribeRequest{LiveID: "123456789", ExternalRef: "merchant:a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Subscribe(f.ctx, b.ID, SubscribeRequest{LiveID: "223456789", ExternalRef: "merchant:b"}); err != nil {
		t.Fatal(err)
	}
	subs, err := f.s.ClientSubscriptions(a.ID)
	if err != nil || len(subs) != 1 || subs[0].ExternalRef != "merchant:a" {
		t.Fatal("unexpected subscriptions", subs, err)
	}
	if _, err = f.s.ClientSubscriptions("missing"); err == nil {
		t.Fatal("missing client accepted")
	}
}

func TestRenewKeepsValidLeasesAndExplainsEndedOnes(t *testing.T) {
	f := setup(t, false)
	owner, c, _ := f.client(t)
	other, _, _ := f.client(t)
	kept, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "234567890"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.s.Subscribe(f.ctx, other.ID, SubscribeRequest{LiveID: "345678901"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Unsubscribe(owner.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	renewed, missing, err := c.Renew(f.ctx, []string{kept.ID, gone.ID, foreign.ID, "sub_unknown", kept.ID})
	if err != nil || len(renewed) != 1 || renewed[0].ID != kept.ID {
		t.Fatal("one ended subscription must not stop the rest from renewing")
	}
	// Another client's ID reads as unknown, never as someone else's.
	want := []sdk.Released{{SubscriptionID: gone.ID, LiveID: gone.LiveID, Reason: "unsubscribed"}, {SubscriptionID: foreign.ID, Reason: "not_found"}, {SubscriptionID: "sub_unknown", Reason: "not_found"}}
	if !slices.Equal(missing, want) {
		t.Fatalf("missing = %+v", missing)
	}
	_, missing, err = c.Renew(f.ctx, []string{gone.ID})
	var api *sdk.APIError
	if !errors.As(err, &api) || api.Status != 404 || len(missing) != 1 || missing[0].Reason != "unsubscribed" {
		t.Fatal("a batch with nothing to renew keeps the 404 and still explains each ID")
	}
	now := time.Now().Add(121 * time.Second)
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return now }
	f.s.mu.Unlock()
	if _, missing, err = c.Renew(f.ctx, []string{kept.ID}); err == nil || len(missing) != 1 || missing[0].Reason != "lease_expired" {
		t.Fatal("an expired lease awaiting the sweep must not be renewed")
	}
}

func TestReconnectReplaysReleasesMissedWhileAway(t *testing.T) {
	f := setup(t, false)
	owner, _, _ := f.client(t)
	stay, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	lapse, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "234567890"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.source.starts.Load() == 2 })
	q, _, err := f.s.Open(owner.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	f.s.CloseStream(q)
	// The client is away with this cursor; only one lease is still renewed.
	since := f.m.Bus.Cursor()
	now := time.Now().Add(100 * time.Second)
	f.s.mu.Lock()
	f.s.Now = func() time.Time { return now }
	f.s.mu.Unlock()
	if _, _, err = f.s.Renew(owner.ID, []string{stay.ID}); err != nil {
		t.Fatal(err)
	}
	f.s.mu.Lock()
	now = now.Add(21 * time.Second)
	f.s.mu.Unlock()
	f.s.Sweep()
	released := func(packets []hub.Packet) []string {
		out := []string{}
		for _, p := range packets {
			if p.Event.Extra["code"] == "subscription_released" {
				out = append(out, p.Event.Extra["subscription_id"].(string)+"/"+p.Event.Extra["reason"].(string))
			}
		}
		return out
	}
	back, packets, err := f.s.Open(owner.ID, "", &since, f.m.BootID)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(back)
	if got := released(packets); !slices.Equal(got, []string{lapse.ID + "/lease_expired"}) || packets[0].Event.Extra["subscriptions"] != 1 {
		t.Fatalf("reconnect must report the lease that ended while away, got %v", got)
	}
	after := f.m.Bus.Cursor()
	cases := []struct {
		since *uint64
		boot  string
	}{{nil, ""}, {new(uint64), f.m.BootID}, {&after, f.m.BootID}, {&since, "old-boot"}}
	for _, c := range cases {
		q, packets, err := f.s.Open(owner.ID, "", c.since, c.boot)
		if err != nil {
			t.Fatal(err)
		}
		f.s.CloseStream(q)
		if got := released(packets); len(got) != 0 {
			t.Fatalf("fresh, newer or other-boot streams must not get old releases, got %v", got)
		}
	}
}

func TestSDKAutoRenewOutlivesEndedSubscriptionsAndStreamHearsMissedRelease(t *testing.T) {
	f := setup(t, false)
	_, c, _ := f.client(t)
	short := sdk.Options{LeaseSeconds: 3}
	keep, err := c.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: "123456789", Options: short})
	if err != nil {
		t.Fatal(err)
	}
	drop, err := c.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: "234567890", Options: short})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.source.starts.Load() == 2 })
	ended := make(chan sdk.Released, 4)
	renewDone := make(chan error, 1)
	go func() {
		renewDone <- c.AutoRenew(f.ctx, []sdk.Subscription{keep, drop}, func(r sdk.Released) { ended <- r })
	}()
	expect := func(id, reason string) {
		t.Helper()
		select {
		case r := <-ended:
			if r.SubscriptionID != id || r.Reason != reason {
				t.Fatalf("released = %+v", r)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ended subscription was not reported")
		}
	}
	if err = c.Unsubscribe(f.ctx, drop.ID); err != nil {
		t.Fatal(err)
	}
	expect(drop.ID, "unsubscribed")
	time.Sleep(3500 * time.Millisecond)
	if _, err = c.Subscription(f.ctx, keep.ID); err != nil {
		t.Fatal("auto renewal stopped after one subscription ended")
	}

	cursor := &sdk.Cursor{}
	stream := func(o sdk.StreamOptions) (context.CancelFunc, chan error) {
		ctx, stop := context.WithCancel(f.ctx)
		done := make(chan error, 1)
		o.Cursor = cursor
		o.OnGap = func(sdk.Event) {}
		go func() { done <- c.Stream(ctx, o) }()
		return stop, done
	}
	var connects atomic.Int64
	chats := make(chan sdk.Event, 4)
	stop, done := stream(sdk.StreamOptions{OnConnect: func() { connects.Add(1) }, OnEvent: func(e sdk.Event) error {
		if e.Type == "chat" {
			chats <- e
		}
		return nil
	}})
	waitFor(t, func() bool { return connects.Load() == 1 })
	f.source.emit(keep.LiveID, "fixture-1")
	select {
	case <-chats:
	case <-time.After(5 * time.Second):
		t.Fatal("event missing")
	}
	stop()
	<-done
	// keep ends while no stream is connected; the next stream learns of it.
	if err = c.Unsubscribe(f.ctx, keep.ID); err != nil {
		t.Fatal(err)
	}
	expect(keep.ID, "unsubscribed")
	select {
	case err = <-renewDone:
		if err != nil {
			t.Fatal("auto renewal should end quietly once nothing is left")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auto renewal kept running with nothing to renew")
	}
	heard := make(chan sdk.Released, 1)
	stop, done = stream(sdk.StreamOptions{OnEvent: func(sdk.Event) error { return nil }, OnReleased: func(r sdk.Released) { heard <- r }})
	defer func() { stop(); <-done }()
	select {
	case r := <-heard:
		if r.SubscriptionID != keep.ID || r.LiveID != keep.LiveID || r.Reason != "unsubscribed" {
			t.Fatalf("heard = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconnected stream did not hear the release it missed")
	}
}
