package northbound

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/Vz7797/LiveTap/sdk/go"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

func heartbeatsFrom(q *hub.Outbox) []hub.Packet {
	var out []hub.Packet
	for {
		p, _, ok := q.Next()
		if !ok {
			return out
		}
		if p.Event.Extra["code"] == "heartbeat" {
			out = append(out, p)
		}
	}
}

func TestHeartbeatDefaultSharingIsolationAndLeaseExpiry(t *testing.T) {
	f := setup(t, true)
	f.s.cancel()
	<-f.s.done // Advance a deterministic clock without the background sweeper.
	if f.s.heartbeatInterval != time.Minute {
		t.Fatal("production heartbeat must default to sixty seconds")
	}
	now := time.Now()
	f.s.Now = func() time.Time { return now }
	a, _, _ := f.client(t)
	b, _, _ := f.client(t)
	stranger, _, _ := f.client(t)
	request := SubscribeRequest{LiveID: "123456789", EventTypes: []string{}, Options: model.SubscriptionOptions{LeaseSeconds: 120, IdleReleaseSeconds: 180}}
	first, err := f.s.Subscribe(f.ctx, a.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	start := now
	waitFor(t, func() bool { st, _ := f.m.Status(first.LiveID); return st.State == "waiting" })
	qa, _, err := f.s.Open(a.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(qa)
	qOther, _, err := f.s.Open(stranger.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(qOther)
	now = start.Add(30 * time.Second)
	request.Options.LeaseSeconds = 3600
	second, err := f.s.Subscribe(f.ctx, b.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	qb, _, err := f.s.Open(b.ID, second.ID, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(qb)
	now = start.Add(59 * time.Second)
	f.s.Sweep()
	if len(heartbeatsFrom(qa)) != 0 {
		t.Fatal("heartbeat emitted early")
	}
	now = start.Add(time.Minute)
	f.s.Sweep()
	x, y := heartbeatsFrom(qa), heartbeatsFrom(qb)
	if len(x) != 1 || len(y) != 1 || x[0].Event.Seq != y[0].Event.Seq || x[0].Event.Seq <= first.StartSeq {
		t.Fatal("shared heartbeat was missing, duplicated, or outside the seq stream")
	}
	if x[0].Event.SubscriptionID != first.ID || y[0].Event.SubscriptionID != second.ID || x[0].Event.Extra["state"] != "waiting" {
		t.Fatal("heartbeat routing or state mismatch")
	}
	var wire sdk.Event
	if json.Unmarshal(x[0].Data, &wire) != nil || string(wire.Extra["last_message_at"]) != "null" || wire.ID != "" {
		t.Fatal("heartbeat invented an upstream message or message timestamp")
	}
	if len(heartbeatsFrom(qOther)) != 0 {
		t.Fatal("heartbeat crossed client isolation")
	}
	f.s.Sweep()
	if len(heartbeatsFrom(qa)) != 0 {
		t.Fatal("same deadline emitted twice")
	}
	// Heartbeat must not renew a lease, even when a stream exists without Pong.
	now = start.Add(120 * time.Second)
	f.s.Sweep()
	if _, err := f.s.Get(a.ID, first.ID); err == nil || len(heartbeatsFrom(qa)) != 0 {
		t.Fatal("heartbeat revived or delivered an expired subscription")
	}
	if len(heartbeatsFrom(qb)) != 1 {
		t.Fatal("remaining subscriber lost its heartbeat")
	}
	st, _ := f.m.Status(first.LiveID)
	now = st.WaitingSince.Add(181 * time.Second)
	f.s.Sweep()
	if _, ok := f.m.Status(first.LiveID); ok {
		t.Fatal("heartbeat prevented idle release")
	}
	if len(f.s.lastHeartbeat) != 0 {
		t.Fatal("released room retained heartbeat schedule")
	}
	if f.m.Budget.Count() != 0 {
		t.Fatal("heartbeat contacted upstream")
	}
}

func TestHeartbeatStatesLastMessageAndCooldown(t *testing.T) {
	f := setup(t, false, Options{StatusHeartbeatInterval: 2 * time.Second})
	f.s.cancel()
	<-f.s.done
	now := time.Now()
	f.s.Now = func() time.Time { return now }
	owner, _, _ := f.client(t)
	sub, err := f.s.Subscribe(f.ctx, owner.ID, SubscribeRequest{LiveID: "123456789"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.State == "collecting" })
	if st, _ := f.m.Status(sub.LiveID); st.LastMessageAt != nil {
		t.Fatal("connection start counted as received message")
	}
	q, _, err := f.s.Open(owner.ID, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(q)
	f.source.emit(sub.LiveID, "synthetic-heartbeat-test")
	waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.LastMessageAt != nil })
	st, _ := f.m.Status(sub.LiveID)
	last := *st.LastMessageAt
	f.source.mu.Lock()
	sink := f.source.sinks[sub.LiveID]
	f.source.mu.Unlock()
	for _, state := range []string{"collecting", "waiting", "probing", "reconnecting"} {
		sink.State(engine.EngineState{State: state, Reason: "synthetic state"})
		waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.State == state })
		now = now.Add(2 * time.Second)
		f.s.Sweep()
		packets := heartbeatsFrom(q)
		if len(packets) != 1 || packets[0].Event.Extra["state"] != state {
			t.Fatal("heartbeat did not reflect current state")
		}
		at, ok := packets[0].Event.Extra["last_message_at"].(*time.Time)
		if !ok || at == nil || !at.Equal(last) {
			t.Fatal("heartbeat changed upstream timestamp")
		}
	}
	f.m.Budget.Restrict()
	now = now.Add(2 * time.Second)
	f.s.Sweep()
	packets := heartbeatsFrom(q)
	if len(packets) != 1 || packets[0].Event.Extra["state"] != "cooling" || packets[0].Event.Extra["base_state"] != "reconnecting" || packets[0].Event.Extra["node_cooldown"] != true {
		t.Fatal("node cooling was not visible")
	}
	sink.State(engine.EngineState{State: "collecting"})
	waitFor(t, func() bool { st, _ := f.m.Status(sub.LiveID); return st.State == "collecting" })
	now = now.Add(2 * time.Second)
	f.s.Sweep()
	packets = heartbeatsFrom(q)
	if len(packets) != 1 || packets[0].Event.Extra["state"] != "collecting" || packets[0].Event.Extra["node_cooldown"] != true {
		t.Fatal("cooling changed established collector state")
	}
	st, _ = f.m.Status(sub.LiveID)
	if !st.LastMessageAt.Equal(last) || f.m.Budget.Count() != 0 {
		t.Fatal("heartbeat touched upstream activity")
	}
}

func TestSDKHeartbeatEndToEndAutomaticScheduleReconnectAndReplay(t *testing.T) {
	f := setup(t, true, Options{StatusHeartbeatInterval: time.Second})
	owner, client, _ := f.client(t)
	sub, err := client.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: "123456789", ExternalRef: "heartbeat-fixture", EventTypes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	events := make(chan sdk.Event, 16)
	done := make(chan error, 1)
	retrying, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var gaps atomic.Int64
	cursor := &sdk.Cursor{}
	go func() {
		done <- client.Stream(ctx, sdk.StreamOptions{Cursor: cursor,
			OnEvent: func(e sdk.Event) error {
				if e.StringField("code") == "heartbeat" {
					select {
					case events <- e:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			},
			OnGap: func(sdk.Event) { gaps.Add(1) },
			OnRetry: func(error) {
				once.Do(func() {
					close(retrying)
					select {
					case <-resume:
					case <-ctx.Done():
					}
				})
			},
		})
	}()
	defer func() { cancel(); <-done }()
	receive := func() sdk.Event {
		t.Helper()
		select {
		case e := <-events:
			return e
		case <-ctx.Done():
			t.Fatal("heartbeat stream timeout")
			return sdk.Event{}
		}
	}
	first := receive()
	second := receive()
	if first.StringField("state") != "waiting" || second.Seq <= first.Seq || second.SubscriptionID != sub.ID || second.ExternalRef != "heartbeat-fixture" || string(second.Extra["last_message_at"]) != "null" {
		t.Fatal("scheduled waiting heartbeat wire contract failed")
	}
	f.s.mu.Lock()
	f.s.closeClient(owner.ID)
	f.s.mu.Unlock()
	select {
	case <-retrying:
	case <-ctx.Done():
		t.Fatal("SDK did not retry disconnected stream")
	}
	before := f.m.Bus.Cursor()
	waitFor(t, func() bool { return f.m.Bus.Cursor() > before })
	cached := f.m.Bus.Cursor()
	close(resume)
	replayed := receive()
	if replayed.Seq != cached || replayed.Seq <= second.Seq || replayed.StringField("code") != "heartbeat" {
		t.Fatal("offline heartbeat was not replayed using the normal cursor")
	}
	if gaps.Load() != 0 || f.m.Budget.Count() != 0 {
		t.Fatal("unexpected gap or upstream request")
	}
}
