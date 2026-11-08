package engine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestCooldownReleaseKeepsRequestSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := NewBudget(10)
		r, _ := http.NewRequest("GET", "https://live.douyin.com/", nil)
		if err := b.BeforeRequest(r); err != nil {
			t.Fatal(err)
		}
		admitted := make(chan time.Time, 2)
		for range 2 {
			go func() {
				if err := b.BeforeRequest(r); err != nil {
					t.Error(err)
				}
				admitted <- time.Now()
			}()
		}
		synctest.Wait()
		b.Restrict()
		time.Sleep(2 * time.Minute)
		first, second := <-admitted, <-admitted
		if second.Sub(first) < 100*time.Millisecond {
			t.Fatal("requests burst together when cooldown reopened")
		}
	})
}

func TestCooldownBackoffCoalescingAndRecovery(t *testing.T) {
	b := NewBudget(100)
	now := time.Now()
	b.gate.now = func() time.Time { return now }
	alerts := 0
	b.OnCooldown(func(Cooldown) { alerts++ })
	for _, minutes := range []int{2, 4, 8, 16, 30, 30} {
		if !b.Restrict() || b.Cooldown().RemainingSeconds != minutes*60 {
			t.Fatal("wrong cooldown")
		}
		for range 20 {
			if b.Restrict() {
				t.Fatal("concurrent response escalated cooldown")
			}
		}
		now = now.Add(time.Duration(minutes) * time.Minute)
		if b.WaitReady(context.Background()) != nil {
			t.Fatal("cooldown did not reopen")
		}
	}
	if alerts != 6 {
		t.Fatal("duplicate alerts")
	}
	now = now.Add(30 * time.Minute)
	b.Restrict()
	if b.Cooldown().RemainingSeconds != 120 {
		t.Fatal("quiet period did not reset backoff")
	}
}

func TestCooldownBlocksRequestsAndCancels(t *testing.T) {
	b := NewBudget(100)
	b.Restrict()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://live.douyin.com/", nil)
	if err := b.BeforeRoomRequest("123", r); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if b.Count() != 0 || b.HomepageCount() != 0 {
		t.Fatal("blocked request counted")
	}
}

func TestHomepageAndRoomRequestMetrics(t *testing.T) {
	b := NewBudget(1000)
	for _, address := range []string{"https://live.douyin.com/", "https://live.douyin.com/123", "https://live.douyin.com/"} {
		r, _ := http.NewRequest("GET", address, nil)
		if err := b.BeforeRoomRequest("123", r); err != nil {
			t.Fatal(err)
		}
	}
	n, total := b.RoomCounts("123")
	if b.HomepageCount() != 2 || n != 3 || total != 3 {
		t.Fatal("wrong request metrics")
	}
	b.mu.Lock()
	b.trim(time.Now().Add(time.Minute))
	b.mu.Unlock()
	if b.Count() != 0 || b.HomepageCount() != 0 {
		t.Fatal("expired minute retained")
	}
	_, total = b.RoomCounts("123")
	if total != 3 {
		t.Fatal("cumulative requests lost")
	}
}

func TestRestrictedResponsePreservesBody(t *testing.T) {
	for _, test := range []struct {
		body       string
		code       int
		restricted bool
	}{
		{"<title>验证码中间页</title>", 200, true},
		{"normal live page with captcha script", 200, false},
		{strings.Repeat("x", 100000), 200, false},
		{"denied", 403, true},
		{"", 429, true},
	} {
		r := &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.body))}
		if RestrictedResponse(r) != test.restricted {
			t.Fatal("wrong response classification")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != test.body {
			t.Fatal("SDK response changed")
		}
	}
}

// Regression: an empty web/enter body is a normal engine fallback, not a restriction.
func TestEmptyWebEnterIsNotRestricted(t *testing.T) {
	for _, body := range []string{"", "{}", "  "} {
		req, _ := http.NewRequest("GET", "https://live.douyin.com/webcast/room/web/enter/?aid=6383", nil)
		r := &http.Response{StatusCode: 200, Request: req, Body: io.NopCloser(strings.NewReader(body))}
		if why := RestrictedReason(r); why != "" {
			t.Fatalf("empty web/enter must not trip cooldown, got %q", why)
		}
	}
	req, _ := http.NewRequest("GET", "https://live.douyin.com/webcast/room/web/enter/?aid=6383", nil)
	r := &http.Response{StatusCode: 403, Request: req, Body: io.NopCloser(strings.NewReader(""))}
	if why := RestrictedReason(r); !strings.HasPrefix(why, "status_") || strings.Contains(why, "aid=") {
		t.Fatalf("403 must be restricted without query string, got %q", why)
	}
}
