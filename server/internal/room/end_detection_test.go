package room

import (
	"context"
	"testing"
	"time"
)

// nudgeUntil skips poll delays (only while waiting) until cond holds.
func nudgeUntil(t *testing.T, m *Manager, id string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			st, _ := m.Status(id)
			t.Fatalf("condition not reached, state %q (%s)", st.State, st.Reason)
		}
		if st, _ := m.Status(id); st.State == "waiting" {
			task := m.get(id)
			task.mu.Lock()
			task.signal()
			task.mu.Unlock()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Regression: after a stream ended, probes that kept failing without a clear
// answer (status_code=4003099 for hours) left the task "unconfirmed" forever.
// Once the end grace has passed such a task ends like an offline one.
func TestUnconfirmableProbesAfterLiveEndTheTask(t *testing.T) {
	shorten(t, 0, time.Hour)
	m, s, _ := dormantManager(t, "online", "err:other")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "ended", true)
	records, _ := s.Rooms()
	if records[0].EndedReason != "live_ended" {
		t.Fatalf("ended reason %q", records[0].EndedReason)
	}
}

// An ambiguous upstream answer counts as offline: the task waits for a restart
// instead of showing as unconfirmed, and the end grace still applies.
func TestUncertainProbeCountsAsOffline(t *testing.T) {
	shorten(t, time.Hour, time.Hour)
	m, _, probes := dormantManager(t, "online", "err:uncertain")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	nudgeUntil(t, m, "123456789", func() bool { return probes() >= 3 })
	if st, _ := m.Status("123456789"); st.State != "waiting" || st.Reason != endingText() {
		t.Fatalf("uncertain probe after a live stream: %q (%s)", st.State, st.Reason)
	}
}

func TestUncertainProbeEndsTaskAfterGrace(t *testing.T) {
	shorten(t, 0, time.Hour)
	m, _, _ := dormantManager(t, "online", "err:uncertain")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "123456789", "ended", true)
}

// Restrictions and timeouts say nothing about the stream, so they never end a task.
func TestRestrictedOrTimedOutProbeNeverEndsTask(t *testing.T) {
	for _, kind := range []string{"err:restricted", "err:timeout"} {
		t.Run(kind, func(t *testing.T) {
			shorten(t, 0, time.Hour)
			m, s, probes := dormantManager(t, "online", kind)
			if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
				t.Fatal(err)
			}
			nudgeUntil(t, m, "123456789", func() bool { return probes() >= 2 })
			waitState(t, m, "123456789", "risk", false)
			records, _ := s.Rooms()
			if records[0].Dormant() {
				t.Fatalf("%s ended the task: %+v", kind, records[0])
			}
		})
	}
}

func TestCollectRetryDelay(t *testing.T) {
	for n, want := range map[int]time.Duration{1: 15 * time.Second, 4: 15 * time.Second, 5: time.Minute, 50: time.Minute} {
		if got := CollectRetryDelay(n); got != want {
			t.Fatalf("CollectRetryDelay(%d) = %s, want %s", n, got, want)
		}
	}
	if ProbeTimeoutRetry != 15*time.Second || CollectStableAfter != 2*time.Minute {
		t.Fatal("retry pacing changed without updating the docs")
	}
}
