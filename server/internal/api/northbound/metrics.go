package northbound

import (
	"slices"
	"strings"

	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

type ClientView struct {
	model.Client
	KeyPrefix   string    `json:"keyPrefix"`
	Used        int       `json:"used"`
	Connections int       `json:"connections"`
	PushPerSec  float64   `json:"pushPerSec"`
	BytesPerSec float64   `json:"bytesPerSec"`
	Queue       int       `json:"queue"`
	Dropped     uint64    `json:"dropped"`
	Spark       []float64 `json:"spark"`
}

func (s *Service) Metrics() ([]ClientView, hub.StreamStats, map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ClientView{}
	total := hub.StreamStats{}
	refs := map[string]int{}
	for _, sub := range s.subs {
		if sub.ExpiresAt.After(s.Now()) {
			refs[sub.LiveID]++
		}
	}
	for _, c := range s.clients {
		stat := s.Rooms.Bus.Stats(c.ID)
		out = append(out, ClientView{c, "lt_live_••••", s.count(c.ID), stat.Connections, stat.PushPerSec, stat.BytesPerSec, stat.Queue, stat.Dropped, []float64{}})
		total.Connections += stat.Connections
		total.Queue += stat.Queue
		total.QueueBytes += stat.QueueBytes
		total.PushPerSec += stat.PushPerSec
		total.BytesPerSec += stat.BytesPerSec
		total.Sent += stat.Sent
		total.Bytes += stat.Bytes
		total.Dropped += stat.Dropped
	}
	slices.SortFunc(out, func(a, b ClientView) int { return strings.Compare(a.ID, b.ID) })
	return out, total, refs
}
func (s *Service) Node() map[string]any {
	used, live := s.Rooms.Capacity()
	maxRooms := s.Rooms.Settings().MaxRooms
	cooldown := s.Rooms.Budget.Cooldown()
	health := 100
	abnormal := 0
	records, _ := s.Rooms.Store.Rooms()
	for _, r := range records {
		st, ok := s.Rooms.Status(r.LiveID)
		if ok && st.State.Abnormal() {
			abnormal++
		}
	}
	if used > 0 {
		health = max(0, 100-abnormal*100/used)
	}
	if cooldown.Active {
		health = min(health, 50)
	}
	load := float64(used) / float64(maxRooms)
	if cooldown.Active {
		load = 1
	}
	return map[string]any{"name": s.Rooms.NodeName, "version": Version, "api_version": APIVersion, "engine": "douyinlive", "engine_version": buildinfo.EngineLabel, "boot_id": s.Rooms.BootID, "capacity": map[string]int{"max": maxRooms, "used": used, "live": live}, "health": health, "load_score": load, "cooldown": cooldown, "seq": s.Rooms.Bus.Cursor()}
}
