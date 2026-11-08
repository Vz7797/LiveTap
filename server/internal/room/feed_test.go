package room

import (
	"context"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func TestRecentEventsFiltersByTypeAndOrdersNewestFirst(t *testing.T) {
	m, _, _ := dormantManager(t, "offline")
	if _, _, err := m.Add(context.Background(), "123456789", nil); err != nil {
		t.Fatal(err)
	}
	task := m.get("123456789")
	for i, typ := range []string{"chat", "member", "chat", "room_stats", "like", "chat"} {
		task.buffer.Add(model.Event{ID: string(rune('a' + i)), Seq: uint64(i + 1), Type: typ, LiveID: "123456789", ReceivedAt: time.Now()})
	}
	chats, names := m.RecentEvents([]string{"chat"}, 2)
	if len(chats) != 2 || chats[0].Seq != 6 || chats[1].Seq != 3 || names["123456789"] == "" {
		t.Fatalf("unexpected chats %+v", chats)
	}
	all, _ := m.RecentEvents(nil, 10)
	for _, e := range all {
		if e.Type == "room_stats" {
			t.Fatal("room_stats returned")
		}
	}
	if len(all) != 5 {
		t.Fatal("expected 5 messages", len(all))
	}
}
