package northbound

import (
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

// Called with s.mu held, after expired subscriptions have been removed. One
// room event fans out through the normal routes to every valid subscription;
// sharing a room must not multiply its heartbeat or upstream request rate.
func (s *Service) heartbeats(now time.Time) {
	seen := map[string]bool{}
	cooldown := s.Rooms.Budget.Cooldown()
	for _, sub := range s.subs {
		if seen[sub.LiveID] || !sub.ExpiresAt.After(now) {
			continue
		}
		seen[sub.LiveID] = true
		if now.Sub(s.lastHeartbeat[sub.LiveID]) < s.heartbeatInterval {
			continue
		}
		status, exists := s.Rooms.Status(sub.LiveID)
		if !exists {
			continue
		}
		extra := map[string]any{
			"code": "heartbeat", "state": status.State, "reason": status.Reason,
			"last_message_at":            status.LastMessageAt,
			"heartbeat_interval_seconds": s.heartbeatInterval.Seconds(),
			"node_cooldown":              cooldown.Active,
			"cooldown_remaining_seconds": cooldown.RemainingSeconds,
		}
		// Cooling blocks new probes/connections, but leaves established collectors
		// and explicitly stopped/notfound tasks in their actual states.
		if cooldown.Active && status.Enabled && status.State != "collecting" && status.State != "stopped" && status.State != "notfound" {
			extra["base_state"] = status.State
			extra["state"] = "cooling"
			extra["reason"] = "节点冷却中，暂停新的探测和建连"
		}
		s.Rooms.Bus.Publish(model.Event{
			Type: "status", LiveID: status.LiveID, RoomID: status.RoomID,
			TS: now, ReceivedAt: now, Extra: extra,
		}, nil)
		s.lastHeartbeat[sub.LiveID] = now
	}
}
