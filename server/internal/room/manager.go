package room

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/pool"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

var ErrCapacity = errors.New("节点容量已满")
var ErrNotFound = errors.New("直播间不存在")

type task struct {
	mu                     sync.Mutex
	record                 model.RoomRecord
	options                model.Options
	state, reason          string
	since                  time.Time
	ctx                    context.Context
	cancel                 context.CancelFunc
	runCancel              context.CancelFunc
	wake                   chan struct{}
	done                   chan struct{}
	last                   atomic.Int64
	lastMessage            atomic.Int64
	buffer                 *hub.Buffer
	silence                Silence
	minutes                map[int64]model.Counts
	onlineMinutes          map[int64]int64 // 每分钟最后一次上报的在线人数，只留最近 30 分钟
	dirtyMinutes           map[int64]bool
	persistedMinutes       map[int64]model.Counts
	counts                 model.Counts
	events                 []model.StateEvent
	reconnects             []time.Time
	rebuilds               []time.Time
	noData                 bool
	minuteIssues           map[int64][2]int // 每分钟的上游重连次数与静默重建次数，随分钟统计落库
	disconnects            uint64
	disconnectedAt         time.Time
	reconnectDurations     []int64
	reconnectRecoveryCount uint64
	silenceFalsePositives  uint64
	online, totalPV        int64
	dropped                atomic.Int64
	drops                  hub.DropCounts // dropped split by reason and type, across reconnects
	queue                  *hub.Queue
	statusEvent            func(model.Event)
	waitingSince           time.Time
	waitReason             string
}
type Rollout struct {
	Running bool   `json:"running"`
	Paused  bool   `json:"paused"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
}

// A merchant's stream that has ended is not probed all night: after the stream
// ends the task keeps checking for EndedGrace (a quick restart is common), then
// stops sending upstream requests until resumed or re-subscribed. A manual or
// test task that never went live stops after NeverLiveLimit; subscription tasks
// use the subscription's idle_release_seconds instead. Manual tasks dormant for
// DormantRetention since they were last live are removed. (Variables so tests
// can shorten them.)
var (
	EndedGrace       = 10 * time.Minute
	NeverLiveLimit   = time.Hour
	DormantRetention = 30 * 24 * time.Hour
)

type Manager struct {
	// Subscribers, when set, returns the external subscriptions per live ID.
	Subscribers func() map[string][]model.SubscriberRef

	mu             sync.RWMutex
	ops            sync.Mutex
	statsMu        sync.Mutex
	tasks          map[string]*task
	settings       model.Settings
	Store          *store.Store
	Pools          *pool.Service
	poolCursor     map[string]int
	Budget, starts *engine.Budget
	factory        engine.Factory
	ctx            context.Context
	cancel         context.CancelFunc
	Bus            *hub.Bus
	witness        chan struct{}
	BootID         string
	StartedAt      time.Time
	NodeName       string
	rollout        Rollout
	rollCancel     context.CancelFunc
	persistDone    chan struct{}
	// EntryDrops counts every entry-queue drop since start, including rooms since removed.
	EntryDrops hub.DropCounts
}

func New(parent context.Context, s *store.Store, b *engine.Budget, f engine.Factory, name string) (*Manager, error) {
	cfg, e := s.Settings()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(parent)
	pools, e := pool.New(s, b)
	if e != nil {
		cancel()
		return nil, e
	}
	m := &Manager{tasks: map[string]*task{}, settings: cfg, Store: s, Budget: b, starts: engine.NewBudget(cfg.StartsPerSecond), factory: f, ctx: ctx, cancel: cancel, witness: make(chan struct{}, 3), BootID: auth.Token(), StartedAt: time.Now(), NodeName: name}
	m.Pools = pools
	m.Bus = hub.NewBus()
	m.poolCursor = map[string]int{}
	b.OnCooldown(func(c engine.Cooldown) {
		message := fmt.Sprintf("节点受限熔断，暂停新的探测与建连 %d 秒；现有连接保持采集；触发原因：%s", c.RemainingSeconds, c.Reason)
		if err := s.Incident("", "node_cooldown", message); err != nil {
			slog.Error("写入节点冷却告警失败")
		}
		slog.Warn("节点受限熔断", "cooldown_seconds", c.RemainingSeconds, "strikes", c.Strikes, "trigger", c.Reason)
	})
	records, e := s.Rooms()
	if e != nil {
		cancel()
		return nil, e
	}
	recent, e := s.Minutes(time.Now().Add(-31 * time.Minute))
	if e != nil {
		cancel()
		return nil, e
	}
	for _, r := range records {
		if (r.Source == "test" || r.Source == "subscription") && r.Enabled {
			r.Enabled = false
			if e = s.SaveRoom(r); e != nil {
				cancel()
				return nil, e
			}
		}
		m.install(r, recent)
	}
	m.persistDone = make(chan struct{})
	go m.persistLoop()
	return m, nil
}
func (m *Manager) Settings() model.Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v := m.settings
	v.Defaults.EventTypes = slices.Clone(v.Defaults.EventTypes)
	return v
}
func (m *Manager) all() []*task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
	return out
}
func (m *Manager) get(id string) *task { m.mu.RLock(); defer m.mu.RUnlock(); return m.tasks[id] }
func (m *Manager) install(r model.RoomRecord, recent []store.Minute) {
	ctx, cancel := context.WithCancel(m.ctx)
	o, _ := model.Merge(m.settings.Defaults, r.Overrides)
	t := &task{record: r, options: o, state: "probing", reason: queuedText, since: time.Now(), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), buffer: hub.NewBuffer(), minutes: map[int64]model.Counts{}, onlineMinutes: map[int64]int64{}, counts: model.Zero()}
	t.dirtyMinutes = map[int64]bool{}
	t.persistedMinutes = map[int64]model.Counts{}
	t.statusEvent = func(e model.Event) { m.Bus.Publish(e, nil) }
	for _, minute := range recent {
		if minute.LiveID == r.LiveID {
			t.minutes[minute.At] = minute.Counts
			t.persistedMinutes[minute.At] = cloneCounts(minute.Counts)
		}
	}
	m.tasks[r.LiveID] = t
	go m.supervise(t)
}
func (m *Manager) used() int {
	n := 0
	for _, t := range m.all() {
		t.mu.Lock()
		if t.record.Enabled && !t.record.Dormant() {
			n++
		}
		t.mu.Unlock()
	}
	return n
}
func (m *Manager) Add(ctx context.Context, input string, tags []string) (model.RoomRecord, bool, error) {
	return m.AddConfigured(ctx, input, tags, true, "manual", nil)
}
func (m *Manager) AddConfigured(ctx context.Context, input string, tags []string, enabled bool, source string, overrides model.Overrides) (model.RoomRecord, bool, error) {
	id, e := Resolve(ctx, input, m.Budget)
	if e != nil {
		return model.RoomRecord{}, false, e
	}
	m.ops.Lock()
	defer m.ops.Unlock()
	if t := m.get(id); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if source == "manual" && t.record.Source == "subscription" {
			t.record.Source = "manual"
			if err := m.Store.SaveRoom(t.record); err != nil {
				return model.RoomRecord{}, false, err
			}
		}
		return t.record, false, nil
	}
	if enabled && m.used() >= m.Settings().MaxRooms {
		return model.RoomRecord{}, false, ErrCapacity
	}
	if len(tags) > 20 {
		return model.RoomRecord{}, false, errors.New("标签最多 20 个")
	}
	for _, v := range tags {
		if len(v) > 100 {
			return model.RoomRecord{}, false, errors.New("标签过长")
		}
	}
	if overrides == nil {
		overrides = model.Overrides{}
	}
	if e = model.ValidateOverrides(overrides); e != nil {
		return model.RoomRecord{}, false, e
	}
	o, e := model.Merge(m.Settings().Defaults, overrides)
	if e != nil {
		return model.RoomRecord{}, false, e
	}
	if e = o.Validate(); e != nil {
		return model.RoomRecord{}, false, e
	}
	if e = m.validateResources(id, o); e != nil {
		return model.RoomRecord{}, false, e
	}
	r := model.RoomRecord{LiveID: id, Name: id, Source: source, Tags: tags, Enabled: enabled, CreatedAt: time.Now(), Overrides: overrides}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	if e = m.Store.SaveRoom(r); e != nil {
		return r, false, e
	}
	m.mu.Lock()
	m.install(r, nil)
	m.mu.Unlock()
	_ = m.Store.Audit("room_add", id, "添加手动采集任务")
	return r, true, nil
}
func (t *task) signal() {
	if t.runCancel != nil {
		t.runCancel()
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) Action(id, action string) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	t := m.get(id)
	if t == nil {
		return ErrNotFound
	}
	if action == "resume" || action == "reconnect" {
		t.mu.Lock()
		idle := !t.record.Enabled || t.record.Dormant()
		t.mu.Unlock()
		if idle && m.used() >= m.Settings().MaxRooms {
			return ErrCapacity
		}
	}
	t.mu.Lock()
	r := t.record
	switch action {
	case "pause":
		r.Enabled = false
	case "resume", "reconnect":
		if action == "reconnect" && !r.Enabled {
			t.mu.Unlock()
			return errors.New("已停止的直播间请先恢复")
		}
		r.Enabled = true
		r.EndedAt, r.EndedReason = 0, ""
	default:
		t.mu.Unlock()
		return errors.New("未知操作")
	}
	if action == "pause" {
		r.EndedAt, r.EndedReason = 0, ""
	}
	if e := m.Store.SaveRoom(r); e != nil {
		t.mu.Unlock()
		return e
	}
	t.record = r
	t.signal()
	t.mu.Unlock()
	return m.Store.Audit("room_"+action, id, "更新采集任务")
}
func (m *Manager) Remove(id string) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	return m.removeLocked(id)
}
func (m *Manager) removeLocked(id string) error {
	t := m.get(id)
	if t == nil {
		return ErrNotFound
	}
	t.cancel()
	<-t.done
	m.flush(t)
	if e := m.Store.DeleteRoom(id); e != nil {
		return e
	}
	m.mu.Lock()
	delete(m.tasks, id)
	m.mu.Unlock()
	m.Bus.ForgetRoom(id)
	m.Budget.Forget(id)
	return m.Store.Audit("room_remove", id, "移除采集任务")
}
func (m *Manager) Update(id string, note *string, tags *[]string, o model.Overrides, inheritAll bool) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	t := m.get(id)
	if t == nil {
		return ErrNotFound
	}
	cfg := m.Settings()
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.record
	if note != nil {
		if len(*note) > 2000 {
			return errors.New("备注过长")
		}
		r.Note = *note
	}
	if tags != nil {
		if len(*tags) > 20 {
			return errors.New("标签最多 20 个")
		}
		r.Tags = slices.Clone(*tags)
	}
	if o != nil || inheritAll {
		if e := model.ValidateOverrides(o); e != nil {
			return e
		}
		nextOverrides := model.Overrides{}
		if !inheritAll {
			for k, v := range r.Overrides {
				nextOverrides[k] = v
			}
			for k, v := range o {
				nextOverrides[k] = v
			}
		}
		r.Overrides = nextOverrides
	}
	next, e := model.Merge(cfg.Defaults, r.Overrides)
	if e != nil {
		return e
	}
	if e = next.Validate(); e != nil {
		return e
	}
	// This task is already locked; other tasks are checked separately.
	if e = m.validateResourcesLocked(id, next); e != nil {
		return e
	}
	next, e = m.Pools.Resolve(next)
	if e != nil {
		return e
	}
	if e = m.Store.SaveRoom(r); e != nil {
		return e
	}
	rebuild := !model.ConnectionEqual(t.options, next)
	t.record = r
	t.options = next
	if rebuild {
		t.signal()
	}
	return m.Store.Audit("room_update", id, "更新备注、标签或单独配置；敏感值不记录")
}
func (m *Manager) ChangeSettings(cfg model.Settings, apply string) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	if apply != "new" && apply != "rolling" {
		return errors.New("请选择仅新任务或滚动应用")
	}
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.RLock()
	busy := m.rollout.Running
	m.mu.RUnlock()
	if busy {
		return errors.New("已有滚动应用任务，请先完成或取消")
	}
	if e := m.Store.SaveSettings(cfg); e != nil {
		return e
	}
	m.mu.Lock()
	m.settings = cfg
	m.mu.Unlock()
	m.Budget.Set(cfg.RequestsPerSecond)
	m.starts.Set(cfg.StartsPerSecond)
	targets := []string{}
	for _, t := range m.all() {
		t.mu.Lock()
		effective, _ := model.Merge(cfg.Defaults, t.record.Overrides)
		if resolved, err := m.Pools.Resolve(effective); err == nil {
			effective = resolved
		}
		t.options.EventTypes = effective.EventTypes
		t.options.PollSeconds = effective.PollSeconds
		t.options.SilenceSeconds = effective.SilenceSeconds
		if t.record.Enabled && !model.ConnectionEqual(effective, t.options) {
			targets = append(targets, t.record.LiveID)
		}
		t.mu.Unlock()
	}
	if apply == "rolling" && len(targets) > 0 {
		sort.Strings(targets)
		ctx, cancel := context.WithCancel(m.ctx)
		m.mu.Lock()
		m.rollCancel = cancel
		m.rollout = Rollout{Running: true, Total: len(targets)}
		m.mu.Unlock()
		go m.roll(ctx, targets, cfg.RollingSeconds)
	}
	return m.Store.Audit("settings_update", "node", "应用方式："+apply+"；敏感值不记录")
}
func (m *Manager) roll(ctx context.Context, ids []string, seconds int) {
	defer func() { m.mu.Lock(); m.rollout.Running = false; m.mu.Unlock() }()
	for _, id := range ids {
		for {
			m.mu.RLock()
			paused := m.rollout.Paused
			m.mu.RUnlock()
			if !paused {
				break
			}
			if !sleep(ctx, time.Second) {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if e := m.Action(id, "reconnect"); e != nil {
			m.mu.Lock()
			m.rollout.Error = "有任务未能重建，请检查房间状态"
			m.mu.Unlock()
		}
		m.mu.Lock()
		m.rollout.Done++
		m.mu.Unlock()
		if !sleep(ctx, time.Duration(seconds)*time.Second) {
			return
		}
	}
}
func (m *Manager) Rollout(action string) Rollout {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch action {
	case "pause":
		m.rollout.Paused = true
	case "resume":
		m.rollout.Paused = false
	case "cancel":
		if m.rollCancel != nil {
			m.rollCancel()
		}
	}
	return m.rollout
}
func (m *Manager) Close() {
	m.cancel()
	<-m.persistDone
	for _, t := range m.all() {
		<-t.done
		m.flush(t)
	}
}
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (t *task) setState(state, reason string) {
	t.mu.Lock()
	if t.state == state && t.reason == reason {
		t.mu.Unlock()
		return
	}
	// A poll that only confirms nothing changed (waiting → probing → the same
	// waiting reason) is not a new event: the state is updated, but the timeline
	// and subscribers are not told about every check.
	quiet := (state == "probing" && t.state == "waiting") || (state == "waiting" && t.state == "probing" && reason == t.waitReason)
	if state == "waiting" {
		t.waitReason = reason
	}
	if state == "reconnecting" && t.state != "reconnecting" {
		t.reconnects = append(t.reconnects, time.Now())
		t.countMinute(time.Now(), 1, 0)
	}
	if t.state == "collecting" && state != "collecting" && state != "stopped" && state != "waiting" && state != "notfound" {
		t.disconnects++
		t.disconnectedAt = time.Now()
	}
	if state == "collecting" && !t.disconnectedAt.IsZero() {
		t.reconnectRecoveryCount++
		t.reconnectDurations = append(t.reconnectDurations, time.Since(t.disconnectedAt).Milliseconds())
		if len(t.reconnectDurations) > 200 {
			t.reconnectDurations = t.reconnectDurations[len(t.reconnectDurations)-200:]
		}
		t.disconnectedAt = time.Time{}
	}
	if state == "stopped" || state == "waiting" || state == "notfound" || state == "ended" {
		t.disconnectedAt = time.Time{}
	}
	t.state = state
	t.reason = reason
	t.since = time.Now()
	if state == "waiting" && t.waitingSince.IsZero() {
		t.waitingSince = t.since
	}
	if state != "waiting" && state != "probing" {
		t.waitingSince = time.Time{}
	}
	if quiet {
		t.mu.Unlock()
		return
	}
	kind := map[string]string{"probing": "probe", "collecting": "ready", "connecting": "online", "reconnecting": "reconnect", "waiting": "offline", "risk": "risk", "stopped": "offline", "ended": "offline", "notfound": "risk"}[state]
	t.events = append(t.events, model.StateEvent{At: time.Now().UnixMilli(), Kind: kind, Text: reason})
	if len(t.events) > 50 {
		t.events = t.events[len(t.events)-50:]
	}
	e := model.Event{Type: "status", LiveID: t.record.LiveID, RoomID: t.record.RoomID, TS: t.since, ReceivedAt: t.since, Extra: map[string]any{"state": state, "reason": reason}}
	fn := t.statusEvent
	t.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}
func (t *task) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-t.ctx.Done():
		return false
	case <-t.wake:
		return true
	case <-timer.C:
		return true
	}
}
func (m *Manager) waitStart(ctx context.Context) error {
	for {
		if err := m.Budget.WaitReady(ctx); err != nil {
			return err
		}
		if err := m.starts.Wait(ctx); err != nil {
			return err
		}
		if !m.Budget.Cooldown().Active {
			return nil
		}
	}
}
func (m *Manager) supervise(t *task) {
	defer close(t.done)
	a := m.factory()
	defer a.Close()
	idle := time.Now()
	failures := 0
	// wasLive: the task collected since it was last started or resumed, so an
	// offline result means the stream ended rather than has not started yet.
	// After a node restart (subscription tasks come back paused until the
	// northbound service re-ensures them) or a pause, a task that was live
	// recently keeps the 10-minute grace instead of the 1-hour never-live wait.
	// Resuming a dormant task starts a new session.
	recentlyLive := func() bool {
		t.mu.Lock()
		defer t.mu.Unlock()
		r := t.record
		return r.LastLiveAt != 0 && !r.Dormant() && time.Since(time.UnixMilli(r.LastLiveAt)) < 24*time.Hour
	}
	wasLive, parked, parkedDormant := recentlyLive(), false, false
	for t.ctx.Err() == nil {
		cfg := m.Settings()
		t.mu.Lock()
		// Consume the signal together with taking the next configuration snapshot.
		select {
		case <-t.wake:
		default:
		}
		enabled := t.record.Enabled
		id := t.record.LiveID
		overrides := t.record.Overrides
		if !enabled || t.record.Dormant() {
			state, reason := "stopped", "已手动停止"
			if enabled {
				state, reason = "ended", endedText(t.record.EndedReason)
			}
			parkedDormant = enabled
			t.mu.Unlock()
			t.setState(state, reason)
			parked = true
			if !t.wait(24 * time.Hour) {
				return
			}
			continue
		}
		if parked {
			idle, failures, parked = time.Now(), 0, false
			// Resumed: show the start queue instead of the old stopped/ended state.
			t.mu.Unlock()
			wasLive = !parkedDormant && recentlyLive()
			t.setState("probing", queuedText)
			t.mu.Lock()
		}
		o, e := model.Merge(cfg.Defaults, overrides)
		if e == nil {
			o, e = m.Pools.Resolve(o)
		}
		if e != nil {
			t.mu.Unlock()
			t.setState("risk", "已分配资源不可用，请检查代理或小号")
			if !t.wait(30 * time.Second) {
				return
			}
			continue
		}
		t.options = o
		ctx, cancel := context.WithCancel(t.ctx)
		t.runCancel = cancel
		t.mu.Unlock()
		spec := engine.RoomSpec{LiveID: id, Options: o, ShareGuestIdentity: cfg.ShareGuestIdentity}
		if o.GuestSharing != nil {
			spec.ShareGuestIdentity = *o.GuestSharing
		}
		if m.Budget.Cooldown().Active {
			t.setState("risk", "节点冷却中，等待恢复探测")
		}
		if e = m.waitStart(ctx); e != nil {
			cancel()
			continue
		}
		t.setState("probing", "正在确认直播状态")
		probeCtx, probeCancel := context.WithTimeout(ctx, 25*time.Second)
		status, err := a.Probe(probeCtx, spec)
		if errors.Is(err, engine.ErrAccessRestricted) {
			m.Budget.RestrictBecause("supervisor_probe")
		}
		probeCancel()
		if ctx.Err() != nil {
			cancel()
			continue
		}
		if status.Name != "" {
			t.mu.Lock()
			t.record.Name = status.Name
			t.record.Title = status.Title
			t.record.RoomID = status.RoomID
			if status.Avatar != "" {
				t.record.Avatar = status.Avatar
			}
			_ = m.Store.SaveRoom(t.record)
			t.mu.Unlock()
		}
		delay := time.Duration(0)
		switch {
		case status.Code == "region_blocked":
			// Not a node restriction: the room is closed to this region. Stop probing it
			// (like not_found) instead of backing off forever as an unknown state.
			t.mu.Lock()
			t.record.Enabled = false
			_ = m.Store.SaveRoom(t.record)
			t.mu.Unlock()
			t.setState("notfound", regionBlockedText)
			_ = m.Store.Incident(id, "region_blocked", regionBlockedText)
			delay = 24 * time.Hour
		case status.Code == "not_found":
			t.mu.Lock()
			t.record.Enabled = false
			_ = m.Store.SaveRoom(t.record)
			t.mu.Unlock()
			t.setState("notfound", "上游已确认直播间不存在")
			delay = 24 * time.Hour
		case errors.Is(err, engine.ErrAccessUncertain) || status.Code == "offline" || status.Code == "account_no_room":
			// No clear answer from the upstream counts as offline: in long tests it
			// almost always meant the stream had ended, and the end grace below still
			// picks up a stream that comes back.
			failures = 0
			if reason := m.endReason(t, wasLive, time.Since(idle)); reason != "" {
				m.markEnded(t, reason)
				cancel()
				continue
			}
			if wasLive {
				t.setState("waiting", endingText())
				delay = EndedPollDelay(time.Since(idle))
			} else {
				t.setState("waiting", "未开播，等待下一次探测")
				delay = PollDelay(time.Since(idle), o.PollSeconds)
			}
		case err != nil || status.Code == "unknown" || status.Code == "":
			restricted := errors.Is(err, engine.ErrAccessRestricted)
			timeout := errors.Is(err, engine.ErrProbeTimeout)
			// A stream that stays unconfirmable after it stopped is treated as ended
			// once the grace has passed, instead of being probed forever. Restrictions
			// and timeouts say nothing about the stream, so they never end a task.
			if !restricted && !timeout {
				if reason := m.endReason(t, wasLive, time.Since(idle)); reason != "" {
					m.markEnded(t, reason)
					cancel()
					continue
				}
			}
			failures++
			reason := "暂时无法确认直播状态，正在退避重试"
			delay = backoff(failures)
			if restricted {
				reason = "上游匿名访问受限，正在退避；可检查 Cookie 或代理配置"
			} else if timeout {
				reason = "直播状态探测超时，稍后重试"
				delay = ProbeTimeoutRetry
			}
			t.setState("risk", reason)
		default:
			wasLive = true
			started := time.Now()
			m.markLive(t, started)
			result := m.collect(ctx, t, a, spec)
			idle = time.Now()
			if time.Since(started) >= CollectStableAfter {
				failures = 0
			}
			if ctx.Err() != nil {
				cancel()
				continue
			}
			if result == "rebuild" {
				delay = 0
			} else if result == "limited" {
				t.mu.Lock()
				delay = max(time.Second, time.Until(t.rebuilds[0].Add(30*time.Minute)))
				t.mu.Unlock()
				t.setState("risk", "30 分钟内已重建 3 次，等待重建额度恢复")
			} else if result == "offline" {
				t.setState("waiting", endingText())
				delay = EndedPollDelay(0)
			} else {
				failures++
				delay = CollectRetryDelay(failures)
				// The stream was live moments ago: show it as reconnecting, not unconfirmed.
				t.setState("reconnecting", fmt.Sprintf("上游连接中断，%d 秒后重新连接", int(delay/time.Second)))
			}
		}
		cancel()
		t.mu.Lock()
		t.runCancel = nil
		t.mu.Unlock()
		if delay > 0 && !t.wait(delay) {
			return
		}
	}
}

// endReason decides whether an offline task should stop probing.
func (m *Manager) endReason(t *task, wasLive bool, offlineFor time.Duration) string {
	if wasLive {
		if offlineFor >= EndedGrace {
			return "live_ended"
		}
		return ""
	}
	t.mu.Lock()
	source := t.record.Source
	t.mu.Unlock()
	if source != "subscription" && offlineFor >= NeverLiveLimit {
		return "never_live"
	}
	return ""
}

func (m *Manager) markEnded(t *task, reason string) {
	t.mu.Lock()
	t.record.EndedAt = time.Now().UnixMilli()
	t.record.EndedReason = reason
	_ = m.Store.SaveRoom(t.record)
	id := t.record.LiveID
	t.mu.Unlock()
	_ = m.Store.Incident(id, "ended", endedText(reason))
}

func (m *Manager) markLive(t *task, at time.Time) {
	t.mu.Lock()
	t.record.LastLiveAt = at.UnixMilli()
	_ = m.Store.SaveRoom(t.record)
	t.mu.Unlock()
}

// regionBlockedText explains a room Douyin does not open to the node's region.
const regionBlockedText = "该直播间在节点所在地区不开放（抖音地区限制），已停止检测"

// queuedText is shown while a new or resumed task waits for the start limiter,
// so a new subscription never reports "stopped".
const queuedText = "排队等待启动"

// EndedPollDelay paces the checks after a stream ended: every 30 s for the
// first 2 minutes (a quick restart after a dropped stream is caught within half
// a minute), then every 2 minutes until EndedGrace stops probing. About 8
// requests per ended room instead of ~30 at the waiting-for-start pace.
func EndedPollDelay(offlineFor time.Duration) time.Duration {
	if offlineFor < 2*time.Minute {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func endingText() string {
	return fmt.Sprintf("直播已结束，%d 分钟内未重新开播将停止检测", int(EndedGrace/time.Minute))
}

func endedText(reason string) string {
	if reason == "never_live" {
		return fmt.Sprintf("%d 小时内未开播，已停止检测", int(NeverLiveLimit/time.Hour))
	}
	return "已下播，已停止检测"
}

// PruneDormant removes manual tasks that have been dormant and not live for
// DormantRetention. Subscription tasks are released by their subscriptions and
// test tasks by the lab, so only manual tasks are considered.
func (m *Manager) PruneDormant(now time.Time) []string {
	removed := []string{}
	for _, t := range m.all() {
		t.mu.Lock()
		r := t.record
		t.mu.Unlock()
		last := r.LastLiveAt
		if last == 0 {
			last = r.CreatedAt.UnixMilli()
		}
		if r.Source != "manual" || !r.Dormant() || now.Sub(time.UnixMilli(last)) < DormantRetention {
			continue
		}
		if m.Remove(r.LiveID) == nil {
			_ = m.Store.Incident(r.LiveID, "pruned", fmt.Sprintf("连续 %d 天未开播，已自动移除", int(DormantRetention/(24*time.Hour))))
			_ = m.Store.Audit("room_prune", r.LiveID, "长期未开播自动移除")
			removed = append(removed, r.LiveID)
		}
	}
	return removed
}

func backoff(n int) time.Duration { return time.Duration(min(300, 15*(1<<min(n-1, 5)))) * time.Second }

// Retry pacing after a collection drops. In long tests a dropped connection usually
// came back within seconds, while doubling waits of up to 5 minutes left live rooms
// uncollected for half an hour; so the first retries are quick and the cap is low.
// A collection that lasted CollectStableAfter resets the count.
var (
	CollectStableAfter = 2 * time.Minute
	// ProbeTimeoutRetry is the wait after a probe timed out (the probe instance is
	// replaced after two in a row), instead of the doubling backoff.
	ProbeTimeoutRetry = 15 * time.Second
)

// CollectRetryDelay is 15 s for the first four consecutive failures, then 60 s.
func CollectRetryDelay(n int) time.Duration {
	if n <= 4 {
		return 15 * time.Second
	}
	return time.Minute
}

type sink struct {
	ctx    context.Context
	t      *task
	q      *hub.Queue
	states chan engine.EngineState
}

func (s *sink) Event(e engine.RawEvent) {
	if s.ctx.Err() != nil {
		return
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now()
	}
	s.t.last.Store(e.ReceivedAt.UnixMilli())
	s.t.lastMessage.Store(e.ReceivedAt.UnixMilli())
	s.q.Push(e)
}
func (s *sink) State(v engine.EngineState) {
	if s.ctx.Err() != nil {
		return
	}
	select {
	case s.states <- v:
	default:
	}
}
func (m *Manager) collect(parent context.Context, t *task, a engine.Engine, spec engine.RoomSpec) string {
	if m.Budget.WaitReady(parent) != nil {
		return "cancelled"
	}
	ctx, cancel := context.WithCancel(parent)
	q := hub.NewNodeQueue(&m.EntryDrops)
	t.mu.Lock()
	t.queue = q
	t.mu.Unlock()
	s := &sink{ctx: ctx, t: t, q: q, states: make(chan engine.EngineState, 32)}
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, spec, s) }()
	defer func() {
		cancel()
		<-done
		t.mu.Lock()
		t.dropped.Add(q.Dropped.Load())
		t.drops.Merge(&q.Drops)
		t.queue = nil
		t.mu.Unlock()
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	t.last.Store(time.Now().UnixMilli())
	results := make(chan string, 1)
	checking := false
	checkedAt := time.Time{}
	nextCheck := time.Time{}
	noData := 0
	for {
		select {
		case <-ctx.Done():
			return "cancelled"
		case e := <-done:
			done <- e
			if e == nil {
				return "offline"
			}
			return "risk"
		case st := <-s.states:
			t.setState(st.State, st.Reason)
		case <-q.Wake:
			for _, raw := range q.Drain() {
				noData = 0
				m.consume(t, raw)
			}
		case outcome := <-results:
			checking = false
			if time.UnixMilli(t.last.Load()).After(checkedAt) {
				t.mu.Lock()
				t.silenceFalsePositives++
				t.mu.Unlock()
				continue
			}
			if outcome == "cooling" || m.Budget.Cooldown().Active {
				continue
			}
			if outcome == "offline" {
				return outcome
			}
			if outcome == "rebuild" {
				t.mu.Lock()
				now := time.Now()
				t.rebuilds = slices.DeleteFunc(t.rebuilds, func(x time.Time) bool { return now.Sub(x) > 30*time.Minute })
				if len(t.rebuilds) >= 3 {
					t.mu.Unlock()
					_ = m.Store.Incident(spec.LiveID, "risk", "30 分钟内静默重建已达到 3 次，停止自动重建")
					return "limited"
				}
				t.rebuilds = append(t.rebuilds, now)
				t.countMinute(now, 0, 1)
				t.mu.Unlock()
				_ = m.Store.Incident(spec.LiveID, "silence", "旁证连接收到消息，重建静默主连接")
				return "rebuild"
			}
			if outcome == "unknown" {
				return "risk"
			}
			noData++
			if noData == 1 {
				_ = m.Store.Incident(spec.LiveID, "silence", "主连接与旁证均无消息，标记无数据")
			}
			t.mu.Lock()
			t.noData = true
			t.reason = "无数据：主连接与旁证连接均未收到消息"
			t.mu.Unlock()
			if noData >= 3 {
				_ = m.Store.Incident(spec.LiveID, "silence", "连续三次旁证未收到消息")
				return "risk"
			}
		case now := <-tick.C:
			t.mu.Lock()
			state := t.state
			threshold := t.options.SilenceSeconds
			if threshold == 0 {
				threshold = t.silence.Threshold(now)
			}
			t.mu.Unlock()
			if state == "collecting" && !m.Budget.Cooldown().Active && !checking && now.After(nextCheck) && now.Sub(time.UnixMilli(t.last.Load())) > time.Duration(threshold)*time.Second {
				select {
				case m.witness <- struct{}{}:
					checking = true
					checkedAt = now
					nextCheck = now.Add(time.Duration(threshold) * time.Second)
					go func() {
						defer func() { <-m.witness }()
						result := m.confirm(ctx, a, spec)
						select {
						case results <- result:
						case <-ctx.Done():
						}
					}()
				default:
				}
			}
		}
	}
}

type witnessSink struct{ received chan struct{} }

func (w *witnessSink) Event(engine.RawEvent) {
	select {
	case w.received <- struct{}{}:
	default:
	}
}
func (w *witnessSink) State(engine.EngineState) {}
func (m *Manager) confirm(ctx context.Context, a engine.Engine, spec engine.RoomSpec) string {
	if m.Budget.Cooldown().Active {
		return "cooling"
	}
	spec.ShareGuestIdentity = m.Settings().ShareGuestIdentity
	if spec.GuestSharing != nil {
		spec.ShareGuestIdentity = *spec.GuestSharing
	}
	pctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	st, e := a.Probe(pctx, spec)
	cancel()
	if errors.Is(e, engine.ErrAccessRestricted) {
		m.Budget.RestrictBecause("witness_probe")
	}
	if m.Budget.Cooldown().Active {
		return "cooling"
	}
	if e != nil {
		return "unknown"
	}
	if st.Code == "offline" || st.Code == "account_no_room" {
		return "offline"
	}
	if st.Code != "online" {
		return "unknown"
	}
	if e = m.starts.Wait(ctx); e != nil {
		return "unknown"
	}
	if m.Budget.Cooldown().Active {
		return "cooling"
	}
	w := m.factory()
	defer w.Close()
	wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
	defer wcancel()
	s := &witnessSink{received: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() { _ = w.Run(wctx, spec, s); close(done) }()
	result := "nodata"
	select {
	case <-s.received:
		result = "rebuild"
	case <-wctx.Done():
		if ctx.Err() != nil {
			result = "unknown"
		}
	case <-done:
		select {
		case <-s.received:
			result = "rebuild"
		default:
			result = "unknown"
		}
	}
	wcancel()
	<-done
	return result
}
func (m *Manager) consume(t *task, raw engine.RawEvent) {
	t.mu.Lock()
	t.silence.Observe(raw.ReceivedAt)
	t.noData = false
	t.mu.Unlock()
	e := raw.Decode()
	if e.Type == "" {
		return
	}
	// Upstream times are often whole seconds; such samples are centred by half a
	// second and flagged as imprecise.
	if !e.TS.IsZero() && !e.TS.Equal(e.ReceivedAt) {
		precise := e.TS.UnixMilli()%1000 != 0
		lag := e.ReceivedAt.Sub(e.TS)
		if !precise {
			lag -= 500 * time.Millisecond
		}
		m.Bus.UpstreamLag.Observe(e.ReceivedAt, lag, precise)
	}
	accepted := false
	e = m.Bus.Publish(e, func(event model.Event) bool { accepted = t.buffer.Add(event); return accepted })
	if !accepted {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.Type == "room_stats" {
		t.online = number(e.Extra["online"])
		t.totalPV = number(e.Extra["total_pv"])
		t.recordOnline(e.ReceivedAt, t.online)
	}
	t.counts[e.Type]++
	minute := e.ReceivedAt.Truncate(time.Minute).UnixMilli()
	if t.minutes[minute] == nil {
		t.minutes[minute] = model.Zero()
	}
	t.minutes[minute][e.Type]++
	t.dirtyMinutes[minute] = true
}
func number(v any) int64 {
	switch x := v.(type) {
	case uint64:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		var n int64
		_, _ = fmt.Sscan(x, &n)
		return n
	}
	return 0
}

// flush writes the task's changed minute rows. The rows are copied under t.mu and
// written without it: the room loop needs t.mu for every message, and SQLite has a
// single connection, so waiting for it under the lock stalled the room until its
// entry queue overflowed. statsMu stays held so MinuteTotals never sees a row both
// in the database and as unpersisted.
func (m *Manager) flush(t *task) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	type row struct {
		at     int64
		counts model.Counts
		issues [2]int
		state  string
	}
	t.mu.Lock()
	liveID := t.record.LiveID
	var rows []row
	for at, c := range t.minutes {
		if t.dirtyMinutes[at] {
			rows = append(rows, row{at, cloneCounts(c), t.minuteIssues[at], t.state})
			delete(t.dirtyMinutes, at) // a message arriving during the write marks it again
		}
	}
	t.mu.Unlock()
	written := make([]row, 0, len(rows))
	failed := rows[:0:0]
	for _, r := range rows {
		if err := m.Store.Minute(liveID, time.UnixMilli(r.at), r.counts, r.issues[0], r.issues[1], r.state); err != nil {
			slog.Error("分钟统计写入失败", "live_id", liveID)
			failed = append(failed, r)
			continue
		}
		written = append(written, r)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range failed {
		t.dirtyMinutes[r.at] = true
	}
	if len(written) > 0 && t.persistedMinutes == nil {
		t.persistedMinutes = map[int64]model.Counts{}
	}
	for _, r := range written {
		t.persistedMinutes[r.at] = r.counts
	}
	now := time.Now()
	for at := range t.minutes {
		if now.Sub(time.UnixMilli(at)) > 31*time.Minute {
			delete(t.minutes, at)
			delete(t.persistedMinutes, at)
			delete(t.minuteIssues, at)
		}
	}
}

// countMinute adds reconnects or silence rebuilds to the minute they happened in.
// Minute rows used to store the rolling 30-minute reconnect count and the task's
// lifetime rebuild count, so the hourly rollup summed them about 60 times over.
// The caller holds t.mu.
func (t *task) countMinute(at time.Time, reconnects, rebuilds int) {
	minute := at.Truncate(time.Minute).UnixMilli()
	if t.minutes == nil {
		t.minutes = map[int64]model.Counts{}
	}
	if t.minutes[minute] == nil {
		t.minutes[minute] = model.Zero()
	}
	if t.minuteIssues == nil {
		t.minuteIssues = map[int64][2]int{}
	}
	if t.dirtyMinutes == nil {
		t.dirtyMinutes = map[int64]bool{}
	}
	v := t.minuteIssues[minute]
	v[0] += reconnects
	v[1] += rebuilds
	t.minuteIssues[minute] = v
	t.dirtyMinutes[minute] = true
}
func (m *Manager) persistLoop() {
	defer close(m.persistDone)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	lastClean := ""
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-tick.C:
			for _, t := range m.all() {
				m.flush(t)
			}
			// Light hourly maintenance: roll minutes up into hours, drop expired rows,
			// remove long-dormant manual tasks, reclaim pages incrementally.
			hour := now.Format("2006-01-02T15")
			if hour != lastClean {
				m.PruneDormant(now)
				if m.Store.Clean(m.ctx, m.Settings()) == nil {
					lastClean = hour
				}
			}
		}
	}
}
