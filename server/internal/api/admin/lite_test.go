package admin

import (
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func TestLiteSnapshotDropsDetailOnlyData(t *testing.T) {
	events := []model.StateEvent{{Kind: "probe"}, {Kind: "online"}, {Kind: "ready"}, {Kind: "reconnect"}, {Kind: "ready"}}
	room := map[string]any{"liveId": "1", "status": "collecting", "spark": []int64{1, 2}, "tags": []string{}, "events": events,
		"minutes": []any{1}, "options": map[string]any{}, "overrides": []string{}, "enabledTypes": []string{}, "diagnostics": map[string]any{}, "counts": map[string]any{}}
	feed := make([]map[string]any, 120)
	full := map[string]any{"rooms": []map[string]any{room}, "history": []any{1}, "feed": feed, "node": map[string]any{}}

	lite := liteSnapshot(full)
	if _, ok := lite["history"]; ok {
		t.Fatal("history must be fetched separately")
	}
	r := lite["rooms"].([]map[string]any)[0]
	for _, k := range detailOnly {
		if _, ok := r[k]; ok {
			t.Fatalf("lite room still carries %s", k)
		}
	}
	if got := r["events"].([]model.StateEvent); len(got) != liteEvents || got[len(got)-1] != events[len(events)-1] {
		t.Fatalf("lite events = %v", got)
	}
	if len(lite["feed"].([]map[string]any)) != liteFeed {
		t.Fatal("feed not trimmed")
	}
	// the full snapshot is not mutated
	if _, ok := full["history"]; !ok || len(room["events"].([]model.StateEvent)) != len(events) || room["minutes"] == nil {
		t.Fatal("liteSnapshot mutated its input")
	}
}
