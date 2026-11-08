package hub

import (
	"fmt"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func event(id, typ, room string) model.Event {
	return model.Event{ID: id, Type: typ, LiveID: room, TS: time.Now(), ReceivedAt: time.Now(), Extra: map[string]any{}}
}
func TestReplayGapIsolationAndAtomicAttach(t *testing.T) {
	b := NewBus()
	r := map[string]Route{"one": {SubscriptionID: "sub-one", ExternalRef: "merchant", Types: map[string]bool{"chat": true}}}
	for i := 0; i < replayRoomItems+5; i++ {
		b.Publish(event(fmt.Sprint(i), "chat", "one"), nil)
	}
	b.Publish(event("secret-room", "chat", "two"), nil)
	since := uint64(1)
	q, packets := b.Open("a", r, &since, "boot", "boot")
	defer b.Close(q)
	gaps := 0
	messages := 0
	last := uint64(0)
	for _, p := range packets {
		if p.Event.Extra["code"] == "gap" {
			gaps++
		}
		if p.Event.LiveID != "" {
			if p.Event.LiveID != "one" || p.Event.SubscriptionID != "sub-one" || p.Event.ExternalRef != "merchant" || p.Event.Seq <= last {
				t.Fatal("replay isolation/order failed")
			}
			last = p.Event.Seq
			messages++
		}
	}
	if gaps != 1 || messages != replayRoomItems {
		t.Fatal("cache bound or explicit gap missing")
	}
	next := b.Publish(event("live", "chat", "one"), nil)
	p, _, ok := q.Next()
	if !ok || p.Event.Seq != next.Seq {
		t.Fatal("live event lost after replay attachment")
	}
	since = last
	q2, packets := b.Open("b", r, &since, "new-boot", "old-boot")
	defer b.Close(q2)
	found := false
	for _, p := range packets {
		if p.Event.Extra["code"] == "gap" {
			found = true
		}
	}
	if !found {
		t.Fatal("restart gap missing")
	}
}
func TestReplayExpiryAndSubscriptionStartBoundary(t *testing.T) {
	b := NewBus()
	at := time.Now()
	b.now = func() time.Time { return at }
	first := b.Publish(event("old", "chat", "one"), nil)
	routes := map[string]Route{"one": {SubscriptionID: "sub", StartSeq: first.Seq, Types: map[string]bool{"chat": true}}}
	b.Publish(event("new", "chat", "one"), nil)
	zero := uint64(0)
	q, packets := b.Open("a", routes, &zero, "boot", "boot")
	defer b.Close(q)
	count := 0
	for _, p := range packets {
		if p.Event.ID == "old" {
			t.Fatal("pre-subscription event exposed")
		}
		if p.Event.ID == "new" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("new event missing")
	}
	at = at.Add(61 * time.Second)
	b.Expire()
	if len(b.rooms["one"].items) != 0 || b.rooms["one"].bytes != 0 {
		t.Fatal("quiet room retained expired payloads")
	}
	q2, packets := b.Open("a", routes, &zero, "boot", "boot")
	defer b.Close(q2)
	gap := false
	for _, p := range packets {
		if p.Event.Extra["code"] == "gap" {
			gap = true
		}
		if p.Event.LiveID != "" {
			t.Fatal("expired cache replayed")
		}
	}
	if !gap {
		t.Fatal("expired cache silently lost")
	}
}

func TestReplayTotalLimitAndInvalidCursorReset(t *testing.T) {
	b := NewBus()
	routes := map[string]Route{}
	for room := range replayMaxEvents/replayRoomItems + 2 {
		id := fmt.Sprint(room)
		routes[id] = Route{SubscriptionID: "sub-" + id, Types: map[string]bool{"chat": true}}
		for i := range replayRoomItems {
			b.Publish(event(fmt.Sprint(i), "chat", id), nil)
		}
	}
	future := uint64(99999)
	q, packets := b.Open("a", routes, &future, "boot", "boot")
	defer b.Close(q)
	count, size, gaps := 0, 0, 0
	for _, p := range packets {
		if p.Event.SubscriptionID != "" {
			count++
			size += len(p.Data)
		} else if p.Event.Extra["code"] == "gap" {
			gaps++
			if p.Event.Extra["reset_cursor"] != true {
				t.Fatal("invalid cursor reset was lost when another gap reason applied")
			}
		}
	}
	if count != replayMaxEvents || size > replayMaxBytes || gaps != 1 {
		t.Fatal("aggregate replay limit or gap notification failed")
	}
}

func TestSubscriptionCommitAttachesExistingStreamBeforePublish(t *testing.T) {
	b := NewBus()
	q, _ := b.Open("a", map[string]Route{}, nil, "boot", "")
	defer b.Close(q)
	started, delivered := make(chan struct{}), make(chan struct{})
	err := b.CommitRoutes(func(cursor uint64) (map[*Outbox]map[string]Route, error) {
		go func() {
			close(started)
			b.Publish(event("after-start", "chat", "one"), nil)
			close(delivered)
		}()
		<-started
		return map[*Outbox]map[string]Route{q: {"one": {StartSeq: cursor, SubscriptionID: "sub", Types: map[string]bool{"chat": true}}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-delivered
	p, _, ok := q.Next()
	if !ok || p.Event.ID != "after-start" || p.Event.SubscriptionID != "sub" {
		t.Fatal("event fell between subscription commit and stream attachment")
	}
}
func TestPriorityBackpressureBoundedAndOtherClientsContinue(t *testing.T) {
	meter := &Meter{}
	q := NewOutbox(3, 10000, meter)
	for _, typ := range []string{"chat", "like", "member", "chat"} {
		q.Push(packet(event(typ, typ, "one"), Route{}))
	}
	_, drops, ok := q.Next()
	if !ok || drops != 1 {
		t.Fatal("drop notice missing")
	}
	for range 3 {
		p, _, ok := q.Next()
		if !ok || p.Event.Type == "like" {
			t.Fatal("low priority was not discarded first")
		}
	}
	b := NewBus()
	routes := map[string]Route{"one": {Types: map[string]bool{"chat": true}, SubscriptionID: "sub"}}
	slow, _ := b.Open("slow", routes, nil, "boot", "")
	fast, _ := b.Open("fast", routes, nil, "boot", "")
	defer b.Close(slow)
	defer b.Close(fast)
	for i := 0; i < 2200; i++ {
		b.Publish(event(fmt.Sprint(i), "chat", "one"), nil)
		if _, _, ok := fast.Next(); !ok {
			t.Fatal("slow client blocked fast client")
		}
	}
	select {
	case <-slow.Done:
	default:
		t.Fatal("persistent overflow did not disconnect")
	}
	if b.Stats("slow").Dropped < 100 || b.Stats("fast").Dropped != 0 {
		t.Fatal("drop counters incorrect")
	}
	tiny := NewOutbox(20, 128, &Meter{})
	tiny.Push(packet(model.Event{Content: string(make([]byte, 1024))}, Route{}))
	if len(tiny.items) != 0 || tiny.meter.Dropped.Load() != 1 {
		t.Fatal("byte bound not enforced")
	}
}

func TestStatusNeverEvictedAndFullStatusQueueDisconnectsWithReplay(t *testing.T) {
	q := NewOutbox(2, 10000, &Meter{})
	q.Push(packet(event("", "status", "one"), Route{}))
	q.Push(packet(event("chat", "chat", "one"), Route{}))
	q.Push(packet(event("", "status", "one"), Route{}))
	_, dropped, _ := q.Next()
	if dropped != 1 {
		t.Fatal("status did not displace lower priority data")
	}
	q.Push(packet(event("low", "chat", "one"), Route{}))
	_, dropped, _ = q.Next()
	if dropped != 1 {
		t.Fatal("lower priority event should be discarded")
	}
	for range 2 {
		p, _, ok := q.Next()
		if !ok || p.Event.Type != "status" {
			t.Fatal("status was evicted")
		}
	}

	b := NewBus()
	routes := map[string]Route{"one": {SubscriptionID: "sub", Types: map[string]bool{}}}
	slow, _ := b.Open("slow", routes, nil, "boot", "")
	defer b.Close(slow)
	slow.limit = 1
	fast, _ := b.Open("fast", routes, nil, "boot", "")
	defer b.Close(fast)
	first := b.Publish(event("", "status", "one"), nil)
	second := b.Publish(event("", "status", "one"), nil)
	select {
	case <-slow.Done:
	default:
		t.Fatal("all-status queue silently replaced a status")
	}
	if b.Stats("slow").Dropped != 0 {
		t.Fatal("status counted as discard instead of disconnect")
	}
	for _, want := range []uint64{first.Seq, second.Seq} {
		p, _, ok := fast.Next()
		if !ok || p.Event.Seq != want {
			t.Fatal("slow status queue affected healthy client")
		}
	}
	since := uint64(0)
	reconnected, replay := b.Open("slow", routes, &since, "boot", "boot")
	defer b.Close(reconnected)
	var recovered []uint64
	for _, p := range replay {
		if p.Event.SubscriptionID != "" {
			recovered = append(recovered, p.Event.Seq)
		}
	}
	if len(recovered) != 2 || recovered[0] != first.Seq || recovered[1] != second.Seq {
		t.Fatal("disconnected status events not retained for replay")
	}
	b.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	late, replay := b.Open("slow", routes, &since, "boot", "boot")
	defer b.Close(late)
	gap := false
	for _, p := range replay {
		if p.Event.Extra["code"] == "gap" {
			gap = true
		}
	}
	if !gap {
		t.Fatal("expired status cache was silently lost")
	}

	tiny := NewOutbox(2, 1, &Meter{})
	tiny.Push(packet(event("", "status", "one"), Route{}))
	select {
	case <-tiny.Done:
	default:
		t.Fatal("oversized status did not disconnect")
	}
	if tiny.meter.Dropped.Load() != 0 {
		t.Fatal("oversized status silently discarded")
	}
}

// The seventh-round case: 100 subscribed rooms, about 20 seconds offline. The old
// 2,000-event catch-up cap turned this into a gap; it must now replay completely.
func TestReplayCoversManyBusyRoomsWithoutGap(t *testing.T) {
	b := NewBus()
	routes := map[string]Route{}
	for room := range 100 {
		id := fmt.Sprint("room-", room)
		routes[id] = Route{SubscriptionID: "sub-" + id, Types: map[string]bool{"chat": true}}
	}
	since := b.Cursor()
	for i := range 150 {
		for room := range 100 {
			b.Publish(event(fmt.Sprint(room, "-", i), "chat", fmt.Sprint("room-", room)), nil)
		}
	}
	q, packets := b.Open("a", routes, &since, "boot", "boot")
	defer b.Close(q)
	count := 0
	for _, p := range packets {
		if p.Event.Extra["code"] == "gap" {
			t.Fatalf("unexpected gap: %v", p.Event.Extra)
		}
		if p.Event.SubscriptionID != "" {
			count++
		}
	}
	if count != 15000 {
		t.Fatalf("replayed %d of 15000", count)
	}
}
func TestHelloCountsRoutesAndNoticesFollowIt(t *testing.T) {
	b := NewBus()
	routes := map[string]Route{"one": {SubscriptionID: "sub-one", Types: map[string]bool{"chat": true}}, "two": {SubscriptionID: "sub-two", Types: map[string]bool{"chat": true}}}
	b.Publish(event("first", "chat", "one"), nil)
	since := uint64(1)
	q, packets := b.Open("a", routes, &since, "boot", "boot", Notice{"subscription_released", map[string]any{"subscription_id": "sub-gone", "live_id": "three", "reason": "lease_expired"}})
	defer b.Close(q)
	if len(packets) < 2 || packets[0].Event.Extra["code"] != "hello" || packets[0].Event.Extra["subscriptions"] != 2 {
		t.Fatal("hello must come first and count the stream's subscriptions")
	}
	n := packets[1].Event
	if n.Extra["code"] != "subscription_released" || n.Extra["subscription_id"] != "sub-gone" || n.SubscriptionID != "" || n.Seq <= packets[0].Event.Seq {
		t.Fatal("missed release must follow hello as a control frame")
	}
}
