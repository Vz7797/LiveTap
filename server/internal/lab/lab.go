package lab

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
)

type Plan struct {
	Rooms           []string `json:"rooms"`
	Stages          []int    `json:"stages"`
	StartupSeconds  int      `json:"startup_seconds"`
	DurationSeconds int      `json:"duration_seconds"`
	Sharing         []bool   `json:"sharing"`
	Modes           []string `json:"modes"`
	ProxyID         string   `json:"proxy_id"`
	AccountID       string   `json:"account_id"`
}
type RoomSample struct {
	ID             string             `json:"id"`
	State          string             `json:"state"`
	Reason         string             `json:"reason"`
	Online         int64              `json:"online"`
	Group          string             `json:"group"`
	Disconnects    uint64             `json:"disconnects"`
	Recoveries     uint64             `json:"recoveries"`
	RecoveryMS     []int64            `json:"recovery_ms"`
	FalsePositives uint64             `json:"false_positives"`
	Requests       int                `json:"requests_per_minute"`
	RequestsTotal  uint64             `json:"requests_total"`
	Messages       model.Counts       `json:"messages"`
	NoData         bool               `json:"no_data"`
	Events         []model.StateEvent `json:"events"`
}
type Sample struct {
	At                 int64           `json:"at"`
	Mode               string          `json:"mode"`
	Sharing            bool            `json:"sharing"`
	Stage              int             `json:"stage"`
	Collecting         int             `json:"collecting"`
	Risk               int             `json:"risk"`
	Silent             int             `json:"silent"`
	CPU                float64         `json:"cpu"`
	MemoryMB           float64         `json:"memory_mb"`
	HomepageRequests   int             `json:"homepage_requests_per_minute"`
	NodeRequests       int             `json:"node_requests_per_minute"`
	BandwidthAvailable bool            `json:"bandwidth_available"`
	DownKBs            float64         `json:"down_kb_s"`
	UpKBs              float64         `json:"up_kb_s"`
	Rooms              []RoomSample    `json:"rooms"`
	Cooldown           engine.Cooldown `json:"cooldown"`
	UpstreamLag        hub.LagStats    `json:"upstream_lag"`
}
type RoomResult struct {
	ID                 string  `json:"id"`
	Disconnects        uint64  `json:"disconnects"`
	RecoveryMS         []int64 `json:"recovery_ms"`
	FalsePositives     uint64  `json:"false_positives"`
	Requests           uint64  `json:"requests"`
	State              string  `json:"state"`
	Reason             string  `json:"reason"`
	RecoveryIncomplete bool    `json:"recovery_incomplete"`
}
type GroupResult struct {
	Rooms          int          `json:"rooms"`
	Collecting     int          `json:"collecting"`
	Disconnects    uint64       `json:"disconnects"`
	FalsePositives uint64       `json:"false_positives"`
	Requests       uint64       `json:"requests"`
	Messages       model.Counts `json:"messages"`
}
type StageResult struct {
	Mode              string                 `json:"mode"`
	Sharing           bool                   `json:"sharing"`
	Size              int                    `json:"size"`
	Started           int64                  `json:"started"`
	Finished          int64                  `json:"finished"`
	Samples           int                    `json:"samples"`
	MinimumCollecting int                    `json:"minimum_collecting"`
	MaxMemoryMB       float64                `json:"max_memory_mb"`
	MaxCPU            float64                `json:"max_cpu"`
	Bandwidth         bool                   `json:"bandwidth_available"`
	MaxDownKBs        float64                `json:"max_down_kb_s"`
	MaxUpKBs          float64                `json:"max_up_kb_s"`
	Rooms             []RoomResult           `json:"rooms"`
	Unconnected       []RoomSample           `json:"startup_unconnected"`
	Groups            map[string]GroupResult `json:"groups"`
	initial           map[string]RoomSample
	last              map[string]RoomSample
	recoveries        map[string][]int64
	incomplete        map[string]bool
}
type Report struct {
	ID             string        `json:"id"`
	Status         string        `json:"status"`
	Started        int64         `json:"started"`
	Finished       int64         `json:"finished"`
	Plan           Plan          `json:"plan"`
	Stages         []StageResult `json:"stages"`
	Recent         []Sample      `json:"recent"`
	Detail         string        `json:"detail"`
	Conclusion     string        `json:"conclusion"`
	CurrentMode    string        `json:"current_mode"`
	CurrentSharing bool          `json:"current_sharing"`
	CurrentStage   int           `json:"current_stage"`
	Phase          string        `json:"phase"`
}
type Service struct {
	m        *room.Manager
	snapshot func() map[string]any
	mu       sync.Mutex
	active   string
	cancel   context.CancelFunc
	done     chan struct{}
	parent   context.Context
	now      func() time.Time
	wait     func(context.Context, time.Duration) bool
}

func New(ctx context.Context, m *room.Manager, snapshot func() map[string]any) (*Service, error) {
	_, e := m.Store.DB.Exec(`CREATE TABLE IF NOT EXISTS lab_runs(id TEXT PRIMARY KEY,at INTEGER NOT NULL,status TEXT NOT NULL,report TEXT NOT NULL);CREATE TABLE IF NOT EXISTS lab_samples(id INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL REFERENCES lab_runs(id) ON DELETE CASCADE,data TEXT NOT NULL);CREATE INDEX IF NOT EXISTS lab_samples_run ON lab_samples(run_id,id);`)
	if e != nil {
		return nil, e
	}
	s := &Service{m: m, snapshot: snapshot, parent: ctx, now: time.Now, wait: wait}
	// A restart must not leave test tasks silently collecting without an owner.
	records, e := m.Store.Rooms()
	if e != nil {
		return nil, e
	}
	for _, r := range records {
		if r.Source == "test" {
			if e = m.Remove(r.LiveID); e != nil {
				return nil, e
			}
		}
	}
	rows, e := s.List()
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if r.Status == "running" {
			r.Status = "interrupted"
			r.Detail = "节点重启，测试中断，测试房间已清理"
			r.Finished = s.now().UnixMilli()
			_ = s.save(r)
		}
	}
	return s, nil
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func Validate(p Plan, maxRooms int) error {
	if len(p.Rooms) == 0 || len(p.Rooms) > 2000 || len(p.Stages) == 0 || len(p.Stages) > 8 || len(p.Sharing) == 0 || len(p.Sharing) > 2 || len(p.Modes) == 0 || len(p.Modes) > 4 || p.StartupSeconds < 0 || p.StartupSeconds > 600 || p.DurationSeconds < 1 || p.DurationSeconds > 86400 {
		return errors.New("实验计划范围无效")
	}
	seen := map[string]bool{}
	for _, id := range p.Rooms {
		if _, e := room.ParseID(id); e != nil || len(id) < 9 {
			return errors.New("实验只接受数字直播间号")
		}
		for _, r := range id {
			if r < '0' || r > '9' {
				return errors.New("实验只接受数字直播间号")
			}
		}
		if seen[id] {
			return errors.New("直播间重复")
		}
		seen[id] = true
	}
	previous := 0
	for _, size := range p.Stages {
		if size <= previous || size > maxRooms || size > len(p.Rooms) {
			return errors.New("加量档位须递增，且不超过样本数或节点上限")
		}
		previous = size
	}
	modes := map[string]bool{}
	for _, mode := range p.Modes {
		if mode != "anonymous" && mode != "proxy" && mode != "account" && mode != "proxy_account" {
			return errors.New("实验模式无效")
		}
		if modes[mode] {
			return errors.New("实验模式重复")
		}
		modes[mode] = true
	}
	if len(p.Sharing) == 2 && p.Sharing[0] == p.Sharing[1] {
		return errors.New("共享身份对照值重复")
	}
	if len(p.Stages)*len(p.Sharing)*len(p.Modes)*(p.StartupSeconds+p.DurationSeconds) > 7*86400 {
		return errors.New("计划总时长不能超过 7 天")
	}
	return nil
}
func (s *Service) Start(p Plan) (Report, error) {
	if e := Validate(p, s.m.Settings().MaxRooms); e != nil {
		return Report{}, e
	}
	existing, _, _ := s.m.Views()
	for _, r := range existing {
		for _, id := range p.Rooms {
			if r["liveId"] == id {
				return Report{}, errors.New("测试样本与已有直播间冲突，请使用独立样本")
			}
		}
	}
	for _, mode := range p.Modes {
		for _, kind := range []string{"proxy", "account"} {
			if (kind == "proxy" && (mode == "proxy" || mode == "proxy_account")) || (kind == "account" && (mode == "account" || mode == "proxy_account")) {
				list, e := s.m.ResourceViews(kind)
				if e != nil {
					return Report{}, e
				}
				found := false
				for _, r := range list {
					selected := p.ProxyID
					if kind == "account" {
						selected = p.AccountID
					}
					if r.Enabled && (r.State == "healthy" || r.State == "unknown") && (selected == "" || selected == r.ID) && len(r.Rooms) < r.Limit {
						found = true
					}
				}
				if !found {
					return Report{}, errors.New("所选对照缺少可用代理或小号")
				}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return Report{}, errors.New("已有实验运行中")
	}
	report := Report{ID: auth.Token()[:16], Started: s.now().UnixMilli(), Status: "running", Plan: p, Stages: []StageResult{}, Recent: []Sample{}, Conclusion: "未完成；本次数据不能代替长时间稳定性观察"}
	if e := s.save(report); e != nil {
		return Report{}, e
	}
	ctx, cancel := context.WithCancel(s.parent)
	s.active = report.ID
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.run(ctx, report)
	_ = s.m.Store.Audit("lab_start", report.ID, "启动对照实验，报告不包含凭据和消息正文")
	return report, nil
}
func (s *Service) Stop(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.active {
		return errors.New("实验未在运行")
	}
	s.cancel()
	return s.m.Store.Audit("lab_stop", id, "请求停止实验并清理测试房间")
}
func (s *Service) Close() {
	s.mu.Lock()
	done := s.done
	if s.active != "" {
		s.cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
func (s *Service) save(r Report) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.m.Store.DB.Exec("INSERT INTO lab_runs VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,report=excluded.report", r.ID, r.Started, r.Status, string(b))
	return e
}
func (s *Service) List() ([]Report, error) {
	rows, e := s.m.Store.DB.Query("SELECT report FROM lab_runs ORDER BY at DESC LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var raw string
		var r Report
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) Get(id string) (Report, error) {
	var raw string
	var r Report
	e := s.m.Store.DB.QueryRow("SELECT report FROM lab_runs WHERE id=?", id).Scan(&raw)
	if e != nil {
		return r, errors.New("实验不存在")
	}
	e = json.Unmarshal([]byte(raw), &r)
	return r, e
}
func (s *Service) run(ctx context.Context, r Report) {
	owned := []string{}
	var current *StageResult
	cleanup := func() bool {
		remaining := []string{}
		for _, id := range owned {
			if e := s.m.Remove(id); e != nil && !errors.Is(e, room.ErrNotFound) {
				remaining = append(remaining, id)
			}
		}
		owned = remaining
		return len(remaining) == 0
	}
	defer func() {
		if current != nil {
			current.Finish(s.now())
			r.Stages = append(r.Stages, *current)
		}
		clean := cleanup()
		if ctx.Err() != nil {
			r.Status = "cancelled"
			r.Detail = "实验已停止，测试房间已清理"
		}
		if r.Status == "running" {
			r.Status = "completed"
			r.Detail = "计划观察已完成"
			r.Conclusion = Conclusion(r.Stages)
		}
		if !clean {
			r.Status = "failed"
			r.Detail = "测试连接已停止，但删除测试房间记录失败，请检查存储"
			slog.Error("实验房间记录清理失败")
		}
		r.Finished = s.now().UnixMilli()
		r.Phase = "finished"
		if e := s.save(r); e != nil {
			slog.Error("实验报告保存失败")
		}
		s.mu.Lock()
		s.active = ""
		close(s.done)
		s.mu.Unlock()
	}()
	fail := func(detail string) { r.Status = "failed"; r.Detail = detail }
	for _, mode := range r.Plan.Modes {
		for _, sharing := range r.Plan.Sharing {
			if !cleanup() {
				fail("清理上一组测试房间失败")
				return
			}
			r.CurrentMode = mode
			r.CurrentSharing = sharing
			for _, size := range r.Plan.Stages {
				if ctx.Err() != nil {
					return
				}
				r.CurrentStage = size
				r.Phase = "starting"
				for len(owned) < size {
					id := r.Plan.Rooms[len(owned)]
					share, _ := json.Marshal(sharing)
					overrides := model.Overrides{"cookie": json.RawMessage(`""`), "proxy_url": json.RawMessage(`""`), "share_guest_identity": share}
					_, created, e := s.m.AddConfigured(ctx, id, []string{"lab:" + r.ID}, false, "test", overrides)
					if e != nil || !created {
						fail("测试房间创建失败或已被其他任务占用")
						return
					}
					owned = append(owned, id)
					for _, kind := range []string{"proxy", "account"} {
						if (kind == "proxy" && (mode == "proxy" || mode == "proxy_account")) || (kind == "account" && (mode == "account" || mode == "proxy_account")) {
							rid := r.Plan.ProxyID
							if kind == "account" {
								rid = r.Plan.AccountID
							}
							policy := ""
							if rid == "" {
								policy = "least_used"
							}
							if e = s.m.Assign(id, kind, rid, policy); e != nil {
								fail("资源分配不足，实验停止并清理测试房间")
								return
							}
						}
					}
					if e = s.m.Action(id, "resume"); e != nil {
						fail("节点容量不足，实验停止")
						return
					}
				}
				if e := s.save(r); e != nil {
					fail("保存实验进度失败")
					return
				}
				if !s.wait(ctx, time.Duration(r.Plan.StartupSeconds)*time.Second) {
					return
				}
				r.Phase = "observing"
				first := s.sample(owned, mode, sharing, size)
				stage := BeginStage(first)
				current = &stage
				end := s.now().Add(time.Duration(r.Plan.DurationSeconds) * time.Second)
				stored := 0
				for {
					if ctx.Err() != nil {
						return
					}
					sample := s.sample(owned, mode, sharing, size)
					stage.Add(sample)
					summary := sample
					summary.Rooms = nil
					r.Recent = append(r.Recent, summary)
					if len(r.Recent) > 120 {
						r.Recent = r.Recent[len(r.Recent)-120:]
					}
					remaining := end.Sub(s.now())
					stored++
					if e := s.storeSample(r.ID, sample, stored%RoomDetailEvery == 1 || remaining <= 0); e != nil {
						fail("保存实验采样失败")
						return
					}
					if e := s.save(r); e != nil {
						fail("保存实验进度失败")
						return
					}
					if remaining <= 0 {
						break
					}
					if !s.wait(ctx, min(5*time.Second, remaining)) {
						return
					}
				}
				stage.Finish(s.now())
				r.Stages = append(r.Stages, stage)
				current = nil
				if e := s.save(r); e != nil {
					fail("保存实验结果失败")
					return
				}
			}
		}
	}
}

// Lab storage budget: the stage results already hold every per-room conclusion, so
// the raw sample stream keeps the node-level summary every 5 s and the per-room
// detail only once a minute and at the end of each stage, with each room's state
// events trimmed to the latest few. Stored rows are gzip-compressed.
const (
	RoomDetailEvery  = 12
	SampleEventsKept = 5
	compressedPrefix = "gz:"
	ListedRecentRuns = 1
)

func (s *Service) storeSample(runID string, sample Sample, detail bool) error {
	if detail {
		rooms := make([]RoomSample, len(sample.Rooms))
		for i, room := range sample.Rooms {
			if n := len(room.Events); n > SampleEventsKept {
				room.Events = room.Events[n-SampleEventsKept:]
			}
			rooms[i] = room
		}
		sample.Rooms = rooms
	} else {
		sample.Rooms = nil
	}
	data, e := json.Marshal(sample)
	if e != nil {
		return e
	}
	packed, e := pack(data)
	if e != nil {
		return e
	}
	_, e = s.m.Store.DB.Exec("INSERT INTO lab_samples(run_id,data) VALUES(?,?)", runID, packed)
	return e
}

func pack(data []byte) (string, error) {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, e := z.Write(data); e != nil {
		return "", e
	}
	if e := z.Close(); e != nil {
		return "", e
	}
	return compressedPrefix + base64.StdEncoding.EncodeToString(b.Bytes()), nil
}

// unpack accepts both compressed rows and plain JSON written by older versions.
func unpack(raw string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(raw, compressedPrefix)
	if !ok {
		return []byte(raw), nil
	}
	b, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return nil, e
	}
	z, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	defer z.Close()
	return io.ReadAll(z)
}

// Delete removes a finished report and its samples; a running one must be stopped first.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.active {
		return errors.New("实验运行中，请先停止")
	}
	if _, e := s.Get(id); e != nil {
		return e
	}
	if _, e := s.m.Store.DB.Exec("DELETE FROM lab_samples WHERE run_id=?", id); e != nil {
		return e
	}
	if _, e := s.m.Store.DB.Exec("DELETE FROM lab_runs WHERE id=?", id); e != nil {
		return e
	}
	return s.m.Store.Audit("lab_delete", id, "删除实验报告及采样")
}

// Listing returns reports for the lab page: only the newest keeps its recent curve,
// so the list stays small however many reports are stored.
func (s *Service) Listing() ([]Report, error) {
	list, e := s.List()
	for i := range list {
		if i >= ListedRecentRuns {
			list[i].Recent = []Sample{}
		}
	}
	return list, e
}

func Conclusion(stages []StageResult) string {
	best := 0
	for _, s := range stages {
		if s.Samples > 0 && s.MinimumCollecting == s.Size {
			best = max(best, s.Size)
		}
	}
	if best == 0 {
		return "本次没有完整稳定档位；请结合未连通原因、断线和冷却记录复核，不建议增加容量"
	}
	return fmt.Sprintf("本次采样均在线的最高档位为 %d 间；需复核断线、静默及样本热度；长时间稳定性需另行观察", best)
}
