package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/api/northbound"
	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
	"github.com/Vz7797/LiveTap/server/internal/lab"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/observe"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Server struct {
	Manager        *room.Manager
	Auth           *auth.Auth
	Snapshots      *observe.Snapshotter
	web            fsHandler
	CookieSecure   bool
	TrustedProxies []netip.Prefix
	Logs           *observe.Logs
	Alerts         *observe.Alerts
	Lab            *lab.Service
	LabEnabled     bool // 关闭时实验室接口返回 404，后台隐藏入口
	Northbound     *northbound.Service
}
type fsHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func New(m *room.Manager, a *auth.Auth, s *observe.Snapshotter, web http.Handler) *Server {
	return &Server{Manager: m, Auth: a, Snapshots: s, web: web}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"ok": true, "version": buildinfo.Version})
	})
	mux.HandleFunc("POST /admin/api/login", s.login)
	mux.HandleFunc("GET /admin/api/session", s.session)
	secure := http.NewServeMux()
	secure.HandleFunc("POST /admin/api/logout", s.logout)
	secure.HandleFunc("POST /admin/api/password", s.password)
	secure.HandleFunc("GET /admin/api/snapshot", s.snapshotRoute)
	secure.HandleFunc("GET /admin/api/history", s.historyRoute)
	secure.HandleFunc("GET /admin/api/rooms/{id}/detail", s.roomDetailRoute)
	if s.Northbound != nil {
		s.clientRoutes(secure)
	}
	secure.HandleFunc("GET /admin/api/stream", s.stream)
	secure.HandleFunc("GET /admin/api/rooms", func(w http.ResponseWriter, r *http.Request) { views, _, _ := s.Manager.Views(); respond(w, 200, views) })
	secure.HandleFunc("POST /admin/api/rooms", s.addRooms)
	secure.HandleFunc("PATCH /admin/api/rooms/{id}", s.updateRoom)
	secure.HandleFunc("DELETE /admin/api/rooms/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Manager.Remove(r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]any{"ok": true})
	})
	secure.HandleFunc("POST /admin/api/rooms/{id}/actions", s.action)
	s.poolRoutes(secure)
	s.observeRoutes(secure)
	s.labRoutes(secure)
	secure.HandleFunc("GET /admin/api/rooms/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Manager.Events(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, out)
	})
	secure.HandleFunc("GET /admin/api/settings", s.settings)
	secure.HandleFunc("PUT /admin/api/settings", s.saveSettings)
	secure.HandleFunc("POST /admin/api/settings/rollout", s.rollout)
	secure.HandleFunc("GET /admin/api/audit", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		v, e := s.Manager.Store.Audits(limit)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	})
	mux.Handle("/admin/api/", s.guard(secure))
	mux.Handle("GET /metrics", s.guard(http.HandlerFunc(s.metrics)))
	mux.Handle("/", s.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src 'self' data: https:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'")
		if strings.HasPrefix(r.URL.Path, "/admin/api") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}
func token(r *http.Request) string {
	c, e := r.Cookie("livetap_session")
	if e != nil {
		return ""
	}
	return c.Value
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	return e == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := token(r)
		valid, change := s.Auth.Session(t)
		if !valid {
			apiError(w, 401, "unauthorized", "请先登录")
			return
		}
		if !sameOrigin(r) {
			apiError(w, 403, "forbidden", "请求来源不受信任")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.Auth.CSRF(t))) != 1 {
				apiError(w, 403, "csrf_invalid", "操作令牌失效，请刷新后重试")
				return
			}
		}
		if change && r.URL.Path != "/admin/api/password" && r.URL.Path != "/admin/api/logout" {
			apiError(w, 403, "password_change_required", "首次登录请先修改密码")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, 403, "forbidden", "请求来源不受信任")
		return
	}
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &v) {
		return
	}
	ip := s.sourceIP(r)
	t, e := s.Auth.Login(ip, v.Username, v.Password)
	if e != nil {
		if errors.Is(e, auth.ErrLocked) {
			apiError(w, 429, "rate_limited", e.Error())
		} else {
			apiError(w, 401, "unauthorized", "账号或密码不正确")
		}
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "livetap_session", Value: t, Path: "/", HttpOnly: true, Secure: s.CookieSecure || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(auth.SessionMax / time.Second)})
	_, change := s.Auth.Session(t)
	respond(w, 200, map[string]any{"authenticated": true, "must_change_password": change, "csrf": s.Auth.CSRF(t), "features": map[string]bool{"lab": s.LabEnabled}})
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	t := token(r)
	valid, change := s.Auth.Session(t)
	if !valid {
		respond(w, 200, map[string]any{"authenticated": false})
		return
	}
	respond(w, 200, map[string]any{"authenticated": true, "must_change_password": change, "csrf": s.Auth.CSRF(t), "features": map[string]bool{"lab": s.LabEnabled}})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(token(r))
	http.SetCookie(w, &http.Cookie{Name: "livetap_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.CookieSecure || r.TLS != nil, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]any{"ok": true})
}
func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if !decode(w, r, &v) {
		return
	}
	ip := s.sourceIP(r)
	if e := s.Auth.ChangePassword(ip, token(r), v.Current, v.Next); e != nil {
		if errors.Is(e, auth.ErrLocked) {
			apiError(w, 429, "rate_limited", e.Error())
			return
		}
		apiError(w, 400, "invalid_password", e.Error())
		return
	}
	respond(w, 200, map[string]any{"ok": true})
}
func (s *Server) addRooms(w http.ResponseWriter, r *http.Request) {
	var v struct {
		LiveIDs []string `json:"live_ids"`
		Tags    []string `json:"tags"`
		Enabled *bool    `json:"enabled"`
	}
	if !decode(w, r, &v) {
		return
	}
	if len(v.LiveIDs) == 0 || len(v.LiveIDs) > 50 {
		apiError(w, 400, "invalid_live_id", "每批请添加 1～50 个直播间")
		return
	}
	out := []map[string]any{}
	for _, id := range v.LiveIDs {
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		room, created, e := s.Manager.AddConfigured(r.Context(), id, v.Tags, enabled, "manual", nil)
		if e != nil {
			out = append(out, map[string]any{"input": id, "ok": false, "error": safeError(e)})
		} else {
			out = append(out, map[string]any{"input": id, "ok": true, "created": created, "live_id": room.LiveID})
		}
	}
	respond(w, 200, map[string]any{"results": out})
}
func (s *Server) updateRoom(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Note       *string         `json:"note"`
		Tags       *[]string       `json:"tags"`
		Overrides  model.Overrides `json:"overrides"`
		InheritAll bool            `json:"inherit_all"`
	}
	if !decode(w, r, &v) {
		return
	}
	if e := s.Manager.Update(r.PathValue("id"), v.Note, v.Tags, v.Overrides, v.InheritAll); e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]any{"ok": true})
}
func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &v) {
		return
	}
	if e := s.Manager.Action(r.PathValue("id"), v.Action); e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]any{"ok": true})
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	v := s.Manager.Settings()
	respond(w, 200, map[string]any{"defaults": model.PublicOptions(v.Defaults), "share_guest_identity": v.ShareGuestIdentity, "max_rooms": v.MaxRooms, "starts_per_second": v.StartsPerSecond, "requests_per_second": v.RequestsPerSecond, "rolling_seconds": v.RollingSeconds, "stats_days": v.StatsDays, "event_days": v.EventDays, "rollout": s.Manager.Rollout(""), "db_bytes": s.Manager.Store.SizeBytes()})
}
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Settings json.RawMessage `json:"settings"`
		Apply    string          `json:"apply"`
	}
	if !decode(w, r, &v) {
		return
	}
	cfg := s.Manager.Settings()
	d := json.NewDecoder(strings.NewReader(string(v.Settings)))
	d.DisallowUnknownFields()
	if e := d.Decode(&cfg); e != nil {
		apiError(w, 400, "invalid_settings", "设置格式无效")
		return
	}
	if e := s.Manager.ChangeSettings(cfg, v.Apply); e != nil {
		fail(w, e)
		return
	}
	s.settings(w, r)
}
func (s *Server) rollout(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &v) {
		return
	}
	switch v.Action {
	case "pause", "resume", "cancel":
	default:
		apiError(w, 400, "invalid_action", "操作无效")
		return
	}
	_ = s.Manager.Store.Audit("rollout_"+v.Action, "node", "修改滚动应用任务")
	respond(w, 200, s.Manager.Rollout(v.Action))
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	// permessage-deflate: consecutive snapshots are near-identical JSON, so context
	// takeover compresses them very well for remote admins on slow links.
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if e != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	tick := time.NewTicker(2500 * time.Millisecond)
	defer tick.Stop()
	for {
		valid, change := s.Auth.Session(token(r))
		if !valid || change {
			_ = c.Close(websocket.StatusPolicyViolation, "session expired")
			return
		}
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = wsjson.Write(wctx, c, liteSnapshot(s.snapshot()))
		cancel()
		if e != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	v := s.Snapshots.Get()
	node := v["node"].(map[string]any)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "collector_rooms %d\ncollector_upstream_connections %v\ncollector_upstream_requests_last_minute %v\ncollector_process_memory_bytes %.0f\ncollector_goroutines %v\n", len(v["rooms"].([]map[string]any)), node["conns"], node["reqPerMin"], node["memMB"].(float64)*1024*1024, node["goroutines"])
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		apiError(w, 400, "invalid_request", "请求格式无效或内容过大")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		apiError(w, 400, "invalid_request", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("管理接口响应编码失败")
		status = 500
		b = []byte(`{"ok":false,"error":{"code":"encode_failed","message":"响应编码失败，请查看服务日志"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message}, "request_id": auth.Token()[:12]})
}
func safeError(e error) string {
	v := e.Error()
	if strings.Contains(v, "SQL") || strings.Contains(v, "constraint") || strings.Contains(v, "database") {
		return "数据库操作失败，请检查服务日志"
	}
	return v
}
func fail(w http.ResponseWriter, e error) {
	status, code := 400, "invalid_request"
	if errors.Is(e, room.ErrCapacity) {
		status = 409
		code = "capacity_exceeded"
	}
	if errors.Is(e, room.ErrNotFound) {
		status = 404
		code = "not_found"
	}
	apiError(w, status, code, safeError(e))
}
