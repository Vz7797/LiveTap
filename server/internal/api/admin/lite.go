package admin

import (
	"maps"
	"net/http"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

// Fields only the room detail panel needs. The list views and the 2.5 s stream
// omit them: at 100 rooms they made each push ~400 KB, enough to saturate a slow
// admin link (docs/design.md, revision v0.18). The panel fetches them on demand from
// GET /admin/api/rooms/{id}/detail.
var detailOnly = []string{"minutes", "options", "overrides", "enabledTypes", "diagnostics", "counts", "subscriptions"}

const (
	liteEvents = 3  // recent state changes shown on cards and the risk radar
	liteFeed   = 50 // the messages page renders at most this many at once
)

// liteSnapshot is the default /snapshot and stream payload. ?view=full on
// GET /admin/api/snapshot keeps the complete form for tools and tests.
func liteSnapshot(full map[string]any) map[string]any {
	v := maps.Clone(full)
	delete(v, "history")
	if rooms, ok := full["rooms"].([]map[string]any); ok {
		out := make([]map[string]any, 0, len(rooms))
		for _, r := range rooms {
			c := maps.Clone(r)
			for _, k := range detailOnly {
				delete(c, k)
			}
			if events, ok := c["events"].([]model.StateEvent); ok && len(events) > liteEvents {
				c["events"] = events[len(events)-liteEvents:]
			}
			out = append(out, c)
		}
		v["rooms"] = out
	}
	if feed, ok := v["feed"].([]map[string]any); ok && len(feed) > liteFeed {
		v["feed"] = feed[:liteFeed]
	}
	return v
}

func (s *Server) snapshotRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "full" {
		respond(w, 200, s.snapshot())
		return
	}
	respond(w, 200, liteSnapshot(s.snapshot()))
}

// history is the per-minute node history used by the 1 h / 24 h charts. It
// changes once a minute, so it is fetched separately instead of every push.
func (s *Server) historyRoute(w http.ResponseWriter, r *http.Request) {
	history, ok := s.Snapshots.Get()["history"]
	if !ok || history == nil {
		history = []any{}
	}
	respond(w, 200, history)
}

func (s *Server) roomDetailRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if rooms, ok := s.snapshot()["rooms"].([]map[string]any); ok {
		for _, room := range rooms {
			if room["liveId"] == id {
				respond(w, 200, room)
				return
			}
		}
	}
	apiError(w, 404, "not_found", "直播间不存在")
}
