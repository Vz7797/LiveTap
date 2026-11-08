package room

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestCooldownReleaseKeepsStartupSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &Manager{Budget: engine.NewBudget(100), starts: engine.NewBudget(2)}
		if err := m.waitStart(context.Background()); err != nil {
			t.Fatal(err)
		}
		admitted := make(chan time.Time, 2)
		for range 2 {
			go func() {
				if err := m.waitStart(context.Background()); err != nil {
					t.Error(err)
				}
				admitted <- time.Now()
			}()
		}
		synctest.Wait()
		m.Budget.Restrict()
		time.Sleep(2 * time.Minute)
		first, second := <-admitted, <-admitted
		if second.Sub(first) < 500*time.Millisecond {
			t.Fatal("startup requests burst together when cooldown reopened")
		}
	})
}

func TestNodeCooldownPreservesLiveConnectionAndWritesOneAlert(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mu sync.Mutex
	starts := map[string][]string{}
	b := engine.NewBudget(100)
	m, err := New(context.Background(), s, b, func() engine.Engine { return &recordingEngine{&mu, starts} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, _, err = m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		v, _, _ := m.Views()
		if len(v) > 0 && v[0]["status"] == "collecting" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.Restrict()
	b.Restrict()
	if _, _, err = m.Add(context.Background(), "223456789", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	first, second := len(starts["123456789"]), len(starts["223456789"])
	mu.Unlock()
	if first != 1 || second != 0 {
		t.Fatal("cooldown restarted live room or allowed new connection")
	}
	if m.confirm(context.Background(), nil, engine.RoomSpec{}) != "cooling" {
		t.Fatal("silence witness ran during cooldown")
	}
	views, _, _ := m.Views()
	if views[0]["status"] != "collecting" {
		t.Fatal("live connection was interrupted")
	}
	var alerts int
	if err = s.DB.QueryRow("SELECT count(*) FROM incidents WHERE kind='node_cooldown'").Scan(&alerts); err != nil || alerts != 1 {
		t.Fatal("cooldown alert missing or duplicated")
	}
}

func TestReconnectDiagnosticsDoNotCountRepeatedStatesOrManualStops(t *testing.T) {
	task := &task{}
	task.setState("collecting", "ready")
	task.setState("reconnecting", "lost")
	task.setState("reconnecting", "retry")
	task.setState("risk", "failed")
	task.setState("collecting", "recovered")
	task.setState("stopped", "manual")
	task.setState("collecting", "manual resume")
	if task.disconnects != 1 || task.reconnectRecoveryCount != 1 || len(task.reconnectDurations) != 1 {
		t.Fatal("reconnect episodes counted incorrectly")
	}
}
