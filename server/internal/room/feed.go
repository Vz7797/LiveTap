package room

import (
	"slices"
	"sort"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

// RecentEvents returns the newest in-memory messages across rooms, newest
// first: only types each room is configured to collect, never room_stats and,
// when types is non-empty, only those types. names maps live IDs to room names.
// Each room keeps its last 200 messages, so a type filter can be served in full
// even when other types dominate the global feed.
func (m *Manager) RecentEvents(types []string, limit int) ([]model.Event, map[string]string) {
	out := []model.Event{}
	names := map[string]string{}
	for _, t := range m.all() {
		t.mu.Lock()
		allowed := slices.Clone(t.options.EventTypes)
		names[t.record.LiveID] = t.record.Name
		t.mu.Unlock()
		for _, e := range t.buffer.Events() {
			if e.Type == "room_stats" || !slices.Contains(allowed, e.Type) || (len(types) > 0 && !slices.Contains(types, e.Type)) {
				continue
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, names
}
