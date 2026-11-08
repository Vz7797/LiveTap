package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestIncidentRulesCooldownAndAcknowledgement(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{8}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := room.New(ctx, s, engine.NewBudget(100), func() engine.Engine { return nil }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	a, e := NewAlerts(ctx, m)
	if e != nil {
		t.Fatal(e)
	}
	a.Close()
	for _, kind := range []string{"silence", "risk", "proxy_invalid", "account_invalid", "node_cooldown"} {
		if e = s.Incident("123", kind, "synthetic alert"); e != nil {
			t.Fatal(e)
		}
	}
	a.Tick(time.Now())
	a.Tick(time.Now())
	list, e := a.List("", "", "")
	if e != nil || len(list) != 5 {
		t.Fatal("rules missing or duplicate", len(list), e)
	}
	if e = a.Ack(list[0].ID); e != nil {
		t.Fatal(e)
	}
	list, _ = a.List("", "node_cooldown", "")
	if len(list) != 1 || !list[0].Acked {
		t.Fatal("ack/filter failed")
	}
	cfg := a.Config()
	cfg.Enabled["silence"] = false
	cfg.Webhook = "https://example.invalid/hook?token=synthetic"
	if e = a.Save(cfg); e != nil {
		t.Fatal(e)
	}
	var raw string
	_ = s.DB.QueryRow("SELECT data FROM private_settings WHERE key='alerts'").Scan(&raw)
	if bytes.Contains([]byte(raw), []byte("synthetic")) {
		t.Fatal("webhook credential not encrypted")
	}
	_ = s.Incident("123", "silence", "disabled")
	a.Tick(time.Now())
	list, _ = a.List("", "silence", "")
	if len(list) != 1 {
		t.Fatal("disabled rule emitted")
	}
}

func TestCapacityAndMassFailureBoundaries(t *testing.T) {
	now := time.Now()
	cfg := AlertConfig{MassPercent: 30, CapacityPercent: 85}
	views := []map[string]any{}
	for i := 0; i < 20; i++ {
		events := []model.StateEvent{}
		if i < 6 {
			events = append(events, model.StateEvent{At: now.UnixMilli(), Kind: "risk"})
		}
		if i == 6 {
			events = append(events, model.StateEvent{At: now.Add(-6 * time.Minute).UnixMilli(), Kind: "reconnect"})
		}
		views = append(views, map[string]any{"status": "collecting", "events": events})
	}
	if alertConditions(views, 100, cfg, now)["mass_failure"] {
		t.Fatal("exactly 30 percent or old event must not trigger")
	}
	views[6]["events"] = []model.StateEvent{{At: now.UnixMilli(), Kind: "reconnect"}}
	if !alertConditions(views, 100, cfg, now)["mass_failure"] {
		t.Fatal("more than 30 percent did not trigger")
	}
	if alertConditions(views[:17], 20, cfg, now)["capacity"] {
		t.Fatal("exactly 85 percent must not trigger")
	}
	if !alertConditions(views[:18], 20, cfg, now)["capacity"] {
		t.Fatal("capacity threshold not enforced")
	}
	views[17]["status"] = "stopped"
	if alertConditions(views[:18], 20, cfg, now)["capacity"] {
		t.Fatal("stopped room counted")
	}
}

func TestWebhookRetriesOnlySanitizedAlert(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload Alert
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Rule != "node_cooldown" {
			t.Error("bad webhook payload")
		}
		if attempts < 3 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	a := &Alerts{cfg: AlertConfig{Webhook: server.URL}}
	a.notify(context.Background(), Alert{Rule: "node_cooldown", Detail: "节点正在冷却"})
	if attempts != 3 {
		t.Fatal("retry count", attempts)
	}
}
