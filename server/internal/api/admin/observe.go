package admin

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/observe"
)

func (s *Server) observeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if s.Logs == nil {
			respond(w, 200, []any{})
			return
		}
		q := r.URL.Query()
		respond(w, 200, s.Logs.Query(q.Get("level"), q.Get("live_id"), q.Get("q")))
	})
	// Recent messages from the rooms' in-memory buffers, optionally by type, so a
	// filtered message view is complete instead of a slice of the global feed.
	mux.HandleFunc("GET /admin/api/feed", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit < 1 || limit > 500 {
			limit = 300
		}
		types := []string{}
		for _, t := range strings.Split(q.Get("types"), ",") {
			if t = strings.TrimSpace(t); t != "" {
				types = append(types, t)
			}
		}
		events, names := s.Manager.RecentEvents(types, limit)
		out := make([]map[string]any, 0, len(events))
		for _, e := range events {
			out = append(out, observe.FeedItem(e, names))
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("GET /admin/api/logs/stats", func(w http.ResponseWriter, r *http.Request) {
		if s.Logs == nil {
			respond(w, 200, observe.LogStats{Files: []observe.LogFile{}, ByLevel: map[string]int{}})
			return
		}
		respond(w, 200, s.Logs.Stats())
	})
	mux.HandleFunc("GET /admin/api/logs/export", func(w http.ResponseWriter, r *http.Request) {
		if s.Logs == nil {
			apiError(w, 503, "unavailable", "日志尚未就绪")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="collector-logs-`+time.Now().Format("20060102-150405")+`.log"`)
		_ = s.Logs.Export(w)
	})
	mux.HandleFunc("POST /admin/api/logs/purge", func(w http.ResponseWriter, r *http.Request) {
		if s.Logs == nil {
			apiError(w, 503, "unavailable", "日志尚未就绪")
			return
		}
		freed, e := s.Logs.Purge()
		if e != nil {
			fail(w, e)
			return
		}
		_ = s.Manager.Store.Audit("logs_purge", "logs", fmt.Sprintf("清理日志文件，释放 %d 字节", freed))
		slog.Info("日志已由管理员清理")
		respond(w, 200, map[string]int64{"freed": freed})
	})
	mux.HandleFunc("GET /admin/api/alerts", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			respond(w, 200, []any{})
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, e := s.Alerts.Filtered(observe.AlertFilter{Level: q.Get("level"), Rule: q.Get("rule"), LiveID: q.Get("live_id"), Acked: q.Get("acked")}, limit)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("GET /admin/api/alerts/summary", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			respond(w, 200, observe.AlertSummary{Open: map[string]int{}, TopRules: []observe.RuleCount{}})
			return
		}
		out, e := s.Alerts.Summary(time.Now())
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, out)
	})
	// Bulk acknowledgement: explicit ids, or every open alert matching the filter.
	mux.HandleFunc("POST /admin/api/alerts/ack", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			apiError(w, 503, "unavailable", "告警尚未就绪")
			return
		}
		var v struct {
			IDs    []int64 `json:"ids"`
			Level  string  `json:"level"`
			Rule   string  `json:"rule"`
			LiveID string  `json:"live_id"`
		}
		if !decode(w, r, &v) {
			return
		}
		if len(v.IDs) > 500 {
			apiError(w, 400, "invalid_request", "一次最多确认 500 条")
			return
		}
		n, e := s.Alerts.AckMatching(observe.AlertFilter{IDs: v.IDs, Level: v.Level, Rule: v.Rule, LiveID: v.LiveID})
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]int64{"acked": n})
	})
	// Cleanup: mode "acked" removes acknowledged alerts; "before" removes all alerts older than days.
	mux.HandleFunc("POST /admin/api/alerts/purge", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			apiError(w, 503, "unavailable", "告警尚未就绪")
			return
		}
		var v struct {
			Mode string `json:"mode"`
			Days int    `json:"days"`
		}
		if !decode(w, r, &v) {
			return
		}
		var before int64
		switch {
		case v.Mode == "acked":
		case v.Mode == "before" && v.Days >= 0 && v.Days <= 3650:
			before = time.Now().AddDate(0, 0, -v.Days).UnixMilli()
		default:
			apiError(w, 400, "invalid_request", "清理方式无效")
			return
		}
		n, e := s.Alerts.Purge(v.Mode == "acked", before)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]int64{"deleted": n})
	})
	mux.HandleFunc("POST /admin/api/alerts/{id}/ack", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			apiError(w, 503, "unavailable", "告警尚未就绪")
			return
		}
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil {
			apiError(w, 400, "invalid_id", "告警编号无效")
			return
		}
		if e = s.Alerts.Ack(id); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /admin/api/alert-rules", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			apiError(w, 503, "unavailable", "告警尚未就绪")
			return
		}
		s.alertConfig(w)
	})
	mux.HandleFunc("PUT /admin/api/alert-rules", func(w http.ResponseWriter, r *http.Request) {
		if s.Alerts == nil {
			apiError(w, 503, "unavailable", "告警尚未就绪")
			return
		}
		var v struct {
			Enabled  map[string]bool `json:"enabled"`
			Mass     float64         `json:"mass_percent"`
			Capacity float64         `json:"capacity_percent"`
			Webhook  string          `json:"webhook"`
			Clear    bool            `json:"clear_webhook"`
		}
		if !decode(w, r, &v) {
			return
		}
		cfg := observe.AlertConfig{Enabled: v.Enabled, MassPercent: v.Mass, CapacityPercent: v.Capacity, Webhook: v.Webhook}
		if cfg.Webhook == "" && !v.Clear {
			cfg.Webhook = s.Alerts.Config().Webhook
		}
		if e := s.Alerts.Save(cfg); e != nil {
			fail(w, e)
			return
		}
		s.alertConfig(w)
	})
}
func (s *Server) alertConfig(w http.ResponseWriter) {
	cfg := s.Alerts.Config()
	respond(w, 200, map[string]any{"enabled": cfg.Enabled, "mass_percent": cfg.MassPercent, "capacity_percent": cfg.CapacityPercent, "webhook": "", "webhook_configured": cfg.Webhook != ""})
}
