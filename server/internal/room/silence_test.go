package room

import (
	"testing"
	"time"
)

func TestAdaptiveSilenceAndPolling(t *testing.T) {
	now := time.Now()
	var s Silence
	for i := 0; i < 10; i++ {
		s.Observe(now.Add(time.Duration(i) * 15 * time.Second))
	}
	if got := s.Threshold(now.Add(150 * time.Second)); got != 90 {
		t.Fatalf("quiet room threshold %d", got)
	}
	s.Observe(now.Add(240 * time.Second))
	if got := s.Threshold(now.Add(240 * time.Second)); got != 300 {
		t.Fatalf("cap %d", got)
	}
	if got := s.Threshold(now.Add(time.Hour)); got != 90 {
		t.Fatalf("stale interval retained %d", got)
	}
	for _, v := range []struct{ idle, delay time.Duration }{{0, 15 * time.Second}, {6 * time.Minute, 30 * time.Second}, {16 * time.Minute, 60 * time.Second}, {31 * time.Minute, 120 * time.Second}} {
		if got := PollDelay(v.idle, 15); got != v.delay {
			t.Fatalf("poll %v", got)
		}
	}
}
func TestIdentityValidation(t *testing.T) {
	for input, want := range map[string]string{" https://live.douyin.com/123456789?x=1 ": "123456789", "small_shop": "small_shop"} {
		got, e := ParseID(input)
		if e != nil || got != want {
			t.Fatalf("%s: %s %v", input, got, e)
		}
	}
	for _, bad := range []string{"https://evil.invalid/123", "https://live.douyin.com@evil.invalid/123", "https://live.douyin.com/../../x", "", "abc!"} {
		if _, e := ParseID(bad); e == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
