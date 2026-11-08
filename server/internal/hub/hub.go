package hub

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

// Queue sits between the engine callback and the bus. A full queue evicts the
// oldest lowest-value message before discarding a chat. Raw payload bytes are
// bounded. Push waits for the lock: Drain only swaps the slice, so the wait is
// tiny, whereas skipping a busy queue dropped messages in every burst.
type Queue struct {
	mu      sync.Mutex
	items   []engine.RawEvent
	bytes   int
	Wake    chan struct{}
	Dropped atomic.Int64
	// Drops splits Dropped by reason and by the type of the discarded message;
	// node, when set, also receives every drop so the total outlives the room.
	Drops DropCounts
	node  *DropCounts
}

func NewQueue() *Queue { return &Queue{Wake: make(chan struct{}, 1)} }

// NewNodeQueue is NewQueue that also reports its drops to a node-wide counter.
func NewNodeQueue(node *DropCounts) *Queue {
	q := NewQueue()
	q.node = node
	return q
}

// A room's entry queue absorbs upstream bursts: messages arrive in batches (151 KiB
// in one second was measured in a 4,000-viewer room, and a catch-up after an
// upstream hiccup can be larger), so 256 KiB / 1,000 messages overflowed. The
// queue is normally empty; 100 rooms all full would hold about 100 MiB.
const (
	maxQueueItems   = 4000
	maxQueueBytes   = 1 << 20
	maxMessageBytes = 256 * 1024 // a single raw payload above this is discarded
)

// Why an entry message was discarded before reaching the bus.
const (
	DropOversize = iota // raw payload above maxMessageBytes
	DropFull            // the queue was at its count or byte bound
	dropReasons
)

// Discarded message types; gifts are not collected by default but are still counted.
const (
	dropChat = iota
	dropGift
	dropSocial
	dropMember
	dropOther
	dropTypes
)

// DropCounts counts discarded entry messages by reason and by message type.
type DropCounts struct {
	reasons [dropReasons]atomic.Int64
	types   [dropTypes]atomic.Int64
	last    atomic.Int64 // Unix ms of the latest drop
	// High-water marks of the queue, so a drop can be told apart as a sudden
	// burst or a backlog that built up while the room loop was held up.
	peakItems, peakBytes atomic.Int64
}

func storeMax(v *atomic.Int64, n int64) {
	for {
		old := v.Load()
		if n <= old || v.CompareAndSwap(old, n) {
			return
		}
	}
}

func (c *DropCounts) observe(items, bytes int) {
	storeMax(&c.peakItems, int64(items))
	storeMax(&c.peakBytes, int64(bytes))
}

// DropStats is a point-in-time copy of DropCounts.
type DropStats struct {
	Total  int64 `json:"total"`
	LastAt int64 `json:"lastAt"` // Unix ms of the latest drop; 0 if none
	// PeakItems and PeakBytes are the most the entry queue has held at once.
	PeakItems int64            `json:"peakItems"`
	PeakBytes int64            `json:"peakBytes"`
	Reason    map[string]int64 `json:"reason"`
	Type      map[string]int64 `json:"type"`
}

func dropType(method string) int {
	switch method {
	case "WebcastChatMessage":
		return dropChat
	case "WebcastGiftMessage":
		return dropGift
	case "WebcastSocialMessage":
		return dropSocial
	case "WebcastMemberMessage":
		return dropMember
	}
	return dropOther
}

func (c *DropCounts) add(reason int, method string) {
	c.reasons[reason].Add(1)
	c.types[dropType(method)].Add(1)
	c.last.Store(time.Now().UnixMilli())
}

// Merge adds a finished queue's counts, so a room keeps its history across reconnects.
func (c *DropCounts) Merge(o *DropCounts) {
	for i := range c.reasons {
		c.reasons[i].Add(o.reasons[i].Load())
	}
	for i := range c.types {
		c.types[i].Add(o.types[i].Load())
	}
	storeMax(&c.last, o.last.Load())
	storeMax(&c.peakItems, o.peakItems.Load())
	storeMax(&c.peakBytes, o.peakBytes.Load())
}

// Stats returns the counts, adding any extra live counters (e.g. a room's current queue).
func (c *DropCounts) Stats(extra ...*DropCounts) DropStats {
	var r [dropReasons]int64
	var t [dropTypes]int64
	var last, peakItems, peakBytes int64
	for _, x := range append([]*DropCounts{c}, extra...) {
		if x == nil {
			continue
		}
		last = max(last, x.last.Load())
		peakItems = max(peakItems, x.peakItems.Load())
		peakBytes = max(peakBytes, x.peakBytes.Load())
		for i := range r {
			r[i] += x.reasons[i].Load()
		}
		for i := range t {
			t[i] += x.types[i].Load()
		}
	}
	return DropStats{
		Total:     r[DropOversize] + r[DropFull],
		LastAt:    last,
		PeakItems: peakItems,
		PeakBytes: peakBytes,
		Reason:    map[string]int64{"oversize": r[DropOversize], "full": r[DropFull]},
		Type:      map[string]int64{"chat": t[dropChat], "gift": t[dropGift], "social": t[dropSocial], "member": t[dropMember], "other": t[dropOther]},
	}
}

func (q *Queue) drop(reason int, method string) {
	q.Dropped.Add(1)
	q.Drops.add(reason, method)
	if q.node != nil {
		q.node.add(reason, method)
	}
}
func priority(method string) int {
	switch method {
	case "WebcastChatMessage":
		return 3
	case "WebcastGiftMessage", "WebcastSocialMessage":
		return 2
	case "WebcastMemberMessage":
		return 1
	}
	return 0
}
func (q *Queue) Push(e engine.RawEvent) {
	if e.Bytes > maxMessageBytes {
		q.drop(DropOversize, e.Method)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) >= maxQueueItems || q.bytes+e.Bytes > maxQueueBytes {
		victim := -1
		rank := priority(e.Method)
		for i, x := range q.items {
			if p := priority(x.Method); p <= rank {
				victim = i
				rank = p
				if rank == 0 {
					break
				}
			}
		}
		if victim < 0 {
			q.drop(DropFull, e.Method)
			return
		}
		q.drop(DropFull, q.items[victim].Method)
		q.bytes -= q.items[victim].Bytes
		copy(q.items[victim:], q.items[victim+1:])
		q.items[len(q.items)-1] = engine.RawEvent{}
		q.items = q.items[:len(q.items)-1]
	}
	q.items = append(q.items, e)
	q.bytes += e.Bytes
	q.Drops.observe(len(q.items), q.bytes)
	if q.node != nil {
		q.node.observe(len(q.items), q.bytes)
	}
	select {
	case q.Wake <- struct{}{}:
	default:
	}
}
func (q *Queue) Drain() []engine.RawEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	q.bytes = 0
	return out
}

type cached struct {
	event model.Event
	bytes int
}
type Seen struct {
	id string
	at time.Time
}
type Buffer struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	order  []Seen
	events []cached
	bytes  int
}

func NewBuffer() *Buffer { return &Buffer{seen: map[string]time.Time{}} }
func (b *Buffer) Add(e model.Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := e.ReceivedAt
	for len(b.order) > 0 && now.Sub(b.order[0].at) > 10*time.Minute {
		delete(b.seen, b.order[0].id)
		b.order = b.order[1:]
	}
	if e.ID != "" {
		if _, ok := b.seen[e.ID]; ok {
			return false
		}
		for len(b.order) >= 2000 {
			delete(b.seen, b.order[0].id)
			b.order = b.order[1:]
		}
		b.seen[e.ID] = now
		b.order = append(b.order, Seen{e.ID, now})
	}
	data, _ := json.Marshal(e)
	if len(data) > 8192 {
		return false
	}
	for len(b.events) > 0 && (len(b.events) >= 200 || b.bytes+len(data) > 256*1024) {
		b.bytes -= b.events[0].bytes
		b.events = b.events[1:]
	}
	b.events = append(b.events, cached{e, len(data)})
	b.bytes += len(data)
	return true
}
func (b *Buffer) Events() []model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]model.Event, len(b.events))
	for i, x := range b.events {
		out[i] = x.event
	}
	return out
}
