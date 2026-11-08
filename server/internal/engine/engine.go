package engine

import (
	"context"
	"errors"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

var ErrAccessRestricted = errors.New("上游匿名访问受限")

// ErrAccessUncertain is the engine's ambiguous "may be restricted" result (live page
// without state and an empty web/enter). Server tests on 2026-10-16 showed it right
// after a stream ends; it only backs off one room and never trips the node cooldown.
var ErrAccessUncertain = errors.New("上游状态暂时不明确")

// ErrProbeTimeout means a status probe ran out of time (network or upstream slowness).
// It is retried at a short fixed interval and never counts as the stream ending.
var ErrProbeTimeout = errors.New("直播状态探测超时")

type RoomSpec struct {
	LiveID             string
	ShareGuestIdentity bool
	model.Options
}
type LiveStatus struct{ Code, LiveID, RoomID, Name, Title, Avatar string }
type EngineState struct{ State, Reason string }

// Decode is deliberately executed by the room worker, never by the upstream callback.
type RawEvent struct {
	Method     string
	ReceivedAt time.Time
	Bytes      int
	Decode     func() model.Event
}
type Sink interface {
	Event(RawEvent)
	State(EngineState)
}
type Engine interface {
	Name() string
	Probe(context.Context, RoomSpec) (LiveStatus, error)
	Run(context.Context, RoomSpec, Sink) error
	Close()
}
type Factory func() Engine
