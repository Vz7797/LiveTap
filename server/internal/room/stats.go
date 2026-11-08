package room

import (
	"sort"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func cloneCounts(c model.Counts) model.Counts {
	out := model.Zero()
	for k, v := range c {
		out[k] = v
	}
	return out
}

// Serialize against flush, then add only the delta not yet persisted.
func (m *Manager) MinuteTotals(since time.Time) ([]store.Minute, error) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	rows, err := m.Store.MinuteTotals(since)
	if err != nil {
		return nil, err
	}
	totals := map[int64]model.Counts{}
	for _, row := range rows {
		totals[row.At] = cloneCounts(row.Counts)
	}
	for _, t := range m.all() {
		t.mu.Lock()
		for at, c := range t.minutes {
			if at < since.UnixMilli() {
				continue
			}
			if totals[at] == nil {
				totals[at] = model.Zero()
			}
			for typ, n := range c {
				totals[at][typ] += max(0, n-t.persistedMinutes[at][typ])
			}
		}
		t.mu.Unlock()
	}
	out := make([]store.Minute, 0, len(totals))
	for at, c := range totals {
		out = append(out, store.Minute{At: at, Counts: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}
