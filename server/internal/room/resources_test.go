package room

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/pool"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestPoolAssignmentCapacityInvalidationAndPause(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{5}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := New(ctx, s, engine.NewBudget(100), func() engine.Engine { return &neverNetworkEngine{} }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	for _, id := range []string{"123456789", "223456789"} {
		if _, _, e = m.AddConfigured(ctx, id, nil, false, "manual", nil); e != nil {
			t.Fatal(e)
		}
	}
	account, e := m.SaveResource(model.Resource{Kind: "account", Name: "test", Enabled: true, Limit: 1, Secret: "sessionid=synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Assign("123456789", "account", account.ID, ""); e != nil {
		t.Fatal(e)
	}
	if e = m.Assign("223456789", "account", account.ID, ""); e == nil {
		t.Fatal("capacity exceeded")
	}
	m.Pools.CheckFunc = func(context.Context, model.Resource, string) pool.Result {
		return pool.Result{State: "invalid", Detail: "synthetic"}
	}
	if _, e = m.CheckResource(ctx, "account", account.ID, ""); e != nil {
		t.Fatal(e)
	}
	task := m.get("123456789")
	task.mu.Lock()
	var id string
	_ = json.Unmarshal(task.record.Overrides["account_id"], &id)
	task.mu.Unlock()
	if id != "" {
		t.Fatal("invalid account retained")
	}
	proxy, e := m.SaveResource(model.Resource{Kind: "proxy", Name: "proxy", Enabled: true, Limit: 5, Secret: "http://127.0.0.1:9000"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Assign("123456789", "proxy", proxy.ID, "least_used"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.CheckResource(ctx, "proxy", proxy.ID, ""); e != nil {
		t.Fatal(e)
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.record.Enabled {
		t.Fatal("invalid proxy silently switched to direct")
	}
}

type neverNetworkEngine struct{}

func (*neverNetworkEngine) Name() string { return "test" }
func (*neverNetworkEngine) Close()       {}
func (*neverNetworkEngine) Probe(ctx context.Context, s engine.RoomSpec) (engine.LiveStatus, error) {
	return engine.LiveStatus{Code: "offline"}, nil
}
func (*neverNetworkEngine) Run(ctx context.Context, s engine.RoomSpec, sink engine.Sink) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestScheduledChecksExcludeAccountsAndDisabledProxies(t *testing.T) {
	s, err := store.Open(t.TempDir(), bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := New(ctx, s, engine.NewBudget(100), func() engine.Engine { return &neverNetworkEngine{} }, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var account model.Resource
	for _, r := range []model.Resource{
		{Kind: "account", Name: "manual", Enabled: true, Limit: 1, Secret: "sessionid=synthetic-local-fixture"},
		{Kind: "proxy", Name: "enabled", Enabled: true, Limit: 1, Secret: "http://127.0.0.1:19090"},
		{Kind: "proxy", Name: "disabled", Enabled: false, Limit: 1, Secret: "http://127.0.0.1:19091"},
	} {
		saved, err := m.SaveResource(r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "account" {
			account = saved
		}
	}
	var checked []model.Resource
	m.Pools.CheckFunc = func(_ context.Context, r model.Resource, _ string) pool.Result {
		checked = append(checked, r)
		return pool.Result{State: "healthy", Detail: "local fixture"}
	}
	m.checkScheduledProxies(ctx)
	if len(checked) != 1 || checked[0].Kind != "proxy" || !checked[0].Enabled {
		t.Fatal("scheduled check touched account or disabled proxy")
	}
	if _, err := m.CheckResource(ctx, "account", account.ID, ""); err != nil {
		t.Fatal(err)
	}
	if len(checked) != 2 || checked[1].Kind != "account" {
		t.Fatal("explicit account check unavailable")
	}
	m.Budget.Restrict()
	m.checkScheduledProxies(ctx)
	if len(checked) != 2 {
		t.Fatal("scheduled probe bypassed node cooldown")
	}
}
