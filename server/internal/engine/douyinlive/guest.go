package douyinlive

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
)

type guestEntry struct {
	value   string
	expires time.Time
}

// Memory only. Different proxy exits and protocol profiles remain isolated.
type guestIdentity struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]guestEntry
	loading map[string]chan struct{}
	epoch   uint64
}

func newGuestIdentity() *guestIdentity {
	return &guestIdentity{now: time.Now, entries: map[string]guestEntry{}, loading: map[string]chan struct{}{}}
}
func (g *guestIdentity) invalidate() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.epoch++
	g.entries = map[string]guestEntry{}
}
func (g *guestIdentity) get(ctx context.Context, key string, fetch func(context.Context) (string, time.Time, error)) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		g.mu.Lock()
		if entry, ok := g.entries[key]; ok && g.now().Before(entry.expires) {
			g.mu.Unlock()
			return entry.value, nil
		}
		if done := g.loading[key]; done != nil {
			g.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-done:
				continue
			}
		}
		done := make(chan struct{})
		g.loading[key] = done
		epoch := g.epoch
		g.mu.Unlock()
		value, expiry, err := fetch(ctx)
		g.mu.Lock()
		if g.epoch != epoch {
			err = engine.ErrAccessRestricted
		}
		if err == nil && value == "" {
			err = errors.New("游客身份为空")
		}
		if err == nil {
			now := g.now()
			if expiry.IsZero() {
				expiry = now.Add(30 * time.Minute)
			}
			if expiry.After(now.Add(6 * time.Hour)) {
				expiry = now.Add(6 * time.Hour)
			}
			if !expiry.After(now) {
				err = errors.New("游客身份已过期")
			} else {
				g.entries[key] = guestEntry{value, expiry}
			}
		}
		delete(g.loading, key)
		close(done)
		g.mu.Unlock()
		return value, err
	}
}
