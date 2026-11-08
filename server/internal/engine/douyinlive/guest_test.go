package douyinlive

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

func TestSharedGuestConcurrentExpiryAndInvalidation(t *testing.T) {
	g := newGuestIdentity()
	now := time.Now()
	g.now = func() time.Time { return now }
	var calls atomic.Int32
	fetch := func(context.Context) (string, time.Time, error) {
		calls.Add(1)
		return "synthetic-guest", now.Add(time.Minute), nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if value, err := g.get(context.Background(), "direct", fetch); err != nil || value != "synthetic-guest" {
				t.Error("guest unavailable")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent startup fetched multiple identities")
	}
	now = now.Add(time.Minute)
	g.get(context.Background(), "direct", fetch)
	if calls.Load() != 2 {
		t.Fatal("expired identity reused")
	}
	g.invalidate()
	g.get(context.Background(), "direct", fetch)
	g.get(context.Background(), "separate-proxy", fetch)
	if calls.Load() != 4 {
		t.Fatal("invalidation or exit isolation failed")
	}
}

func TestSharedGuestCancellationAndRejectedInFlightFetch(t *testing.T) {
	g := newGuestIdentity()
	started, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := g.get(context.Background(), "direct", func(context.Context) (string, time.Time, error) {
			close(started)
			<-finish
			return "synthetic", time.Time{}, nil
		})
		done <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.get(ctx, "direct", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("waiter did not cancel")
	}
	g.invalidate()
	close(finish)
	if err := <-done; !errors.Is(err, engine.ErrAccessRestricted) {
		t.Fatal("rejected identity cached")
	}
	if len(g.entries) != 0 {
		t.Fatal("rejected identity retained")
	}
}

func TestGuestSharingDefaultsOffAndFactorySharesOnlyWithinNode(t *testing.T) {
	if model.DefaultSettings().ShareGuestIdentity {
		t.Fatal("sharing default changed")
	}
	f := Factory(engine.NewBudget(100))
	a, b := f().(*Adapter), f().(*Adapter)
	if a.guests != b.guests {
		t.Fatal("factory did not share node cache")
	}
	if a.guests == Factory(engine.NewBudget(100))().(*Adapter).guests {
		t.Fatal("identity shared between nodes")
	}
}
