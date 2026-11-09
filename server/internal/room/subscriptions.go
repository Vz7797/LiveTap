package room

import (
	"context"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

type Status struct {
	LiveID        string          `json:"live_id"`
	RoomID        string          `json:"room_id"`
	State         model.RoomState `json:"state"`
	Reason        string          `json:"reason"`
	ReasonCode    string          `json:"reason_code"`
	Source        string          `json:"-"`
	Enabled       bool            `json:"-"`
	Dormant       bool            `json:"-"`
	EndedReason   string          `json:"-"`
	WaitingSince  time.Time       `json:"-"`
	LastMessageAt *time.Time      `json:"last_message_at"`
}

func (m *Manager) Status(id string) (Status, bool) {
	t := m.get(id)
	if t == nil {
		return Status{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	status := Status{LiveID: t.record.LiveID, RoomID: t.record.RoomID, State: t.state, Reason: t.reason, ReasonCode: t.reasonCode, Source: t.record.Source, Enabled: t.record.Enabled, Dormant: t.record.Dormant(), EndedReason: t.record.EndedReason, WaitingSince: t.waitingSince}
	if stamp := t.lastMessage.Load(); stamp != 0 {
		at := time.UnixMilli(stamp).UTC()
		status.LastMessageAt = &at
	}
	return status, true
}
func (m *Manager) EnsureSubscription(ctx context.Context, id string) error {
	r, _, err := m.AddConfigured(ctx, id, nil, true, "subscription", nil)
	if err != nil {
		return err
	}
	// A new subscription restarts a task that stopped on its own (the merchant is
	// going live again); a manual task paused by the administrator stays paused.
	if (r.Source == "subscription" && !r.Enabled) || (r.Enabled && r.Dormant()) {
		return m.Action(id, "resume")
	}
	return nil
}

// Manual tasks retain their owner even when the last external reference expires.
func (m *Manager) ReleaseSubscription(id string) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if status, ok := m.Status(id); ok && status.Source == "subscription" {
		return m.removeLocked(id)
	}
	return nil
}
func (m *Manager) Capacity() (used, live int) {
	for _, t := range m.all() {
		t.mu.Lock()
		if t.record.Enabled && !t.record.Dormant() {
			used++
		}
		if t.state.Live() {
			live++
		}
		t.mu.Unlock()
	}
	return
}
func (m *Manager) StatusEvent(id, reason string) {
	m.Bus.Publish(model.Event{LiveID: id, Type: "status", TS: time.Now(), ReceivedAt: time.Now(), Extra: map[string]any{"state": string(model.StateStopped), "reason": reason, "reason_code": model.ReasonStopped}}, nil)
}
