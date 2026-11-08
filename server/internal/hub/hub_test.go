package hub

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

func TestQueueBoundAndPriority(t *testing.T) {
	q := NewQueue()
	for i := 0; i < maxQueueItems; i++ {
		q.Push(engine.RawEvent{Method: "WebcastLikeMessage", Bytes: 10})
	}
	q.Push(engine.RawEvent{Method: "WebcastChatMessage", Bytes: 10})
	items := q.Drain()
	if len(items) != maxQueueItems || items[len(items)-1].Method != "WebcastChatMessage" || q.Dropped.Load() != 1 {
		t.Fatal("priority eviction failed")
	}
}

func queued(q *Queue) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Bursts arrive while the room loop drains: nothing may be lost to that race.
func TestQueueConcurrentDrainLosesNothing(t *testing.T) {
	q := NewQueue()
	const n = 200000
	var pushed atomic.Bool
	result := make(chan int)
	go func() {
		got := 0
		for {
			finished := pushed.Load()
			got += len(q.Drain())
			if finished {
				result <- got + len(q.Drain())
				return
			}
			runtime.Gosched()
		}
	}()
	for i := 0; i < n; i++ {
		q.Push(engine.RawEvent{Method: "WebcastChatMessage", Bytes: 1})
		for i%100 == 0 && queued(q) > 800 { // keep below the count bound: this test is about the race, not overflow
			runtime.Gosched()
		}
	}
	pushed.Store(true)
	if got := <-result; got != n || q.Dropped.Load() != 0 {
		t.Fatalf("delivered %d of %d, dropped %d", got, n, q.Dropped.Load())
	}
}

func TestQueueDropsCountedByReasonAndType(t *testing.T) {
	var node DropCounts
	q := NewNodeQueue(&node)
	q.Push(engine.RawEvent{Method: "WebcastMemberMessage", Bytes: 300 * 1024}) // oversize
	q.Push(engine.RawEvent{Method: "WebcastChatMessage", Bytes: 40000})        // large but kept
	q.Drain()
	for i := 0; i < maxQueueItems; i++ {
		q.Push(engine.RawEvent{Method: "WebcastLikeMessage", Bytes: 10})
	}
	q.Push(engine.RawEvent{Method: "WebcastSocialMessage", Bytes: 10}) // evicts a like
	// Peaks: the 40,000-byte chat alone, then 4,000 likes of 10 bytes.
	if p := q.Drops.Stats(); p.PeakItems != maxQueueItems || p.PeakBytes != 40000 {
		t.Fatalf("peak = %d items / %d bytes", p.PeakItems, p.PeakBytes)
	}
	got := q.Drops.Stats()
	if got.Total != 2 || got.Reason["oversize"] != 1 || got.Reason["full"] != 1 ||
		got.Type["member"] != 1 || got.Type["other"] != 1 || q.Dropped.Load() != 2 {
		t.Fatalf("room drops = %+v (dropped %d)", got, q.Dropped.Load())
	}
	// The node counter matches and survives the room; a room merges its finished queues.
	if n := node.Stats(); n.Total != 2 || n.Type["member"] != 1 {
		t.Fatalf("node drops = %+v", n)
	}
	var room DropCounts
	room.Merge(&q.Drops)
	if s := room.Stats(&q.Drops); s.Total != 4 || s.Reason["full"] != 2 {
		t.Fatalf("merged drops = %+v", s)
	}
}
func TestBufferDedupeExpiryAndBound(t *testing.T) {
	b := NewBuffer()
	now := time.Now()
	e := model.Event{ID: "one", ReceivedAt: now}
	if !b.Add(e) || b.Add(e) {
		t.Fatal("dedupe failed")
	}
	e.ReceivedAt = now.Add(11 * time.Minute)
	if !b.Add(e) {
		t.Fatal("expiry failed")
	}
	for i := 0; i < 300; i++ {
		b.Add(model.Event{ReceivedAt: now, Content: "test"})
	}
	if len(b.Events()) != 200 {
		t.Fatal("cache unbounded")
	}
}

// A catch-up burst bigger than the old 256 KiB bound fits without losing anything.
func TestQueueAbsorbsUpstreamBurst(t *testing.T) {
	q := NewQueue()
	for i := 0; i < 200; i++ {
		q.Push(engine.RawEvent{Method: "WebcastLikeMessage", Bytes: 4 << 10})
	}
	if got := len(q.Drain()); got != 200 || q.Dropped.Load() != 0 {
		t.Fatalf("kept %d of 200 (800 KiB), dropped %d", got, q.Dropped.Load())
	}
	if p := q.Drops.Stats(); p.PeakItems != 200 || p.PeakBytes != 200*4<<10 {
		t.Fatalf("peak = %+v", p)
	}
}
