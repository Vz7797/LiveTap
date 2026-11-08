package room

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

// scriptEngine answers probes from a script (the last answer repeats) and ends
// every collection immediately, as when a stream stops.
type scriptEngine struct {
	mu     *sync.Mutex
	script []string
	probes *int
}

func (e *scriptEngine) Name() string { return "script" }
func (e *scriptEngine) Close()       {}
func (e *scriptEngine) Probe(context.Context, engine.RoomSpec) (engine.LiveStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	code := e.script[min(*e.probes, len(e.script)-1)]
	*e.probes++
	// "err:<kind>" answers with a failed probe, as the douyinlive adapter reports it.
	if err, ok := map[string]error{"err:uncertain": engine.ErrAccessUncertain, "err:restricted": engine.ErrAccessRestricted, "err:timeout": engine.ErrProbeTimeout, "err:other": errors.New("直播间信息接口返回异常 status_code=4003034")}[code]; ok {
		return engine.LiveStatus{Code: "unknown"}, err
	}
	return engine.LiveStatus{Code: code, Name: "anchor", Avatar: "https://example.invalid/a.jpeg"}, nil
}
func (e *scriptEngine) Run(_ context.Context, _ engine.RoomSpec, sink engine.Sink) error {
	sink.State(engine.EngineState{State: "collecting"})
	return nil
}

func dormantManager(t *testing.T, script ...string) (*Manager, *store.Store, func() int) {
	t.Helper()
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := model.DefaultSettings()
	cfg.StartsPerSecond = 20
	cfg.RequestsPerSecond = 100
	if err = s.SaveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	probes := 0
	m, err := New(context.Background(), s, engine.NewBudget(100), func() engine.Engine { return &scriptEngine{&mu, script, &probes} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, s, func() int { mu.Lock(); defer mu.Unlock(); return probes }
}

func shorten(t *testing.T, grace, never time.Duration) {
	oldGrace, oldNever := EndedGrace, NeverLiveLimit
	EndedGrace, NeverLiveLimit = grace, never
	t.Cleanup(func() { EndedGrace, NeverLiveLimit = oldGrace, oldNever })
}

func waitState(t *testing.T, m *Manager, id, state string, nudge bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := m.Status(id); ok && st.State == state {
			return
		}
		// Only skip the poll delay; waking a probe or collection would cancel it.
		if st, _ := m.Status(id); nudge && st.State == "waiting" {
			task := m.get(id)
			task.mu.Lock()
			task.signal()
			task.mu.Unlock()
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := m.Status(id)
	t.Fatalf("state %q not reached, still %q (%s)", state, st.State, st.Reason)
}

func TestEndedStreamStopsProbingUntilResumed(t *testing.T) {
	shorten(t, 0, time.Hour)
	// live → the stream ends → still offline after the grace period
	m, s, probes := dormantManager(t, "online", "offline")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "ended", true)
	records, _ := s.Rooms()
	r := records[0]
	if r.EndedReason != "live_ended" || r.EndedAt == 0 || r.LastLiveAt == 0 || r.Avatar == "" || !r.Enabled {
		t.Fatalf("dormant record not persisted: %+v", r)
	}
	if used, _ := m.Capacity(); used != 0 || m.used() != 0 {
		t.Fatal("a dormant task still counts against capacity")
	}
	before := probes()
	time.Sleep(200 * time.Millisecond)
	if probes() != before {
		t.Fatal("a dormant task kept probing")
	}
	if err := m.Action("123456789", "resume"); err != nil {
		t.Fatal(err)
	}
	records, _ = s.Rooms()
	if records[0].Dormant() {
		t.Fatal("resume did not clear the dormant mark")
	}
	deadline := time.Now().Add(5 * time.Second)
	for probes() == before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if probes() == before {
		t.Fatal("resumed task did not probe again")
	}
}

func TestNeverLiveLimitAppliesToManualTasksOnly(t *testing.T) {
	shorten(t, time.Hour, 0)
	m, _, _ := dormantManager(t, "offline")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.AddConfigured(context.Background(), "223456789", nil, true, "subscription", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "ended", true)
	if st, _ := m.Status("123456789"); st.EndedReason != "never_live" {
		t.Fatal("wrong reason", st.EndedReason)
	}
	// subscription tasks follow the subscription's idle_release_seconds instead
	waitState(t, m, "223456789", "waiting", false)
	time.Sleep(100 * time.Millisecond)
	if st, _ := m.Status("223456789"); st.Dormant {
		t.Fatal("subscription task stopped by the manual never-live limit")
	}
}

func TestSubscriptionRestartsDormantButNotPausedTasks(t *testing.T) {
	shorten(t, time.Hour, 0)
	m, _, _ := dormantManager(t, "offline")
	for _, id := range []string{"123456789", "223456789"} {
		if _, _, err := m.Add(context.Background(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitState(t, m, "123456789", "ended", true)
	if err := m.Action("223456789", "pause"); err != nil {
		t.Fatal(err)
	}
	NeverLiveLimit = time.Hour
	if err := m.EnsureSubscription(context.Background(), "123456789"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureSubscription(context.Background(), "223456789"); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status("123456789"); st.Dormant {
		t.Fatal("a new subscription did not restart the dormant task")
	}
	if st, _ := m.Status("223456789"); st.Enabled {
		t.Fatal("a subscription resumed a task paused by the administrator")
	}
}

func TestPruneDormantRemovesOnlyOldManualTasks(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(-DormantRetention - time.Hour).UnixMilli()
	for _, r := range []model.RoomRecord{
		{LiveID: "123456789", Source: "manual", Enabled: true, EndedAt: old, EndedReason: "live_ended", LastLiveAt: old},
		{LiveID: "223456789", Source: "manual", Enabled: true, EndedAt: old, EndedReason: "live_ended", LastLiveAt: time.Now().UnixMilli()},
		{LiveID: "323456789", Source: "manual", Enabled: false, LastLiveAt: old},
	} {
		r.Name, r.Tags, r.CreatedAt = "x", []string{}, time.UnixMilli(old)
		if err = s.SaveRoom(r); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(context.Background(), s, engine.NewBudget(100), func() engine.Engine { return &scriptEngine{&sync.Mutex{}, []string{"offline"}, new(int)} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	removed := m.PruneDormant(time.Now())
	if len(removed) != 1 || removed[0] != "123456789" {
		t.Fatal("unexpected prune", removed)
	}
	if _, ok := m.Status("223456789"); !ok {
		t.Fatal("recently live task removed")
	}
	if _, ok := m.Status("323456789"); !ok {
		t.Fatal("paused task removed")
	}
}

func TestNewAndResumedTasksReportQueuedNotStopped(t *testing.T) {
	m, _, _ := dormantManager(t, "offline")
	if _, _, err := m.AddConfigured(context.Background(), "123456789", nil, true, "subscription", nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status("123456789"); st.State == "stopped" {
		t.Fatal("a new subscription task reported stopped while queued")
	}
	if err := m.Action("123456789", "pause"); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "stopped", false)
	if err := m.Action("123456789", "resume"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := m.Status("123456789"); st.State != "stopped" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a resumed task kept reporting stopped")
}

func TestRestartKeepsEndedGraceForRecentlyLiveTasks(t *testing.T) {
	shorten(t, 0, time.Hour)
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := model.DefaultSettings()
	cfg.StartsPerSecond = 20
	_ = s.SaveSettings(cfg)
	// A subscription task that was live a moment before the node restarted; it
	// comes back paused until the northbound service re-ensures it.
	r := model.RoomRecord{LiveID: "123456789", Name: "x", Source: "subscription", Enabled: true, Tags: []string{}, CreatedAt: time.Now(), LastLiveAt: time.Now().Add(-time.Minute).UnixMilli()}
	if err = s.SaveRoom(r); err != nil {
		t.Fatal(err)
	}
	m, err := New(context.Background(), s, engine.NewBudget(100), func() engine.Engine { return &scriptEngine{&sync.Mutex{}, []string{"offline"}, new(int)} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.EnsureSubscription(context.Background(), "123456789"); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "ended", true)
	if st, _ := m.Status("123456789"); st.EndedReason != "live_ended" {
		t.Fatal("wrong reason after restart", st.EndedReason)
	}
}

func TestEndedPollDelayBacksOff(t *testing.T) {
	if EndedPollDelay(0) != 30*time.Second || EndedPollDelay(119*time.Second) != 30*time.Second || EndedPollDelay(2*time.Minute) != 2*time.Minute {
		t.Fatal("unexpected ended poll schedule")
	}
	// About 8 checks across the 10-minute grace, instead of ~30 at the waiting pace.
	n, at := 0, time.Duration(0)
	for at < 10*time.Minute {
		at += EndedPollDelay(at)
		n++
	}
	if n > 9 {
		t.Fatal("too many checks during the grace period", n)
	}
}

func TestRepeatedOfflineChecksDoNotFloodTimeline(t *testing.T) {
	shorten(t, time.Hour, time.Hour)
	m, _, probes := dormantManager(t, "online", "offline")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "waiting", false)
	task := m.get("123456789")
	task.mu.Lock()
	before := len(task.events)
	task.mu.Unlock()
	start := probes()
	deadline := time.Now().Add(5 * time.Second)
	for probes() < start+4 && time.Now().Before(deadline) {
		if st, _ := m.Status("123456789"); st.State == "waiting" {
			task.mu.Lock()
			task.signal()
			task.mu.Unlock()
		}
		time.Sleep(20 * time.Millisecond)
	}
	if probes() < start+4 {
		t.Fatal("checks did not run")
	}
	task.mu.Lock()
	after := len(task.events)
	task.mu.Unlock()
	if after != before {
		t.Fatalf("timeline grew by %d entries for checks that found nothing new", after-before)
	}
}

func TestRegionBlockedRoomStopsProbing(t *testing.T) {
	m, s, probes := dormantManager(t, "region_blocked")
	if _, _, err := m.AddConfigured(context.Background(), "416229594796", nil, true, "subscription", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "416229594796", "notfound", false)
	st, _ := m.Status("416229594796")
	if st.Reason != regionBlockedText {
		t.Fatalf("reason = %q", st.Reason)
	}
	records, _ := s.Rooms()
	if records[0].Enabled {
		t.Fatal("a region-blocked room must stop probing")
	}
	before := probes()
	time.Sleep(200 * time.Millisecond)
	if probes() != before {
		t.Fatal("a region-blocked room kept probing")
	}
}
