package hub

import (
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

type Route struct {
	SubscriptionID string
	ExternalRef    string
	Types          map[string]bool
	StartSeq       uint64
}
type Delivery struct {
	model.Event
	SubscriptionID string `json:"subscription_id,omitempty"`
	ExternalRef    string `json:"external_ref,omitempty"`
}
type Packet struct {
	Event Delivery
	Data  []byte
}
type traffic struct {
	second          int64
	messages, bytes uint64
}
type Meter struct {
	mu                   sync.Mutex
	buckets              [60]traffic
	Sent, Bytes, Dropped atomic.Uint64
}

func (m *Meter) sent(n int) {
	m.Sent.Add(1)
	m.Bytes.Add(uint64(n))
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	p := &m.buckets[now%60]
	if p.second != now {
		*p = traffic{second: now}
	}
	p.messages++
	p.bytes += uint64(n)
}

type StreamStats struct {
	Connections int     `json:"connections"`
	Queue       int     `json:"queue"`
	QueueBytes  int     `json:"queue_bytes"`
	PushPerSec  float64 `json:"push_per_sec"`
	BytesPerSec float64 `json:"bytes_per_sec"`
	Sent        uint64  `json:"sent"`
	Bytes       uint64  `json:"bytes"`
	Dropped     uint64  `json:"dropped"`
}

func (m *Meter) stats() StreamStats {
	v := StreamStats{Sent: m.Sent.Load(), Bytes: m.Bytes.Load(), Dropped: m.Dropped.Load()}
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.buckets {
		if now-b.second < 60 {
			v.PushPerSec += float64(b.messages) / 60
			v.BytesPerSec += float64(b.bytes) / 60
		}
	}
	return v
}

// Outbox has no network I/O. Each client owns a bounded queue, so slow sockets
// cannot block collection or the other clients. Status messages have top priority.
type Outbox struct {
	mu                      sync.Mutex
	items                   []Packet
	bytes, limit, byteLimit int
	pendingDrops            uint64
	overflow                int
	meter                   *Meter
	Wake                    chan struct{}
	Done                    chan struct{}
	closed                  bool
}

func NewOutbox(limit, byteLimit int, meter *Meter) *Outbox {
	return &Outbox{limit: limit, byteLimit: byteLimit, meter: meter, Wake: make(chan struct{}, 1), Done: make(chan struct{})}
}
func valueRank(typ string) int {
	switch typ {
	case "status":
		return 4
	case "chat":
		return 3
	case "gift", "social":
		return 2
	case "member":
		return 1
	}
	return 0
}
func (q *Outbox) wake() {
	select {
	case q.Wake <- struct{}{}:
	default:
	}
}
func (q *Outbox) closeLocked() {
	if !q.closed {
		q.closed = true
		close(q.Done)
		q.items = nil
		q.bytes = 0
	}
	q.wake()
}
func (q *Outbox) Close() { q.mu.Lock(); defer q.mu.Unlock(); q.closeLocked() }
func (q *Outbox) drop()  { q.pendingDrops++; q.meter.Dropped.Add(1); q.overflow++; q.wake() }
func (q *Outbox) Push(p Packet) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if len(p.Data) > q.byteLimit {
		if p.Event.Type == "status" {
			q.closeLocked()
			return
		}
		q.drop()
		if q.overflow >= 100 {
			q.closeLocked()
		}
		return
	}
	for len(q.items) >= q.limit || q.bytes+len(p.Data) > q.byteLimit {
		victim := -1
		rank := valueRank(p.Event.Type)
		for i, x := range q.items {
			if x.Event.Type == "status" {
				continue
			}
			if r := valueRank(x.Event.Type); r <= rank {
				victim = i
				rank = r
				if r == 0 {
					break
				}
			}
		}
		if victim < 0 && p.Event.Type == "status" {
			// Never replace or discard a status to keep a slow stream alive.
			// Disconnect; its last consumed cursor can replay from the bus cache.
			q.closeLocked()
			return
		}
		q.drop()
		if q.overflow >= 100 {
			q.closeLocked()
			return
		}
		if victim < 0 {
			return
		}
		q.bytes -= len(q.items[victim].Data)
		copy(q.items[victim:], q.items[victim+1:])
		q.items[len(q.items)-1] = Packet{}
		q.items = q.items[:len(q.items)-1]
	}
	q.items = append(q.items, p)
	q.bytes += len(p.Data)
	q.wake()
}
func (q *Outbox) Next() (Packet, uint64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	drops := q.pendingDrops
	q.pendingDrops = 0
	if drops > 0 {
		return Packet{}, drops, true
	}
	if len(q.items) == 0 {
		return Packet{}, 0, false
	}
	p := q.items[0]
	q.items[0] = Packet{}
	q.items = q.items[1:]
	q.bytes -= len(p.Data)
	if len(q.items) < q.limit/2 {
		q.overflow = 0
	}
	return p, 0, true
}
func (q *Outbox) Sent(n int) { q.meter.sent(n) }
func (q *Outbox) filter(routes map[string]Route) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	q.bytes = 0
	for _, p := range q.items {
		r, ok := routes[p.Event.LiveID]
		if (p.Event.LiveID == "" && p.Event.Type == "status") || (ok && r.SubscriptionID == p.Event.SubscriptionID && (p.Event.Type == "status" || r.Types[p.Event.Type])) {
			kept = append(kept, p)
			q.bytes += len(p.Data)
		}
	}
	clear(q.items[len(kept):])
	q.items = kept
}

// Replay cache and the reconnect catch-up limit. In the seventh-round test (100
// rooms, chat/member/social) the old catch-up cap of 2,000 events was reached
// after about 20 seconds offline and the 200-event room cache was the next limit,
// so the documented 60-second window was never available. A room now keeps 60
// seconds up to 1,500 events / 1 MiB; one reconnect replays up to 20,000 events
// / 32 MiB, which covers 60 seconds of 100 busy rooms.
const (
	replayWindow    = 60 * time.Second
	replayRoomItems = 1500
	replayRoomBytes = 1 << 20
	replayMaxEvents = 20000
	replayMaxBytes  = 32 << 20
)

type replayRoom struct {
	items []cached
	bytes int
	lost  uint64
}
type stream struct {
	client string
	routes map[string]Route
	q      *Outbox
}
type Bus struct {
	// UpstreamLag is Douyin's message time to arrival at this node; PushLag is
	// arrival to a successful northbound WebSocket write (live delivery only).
	UpstreamLag, PushLag Lag

	mu      sync.Mutex
	seq     uint64
	rooms   map[string]*replayRoom
	streams map[*Outbox]*stream
	meters  map[string]*Meter
	now     func() time.Time
}

func NewBus() *Bus {
	return &Bus{rooms: map[string]*replayRoom{}, streams: map[*Outbox]*stream{}, meters: map[string]*Meter{}, now: time.Now}
}
func (b *Bus) Cursor() uint64 { b.mu.Lock(); defer b.mu.Unlock(); return b.seq }
func (b *Bus) meter(id string) *Meter {
	m := b.meters[id]
	if m == nil {
		m = &Meter{}
		b.meters[id] = m
	}
	return m
}
func (b *Bus) prune(r *replayRoom) {
	for len(r.items) > 0 && (b.now().Sub(r.items[0].event.ReceivedAt) > replayWindow || len(r.items) > replayRoomItems || r.bytes > replayRoomBytes) {
		x := r.items[0]
		r.lost = x.event.Seq
		r.bytes -= x.bytes
		r.items[0] = cached{}
		r.items = r.items[1:]
	}
}

// Expire releases payloads from quiet rooms as well as actively publishing rooms.
// The lost cursor remains so a later reconnect still receives a gap notice.
func (b *Bus) Expire() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.rooms {
		b.prune(r)
	}
}
func packet(e model.Event, r Route) Packet {
	v := Delivery{e, r.SubscriptionID, r.ExternalRef}
	data, _ := json.Marshal(v)
	return Packet{v, data}
}

// Accept is serialized with stream attachment: replay and live delivery cannot
// leave a hole between reading the cache and attaching the queue.
func (b *Bus) Publish(e model.Event, accept func(model.Event) bool) model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e.Seq = b.seq
	if accept != nil && !accept(e) {
		return e
	}
	r := b.rooms[e.LiveID]
	if r == nil {
		r = &replayRoom{}
		b.rooms[e.LiveID] = r
	}
	data, _ := json.Marshal(e)
	r.items = append(r.items, cached{e, len(data)})
	r.bytes += len(data)
	b.prune(r)
	for _, s := range b.streams {
		route, ok := s.routes[e.LiveID]
		if ok && e.Seq > route.StartSeq && (e.Type == "status" || route.Types[e.Type]) {
			s.q.Push(packet(e, route))
		}
	}
	return e
}
func (b *Bus) controlLocked(code string, extra map[string]any) Packet {
	b.seq++
	if extra == nil {
		extra = map[string]any{}
	}
	extra["code"] = code
	now := b.now()
	return packet(model.Event{Seq: b.seq, Type: "status", TS: now, ReceivedAt: now, Extra: extra}, Route{})
}
func (b *Bus) Control(code string, extra map[string]any) Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.controlLocked(code, extra)
}

// Notice is a control frame that Open sends right after hello, such as a
// subscription release the client missed while it had no stream.
type Notice struct {
	Code  string
	Extra map[string]any
}

// Replay is bounded separately and written first; oversize replay is explicitly
// reported as a gap rather than allocating an unbounded backlog per connection.
// hello reports how many subscriptions the stream carries, so a client whose
// leases all ended while it was away can tell at once.
func (b *Bus) Open(client string, routes map[string]Route, since *uint64, boot, expectedBoot string, notices ...Notice) (*Outbox, []Packet) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := NewOutbox(2000, 4<<20, b.meter(client))
	b.streams[q] = &stream{client, routes, q}
	out := []Packet{b.controlLocked("hello", map[string]any{"boot_id": boot, "current_seq": b.seq, "subscriptions": len(routes)})}
	for _, n := range notices {
		out = append(out, b.controlLocked(n.Code, n.Extra))
	}
	if since == nil {
		return q, out
	}
	cursor := *since
	gap := ""
	reset := false
	if expectedBoot != boot {
		gap = "node_restarted"
		cursor = 0
		reset = true
	} else if cursor > b.seq {
		gap = "invalid_cursor"
		cursor = 0
		reset = true
	}
	events := []Packet{}
	bytes := 0
	for id, route := range routes {
		r := b.rooms[id]
		if r == nil {
			continue
		}
		b.prune(r)
		if r.lost > max(cursor, route.StartSeq) {
			gap = "buffer_exceeded"
		}
		for _, x := range r.items {
			e := x.event
			if e.Seq > max(cursor, route.StartSeq) && (e.Type == "status" || route.Types[e.Type]) {
				p := packet(e, route)
				if len(events) >= replayMaxEvents || bytes+len(p.Data) > replayMaxBytes {
					gap = "replay_limit"
					continue
				}
				bytes += len(p.Data)
				events = append(events, p)
			}
		}
	}
	if gap != "" {
		out = append(out, b.controlLocked("gap", map[string]any{"reason": gap, "boot_id": boot, "since_seq": *since, "reset_cursor": reset}))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Event.Seq < events[j].Event.Seq })
	return q, append(out, events...)
}
func (b *Bus) Routes(q *Outbox, routes map[string]Route) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.streams[q]; s != nil {
		s.routes = routes
		q.filter(routes)
	}
}

// CommitRoutes establishes a subscription's start cursor and its stream routes
// under the same publication lock. No event can fall between these two steps.
func (b *Bus) CommitRoutes(commit func(uint64) (map[*Outbox]map[string]Route, error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	updates, err := commit(b.seq)
	if err != nil {
		return err
	}
	for q, routes := range updates {
		if s := b.streams[q]; s != nil {
			s.routes = routes
			q.filter(routes)
		}
	}
	return nil
}
func (b *Bus) Close(q *Outbox)      { b.mu.Lock(); delete(b.streams, q); b.mu.Unlock(); q.Close() }
func (b *Bus) ForgetRoom(id string) { b.mu.Lock(); defer b.mu.Unlock(); delete(b.rooms, id) }

// ForgetClient drops a deleted client's traffic meter; its streams are closed first.
func (b *Bus) ForgetClient(id string) { b.mu.Lock(); defer b.mu.Unlock(); delete(b.meters, id) }
func (b *Bus) Stats(client string) StreamStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.meter(client).stats()
	for _, s := range b.streams {
		if s.client == client {
			s.q.mu.Lock()
			if !s.q.closed {
				v.Connections++
				v.Queue += len(s.q.items)
				v.QueueBytes += s.q.bytes
			}
			s.q.mu.Unlock()
		}
	}
	return v
}
