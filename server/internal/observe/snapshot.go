package observe

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Snapshotter struct {
	m                         *room.Manager
	mu                        sync.RWMutex
	snapshot                  map[string]any
	proc                      *process.Process
	cpu, previousCPU          float64
	previousAt                time.Time
	cpuHist, memHist, reqHist []float64
	net                       bandwidth
	netHist, upHist           []float64
	series                    []map[string]any
	// previousCounts holds each room's cumulative counts at the last sample, so
	// the node rate is the sum of per-room deltas: a room that is removed (for
	// example released after its stream ended) must not turn the total negative.
	previousCounts map[string]model.Counts
	memoryLimit    uint64
	history        []store.Minute
	historyAt      time.Time
	historySince   time.Time
	cancel         context.CancelFunc
	done           chan struct{}
	upstream       atomic.Pointer[UpstreamCheck]
}

// SetUpstream attaches the kernel release check shown next to the version badge.
func (s *Snapshotter) SetUpstream(u *UpstreamCheck) { s.upstream.Store(u) }

func New(ctx context.Context, m *room.Manager) *Snapshotter {
	ctx, cancel := context.WithCancel(ctx)
	s := &Snapshotter{m: m, previousCounts: map[string]model.Counts{}, netHist: []float64{}, upHist: []float64{}, cancel: cancel, done: make(chan struct{})}
	s.proc, _ = process.NewProcess(int32(os.Getpid()))
	if v, e := mem.VirtualMemory(); e == nil {
		s.memoryLimit = v.Total
	}
	s.sample()
	go func() {
		defer close(s.done)
		tick := time.NewTicker(2500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.sample()
			}
		}
	}()
	return s
}
func (s *Snapshotter) Close() { s.cancel(); <-s.done }
func tail(v []float64, x float64) []float64 {
	v = append(v, x)
	if len(v) > 72 {
		v = v[len(v)-72:]
	}
	return v
}

// Closed minutes do not change, so each sample re-reads only the recent tail.
// A full reload of the window (up to two days of every room's minutes) runs
// hourly and when the day rolls over; re-reading it every 2.5 s made the node's
// CPU climb with the amount of stored history.
const (
	historyTail   = 5 * time.Minute
	historyReload = time.Hour
)

func (s *Snapshotter) refreshHistory(now, since time.Time) {
	full := s.historyAt.IsZero() || !s.historySince.Equal(since) || now.Sub(s.historyAt) >= historyReload
	from := since
	if !full {
		if tail := now.Truncate(time.Minute).Add(-historyTail); tail.After(since) {
			from = tail
		}
	}
	rows, err := s.m.MinuteTotals(from)
	if err != nil {
		slog.Error("读取分钟历史失败")
		return
	}
	if full {
		s.history, s.historyAt, s.historySince = rows, now, since
		return
	}
	keep := sort.Search(len(s.history), func(i int) bool { return s.history[i].At >= from.UnixMilli() })
	s.history = append(s.history[:keep:keep], rows...)
}
func (s *Snapshotter) sample() {
	now := time.Now()
	views, events, timeline := s.m.Views()
	cfg := s.m.Settings()
	rss := float64(0)
	if s.proc != nil {
		if info, e := s.proc.MemoryInfo(); e == nil {
			rss = float64(info.RSS) / (1024 * 1024)
		}
		if ts, e := s.proc.Times(); e == nil {
			cpu := ts.User + ts.System
			if !s.previousAt.IsZero() {
				s.cpu = max(0, (cpu-s.previousCPU)/now.Sub(s.previousAt).Seconds()*100)
			}
			s.previousCPU = cpu
		}
	}
	seconds := 2.5
	if !s.previousAt.IsZero() {
		seconds = now.Sub(s.previousAt).Seconds()
	}
	s.previousAt = now
	totals := model.Zero()
	current := map[string]model.Counts{}
	names := map[string]string{}
	collecting, abnormal, active := 0, 0, 0
	incidents := []map[string]any{}
	for _, v := range views {
		id := v["liveId"].(string)
		names[id] = v["name"].(string)
		roomCounts := v["counts"].(model.Counts)
		current[id] = roomCounts
		state := model.RoomState(v["status"].(string))
		if state.Live() {
			collecting++
		}
		if state.Active() {
			active++
		}
		if state.Abnormal() {
			abnormal++
			incidents = append(incidents, map[string]any{"id": v["liveId"], "at": v["since"], "level": "warning", "title": v["name"], "detail": v["reason"], "liveId": v["liveId"], "acked": false})
		}
	}
	rate := RateBetween(s.previousCounts, current, seconds)
	s.previousCounts = current
	s.series = append(s.series, map[string]any{"at": now.UnixMilli(), "v": rate})
	if len(s.series) > 72 {
		s.series = s.series[len(s.series)-72:]
	}
	// Minute aggregates contain counts only; message bodies never enter SQLite.
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	s.refreshHistory(now, day.AddDate(0, 0, -1))
	history := s.history
	buckets := map[int64]model.Counts{}
	for _, h := range history {
		if buckets[h.At] == nil {
			buckets[h.At] = model.Zero()
		}
		for typ, n := range h.Counts {
			buckets[h.At][typ] += n
			if h.At >= day.UnixMilli() {
				totals[typ] += n
			}
		}
	}
	today, yesterday := make([]int64, 48), make([]int64, 48)
	historical := []map[string]any{}
	for at, c := range buckets {
		tm := time.UnixMilli(at).In(now.Location())
		slot := (tm.Hour()*60 + tm.Minute()) / 30
		var sum int64
		for typ, n := range c {
			if typ != "room_stats" {
				sum += n
			}
		}
		if at >= day.UnixMilli() {
			today[slot] += sum
		} else {
			yesterday[slot] += sum
		}
		historical = append(historical, map[string]any{"at": at, "v": c})
	}
	sort.Slice(historical, func(i, j int) bool { return historical[i]["at"].(int64) < historical[j]["at"].(int64) })
	feed := []map[string]any{}
	for _, e := range events {
		if e.Type == "room_stats" {
			continue
		}
		feed = append(feed, FeedItem(e, names))
		if len(feed) >= 100 {
			break
		}
	}
	s.cpuHist = tail(s.cpuHist, s.cpu)
	s.memHist = tail(s.memHist, rss)
	if s.net.sample(now); s.net.ok {
		s.netHist = tail(s.netHist, s.net.down)
		s.upHist = tail(s.upHist, s.net.up)
	}
	reqs := s.m.Budget.Count()
	s.reqHist = tail(s.reqHist, float64(reqs))
	health := 100
	cooldown := s.m.Budget.Cooldown()
	if cooldown.Active {
		incidents = append(incidents, map[string]any{"id": "node-cooldown", "at": now.UnixMilli(), "level": "warning", "title": "节点冷却中", "detail": "暂停新的探测与建连，现有连接继续采集", "acked": false})
	}
	if active > 0 {
		health = 100 - abnormal*100/active
	}
	snapshot := map[string]any{"tick": now.Unix(), "rooms": views, "feed": feed, "seq": 0, "series": s.series, "totals": totals, "history": historical, "rhythm": map[string]any{"today": today, "yesterday": yesterday}, "incidents": incidents, "timeline": timeline, "logs": []any{}, "proxies": []any{}, "accounts": []any{}, "clients": []any{}, "rollout": s.m.Rollout(""), "node": map[string]any{"name": s.m.NodeName, "version": buildinfo.Version, "revision": buildinfo.Revision(), "engineVersion": buildinfo.EngineLabel, "engine": s.upstream.Load().Status(), "bootId": s.m.BootID, "startedAt": s.m.StartedAt.UnixMilli(), "cpu": s.cpu, "memMB": rss, "memLimitMB": float64(s.memoryLimit) / (1024 * 1024), "goroutines": runtime.NumGoroutine(), "upKBs": s.net.up, "downKBs": s.net.down, "bandwidthAvailable": s.net.ok, "conns": collecting, "reqPerMin": reqs, "reqBudgetPerSec": cfg.RequestsPerSecond, "maxRooms": cfg.MaxRooms, "health": health, "cpuHist": s.cpuHist, "memHist": s.memHist, "netHist": s.netHist, "upHist": s.upHist, "reqHist": s.reqHist, "latency": map[string]any{"upstream": s.m.Bus.UpstreamLag.Stats(now, time.Minute), "push": s.m.Bus.PushLag.Stats(now, time.Minute)}, "entryDrops": s.m.EntryDrops.Stats()}}
	node := snapshot["node"].(map[string]any)
	node["cooldown"] = cooldown
	node["homepageRequestsPerMinute"] = s.m.Budget.HomepageCount()
	node["shareGuestIdentity"] = cfg.ShareGuestIdentity
	s.mu.Lock()
	s.snapshot = snapshot
	s.mu.Unlock()
}
func (s *Snapshotter) Get() map[string]any { s.mu.RLock(); defer s.mu.RUnlock(); return s.snapshot }

// FeedItem is the admin view of one message (shared by the snapshot feed and /feed).
func FeedItem(e model.Event, names map[string]string) map[string]any {
	extra := map[string]any{}
	for k, v := range e.Extra {
		extra[k] = v
	}
	extra["gift"] = e.Extra["gift_name"]
	if e.Type == "gift" {
		extra["count"] = e.Extra["repeat_count"]
	}
	if e.Type == "social" {
		if e.Extra["action"] == uint64(1) {
			extra["action"] = "关注"
		} else {
			extra["action"] = "分享"
		}
	}
	return map[string]any{"id": e.ID, "seq": e.Seq, "type": e.Type, "liveId": e.LiveID, "roomName": names[e.LiveID], "nickname": e.User.Nickname, "avatar": e.User.Avatar, "content": e.Content, "extra": extra, "ts": e.ReceivedAt.UnixMilli()}
}

// RateBetween is the node message rate per minute from two samples of each
// room's cumulative counts. It sums per-room increases: a room that disappeared
// (released after its stream ended, removed by an administrator) contributes
// nothing instead of dragging the total below the previous sample, and a room
// seen for the first time contributes from the next sample on.
func RateBetween(previous, current map[string]model.Counts, seconds float64) model.Counts {
	rate := model.Zero()
	if seconds <= 0 {
		return rate
	}
	for id, counts := range current {
		before, ok := previous[id]
		if !ok {
			continue
		}
		for typ, n := range counts {
			rate[typ] += max(0, n-before[typ])
		}
	}
	for typ, n := range rate {
		rate[typ] = int64(float64(n) / seconds * 60)
	}
	return rate
}
