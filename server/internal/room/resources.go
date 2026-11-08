package room

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/pool"
)

func resourceID(o model.Options, kind string) string {
	if kind == "proxy" {
		return o.ProxyID
	}
	return o.AccountID
}
func (m *Manager) ResourceViews(kind string) ([]model.Resource, error) {
	list, e := m.Pools.Store.Resources(kind)
	if e != nil {
		return nil, e
	}
	for i := range list {
		list[i].Secret = ""
		list[i].Rooms = []string{}
		for _, t := range m.all() {
			t.mu.Lock()
			o, _ := model.Merge(m.Settings().Defaults, t.record.Overrides)
			if resourceID(o, kind) == list[i].ID {
				list[i].Rooms = append(list[i].Rooms, t.record.LiveID)
			}
			t.mu.Unlock()
		}
	}
	return list, nil
}
func (m *Manager) validateResources(id string, o model.Options) error {
	return m.validateResourcesLocked(id, o)
}
func (m *Manager) validateResourcesLocked(id string, o model.Options) error {
	self := m.get(id)
	for _, kind := range []string{"proxy", "account"} {
		rid := resourceID(o, kind)
		if rid == "" {
			continue
		}
		r, e := m.Pools.Get(kind, rid)
		if e != nil || !pool.Usable(r) {
			return errors.New("所选资源不可用")
		}
		used := 0
		for _, t := range m.all() {
			if t == self {
				continue
			}
			t.mu.Lock()
			other, _ := model.Merge(m.Settings().Defaults, t.record.Overrides)
			if resourceID(other, kind) == rid {
				used++
			}
			t.mu.Unlock()
		}
		if used >= r.Limit {
			return errors.New("所选资源已达到分配上限")
		}
	}
	return nil
}
func (m *Manager) SaveResource(r model.Resource) (model.Resource, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	if r.ID != "" {
		list, e := m.ResourceViews(r.Kind)
		if e != nil {
			return r, e
		}
		for _, old := range list {
			if old.ID == r.ID && len(old.Rooms) > r.Limit {
				return r, errors.New("上限不能小于已分配数量")
			}
		}
	}
	out, e := m.Pools.Save(r)
	if e != nil {
		return out, e
	}
	e = m.refreshResource(out.Kind, out.ID, !out.Enabled)
	if e == nil {
		e = m.Store.Audit("pool_save", out.ID, "更新资源配置，敏感值不记录")
	}
	out.Secret = ""
	return out, e
}
func (m *Manager) DeleteResource(kind, id string) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if _, e := m.Pools.Get(kind, id); e != nil {
		return e
	}
	if e := m.refreshResource(kind, id, true); e != nil {
		return e
	}
	if e := m.Pools.Store.DeleteResource(kind, id); e != nil {
		return e
	}
	return m.Store.Audit("pool_delete", id, "删除资源并解除分配")
}
func (m *Manager) refreshResource(kind, id string, detach bool) error {
	for _, t := range m.all() {
		t.mu.Lock()
		o, _ := model.Merge(m.Settings().Defaults, t.record.Overrides)
		if resourceID(o, kind) != id {
			t.mu.Unlock()
			continue
		}
		if detach {
			field, raw := "account_id", "cookie"
			if kind == "proxy" {
				field, raw = "proxy_id", "proxy_url"
				t.record.Enabled = false
			}
			t.record.Overrides[field] = json.RawMessage(`""`)
			t.record.Overrides[raw] = json.RawMessage(`""`)
			if e := m.Store.SaveRoom(t.record); e != nil {
				t.mu.Unlock()
				return e
			}
		}
		t.signal()
		t.mu.Unlock()
	}
	return nil
}
func (m *Manager) CheckResource(ctx context.Context, kind, id, proxyID string) (model.Resource, error) {
	proxy := ""
	if proxyID != "" {
		r, e := m.Pools.Get("proxy", proxyID)
		if e != nil || !pool.Usable(r) {
			return model.Resource{}, errors.New("检测代理不可用")
		}
		proxy = r.Secret
	}
	result, e := m.Pools.Check(ctx, kind, id, proxy)
	if e != nil {
		return model.Resource{}, e
	}
	m.ops.Lock()
	defer m.ops.Unlock()
	current, err := m.Pools.Get(kind, id)
	if err != nil {
		return model.Resource{}, err
	}
	if current.Version != result.Version {
		return model.Resource{}, errors.New("检测完成后配置已变更，未执行摘除")
	}
	if result.State == "invalid" {
		if e = m.refreshResource(kind, id, true); e != nil {
			return result, e
		}
		_ = m.Store.Incident("", kind+"_invalid", "资源检测失败并已摘除；代理房间暂停，小号房间退回游客")
	}
	_ = m.Store.Audit("pool_check", id, "资源检测完成："+result.State)
	result.Secret = ""
	return result, nil
}
func (m *Manager) Assign(id, kind, rid, policy string) error {
	if kind != "proxy" && kind != "account" {
		return errors.New("资源类型无效")
	}
	// Update serializes and rechecks occupancy, so concurrent automatic picks cannot exceed limits.
	if policy != "" {
		list, e := m.ResourceViews(kind)
		if e != nil {
			return e
		}
		eligible := []model.Resource{}
		for _, r := range list {
			if pool.Usable(r) && len(r.Rooms) < r.Limit {
				eligible = append(eligible, r)
			}
		}
		if len(eligible) == 0 {
			return errors.New("没有可分配资源")
		}
		if policy == "least_used" {
			sort.SliceStable(eligible, func(i, j int) bool { return len(eligible[i].Rooms) < len(eligible[j].Rooms) })
			rid = eligible[0].ID
		} else if policy == "round_robin" {
			m.mu.Lock()
			cursor := m.poolCursor[kind]
			m.poolCursor[kind]++
			m.mu.Unlock()
			rid = eligible[cursor%len(eligible)].ID
		} else {
			return errors.New("分配策略无效")
		}
	}
	field, raw := "account_id", "cookie"
	if kind == "proxy" {
		field, raw = "proxy_id", "proxy_url"
	}
	data, _ := json.Marshal(rid)
	return m.Update(id, nil, nil, model.Overrides{field: data, raw: json.RawMessage(`""`)}, false)
}

// Only proxies are checked periodically. Account checks require an explicit manual action.
func (m *Manager) MonitorResources(ctx context.Context) {
	tick := time.NewTicker(30 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.checkScheduledProxies(ctx)
		}
	}
}

func (m *Manager) checkScheduledProxies(ctx context.Context) {
	list, err := m.Pools.Store.Resources("proxy")
	if err != nil {
		return
	}
	for _, r := range list {
		if ctx.Err() != nil || m.Budget.Cooldown().Active {
			return
		}
		if r.Enabled {
			_, _ = m.CheckResource(ctx, "proxy", r.ID, "")
		}
	}
}
