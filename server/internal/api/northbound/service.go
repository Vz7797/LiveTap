package northbound

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
	"github.com/Vz7797/LiveTap/server/internal/hub"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

// Version 取自 buildinfo，保留这个名字供已有调用方使用。
var Version = buildinfo.Version

const APIVersion = "v1"

type Options struct {
	StatusHeartbeatInterval time.Duration
}

type Fault struct {
	Code, Message, Suggestion string
	Status                    int
}

func (e *Fault) Error() string { return e.Message }
func fault(code, msg string, status int) *Fault {
	return &Fault{code, msg, "请核对请求与接入方权限后重试", status}
}

var ErrMissing = fault("not_found", "订阅或接入方不存在", 404)

type connection struct {
	client, subscription string
	q                    *hub.Outbox
}
type Service struct {
	mu                sync.Mutex
	Store             *store.Store
	Rooms             *room.Manager
	clients           map[string]model.Client
	hashes            map[string]string
	subs              map[string]model.Subscription
	connections       map[*hub.Outbox]connection
	Now               func() time.Time
	Resolve           func(context.Context, string) (string, error)
	cancel            context.CancelFunc
	done              chan struct{}
	heartbeatInterval time.Duration
	lastHeartbeat     map[string]time.Time
	released          map[string][]releaseNote
}

// releaseNote remembers a recent release for a client that may have been offline
// when it happened: renew reports its reason, and a reconnect with a cursor gets
// the subscription_released frame it missed. In the seventh-round test a client
// was away when all its leases expired, came back to a healthy stream carrying no
// subscriptions and received nothing for 12 minutes with no notice at all.
type releaseNote struct {
	id, live, reason string
	seq              uint64
	at               time.Time
}

const (
	releaseKeep  = 24 * time.Hour
	releaseLimit = 2000
)

func New(parent context.Context, m *room.Manager, options ...Options) (*Service, error) {
	if err := m.Store.InitNorthbound(); err != nil {
		return nil, err
	}
	clients, err := m.Store.Clients()
	if err != nil {
		return nil, err
	}
	subs, err := m.Store.Subscriptions()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Service{Store: m.Store, Rooms: m, clients: map[string]model.Client{}, hashes: map[string]string{}, subs: map[string]model.Subscription{}, connections: map[*hub.Outbox]connection{}, Now: time.Now, cancel: cancel, done: make(chan struct{})}
	s.heartbeatInterval = 60 * time.Second
	if len(options) > 0 && options[0].StatusHeartbeatInterval > 0 {
		s.heartbeatInterval = options[0].StatusHeartbeatInterval
	}
	s.lastHeartbeat = map[string]time.Time{}
	s.released = map[string][]releaseNote{}
	s.Resolve = func(ctx context.Context, input string) (string, error) { return room.Resolve(ctx, input, m.Budget) }
	m.Subscribers = s.SubscriberRefs
	for _, c := range clients {
		s.clients[c.ID] = c
		s.hashes[c.KeyHash] = c.ID
	}
	for _, sub := range subs {
		if !s.clients[sub.ClientID].Enabled || !sub.ExpiresAt.After(s.Now()) {
			if err = s.Store.DeleteSubscription(sub.ID); err != nil {
				cancel()
				return nil, err
			}
			continue
		}
		if err = m.EnsureSubscription(ctx, sub.LiveID); err != nil {
			cancel()
			return nil, err
		}
		s.subs[sub.ID] = sub
		s.lastHeartbeat[sub.LiveID] = s.Now()
	}
	records, err := m.Store.Rooms()
	if err != nil {
		cancel()
		return nil, err
	}
	for _, r := range records {
		if r.Source == "subscription" && !s.referenced(r.LiveID) {
			if err = m.ReleaseSubscription(r.LiveID); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	go func() {
		defer close(s.done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.Sweep()
			}
		}
	}()
	return s, nil
}
func (s *Service) Close() {
	s.cancel()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	for q := range s.connections {
		s.Rooms.Bus.Close(q)
	}
}
func validateClient(c model.Client) error {
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 || c.Quota < 1 || c.Quota > 2000 || len(c.Whitelist) > 32 {
		return fault("invalid_request", "名称、额度或 IP 网段数量无效", 400)
	}
	if c.Env != "生产" && c.Env != "预发" && c.Env != "测试" {
		return fault("invalid_request", "环境无效", 400)
	}
	for _, v := range c.Whitelist {
		p, err := netip.ParsePrefix(v)
		if err != nil || p.Addr().Is4In6() {
			return fault("invalid_request", "来源限制请使用 IPv4 或 IPv6 CIDR 网段", 400)
		}
	}
	return nil
}
func (s *Service) CreateClient(c model.Client) (model.Client, string, error) {
	if err := validateClient(c); err != nil {
		return model.Client{}, "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return model.Client{}, "", errors.New("生成密钥失败")
	}
	encoded := new(big.Int).SetBytes(random).Text(62)
	key := "lt_live_" + strings.Repeat("0", 43-len(encoded)) + encoded
	c.ID = "cli_" + auth.Token()[:16]
	c.CreatedAt = s.Now().UnixMilli()
	c.LastSeen = 0
	c.KeyHash = auth.Digest(key)
	if c.Whitelist == nil {
		c.Whitelist = []string{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Store.SaveClient(c); err != nil {
		return model.Client{}, "", err
	}
	s.clients[c.ID] = c
	s.hashes[c.KeyHash] = c.ID
	_ = s.Store.Audit("client_create", c.ID, "创建接入方；密钥只返回一次")
	return c, key, nil
}
func (s *Service) UpdateClient(id string, c model.Client) error {
	if err := validateClient(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.clients[id]
	if !ok {
		return ErrMissing
	}
	if c.Quota < s.count(id) {
		return fault("capacity_exceeded", "额度不能小于现有订阅数", 409)
	}
	c.ID = id
	c.KeyHash = old.KeyHash
	c.CreatedAt = old.CreatedAt
	c.LastSeen = old.LastSeen
	if err := s.Store.SaveClient(c); err != nil {
		return err
	}
	s.clients[id] = c
	// Policy changes close existing streams; the next handshake checks the new IP policy.
	if !c.Enabled || !slices.Equal(old.Whitelist, c.Whitelist) {
		s.closeClient(id)
	}
	if !c.Enabled {
		for sid, sub := range s.subs {
			if sub.ClientID == id {
				if err := s.remove(sid, "client_disabled"); err != nil {
					return err
				}
			}
		}
	}
	return s.Store.Audit("client_update", id, "更新接入方权限")
}
func (s *Service) DeleteClient(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[id]
	if !ok {
		return ErrMissing
	}
	s.closeClient(id)
	for sid, sub := range s.subs {
		if sub.ClientID == id {
			if err := s.remove(sid, "client_deleted"); err != nil {
				return err
			}
		}
	}
	if err := s.Store.DeleteClient(id); err != nil {
		return err
	}
	delete(s.clients, id)
	delete(s.hashes, c.KeyHash)
	delete(s.released, id)
	s.Rooms.Bus.ForgetClient(id)
	return s.Store.Audit("client_delete", id, "删除接入方并撤销订阅")
}
func (s *Service) Authenticate(key string, ip netip.Addr) (string, error) {
	if !strings.HasPrefix(key, "lt_live_") || len(key) != 51 {
		return "", fault("unauthorized", "API Key 无效", 401)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.hashes[auth.Digest(key)]
	c, ok := s.clients[id]
	if !ok || !c.Enabled {
		return "", fault("unauthorized", "API Key 无效或已停用", 401)
	}
	allowed := len(c.Whitelist) == 0
	for _, v := range c.Whitelist {
		p, _ := netip.ParsePrefix(v)
		if p.Contains(ip.Unmap()) {
			allowed = true
		}
	}
	if !allowed {
		return "", fault("forbidden", "来源 IP 不在允许范围", 403)
	}
	if now := s.Now().UnixMilli(); c.LastSeen == 0 || now-c.LastSeen >= 30000 {
		c.LastSeen = now
		if err := s.Store.SaveClient(c); err != nil {
			return "", err
		}
		s.clients[id] = c
	}
	return id, nil
}
func (s *Service) count(client string) int {
	n := 0
	for _, sub := range s.subs {
		if sub.ClientID == client {
			n++
		}
	}
	return n
}
func (s *Service) referenced(live string) bool {
	for _, sub := range s.subs {
		if sub.LiveID == live {
			return true
		}
	}
	return false
}

type SubscribeRequest struct {
	LiveID      string                    `json:"live_id"`
	ExternalRef string                    `json:"external_ref"`
	EventTypes  []string                  `json:"event_types"`
	Options     model.SubscriptionOptions `json:"options"`
}

func (s *Service) Subscribe(ctx context.Context, client string, v SubscribeRequest) (model.Subscription, error) {
	if _, err := room.ParseID(v.LiveID); err != nil {
		return model.Subscription{}, fault("invalid_live_id", "直播间标识无效", 400)
	}
	if len(v.ExternalRef) > 256 {
		return model.Subscription{}, fault("invalid_request", "external_ref 最长 256 字节", 400)
	}
	if v.EventTypes == nil {
		v.EventTypes = slices.Clone(model.MessageTypes)
	}
	for _, typ := range v.EventTypes {
		if typ != "status" && !slices.Contains(model.MessageTypes, typ) {
			return model.Subscription{}, fault("invalid_request", "事件类型无效", 400)
		}
	}
	if v.Options.LeaseSeconds == 0 {
		v.Options.LeaseSeconds = 120
	}
	if v.Options.IdleReleaseSeconds == 0 {
		v.Options.IdleReleaseSeconds = 3600
	}
	if v.Options.LeaseSeconds < 1 || v.Options.LeaseSeconds > 3600 || v.Options.IdleReleaseSeconds < 1 || v.Options.IdleReleaseSeconds > 86400 {
		return model.Subscription{}, fault("invalid_request", "租约或闲置时长超出范围", 400)
	}
	id, err := s.Resolve(ctx, v.LiveID)
	if err != nil {
		return model.Subscription{}, fault("upstream_unverified", "暂时无法确认唯一直播间号", 422)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[client]
	if !ok || !c.Enabled {
		return model.Subscription{}, fault("unauthorized", "接入方已停用", 401)
	}
	for _, sub := range s.subs {
		if sub.ClientID == client && sub.LiveID == id {
			if !sub.ExpiresAt.After(s.Now()) {
				if err = s.remove(sub.ID, "lease_expired"); err != nil {
					return model.Subscription{}, err
				}
				break
			}
			if sub.ExternalRef != v.ExternalRef || !slices.Equal(sub.EventTypes, v.EventTypes) || sub.Options != v.Options {
				return model.Subscription{}, fault("subscription_conflict", "同一直播间已有不同参数的订阅，请先退订", 409)
			}
			sub.ExpiresAt = s.Now().Add(time.Duration(sub.Options.LeaseSeconds) * time.Second)
			if err = s.Store.SaveSubscription(sub); err == nil {
				s.subs[sub.ID] = sub
			}
			return sub, err
		}
	}
	if s.count(client) >= c.Quota {
		return model.Subscription{}, fault("capacity_exceeded", "接入方订阅额度已满", 409)
	}
	if err = s.Rooms.EnsureSubscription(ctx, id); err != nil {
		if errors.Is(err, room.ErrCapacity) {
			return model.Subscription{}, &Fault{"capacity_exceeded", "节点容量已满", "请分配到其他节点", 409}
		}
		return model.Subscription{}, err
	}
	sub := model.Subscription{ID: "sub_" + auth.Token()[:16], ClientID: client, LiveID: id, ExternalRef: v.ExternalRef, EventTypes: v.EventTypes, Options: v.Options, ExpiresAt: s.Now().Add(time.Duration(v.Options.LeaseSeconds) * time.Second), CreatedAt: s.Now()}
	err = s.Rooms.Bus.CommitRoutes(func(cursor uint64) (map[*hub.Outbox]map[string]hub.Route, error) {
		sub.StartSeq = cursor
		if err := s.Store.SaveSubscription(sub); err != nil {
			return nil, err
		}
		s.subs[sub.ID] = sub
		if _, exists := s.lastHeartbeat[sub.LiveID]; !exists {
			s.lastHeartbeat[sub.LiveID] = s.Now()
		}
		updates := map[*hub.Outbox]map[string]hub.Route{}
		for q, c := range s.connections {
			if c.client == client {
				updates[q] = s.routes(client, c.subscription)
			}
		}
		return updates, nil
	})
	if err != nil {
		if !s.referenced(id) {
			_ = s.Rooms.ReleaseSubscription(id)
		}
		return model.Subscription{}, err
	}
	return sub, nil
}
func (s *Service) List(client string) []model.Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Subscription{}
	for _, sub := range s.subs {
		if sub.ClientID == client && sub.ExpiresAt.After(s.Now()) {
			out = append(out, sub)
		}
	}
	slices.SortFunc(out, func(a, b model.Subscription) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (s *Service) Get(client, id string) (model.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok || sub.ClientID != client || !sub.ExpiresAt.After(s.Now()) {
		return model.Subscription{}, ErrMissing
	}
	return sub, nil
}
func (s *Service) Unsubscribe(client, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok || sub.ClientID != client {
		return ErrMissing
	}
	return s.remove(id, "unsubscribed")
}

// Missing is a renew ID without a lease. Reason is the release reason when this
// client owned it recently, lease_expired before the sweep removes it, and
// not_found otherwise; whether another client owns the ID is never revealed.
type Missing struct {
	ID     string `json:"subscription_id"`
	LiveID string `json:"live_id,omitempty"`
	Reason string `json:"reason"`
}

// Renew extends every valid lease in the batch and reports the rest. It used to
// fail the whole batch on one invalid ID, so a single room ending between listing
// and renewing let every other lease of an offline client expire. ErrMissing is
// returned only when nothing could be renewed; missing still explains each ID.
func (s *Service) Renew(client string, ids []string) ([]model.Subscription, []Missing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.clients[client].Enabled {
		return nil, nil, fault("unauthorized", "接入方已停用", 401)
	}
	if len(ids) < 1 || len(ids) > 2000 {
		return nil, nil, fault("invalid_request", "请提供 1～2000 个订阅 ID", 400)
	}
	out, missing := []model.Subscription{}, []Missing{}
	var notes map[string]releaseNote
	done := map[string]bool{}
	for _, id := range ids {
		if done[id] {
			continue
		}
		done[id] = true
		sub, ok := s.subs[id]
		if ok && sub.ClientID == client && sub.ExpiresAt.After(s.Now()) {
			sub.ExpiresAt = s.Now().Add(time.Duration(sub.Options.LeaseSeconds) * time.Second)
			if err := s.Store.SaveSubscription(sub); err != nil {
				return nil, nil, err
			}
			s.subs[id] = sub
			out = append(out, sub)
			continue
		}
		if ok && sub.ClientID == client {
			missing = append(missing, Missing{id, sub.LiveID, "lease_expired"})
			continue
		}
		if notes == nil {
			notes = map[string]releaseNote{}
			for _, n := range s.released[client] {
				notes[n.id] = n
			}
		}
		if n, ok := notes[id]; ok {
			missing = append(missing, Missing{id, n.live, n.reason})
		} else {
			missing = append(missing, Missing{ID: id, Reason: "not_found"})
		}
	}
	if len(out) == 0 {
		return nil, missing, ErrMissing
	}
	return out, missing, nil
}
func (s *Service) routes(client, only string) map[string]hub.Route {
	out := map[string]hub.Route{}
	for _, sub := range s.subs {
		if sub.ClientID == client && (only == "" || only == sub.ID) && sub.ExpiresAt.After(s.Now()) {
			types := map[string]bool{}
			for _, v := range sub.EventTypes {
				types[v] = true
			}
			out[sub.LiveID] = hub.Route{SubscriptionID: sub.ID, ExternalRef: sub.ExternalRef, Types: types, StartSeq: sub.StartSeq}
		}
	}
	return out
}
func (s *Service) refresh(client string) {
	for q, c := range s.connections {
		if c.client == client {
			s.Rooms.Bus.Routes(q, s.routes(client, c.subscription))
		}
	}
}
func (s *Service) closeClient(client string) {
	for q, c := range s.connections {
		if c.client == client {
			s.Rooms.Bus.Close(q)
			delete(s.connections, q)
		}
	}
}
func (s *Service) remove(id, reason string) error {
	sub := s.subs[id]
	if err := s.Store.DeleteSubscription(id); err != nil {
		return err
	}
	delete(s.subs, id)
	s.refresh(sub.ClientID)
	p := s.Rooms.Bus.Control("subscription_released", map[string]any{"subscription_id": id, "live_id": sub.LiveID, "reason": reason})
	for q, c := range s.connections {
		if c.client == sub.ClientID && (c.subscription == "" || c.subscription == id) {
			q.Push(p)
		}
	}
	notes := append(s.released[sub.ClientID], releaseNote{id, sub.LiveID, reason, p.Event.Seq, s.Now()})
	cut := 0
	for cut < len(notes) && (len(notes)-cut > releaseLimit || s.Now().Sub(notes[cut].at) > releaseKeep) {
		cut++
	}
	if cut > 0 {
		notes = slices.Clone(notes[cut:])
	}
	s.released[sub.ClientID] = notes
	if !s.referenced(sub.LiveID) {
		delete(s.lastHeartbeat, sub.LiveID)
		return s.Rooms.ReleaseSubscription(sub.LiveID)
	}
	return nil
}

// ClientSubscriptions lists one client's subscriptions for the admin view,
// newest first. ErrMissing when the client does not exist.
func (s *Service) ClientSubscriptions(client string) ([]model.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[client]; !ok {
		return nil, ErrMissing
	}
	out := []model.Subscription{}
	for _, sub := range s.subs {
		if sub.ClientID == client {
			out = append(out, sub)
		}
	}
	slices.SortFunc(out, func(a, b model.Subscription) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// SubscriberRefs lists active subscriptions per live ID for the admin views.
func (s *Service) SubscriberRefs() map[string][]model.SubscriberRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]model.SubscriberRef{}
	for _, sub := range s.subs {
		out[sub.LiveID] = append(out[sub.LiveID], model.SubscriberRef{Client: s.clients[sub.ClientID].Name, ExternalRef: sub.ExternalRef})
	}
	for _, refs := range out {
		slices.SortFunc(refs, func(a, b model.SubscriberRef) int {
			return cmp.Or(strings.Compare(a.Client, b.Client), strings.Compare(a.ExternalRef, b.ExternalRef))
		})
	}
	return out
}

func (s *Service) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rooms.Bus.Expire()
	now := s.Now()
	for id, sub := range s.subs {
		status, exists := s.Rooms.Status(sub.LiveID)
		reason := ""
		if !exists {
			reason = "room_removed"
		} else if status.Dormant && status.EndedReason == "live_ended" {
			reason = "live_ended"
		} else if status.Dormant || (status.Source == "subscription" && !status.WaitingSince.IsZero() && now.Sub(status.WaitingSince) >= time.Duration(sub.Options.IdleReleaseSeconds)*time.Second) {
			reason = "idle_release"
		} else if !sub.ExpiresAt.After(now) {
			reason = "lease_expired"
		}
		if reason != "" {
			_ = s.remove(id, reason)
		}
	}
	s.heartbeats(now)
}

// Only a successful WebSocket ping extends leases, so a dead peer cannot hold a
// room forever merely because the TCP socket has not timed out yet.
func (s *Service) KeepAlive(q *hub.Outbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.connections[q]
	if !ok || !s.clients[c.client].Enabled {
		return ErrMissing
	}
	client := s.clients[c.client]
	if s.Now().UnixMilli()-client.LastSeen >= 30000 {
		client.LastSeen = s.Now().UnixMilli()
		if err := s.Store.SaveClient(client); err != nil {
			return err
		}
		s.clients[c.client] = client
	}
	for id, sub := range s.subs {
		if sub.ClientID == c.client && (c.subscription == "" || id == c.subscription) && sub.ExpiresAt.After(s.Now()) {
			if sub.ExpiresAt.Sub(s.Now()) > time.Duration(sub.Options.LeaseSeconds)*time.Second/2 {
				continue
			}
			sub.ExpiresAt = s.Now().Add(time.Duration(sub.Options.LeaseSeconds) * time.Second)
			if err := s.Store.SaveSubscription(sub); err != nil {
				return err
			}
			s.subs[id] = sub
		}
	}
	return nil
}
func (s *Service) Open(client, only string, since *uint64, boot string) (*hub.Outbox, []hub.Packet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.clients[client].Enabled {
		return nil, nil, fault("unauthorized", "接入方已停用", 401)
	}
	count := 0
	for _, c := range s.connections {
		if c.client == client {
			count++
		}
	}
	if count >= 8 {
		return nil, nil, fault("rate_limited", "每个接入方最多 8 条推送连接", 429)
	}
	if only != "" {
		sub, ok := s.subs[only]
		if !ok || sub.ClientID != client || !sub.ExpiresAt.After(s.Now()) {
			return nil, nil, ErrMissing
		}
	}
	q, replay := s.Rooms.Bus.Open(client, s.routes(client, only), since, s.Rooms.BootID, boot, s.missed(client, only, since, boot)...)
	s.connections[q] = connection{client, only, q}
	return q, replay, nil
}

// missed lists the releases a reconnecting all-subscription stream has not seen:
// those after its cursor on this boot. A stream without a cursor, or with cursor
// 0, starts afresh and should list its subscriptions instead; a single-
// subscription stream is refused once its subscription ends. Repeats are possible
// and harmless.
func (s *Service) missed(client, only string, since *uint64, boot string) []hub.Notice {
	if only != "" || since == nil || *since == 0 || boot != s.Rooms.BootID {
		return nil
	}
	out := []hub.Notice{}
	for _, n := range s.released[client] {
		if n.seq > *since && s.Now().Sub(n.at) <= releaseKeep {
			out = append(out, hub.Notice{Code: "subscription_released", Extra: map[string]any{"subscription_id": n.id, "live_id": n.live, "reason": n.reason}})
		}
	}
	return out
}
func (s *Service) CloseStream(q *hub.Outbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, q)
	s.Rooms.Bus.Close(q)
}
func (s *Service) DeliveryAllowed(client string, p hub.Packet) bool {
	if p.Event.SubscriptionID == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[p.Event.SubscriptionID]
	return ok && sub.ClientID == client && sub.ExpiresAt.After(s.Now())
}
