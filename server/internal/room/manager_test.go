package room

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

type evidenceEngine struct {
	status  string
	message bool
}

type recordingEngine struct {
	mu     *sync.Mutex
	starts map[string][]string
}

func (e *recordingEngine) Name() string { return "test" }
func (e *recordingEngine) Close()       {}
func (e *recordingEngine) Probe(context.Context, engine.RoomSpec) (engine.LiveStatus, error) {
	return engine.LiveStatus{Code: "online"}, nil
}
func (e *recordingEngine) Run(ctx context.Context, spec engine.RoomSpec, sink engine.Sink) error {
	e.mu.Lock()
	e.starts[spec.LiveID] = append(e.starts[spec.LiveID], spec.ProtocolMode)
	e.mu.Unlock()
	sink.State(engine.EngineState{State: "collecting"})
	<-ctx.Done()
	return ctx.Err()
}
func TestRollingAppliesPendingDefaultsAndKeepsOverrides(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := model.DefaultSettings()
	cfg.StartsPerSecond = 20
	cfg.RollingSeconds = 1
	if err = s.SaveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	starts := map[string][]string{}
	m, err := New(context.Background(), s, engine.NewBudget(100), func() engine.Engine { return &recordingEngine{&mu, starts} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, id := range []string{"123456789", "223456789", "323456789"} {
		if _, _, err = m.Add(context.Background(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(fn func() bool) {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			if fn() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("state transition timed out")
	}
	waitFor(func() bool { mu.Lock(); defer mu.Unlock(); return len(starts) == 3 })
	if err = m.Update("223456789", nil, nil, model.Overrides{"protocol_mode": json.RawMessage(`"pc"`)}, false); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { mu.Lock(); defer mu.Unlock(); return len(starts["223456789"]) == 2 })
	cfg.Defaults.ProtocolMode = "pc"
	if err = m.ChangeSettings(cfg, "new"); err != nil {
		t.Fatal(err)
	}
	options, _ := m.RoomOptions("123456789")
	if options["protocol_mode"] != "web" {
		t.Fatal("new-only restarted existing connection")
	}
	if err = m.ChangeSettings(cfg, "rolling"); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { return m.Rollout("").Done == 1 })
	m.Rollout("pause")
	time.Sleep(1100 * time.Millisecond)
	if m.Rollout("").Done != 1 {
		t.Fatal("paused rollout continued")
	}
	m.Rollout("resume")
	waitFor(func() bool { return m.Rollout("").Done == 2 })
	// Done counts submitted rebuilds; the reconnect itself follows asynchronously.
	restarted := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(starts["123456789"]) == 2 && len(starts["323456789"]) == 2
	}
	waitFor(restarted)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(starts["123456789"]) != 2 || len(starts["323456789"]) != 2 || len(starts["223456789"]) != 2 {
		t.Fatal("rollout missed pending defaults or restarted overridden room")
	}
}

func (e *evidenceEngine) Name() string { return "test" }
func (e *evidenceEngine) Close()       {}
func (e *evidenceEngine) Probe(context.Context, engine.RoomSpec) (engine.LiveStatus, error) {
	return engine.LiveStatus{Code: e.status}, nil
}
func (e *evidenceEngine) Run(ctx context.Context, _ engine.RoomSpec, s engine.Sink) error {
	if !e.message {
		return errors.New("cannot establish witness")
	}
	s.Event(engine.RawEvent{Method: "WebcastRoomUserSeqMessage"})
	<-ctx.Done()
	return ctx.Err()
}
func TestSilenceEvidence(t *testing.T) {
	for _, tc := range []struct {
		status  string
		message bool
		want    string
	}{{"offline", false, "offline"}, {"unknown", false, "unknown"}, {"online", true, "rebuild"}, {"online", false, "unknown"}} {
		t.Run(tc.status+tc.want, func(t *testing.T) {
			m := &Manager{Budget: engine.NewBudget(100), starts: engine.NewBudget(100), factory: func() engine.Engine { return &evidenceEngine{message: tc.message} }}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if got := m.confirm(ctx, &evidenceEngine{status: tc.status}, engine.RoomSpec{}); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
func TestRestartPreservesMinuteAndOverrides(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().Truncate(time.Minute)
	r := model.RoomRecord{LiveID: "123456789", Name: "test", CreatedAt: time.Now(), Tags: []string{}, Overrides: model.Overrides{"protocol_mode": json.RawMessage(`"pc"`)}}
	if err = s.SaveRoom(r); err != nil {
		t.Fatal(err)
	}
	c := model.Zero()
	c["chat"] = 7
	if err = s.Minute(r.LiveID, at, c, 0, 0, "stopped"); err != nil {
		t.Fatal(err)
	}
	b := engine.NewBudget(100)
	m, err := New(context.Background(), s, b, func() engine.Engine { return &evidenceEngine{status: "offline"} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	task := m.get(r.LiveID)
	if task.minutes[at.UnixMilli()]["chat"] != 7 {
		t.Fatal("restart reset current minute")
	}
	if err = m.Update(r.LiveID, nil, nil, model.Overrides{"silence_seconds": json.RawMessage(`120`)}, false); err != nil {
		t.Fatal(err)
	}
	opts, _ := m.RoomOptions(r.LiveID)
	if opts["protocol_mode"] != "pc" {
		t.Fatal("partial edit erased previous override")
	}
	if err = m.Update(r.LiveID, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	opts, _ = m.RoomOptions(r.LiveID)
	if opts["protocol_mode"] != "web" {
		t.Fatal("restore inheritance failed")
	}
	m.Close()
	saved, err := s.MinuteTotals(at)
	if err != nil || len(saved) != 1 || saved[0].Counts["chat"] != 7 {
		t.Fatal("shutdown overwrote persisted statistics")
	}
}
func TestPageIdentityExcludesRecommendations(t *testing.T) {
	body := `{"recommend":[{"web_rid":"999999999"}],"roomStore":{"roomInfo":{"web_rid":"123456789","room":{"title":"hello"}}}}`
	if canonicalFromPage(body) != "123456789" {
		t.Fatal("wrong room identity")
	}
	if canonicalFromPage(`{"recommend":[{"web_rid":"999999999"}]}`) != "" {
		t.Fatal("recommendation used as canonical identity")
	}
	if canonicalFromPage(`{\"roomStore\":{\"roomInfo\":{\"web_rid\":\"123456789\"}}}`) != "123456789" {
		t.Fatal("escaped state not resolved")
	}
}
