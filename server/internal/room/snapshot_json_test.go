package room

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

// Regression (2026-10-16 review): rooms added by northbound subscriptions carry no
// tags; "tags": null made the admin UI throw and go blank. List fields must encode
// as [] — never null — for rooms with no tags, events or history yet.
func TestRoomViewsNeverEncodeNullLists(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := New(ctx, s, engine.NewBudget(100), func() engine.Engine { return &neverNetworkEngine{} }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	if _, _, e = m.AddConfigured(ctx, "323456789", nil, false, "subscription", nil); e != nil {
		t.Fatal(e)
	}
	views, feed, timeline := m.Views()
	raw, e := json.Marshal(map[string]any{"rooms": views, "feed": feed, "timeline": timeline})
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{`"tags":null`, `"events":null`, `"enabledTypes":null`, `"reconnectDurationsMs":null`, `"feed":null`, `"timeline":null`, `"spark":null`, `"minutes":null`} {
		if strings.Contains(string(raw), field) {
			t.Errorf("room view encodes %s", field)
		}
	}
	events, e := m.Events("323456789")
	if e != nil {
		t.Fatal(e)
	}
	if raw, _ = json.Marshal(events); string(raw) != "[]" {
		t.Errorf("events of a fresh room = %s, want []", raw)
	}
}
