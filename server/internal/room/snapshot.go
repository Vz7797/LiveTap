package room

import (
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (m *Manager) Views() ([]map[string]any, []model.Event, []map[string]any) {
	views := []map[string]any{}
	feed := []model.Event{}
	timeline := []map[string]any{}
	now := time.Now()
	// The current minute is still filling up: msgPerMin counts it in full and the
	// previous minute for the part of the last 60 s it still covers. Averaging the two
	// minutes halved the value at every minute start and climbed back (a sawtooth).
	filled := float64(now.Sub(now.Truncate(time.Minute))) / float64(time.Minute)
	refs := map[string][]model.SubscriberRef{}
	if m.Subscribers != nil {
		refs = m.Subscribers()
	}
	for _, t := range m.all() {
		t.mu.Lock()
		r := t.record
		o := t.options
		threshold := o.SilenceSeconds
		if threshold == 0 {
			threshold = t.silence.Threshold(now)
		}
		t.reconnects = slices.DeleteFunc(t.reconnects, func(x time.Time) bool { return now.Sub(x) > 30*time.Minute })
		counts := maps.Clone(t.counts)
		minutes := []model.Counts{}
		spark := []int64{}
		var previous, current int64
		for i := 29; i >= 0; i-- {
			at := now.Truncate(time.Minute).Add(-time.Duration(i) * time.Minute).UnixMilli()
			c := maps.Clone(t.minutes[at])
			if c == nil {
				c = model.Zero()
			}
			minutes = append(minutes, c)
			var sum int64
			for _, typ := range model.MessageTypes {
				if typ != "room_stats" {
					sum += c[typ]
				}
			}
			spark = append(spark, sum)
			if i == 1 {
				previous = sum
			} else if i == 0 {
				current = sum
			}
		}
		rate := current + int64(float64(previous)*(1-filled)+0.5)
		overrides := []string{}
		for k := range r.Overrides {
			overrides = append(overrides, k)
		}
		sort.Strings(overrides)
		dropped := t.dropped.Load()
		var live *hub.DropCounts
		if t.queue != nil {
			dropped += t.queue.Dropped.Load()
			live = &t.queue.Drops
		}
		v := map[string]any{"liveId": r.LiveID, "roomId": r.RoomID, "name": r.Name, "title": r.Title, "city": "", "category": "", "status": t.state, "reason": t.reason, "source": r.Source, "tags": list(r.Tags), "note": r.Note, "engine": o.Engine, "sign": o.SignProvider, "protocol": o.ProtocolMode, "proxyId": nil, "accountId": nil, "online": t.online, "totalPv": t.totalPV, "heat": 0, "msgPerMin": rate, "reconnects30m": len(t.reconnects), "lastMsgAt": t.last.Load(), "since": t.since.UnixMilli(), "spark": spark, "onlineSpark": t.onlineSpark(now), "counts": counts, "subscribers": len(refs[r.LiveID]), "subscriberClients": clientNames(refs[r.LiveID]), "subscriptions": list(refs[r.LiveID]), "avatar": r.Avatar, "endedAt": r.EndedAt, "endedReason": r.EndedReason, "lastLiveAt": r.LastLiveAt, "silenceThreshold": threshold, "events": list(t.events), "enabledTypes": list(o.EventTypes), "minutes": minutes, "noData": t.noData, "dropped": t.dropped.Load(), "options": model.PublicOptions(o), "overrides": overrides}
		v["dropped"] = dropped
		v["drops"] = t.drops.Stats(live)
		requests, totalRequests := m.Budget.RoomCounts(r.LiveID)
		v["diagnostics"] = map[string]any{"disconnects": t.disconnects, "reconnectDurationsMs": list(t.reconnectDurations), "reconnectRecoveryCount": t.reconnectRecoveryCount, "disconnectedAt": int64(0), "silenceFalsePositives": t.silenceFalsePositives, "requestsPerMinute": requests, "requestsTotal": totalRequests}
		if !t.disconnectedAt.IsZero() {
			v["diagnostics"].(map[string]any)["disconnectedAt"] = t.disconnectedAt.UnixMilli()
		}
		if o.ProxyURL != "" {
			v["proxyId"] = "custom"
		}
		if o.ProxyID != "" {
			v["proxyId"] = o.ProxyID
		}
		if o.Cookie != "" {
			v["accountId"] = "custom"
		}
		if o.AccountID != "" {
			v["accountId"] = o.AccountID
		}
		views = append(views, v)
		for i, x := range t.events {
			tone := "info"
			if x.Kind == "risk" {
				tone = "warn"
			}
			timeline = append(timeline, map[string]any{"id": r.LiveID + ":" + time.UnixMilli(x.At).Format(time.RFC3339Nano) + ":" + string(rune(i+65)), "at": x.At, "tone": tone, "title": r.Name, "detail": x.Text})
		}
		allowed := slices.Clone(o.EventTypes)
		t.mu.Unlock()
		for _, e := range t.buffer.Events() {
			if slices.Contains(allowed, e.Type) {
				feed = append(feed, e)
			}
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i]["liveId"].(string) < views[j]["liveId"].(string) })
	sort.Slice(feed, func(i, j int) bool { return feed[i].Seq > feed[j].Seq })
	sort.Slice(timeline, func(i, j int) bool { return timeline[i]["at"].(int64) > timeline[j]["at"].(int64) })
	if len(timeline) > 30 {
		timeline = timeline[:30]
	}
	return views, feed, timeline
}
func (m *Manager) Events(id string) ([]model.Event, error) {
	t := m.get(id)
	if t == nil {
		return nil, ErrNotFound
	}
	t.mu.Lock()
	allowed := slices.Clone(t.options.EventTypes)
	t.mu.Unlock()
	items := t.buffer.Events()
	items = slices.DeleteFunc(items, func(e model.Event) bool { return !slices.Contains(allowed, e.Type) })
	slices.Reverse(items)
	return list(items), nil
}
func (m *Manager) RoomOptions(id string) (map[string]any, error) {
	t := m.get(id)
	if t == nil {
		return nil, ErrNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return model.PublicOptions(t.options), nil
}

// list copies a slice for JSON output and never returns nil: a nil slice encodes as
// null, and the admin UI (and northbound consumers) iterate these fields directly.
// Rooms added by northbound subscriptions have no tags, which previously produced
// "tags": null and blanked the whole admin UI (2026-10-16 review).
func clientNames(refs []model.SubscriberRef) []string {
	names := []string{}
	for _, ref := range refs {
		if !slices.Contains(names, ref.Client) {
			names = append(names, ref.Client)
		}
	}
	return names
}

func list[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return slices.Clone(s)
}
