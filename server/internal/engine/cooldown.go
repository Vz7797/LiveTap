package engine

import (
	"context"
	"math"
	"sync"
	"time"
)

type Cooldown struct {
	Active           bool  `json:"active"`
	Until            int64 `json:"until"`
	RemainingSeconds int   `json:"remainingSeconds"`
	Strikes          int   `json:"strikes"`
	// Reason names the rule that tripped the latest cooldown (no URLs' queries or secrets).
	Reason string `json:"reason"`
}
type circuit struct {
	mu      sync.Mutex
	now     func() time.Time
	until   time.Time
	strikes int
	reason  string
	changed chan struct{}
	onTrip  func(Cooldown)
}

func newCircuit() *circuit { return &circuit{now: time.Now, changed: make(chan struct{})} }
func (c *circuit) snapshotLocked() Cooldown {
	left := max(0, int(math.Ceil(c.until.Sub(c.now()).Seconds())))
	until := int64(0)
	if !c.until.IsZero() {
		until = c.until.UnixMilli()
	}
	return Cooldown{Active: left > 0, Until: until, RemainingSeconds: left, Strikes: c.strikes, Reason: c.reason}
}
func (c *circuit) snapshot() Cooldown {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}
func (c *circuit) trip() bool { return c.tripBecause("unspecified") }
func (c *circuit) tripBecause(reason string) bool {
	c.mu.Lock()
	now := c.now()
	if now.Before(c.until) {
		c.mu.Unlock()
		return false
	}
	// Coalesce concurrent restrictions. Reset after 30 further quiet minutes.
	if now.Sub(c.until) >= 30*time.Minute {
		c.strikes = 0
	}
	c.strikes = min(c.strikes+1, 5)
	duration := min(2*time.Minute*time.Duration(1<<(c.strikes-1)), 30*time.Minute)
	c.until = now.Add(duration)
	c.reason = reason
	close(c.changed)
	c.changed = make(chan struct{})
	state, notify := c.snapshotLocked(), c.onTrip
	c.mu.Unlock()
	if notify != nil {
		notify(state)
	}
	return true
}
func (c *circuit) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		left, changed := c.until.Sub(c.now()), c.changed
		c.mu.Unlock()
		if left <= 0 {
			return nil
		}
		timer := time.NewTimer(left)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}
