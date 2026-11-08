package northbound

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/coder/websocket"
)

type Server struct {
	Service        *Service
	TrustedProxies []netip.Prefix
	PingInterval   time.Duration
}

func (s *Server) pingInterval() time.Duration {
	if s.PingInterval > 0 {
		return s.PingInterval
	}
	return 15 * time.Second
}

type clientContext struct{}

func identity(r *http.Request) string { return r.Context().Value(clientContext{}).(string) }
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func WriteError(w http.ResponseWriter, err error) {
	f := &Fault{"internal_error", "服务暂时不可用", "请稍后重试", 500}
	var known *Fault
	if errors.As(err, &known) {
		f = known
	}
	respond(w, f.Status, map[string]any{"ok": false, "error": map[string]string{"code": f.Code, "message": f.Message, "suggestion": f.Suggestion}, "request_id": auth.Token()[:16]})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		WriteError(w, fault("invalid_request", "JSON 请求格式无效或包含未知字段", 400))
		return false
	}
	return true
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/node", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, s.Service.Node()) })
	mux.HandleFunc("POST /v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		var v SubscribeRequest
		if !decode(w, r, &v) {
			return
		}
		sub, err := s.Service.Subscribe(r.Context(), identity(r), v)
		if err != nil {
			WriteError(w, err)
			return
		}
		respond(w, 200, s.Service.view(sub))
	})
	mux.HandleFunc("GET /v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		out := []any{}
		for _, sub := range s.Service.List(identity(r)) {
			out = append(out, s.Service.view(sub))
		}
		respond(w, 200, map[string]any{"subscriptions": out})
	})
	mux.HandleFunc("GET /v1/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		sub, err := s.Service.Get(identity(r), r.PathValue("id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		respond(w, 200, s.Service.view(sub))
	})
	mux.HandleFunc("DELETE /v1/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Service.Unsubscribe(identity(r), r.PathValue("id")); err != nil {
			WriteError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /v1/subscriptions:renew", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			IDs []string `json:"subscription_ids"`
		}
		if !decode(w, r, &v) {
			return
		}
		subs, missing, err := s.Service.Renew(identity(r), v.IDs)
		if errors.Is(err, ErrMissing) {
			// Nothing was renewed: keep the 404, and still say why for each ID.
			respond(w, 404, map[string]any{"ok": false, "error": map[string]string{"code": ErrMissing.Code, "message": "订阅均已结束或不存在", "suggestion": "按 missing 的原因重新订阅或停止续租"}, "missing": missing, "request_id": auth.Token()[:16]})
			return
		}
		if err != nil {
			WriteError(w, err)
			return
		}
		respond(w, 200, map[string]any{"subscriptions": subs, "missing": missing})
	})
	mux.HandleFunc("POST /v1/rooms/status:batch", s.batch)
	mux.HandleFunc("GET /v1/stream", s.stream)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { WriteError(w, ErrMissing) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		values := r.Header.Values("Authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			WriteError(w, fault("unauthorized", "请通过 Authorization Bearer 提供 API Key", 401))
			return
		}
		ip, _ := netip.ParseAddr(auth.SourceIP(r, s.TrustedProxies))
		client, err := s.Service.Authenticate(strings.TrimPrefix(values[0], "Bearer "), ip)
		if err != nil {
			WriteError(w, err)
			return
		}
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientContext{}, client)))
	})
}
func (s *Service) view(sub model.Subscription) any {
	status, _ := s.Rooms.Status(sub.LiveID)
	return struct {
		model.Subscription
		Status room.Status `json:"status"`
	}{sub, status}
}
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	var v struct {
		IDs []string `json:"live_ids"`
	}
	if !decode(w, r, &v) {
		return
	}
	if len(v.IDs) < 1 || len(v.IDs) > 50 {
		WriteError(w, fault("invalid_request", "每批提供 1～50 个直播间", 400))
		return
	}
	own := map[string]bool{}
	for _, sub := range s.Service.List(identity(r)) {
		own[sub.LiveID] = true
	}
	ids := []string{}
	for _, input := range v.IDs {
		id, err := room.ParseID(input)
		if err != nil {
			WriteError(w, fault("invalid_live_id", "直播间标识无效", 400))
			return
		}
		if !own[id] {
			WriteError(w, ErrMissing)
			return
		}
		ids = append(ids, id)
	}
	out := []room.Status{}
	for _, id := range ids {
		status, ok := s.Service.Rooms.Status(id)
		if !ok {
			WriteError(w, ErrMissing)
			return
		}
		out = append(out, status)
	}
	respond(w, 200, map[string]any{"rooms": out})
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	// Rejected handshakes use the same error envelope as other /v1 calls.
	upgrade := false
	for _, token := range strings.Split(strings.Join(r.Header.Values("Connection"), ","), ",") {
		upgrade = upgrade || strings.EqualFold(strings.TrimSpace(token), "upgrade")
	}
	key, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
	if !upgrade || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" || err != nil || len(key) != 16 {
		WriteError(w, fault("invalid_request", "请使用有效的 WebSocket 握手", 400))
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			WriteError(w, fault("forbidden", "不允许跨站 WebSocket 握手", 403))
			return
		}
	}
	var since *uint64
	if r.URL.Query().Has("since_seq") {
		value, err := strconv.ParseUint(r.URL.Query().Get("since_seq"), 10, 64)
		if err != nil {
			WriteError(w, fault("invalid_request", "since_seq 无效", 400))
			return
		}
		since = &value
	}
	client := identity(r)
	q, replay, err := s.Service.Open(client, r.URL.Query().Get("subscription"), since, r.URL.Query().Get("boot_id"))
	if err != nil {
		WriteError(w, err)
		return
	}
	defer s.Service.CloseStream(q)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ctx = c.CloseRead(ctx)
	go func() {
		select {
		case <-ctx.Done():
		case <-q.Done:
		}
		cancel()
		_ = c.CloseNow()
	}()
	interval := s.pingInterval()
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				pingCtx, done := context.WithTimeout(ctx, 5*time.Second)
				err := c.Ping(pingCtx)
				done()
				if err != nil || s.Service.KeepAlive(q) != nil {
					cancel()
					_ = c.CloseNow()
					return
				}
			}
		}
	}()
	write := func(p hub.Packet, live bool) bool {
		if !s.Service.DeliveryAllowed(client, p) {
			return true
		}
		sendCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		if c.Write(sendCtx, websocket.MessageText, p.Data) != nil {
			return false
		}
		q.Sent(len(p.Data))
		if live && p.Event.ID != "" && !p.Event.ReceivedAt.IsZero() {
			now := time.Now()
			s.Service.Rooms.Bus.PushLag.Observe(now, now.Sub(p.Event.ReceivedAt), true)
		}
		return true
	}
	for _, p := range replay {
		if !write(p, false) {
			return
		}
	}
	for {
		p, drops, ok := q.Next()
		if ok {
			if drops > 0 {
				p = s.Service.Rooms.Bus.Control("backpressure", map[string]any{"dropped": drops, "has_gap": true})
			}
			if !write(p, true) {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-q.Done:
			return
		case <-q.Wake:
		}
	}
}
