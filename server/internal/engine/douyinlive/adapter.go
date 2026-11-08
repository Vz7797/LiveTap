package douyinlive

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	dl "github.com/jwwsjlm/douyinLive/v2"
)

// One adapter belongs to one supervisor. The probe instance is reused until its
// connection settings change; Run always creates a fresh single-use instance.
type Adapter struct {
	budget    *engine.Budget
	probe     *dl.DouyinLive
	probeSpec engine.RoomSpec
	timeouts  int // consecutive probe timeouts on the current probe instance
	mu        sync.Mutex
	guests    *guestIdentity
}

func New(b *engine.Budget) *Adapter { return &Adapter{budget: b, guests: newGuestIdentity()} }
func Factory(b *engine.Budget) engine.Factory {
	guests := newGuestIdentity()
	return func() engine.Engine { return &Adapter{budget: b, guests: guests} }
}
func (a *Adapter) restricted(reason string) {
	a.guests.invalidate()
	a.budget.RestrictBecause(reason)
}
func (a *Adapter) Name() string { return "douyinlive" }
func (a *Adapter) client(spec engine.RoomSpec, sink engine.Sink) (*dl.DouyinLive, error) {
	options := dl.Options{Cookie: spec.Cookie, ProxyURL: spec.ProxyURL, SignProvider: spec.SignProvider, TikHubToken: spec.TikHubToken, ProtocolMode: spec.ProtocolMode,
		BeforeRequest: func(r *http.Request) error { return a.budget.BeforeRoomRequest(spec.LiveID, r) },
		AfterResponse: func(r *http.Response) error {
			if why := engine.RestrictedReason(r); why != "" {
				a.restricted("engine_response " + why)
				return engine.ErrAccessRestricted
			}
			return nil
		},
	}
	if spec.ShareGuestIdentity && spec.Cookie == "" {
		options.GuestTTWID = func(ctx context.Context, fetch func(context.Context) (string, time.Time, error)) (string, error) {
			return a.guests.get(ctx, spec.ProxyURL+"\n"+spec.ProtocolMode, fetch)
		}
	}
	return dl.NewDouyinLiveWithOptions(spec.LiveID, dl.NewSlogLogger(slog.New(&handler{sink: sink})), options)
}

// statusCodeText picks the upstream status_code out of the engine's flattened
// error text ("直播间信息接口返回异常 status_code=4003073").
var statusCodeText = regexp.MustCompile(`status_code=-?\d+`)

// RegionBlockedCode is web/enter's answer for a room that is not available in
// the node's region (the page shows “当前直播间在本区域未开放”). It is a fact
// about the room, not a restriction on the node: retrying cannot help.
const RegionBlockedCode = "status_code=4003073"

// EndedCode is web/enter's answer while a stream is ending or has just ended: in a
// 24-hour test 61 of the 62 rooms that returned it were confirmed ended within about
// ten minutes. It is reported as offline so the normal end grace applies, instead of
// an unconfirmed state that backs off and looks like a restriction in the admin page.
const EndedCode = "status_code=30003"

// UncertainEndCode is web/enter's answer seen after a stream ended in long tests: a
// fresh probe instance could not decide either. It is handled like the engine's
// ambiguous result, i.e. as offline with the normal end grace.
const UncertainEndCode = "status_code=4003099"

// RegionBlocked is the LiveStatus.Code for such a room.
const RegionBlocked = "region_blocked"

func upstreamStatusCode(err error) string {
	if err == nil {
		return ""
	}
	return statusCodeText.FindString(err.Error())
}

func (a *Adapter) Probe(ctx context.Context, spec engine.RoomSpec) (engine.LiveStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probe == nil || a.probeSpec.LiveID != spec.LiveID || !same(a.probeSpec, spec) {
		if a.probe != nil {
			a.probe.Close()
		}
		p, e := a.client(spec, nil)
		if e != nil {
			return engine.LiveStatus{}, e
		}
		a.probe = p
		a.probeSpec = spec
	}
	s, e := a.probe.CheckLiveStatus(ctx)
	code := upstreamStatusCode(e)
	if code == RegionBlockedCode {
		slog.Info("直播间在节点所在地区不开放", "live_id", spec.LiveID, "cause", RegionBlockedCode)
		return engine.LiveStatus{Code: RegionBlocked, LiveID: spec.LiveID}, nil
	}
	if code == EndedCode {
		slog.Info("上游表示直播已结束", "live_id", spec.LiveID, "cause", EndedCode)
		return engine.LiveStatus{Code: string(dl.LiveStatusOffline), LiveID: spec.LiveID}, nil
	}
	out, cause := classifyProbe(e, ctx.Err())
	if errors.Is(out, engine.ErrProbeTimeout) {
		// A probe instance that keeps timing out is replaced: its connections or
		// guest state may have gone bad (seen as minutes of timeouts on live rooms).
		if a.timeouts++; a.timeouts >= probeTimeoutsBeforeReset {
			a.probe.Close()
			a.probe = nil
			a.timeouts = 0
		}
	} else {
		a.timeouts = 0
	}
	if e != nil && !errors.Is(ctx.Err(), context.Canceled) {
		args := []any{"live_id", spec.LiveID, "cause", cause}
		if code != "" {
			// The exact upstream answer, kept even when the cause is a broader class.
			args = append(args, "code", code)
		}
		slog.Warn("直播状态探测未成功", args...)
	}
	// Concrete evidence (captcha page, 403/429) already tripped the node cooldown in
	// AfterResponse. Everything else only affects this room.
	return engine.LiveStatus{Code: string(s.Code), LiveID: s.LiveID, RoomID: s.RoomID, Name: s.LiveName, Title: s.Title, Avatar: s.AvatarThumb}, out
}

// probeTimeoutsBeforeReset is how many consecutive timeouts replace the probe instance.
const probeTimeoutsBeforeReset = 2

// classifyProbe maps a failed probe to the error the supervisor acts on and the
// cause recorded in the log:
//   - engine.ErrAccessRestricted: concrete evidence; the node cooldown is already tripped.
//   - engine.ErrAccessUncertain: the upstream gave no clear answer, which in long tests
//     almost always meant the stream had ended; the supervisor treats it as offline.
//   - engine.ErrProbeTimeout: the probe ran out of time; retried soon, never "ended".
//
// Any other failure is returned as is.
func classifyProbe(e, ctxErr error) (error, string) {
	if e == nil {
		return nil, ""
	}
	concrete, uncertain := classifyRestriction(e)
	code := upstreamStatusCode(e)
	switch {
	case concrete:
		return engine.ErrAccessRestricted, "anonymous_access_restricted"
	case uncertain || code == UncertainEndCode:
		return engine.ErrAccessUncertain, "upstream_uncertain"
	case errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(e, context.DeadlineExceeded):
		return engine.ErrProbeTimeout, "deadline"
	}
	cause := "unverified_upstream"
	for _, c := range []string{"deadline", "timeout", "exceed context deadline", "status=403", "status=429", "empty", "certificate", "已关闭", "TTWID", "无法提取房间信息", "未返回 status", "context canceled", "live page"} {
		if strings.Contains(e.Error(), c) {
			cause = c
			break
		}
	}
	switch {
	case cause == "deadline" || cause == "timeout" || cause == "exceed context deadline":
		return engine.ErrProbeTimeout, cause
	case cause == "unverified_upstream" && code != "":
		// Keep the number: “status_code=” alone hid which upstream answer it was.
		cause = code
	}
	return e, cause
}
func accessRestricted(err error) bool {
	concrete, uncertain := classifyRestriction(err)
	return concrete || uncertain
}

// classifyRestriction separates concrete evidence seen by AfterResponse (captcha
// page, 403/429 → engine.ErrAccessRestricted) from the engine's ambiguous "may be
// restricted" diagnostic. v2.3.x formats inner errors with %v, losing identity, so
// only the pinned library's fixed texts are matched; raw errors are never exposed.
func classifyRestriction(err error) (concrete, uncertain bool) {
	if err == nil {
		return false, false
	}
	if errors.Is(err, engine.ErrAccessRestricted) || strings.Contains(err.Error(), engine.ErrAccessRestricted.Error()) {
		return true, false
	}
	if errors.Is(err, dl.ErrUpstreamAccessRestricted) || strings.Contains(err.Error(), dl.ErrUpstreamAccessRestricted.Error()+":") {
		return false, true
	}
	return false, false
}
func same(a, b engine.RoomSpec) bool {
	return a.ShareGuestIdentity == b.ShareGuestIdentity && a.Cookie == b.Cookie && a.ProxyURL == b.ProxyURL && a.SignProvider == b.SignProvider && a.TikHubToken == b.TikHubToken && a.ProtocolMode == b.ProtocolMode
}
func (a *Adapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probe != nil {
		a.probe.Close()
		a.probe = nil
	}
}
func (a *Adapter) Run(ctx context.Context, spec engine.RoomSpec, sink engine.Sink) error {
	c, e := a.client(spec, sink)
	if e != nil {
		return e
	}
	defer c.Close()
	c.SubscribeMessage(func(msg *dl.LiveMessage) {
		if ctx.Err() != nil {
			return
		}
		sink.Event(engine.RawEvent{Method: msg.GetMethod(), ReceivedAt: msg.ReceivedAt, Bytes: len(msg.GetPayload()), Decode: func() modelEvent { return normalize(msg) }})
	})
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	sink.State(engine.EngineState{State: "connecting", Reason: "正在建立上游连接"})
	e = c.Start()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(e, dl.ErrLiveNotStarted) || e == nil {
		sink.State(engine.EngineState{State: "waiting", Reason: "直播已结束或尚未开播"})
		return nil
	}
	return errors.New("上游连接失败，请查看房间状态后重试")
}

type handler struct {
	sink engine.Sink
}

func (h *handler) Enabled(context.Context, slog.Level) bool { return true }
func (h *handler) Handle(_ context.Context, r slog.Record) error {
	// The engine's "may be restricted" log is ambiguous (seen right after streams end):
	// it only updates this room's state below. Concrete evidence trips in AfterResponse.
	if h.sink == nil {
		return nil
	}
	state := ClassifyLog(r.Message)
	if state != "" {
		h.sink.State(engine.EngineState{State: state, Reason: Reason(state)})
	}
	return nil
}
func (h *handler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *handler) WithGroup(string) slog.Handler      { return h }
func ClassifyLog(msg string) string {
	switch msg {
	case "WebSocket 连接成功", "重连成功":
		return "collecting"
	case "WebSocket 连接失败，准备重连", "检测到需重连，将稍后尝试", "重试连接失败":
		return "reconnecting"
	case "上游可能限制匿名访问", "连接最终失败":
		return "risk"
	case "直播状态已结束，不进行重连", "WS 读错后 HTTP 兜底确认直播已下播，不再重连":
		return "waiting"
	}
	return ""
}
func Reason(state string) string {
	switch state {
	case "collecting":
		return "上游连接已就绪"
	case "reconnecting":
		return "上游连接中断，正在退避重连"
	case "risk":
		return "上游状态无法确认或连续连接失败"
	case "waiting":
		return "上游已确认下播"
	}
	return state
}
