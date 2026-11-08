package observe

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestSnapshotExposesCooldownHomepageCountAndSharingSetting(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ShareGuestIdentity = true
	if err = s.SaveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	b := engine.NewBudget(100)
	m, err := room.New(context.Background(), s, b, nil, "offline")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, _ := http.NewRequest("GET", "https://live.douyin.com/", nil)
	if err = b.BeforeRoomRequest("synthetic", r); err != nil {
		t.Fatal(err)
	}
	b.Restrict()
	snapshot := New(context.Background(), m)
	defer snapshot.Close()
	v := snapshot.Get()
	node := v["node"].(map[string]any)
	cooldown := node["cooldown"].(engine.Cooldown)
	if !cooldown.Active || cooldown.RemainingSeconds < 119 || node["homepageRequestsPerMinute"] != 1 || node["shareGuestIdentity"] != true {
		t.Fatal("node diagnostics missing")
	}
	incidents := v["incidents"].([]map[string]any)
	if len(incidents) != 1 || incidents[0]["id"] != "node-cooldown" {
		t.Fatal("node alert missing")
	}
}

func TestTodayStartsAtBeijingMidnight(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = zone
	defer func() { time.Local = previous }()
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	for _, fixture := range []struct {
		at    time.Time
		count int64
	}{{day.Add(-time.Minute), 11}, {day, 7}} {
		if err := s.Minute("timezone-fixture", fixture.at, model.Counts{"chat": fixture.count}, 0, 0, "stopped"); err != nil {
			t.Fatal(err)
		}
	}
	m, err := room.New(context.Background(), s, engine.NewBudget(100), nil, "offline")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	snapshot := New(context.Background(), m)
	defer snapshot.Close()
	v := snapshot.Get()
	if v["totals"].(model.Counts)["chat"] != 7 {
		t.Fatal("today included events before Beijing midnight")
	}
	rhythm := v["rhythm"].(map[string]any)
	if rhythm["today"].([]int64)[0] != 7 || rhythm["yesterday"].([]int64)[47] != 11 {
		t.Fatal("rhythm did not split at Beijing midnight")
	}
}

func TestRateIgnoresRemovedRooms(t *testing.T) {
	previous := map[string]model.Counts{"a": {"chat": 100, "member": 50}, "b": {"chat": 900_000, "member": 400_000}}
	// room b was released after its stream ended; room c is new
	current := map[string]model.Counts{"a": {"chat": 110, "member": 55}, "c": {"chat": 5000}}
	rate := RateBetween(previous, current, 2.5)
	if rate["chat"] != 240 || rate["member"] != 120 {
		t.Fatalf("rate dropped or spiked when rooms changed: %+v", rate)
	}
}

func TestHistoryRefreshReadsOnlyRecentTail(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := room.New(context.Background(), s, engine.NewBudget(100), nil, "offline")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	now := time.Now().Truncate(time.Minute)
	since := now.Add(-3 * time.Hour)
	save := func(at time.Time, n int64) {
		if err := s.Minute("tail-fixture", at, model.Counts{"chat": n}, 0, 0, "stopped"); err != nil {
			t.Fatal(err)
		}
	}
	chat := func(h []store.Minute, at time.Time) int64 {
		for _, x := range h {
			if x.At == at.UnixMilli() {
				return x.Counts["chat"]
			}
		}
		return -1
	}
	old, recent := now.Add(-2*time.Hour), now.Add(-2*time.Minute)
	save(old, 1)
	save(recent, 2)
	snap := &Snapshotter{m: m}
	snap.refreshHistory(now, since)
	if chat(snap.history, old) != 1 || chat(snap.history, recent) != 2 {
		t.Fatalf("first refresh must load the whole window: %+v", snap.history)
	}

	// An old minute changed behind the cache is left alone until the hourly reload;
	// the recent tail and new minutes are picked up on every sample.
	save(old, 10)
	save(recent, 20)
	save(now, 30)
	snap.refreshHistory(now.Add(10*time.Second), since)
	if chat(snap.history, old) != 1 || chat(snap.history, recent) != 20 || chat(snap.history, now) != 30 || len(snap.history) != 3 {
		t.Fatalf("tail refresh merged wrongly: %+v", snap.history)
	}
	snap.refreshHistory(now.Add(historyReload), since)
	if chat(snap.history, old) != 10 {
		t.Fatal("hourly reload did not reread the whole window")
	}

	// A new day moves the window start, which forces a full reload.
	snap.refreshHistory(now.Add(historyReload+time.Minute), old.Add(time.Minute))
	if chat(snap.history, old) != -1 || chat(snap.history, now) != 30 {
		t.Fatalf("window change did not drop minutes before the new start: %+v", snap.history)
	}
}
