package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
)

type AlertConfig struct {
	Enabled         map[string]bool `json:"enabled"`
	MassPercent     float64         `json:"mass_percent"`
	CapacityPercent float64         `json:"capacity_percent"`
	Webhook         string          `json:"webhook"`
}
type Alert struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Rule   string `json:"rule"`
	Level  string `json:"level"`
	LiveID string `json:"live_id"`
	Detail string `json:"detail"`
	Acked  bool   `json:"acked"`
}
type Alerts struct {
	m         *room.Manager
	mu        sync.Mutex
	cfg       AlertConfig
	lastEvent int64
	active    map[string]bool
	queue     chan Alert
	cancel    context.CancelFunc
	done      chan struct{}
}

var ruleLevels = map[string]string{"mass_failure": "critical", "silence": "warning", "risk": "warning", "capacity": "info", "proxy_invalid": "warning", "account_invalid": "warning", "node_cooldown": "warning"}

func NewAlerts(ctx context.Context, m *room.Manager) (*Alerts, error) {
	_, e := m.Store.DB.Exec(`CREATE TABLE IF NOT EXISTS alerts(id INTEGER PRIMARY KEY AUTOINCREMENT,at INTEGER NOT NULL,rule TEXT NOT NULL,level TEXT NOT NULL,live_id TEXT NOT NULL,detail TEXT NOT NULL,acked INTEGER NOT NULL DEFAULT 0);CREATE TABLE IF NOT EXISTS alert_cursor(id INTEGER PRIMARY KEY CHECK(id=1),last_event INTEGER NOT NULL);INSERT OR IGNORE INTO alert_cursor VALUES(1,0);`)
	if e != nil {
		return nil, e
	}
	cfg := AlertConfig{Enabled: map[string]bool{}, MassPercent: 30, CapacityPercent: 85}
	for rule := range ruleLevels {
		cfg.Enabled[rule] = true
	}
	if e = m.Store.LoadPrivate("alerts", &cfg); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	a := &Alerts{m: m, cfg: cfg, active: map[string]bool{}, queue: make(chan Alert, 32), cancel: cancel, done: make(chan struct{})}
	_ = m.Store.DB.QueryRow("SELECT last_event FROM alert_cursor WHERE id=1").Scan(&a.lastEvent)
	go a.run(ctx)
	return a, nil
}
func (a *Alerts) Close() { a.cancel(); <-a.done }
func (a *Alerts) Config() AlertConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.cfg
	c.Enabled = map[string]bool{}
	for k, v := range a.cfg.Enabled {
		c.Enabled[k] = v
	}
	return c
}
func (a *Alerts) Save(c AlertConfig) error {
	if c.MassPercent <= 0 || c.MassPercent > 100 || c.CapacityPercent <= 0 || c.CapacityPercent > 100 {
		return errors.New("告警阈值应在 0～100 之间")
	}
	for k := range c.Enabled {
		if _, ok := ruleLevels[k]; !ok {
			return errors.New("未知告警规则")
		}
	}
	if c.Webhook != "" {
		u, e := url.Parse(c.Webhook)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("Webhook 地址无效")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.m.Store.SavePrivate("alerts", c); e != nil {
		return e
	}
	a.cfg = c
	return a.m.Store.Audit("alert_rules", "node", "更新告警规则与通知配置，敏感值不记录")
}
func (a *Alerts) emit(rule, id, detail string, at int64) {
	cfg := a.Config()
	if !cfg.Enabled[rule] {
		return
	}
	level := ruleLevels[rule]
	result, e := a.m.Store.DB.Exec("INSERT INTO alerts(at,rule,level,live_id,detail) VALUES(?,?,?,?,?)", at, rule, level, id, detail)
	if e != nil {
		slog.Error("保存告警失败")
		return
	}
	key, _ := result.LastInsertId()
	alert := Alert{ID: key, At: at, Rule: rule, Level: level, LiveID: id, Detail: detail}
	slog.Warn(detail, "live_id", id, "kind", rule)
	if cfg.Webhook != "" {
		select {
		case a.queue <- alert:
		default:
			slog.Warn("告警通知队列已满，告警已保存在后台")
		}
	}
}
func (a *Alerts) Tick(now time.Time) {
	cfg := a.Config()
	views, _, _ := a.m.Views()
	conditions := alertConditions(views, a.m.Settings().MaxRooms, cfg, now)
	for rule, on := range conditions {
		if on && !a.active[rule] {
			detail := "节点容量超过告警阈值"
			if rule == "mass_failure" {
				detail = "5 分钟内大量直播间出现风控或重连"
			}
			a.emit(rule, "", detail, now.UnixMilli())
		}
		a.active[rule] = on
	}
	a.incidents(now)
}
func alertConditions(views []map[string]any, capacity int, cfg AlertConfig, now time.Time) map[string]bool {
	active, recent := 0, 0
	for _, v := range views {
		state := v["status"].(string)
		if state == "stopped" || state == "notfound" {
			continue
		}
		active++
		failed := false
		for _, ev := range v["events"].([]model.StateEvent) {
			if (ev.Kind == "risk" || ev.Kind == "reconnect") && ev.At <= now.UnixMilli() && now.UnixMilli()-ev.At <= int64(5*time.Minute/time.Millisecond) {
				failed = true
			}
		}
		if failed {
			recent++
		}
	}
	return map[string]bool{"capacity": float64(active) > float64(capacity)*cfg.CapacityPercent/100, "mass_failure": active > 0 && float64(recent) > float64(active)*cfg.MassPercent/100}
}
func (a *Alerts) incidents(now time.Time) {
	rows, e := a.m.Store.DB.Query("SELECT id,at,live_id,kind,detail FROM incidents WHERE id>? ORDER BY id LIMIT 200", a.lastEvent)
	if e != nil {
		return
	}
	var events []Alert
	last := a.lastEvent
	for rows.Next() {
		var v Alert
		if rows.Scan(&v.ID, &v.At, &v.LiveID, &v.Rule, &v.Detail) == nil {
			events = append(events, v)
			last = v.ID
		}
	}
	rows.Close()
	for _, v := range events {
		if _, ok := ruleLevels[v.Rule]; ok {
			a.emit(v.Rule, v.LiveID, v.Detail, v.At)
		}
	}
	if last != a.lastEvent {
		a.lastEvent = last
		_, _ = a.m.Store.DB.Exec("UPDATE alert_cursor SET last_event=? WHERE id=1", last)
	}
	// Expired alerts are deleted by the hourly Store.Clean; deleting here every 5 s
	// scanned the whole table (no index on at) on the single database connection.
}
func (a *Alerts) run(ctx context.Context) {
	defer close(a.done)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	a.Tick(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			a.Tick(now)
		case v := <-a.queue:
			a.notify(ctx, v)
		}
	}
}
func (a *Alerts) notify(ctx context.Context, v Alert) {
	endpoint := a.Config().Webhook
	if endpoint == "" {
		return
	}
	body, _ := json.Marshal(v)
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; attempt < 3; attempt++ {
		req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if e != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(req)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
	}
	slog.Warn("告警 Webhook 投递失败，告警已保存在后台")
}
func (a *Alerts) List(level, rule, liveID string) ([]Alert, error) {
	query := "SELECT id,at,rule,level,live_id,detail,acked FROM alerts WHERE 1=1"
	args := []any{}
	for _, f := range []struct{ key, value string }{{"level", level}, {"rule", rule}, {"live_id", liveID}} {
		if f.value != "" {
			query += " AND " + f.key + "=?"
			args = append(args, f.value)
		}
	}
	query += " ORDER BY id DESC LIMIT 100"
	rows, e := a.m.Store.DB.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var v Alert
		if e = rows.Scan(&v.ID, &v.At, &v.Rule, &v.Level, &v.LiveID, &v.Detail, &v.Acked); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (a *Alerts) Ack(id int64) error {
	if _, e := a.m.Store.DB.Exec("UPDATE alerts SET acked=1 WHERE id=?", id); e != nil {
		return e
	}
	return a.m.Store.Audit("alert_ack", fmt.Sprint(id), "确认告警")
}
