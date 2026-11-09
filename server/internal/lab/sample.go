package lab

import (
	"sort"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (s *Service) sample(ids []string, mode string, sharing bool, size int) Sample {
	sample := Sample{At: s.now().UnixMilli(), Mode: mode, Sharing: sharing, Stage: size, Rooms: []RoomSample{}}
	node := s.snapshot()["node"].(map[string]any)
	sample.CPU, _ = node["cpu"].(float64)
	sample.MemoryMB, _ = node["memMB"].(float64)
	sample.HomepageRequests = s.m.Budget.HomepageCount()
	sample.NodeRequests = s.m.Budget.Count()
	sample.Cooldown = s.m.Budget.Cooldown()
	sample.BandwidthAvailable, _ = node["bandwidthAvailable"].(bool)
	if sample.BandwidthAvailable {
		sample.DownKBs, _ = node["downKBs"].(float64)
		sample.UpKBs, _ = node["upKBs"].(float64)
	}
	sample.UpstreamLag = s.m.Bus.UpstreamLag.Stats(time.Now(), time.Minute)
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	views, _, _ := s.m.Views()
	for _, v := range views {
		id := v["liveId"].(string)
		if !wanted[id] {
			continue
		}
		d := v["diagnostics"].(map[string]any)
		r := RoomSample{ID: id, State: v["status"].(string), Reason: v["reason"].(string), Online: v["online"].(int64), Disconnects: d["disconnects"].(uint64), Recoveries: d["reconnectRecoveryCount"].(uint64), RecoveryMS: d["reconnectDurationsMs"].([]int64), FalsePositives: d["silenceFalsePositives"].(uint64), Requests: d["requestsPerMinute"].(int), RequestsTotal: d["requestsTotal"].(uint64), Messages: v["counts"].(model.Counts), NoData: v["noData"].(bool)}
		r.Group = "unknown"
		r.Events, _ = v["events"].([]model.StateEvent)
		if model.RoomState(r.State).Live() {
			r.Group = "hot"
			if r.Online <= 5 {
				r.Group = "0-5"
			} else if r.Online <= 100 {
				r.Group = "6-100"
			}
			sample.Collecting++
		}
		// The lab counts rooms still trying (not missing ones) as at risk.
		if s := model.RoomState(r.State); s == model.StateRisk || s == model.StateReconnecting {
			sample.Risk++
		}
		if r.NoData {
			sample.Silent++
		}
		sample.Rooms = append(sample.Rooms, r)
	}
	return sample
}
func BeginStage(first Sample) StageResult {
	stage := StageResult{Mode: first.Mode, Sharing: first.Sharing, Size: first.Stage, Started: first.At, MinimumCollecting: first.Stage, Unconnected: []RoomSample{}, Rooms: []RoomResult{}, initial: map[string]RoomSample{}, last: map[string]RoomSample{}}
	stage.recoveries = map[string][]int64{}
	stage.incomplete = map[string]bool{}
	for _, r := range first.Rooms {
		stage.initial[r.ID] = r
		stage.last[r.ID] = r
		if !model.RoomState(r.State).Live() {
			stage.Unconnected = append(stage.Unconnected, r)
		}
	}
	return stage
}
func (s *StageResult) Add(v Sample) {
	s.Samples++
	s.MinimumCollecting = min(s.MinimumCollecting, v.Collecting)
	s.MaxCPU = max(s.MaxCPU, v.CPU)
	s.MaxMemoryMB = max(s.MaxMemoryMB, v.MemoryMB)
	if v.BandwidthAvailable {
		s.Bandwidth = true
		s.MaxDownKBs = max(s.MaxDownKBs, v.DownKBs)
		s.MaxUpKBs = max(s.MaxUpKBs, v.UpKBs)
	}
	for _, r := range v.Rooms {
		previous := s.last[r.ID]
		if r.Recoveries < previous.Recoveries {
			s.incomplete[r.ID] = true
		} else {
			count := r.Recoveries - previous.Recoveries
			if count > uint64(len(r.RecoveryMS)) {
				s.incomplete[r.ID] = true
			}
			count = min(count, uint64(len(r.RecoveryMS)))
			s.recoveries[r.ID] = append(s.recoveries[r.ID], r.RecoveryMS[len(r.RecoveryMS)-int(count):]...)
		}
		s.last[r.ID] = r
	}
}
func (s *StageResult) Finish(now time.Time) {
	s.Finished = now.UnixMilli()
	s.Groups = map[string]GroupResult{}
	for id, first := range s.initial {
		last := s.last[id]
		group := last.Group
		if group == "" {
			group = "unknown"
		}
		g := s.Groups[group]
		if g.Messages == nil {
			g.Messages = model.Zero()
		}
		g.Rooms++
		if model.RoomState(last.State).Live() {
			g.Collecting++
		}
		g.Disconnects += last.Disconnects - first.Disconnects
		g.FalsePositives += last.FalsePositives - first.FalsePositives
		g.Requests += last.RequestsTotal - first.RequestsTotal
		for kind, count := range last.Messages {
			g.Messages[kind] += max(0, count-first.Messages[kind])
		}
		s.Groups[group] = g
		durations := append([]int64{}, s.recoveries[id]...)
		s.Rooms = append(s.Rooms, RoomResult{ID: id, Disconnects: last.Disconnects - first.Disconnects, RecoveryMS: durations, RecoveryIncomplete: s.incomplete[id], FalsePositives: last.FalsePositives - first.FalsePositives, Requests: last.RequestsTotal - first.RequestsTotal, State: last.State, Reason: last.Reason})
	}
	sort.Slice(s.Rooms, func(i, j int) bool { return s.Rooms[i].ID < s.Rooms[j].ID })
	s.initial = nil
	s.last = nil
	s.recoveries = nil
	s.incomplete = nil
}
