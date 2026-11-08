package engine

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Budget struct {
	lim      *rate.Limiter
	gate     *circuit
	mu       sync.Mutex
	requests []requestStamp
	totals   map[string]uint64
}
type requestStamp struct {
	at   time.Time
	room string
	home bool
}

func NewBudget(perSecond float64) *Budget {
	return &Budget{lim: rate.NewLimiter(rate.Limit(perSecond), 1), gate: newCircuit(), totals: map[string]uint64{}}
}
func (b *Budget) Set(v float64)                       { b.lim.SetLimit(rate.Limit(v)) }
func (b *Budget) Wait(ctx context.Context) error      { return b.lim.Wait(ctx) }
func (b *Budget) WaitReady(ctx context.Context) error { return b.gate.wait(ctx) }
func (b *Budget) Cooldown() Cooldown                  { return b.gate.snapshot() }
func (b *Budget) Restrict() bool                      { return b.gate.trip() }
func (b *Budget) RestrictBecause(reason string) bool  { return b.gate.tripBecause(reason) }
func (b *Budget) OnCooldown(fn func(Cooldown)) {
	b.gate.mu.Lock()
	b.gate.onTrip = fn
	b.gate.mu.Unlock()
}
func (b *Budget) BeforeRequest(r *http.Request) error { return b.BeforeRoomRequest("", r) }
func (b *Budget) BeforeRoomRequest(room string, r *http.Request) error {
	for {
		if e := b.WaitReady(r.Context()); e != nil {
			return e
		}
		if e := b.Wait(r.Context()); e != nil {
			return e
		}
		// A token obtained during cooldown must not accumulate for reopening.
		if !b.Cooldown().Active {
			break
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trim(time.Now())
	home := r.URL.Hostname() == "live.douyin.com" && (r.URL.Path == "" || r.URL.Path == "/")
	b.requests = append(b.requests, requestStamp{time.Now(), room, home})
	b.totals[room]++
	return nil
}
func (b *Budget) trim(now time.Time) {
	i := 0
	for i < len(b.requests) && now.Sub(b.requests[i].at) >= time.Minute {
		i++
	}
	b.requests = append(b.requests[:0], b.requests[i:]...)
}
func (b *Budget) HomepageCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trim(time.Now())
	n := 0
	for _, r := range b.requests {
		if r.home {
			n++
		}
	}
	return n
}
func (b *Budget) RoomCounts(room string) (int, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trim(time.Now())
	n := 0
	for _, r := range b.requests {
		if r.room == room {
			n++
		}
	}
	return n, b.totals[room]
}

// Forget drops a removed room's lifetime request total, so the map does not keep
// an entry for every room the node has ever collected.
func (b *Budget) Forget(room string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.totals, room)
}
func (b *Budget) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trim(time.Now())
	return len(b.requests)
}
