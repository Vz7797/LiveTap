package model

// RoomState is the lifecycle state of one room's collection task (docs/design.md
// §5.2). It is sent to integrators as-is, so values are only ever added, never
// renamed. Every classification of states lives here; callers ask a state what
// it is instead of comparing strings, so the rules cannot drift apart.
type RoomState string

const (
	StateProbing      RoomState = "probing"      // asking the upstream whether the room is live
	StateWaiting      RoomState = "waiting"      // not live (or just ended); polled again later
	StateConnecting   RoomState = "connecting"   // live; establishing the upstream connection
	StateCollecting   RoomState = "collecting"   // receiving messages
	StateReconnecting RoomState = "reconnecting" // the connection dropped; retrying soon
	StateRisk         RoomState = "risk"         // unconfirmed: restriction, timeout or repeated failure
	StateNotFound     RoomState = "notfound"     // the room does not exist or is closed to this region
	StateStopped      RoomState = "stopped"      // paused by an operator
	StateEnded        RoomState = "ended"        // the stream ended (or never started); no more probing
)

// StateCooling never belongs to a task. Northbound status heartbeats report it
// while the node cooldown blocks a task that is still trying to start.
const StateCooling RoomState = "cooling"

// RoomStates lists every task state in display order.
var RoomStates = []RoomState{StateCollecting, StateConnecting, StateReconnecting, StateProbing, StateWaiting, StateRisk, StateNotFound, StateEnded, StateStopped}

// Known reports whether s is one of RoomStates.
func (s RoomState) Known() bool {
	for _, k := range RoomStates {
		if s == k {
			return true
		}
	}
	return false
}

// Live: messages are being received.
func (s RoomState) Live() bool { return s == StateCollecting }

// Abnormal: needs attention; counted against node health and shown as incidents.
func (s RoomState) Abnormal() bool {
	return s == StateRisk || s == StateReconnecting || s == StateNotFound
}

// Final: the task neither probes nor collects until an operator or a new
// subscription resumes it. Final tasks hold no capacity.
func (s RoomState) Final() bool {
	return s == StateStopped || s == StateNotFound || s == StateEnded
}

// Active: the task holds node capacity and counts toward health and capacity
// alerts. It is exactly the opposite of Final.
func (s RoomState) Active() bool { return !s.Final() }

// EventKind is the room timeline kind recorded when a task enters s.
func (s RoomState) EventKind() string {
	switch s {
	case StateProbing:
		return "probe"
	case StateCollecting:
		return "ready"
	case StateConnecting:
		return "online"
	case StateReconnecting:
		return "reconnect"
	case StateRisk, StateNotFound:
		return "risk"
	default: // waiting, stopped, ended
		return "offline"
	}
}

// Reason codes: a stable, machine-readable cause sent with every status as
// reason_code, next to the human-readable reason text. Codes are only added,
// never renamed (docs/api.md lists them).
const (
	ReasonQueued               = "queued"                // waiting for the start limiter
	ReasonProbing              = "probing"               // probing the upstream
	ReasonNotLive              = "not_live"              // not live yet; polled again later
	ReasonEndedGrace           = "ended_grace"           // just ended; kept for a quick restart
	ReasonUpstreamOffline      = "upstream_offline"      // the engine saw the stream end
	ReasonConnecting           = "connecting"            // establishing the upstream connection
	ReasonCollecting           = "collecting"            // receiving messages
	ReasonNoData               = "no_data"               // connected, but neither connection receives anything
	ReasonConnectionDropped    = "connection_dropped"    // the collection ended; retrying shortly
	ReasonEngineReconnecting   = "engine_reconnecting"   // the engine is reconnecting by itself
	ReasonUpstreamFailed       = "upstream_failed"       // the engine gave up connecting
	ReasonProbeFailed          = "probe_failed"          // the probe failed without a clear answer
	ReasonProbeTimeout         = "probe_timeout"         // the probe ran out of time
	ReasonAccessRestricted     = "access_restricted"     // verification page or 403/429 seen
	ReasonNodeCooldown         = "node_cooldown"         // the node cooldown blocks new requests
	ReasonResourcesUnavailable = "resources_unavailable" // the assigned proxy or account cannot be used
	ReasonRebuildLimited       = "rebuild_limited"       // too many silent rebuilds in 30 minutes
	ReasonRoomNotFound         = "room_not_found"        // the upstream says the room does not exist
	ReasonRegionBlocked        = "region_blocked"        // the room is closed to the node's region
	ReasonStopped              = "stopped"               // paused by an operator
	ReasonEndedAfterLive       = "ended_after_live"      // ended and did not restart within the grace
	ReasonEndedNeverLive       = "ended_never_live"      // a manual or test task never went live
)

// ReasonCodes lists every reason code (for documentation checks).
var ReasonCodes = []string{
	ReasonQueued, ReasonProbing, ReasonNotLive, ReasonEndedGrace, ReasonUpstreamOffline, ReasonConnecting,
	ReasonCollecting, ReasonNoData, ReasonConnectionDropped, ReasonEngineReconnecting, ReasonUpstreamFailed,
	ReasonProbeFailed, ReasonProbeTimeout, ReasonAccessRestricted, ReasonNodeCooldown, ReasonResourcesUnavailable,
	ReasonRebuildLimited, ReasonRoomNotFound, ReasonRegionBlocked, ReasonStopped, ReasonEndedAfterLive, ReasonEndedNeverLive,
}
