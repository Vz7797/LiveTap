package engine

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestBudgetGatesActualRequestsAndCancellation(t *testing.T) {
	b := NewBudget(10)
	r, _ := http.NewRequest("GET", "https://example.invalid", nil)
	start := time.Now()
	for range 3 {
		if err := b.BeforeRequest(r); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(start) < 180*time.Millisecond || b.Count() != 3 {
		t.Fatal("requests bypassed limiter")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.BeforeRequest(r.WithContext(ctx)) == nil || b.Count() != 3 {
		t.Fatal("cancelled request counted or allowed")
	}
}
