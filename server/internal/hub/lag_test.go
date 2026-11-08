package hub

import (
	"testing"
	"time"
)

func TestLagQuantilesOverWindow(t *testing.T) {
	var l Lag
	now := time.Now()
	l.Observe(now.Add(-2*time.Minute), 9*time.Second, true) // outside the window
	for i := 1; i <= 100; i++ {
		l.Observe(now, time.Duration(i)*time.Millisecond, i%4 != 0)
	}
	got := l.Stats(now, time.Minute)
	if got.Samples != 100 || got.P50 != 51 || got.P95 != 96 || !got.Precise {
		t.Fatalf("unexpected stats %+v", got)
	}
	var empty Lag
	if empty.Stats(now, time.Minute).Samples != 0 {
		t.Fatal("empty lag reported samples")
	}
}
