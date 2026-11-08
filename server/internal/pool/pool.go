package pool

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

type Result struct {
	State     string
	Detail    string
	LatencyMS int64
}
type CheckFunc func(context.Context, model.Resource, string) Result
type Service struct {
	Store     *store.Store
	Budget    *engine.Budget
	mu        sync.Mutex
	CheckFunc CheckFunc
}

func New(s *store.Store, b *engine.Budget) (*Service, error) {
	if e := s.InitResources(); e != nil {
		return nil, e
	}
	p := &Service{Store: s, Budget: b}
	p.CheckFunc = p.check
	return p, nil
}
func (p *Service) Get(kind, id string) (model.Resource, error) {
	list, e := p.Store.Resources(kind)
	if e != nil {
		return model.Resource{}, e
	}
	for _, r := range list {
		if r.ID == id {
			return r, nil
		}
	}
	return model.Resource{}, errors.New("资源不存在")
}
func Validate(r model.Resource) error {
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 || len(r.Tags) > 20 || r.Limit < 1 || r.Limit > 2000 {
		return errors.New("名称、标签或分配上限无效")
	}
	for _, tag := range r.Tags {
		if len(tag) > 100 {
			return errors.New("标签过长")
		}
	}
	if r.Kind == "proxy" {
		u, e := url.Parse(r.Secret)
		if e != nil || u.Hostname() == "" || u.Port() == "" || (u.Scheme != "http" && u.Scheme != "socks5") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("代理须为带端口的 HTTP 或 SOCKS5 地址")
		}
	} else if r.Kind == "account" {
		if len(r.Secret) > 32768 || strings.ContainsAny(r.Secret, "\r\n") || !strings.Contains(r.Secret, "=") {
			return errors.New("请填写单行 Cookie，最长 32 KB")
		}
		request := &http.Request{Header: http.Header{}}
		request.Header.Set("Cookie", r.Secret)
		if len(request.Cookies()) == 0 {
			return errors.New("Cookie 格式无效")
		}
	} else {
		return errors.New("资源类型无效")
	}
	return nil
}
func (p *Service) Save(r model.Resource) (model.Resource, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.ID != "" {
		old, e := p.Get(r.Kind, r.ID)
		if e != nil {
			return r, e
		}
		if r.Secret == "" {
			r.Secret = old.Secret
		}
		r.State, r.Detail, r.CheckedAt, r.LatencyMS = old.State, old.Detail, old.CheckedAt, old.LatencyMS
		if r.Secret != old.Secret {
			r.State = "unknown"
			r.CheckedAt = 0
			r.Detail = "配置已更新，待检测"
		}
	} else {
		r.ID = auth.Token()[:16]
		r.State = "unknown"
		r.Detail = "尚未检测"
	}
	if e := Validate(r); e != nil {
		return model.Resource{}, e
	}
	r.Version = time.Now().UnixNano()
	r.Mask = "Cookie 已加密保存"
	if r.Kind == "proxy" {
		u, _ := url.Parse(r.Secret)
		if u.User != nil {
			u.User = url.User("***")
		}
		r.Mask = u.String()
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r, p.Store.SaveResource(r)
}
func Usable(r model.Resource) bool {
	return r.Enabled && (r.State == "healthy" || r.State == "unknown")
}
func (p *Service) Resolve(o model.Options) (model.Options, error) {
	for _, entry := range []struct{ kind, id string }{{"proxy", o.ProxyID}, {"account", o.AccountID}} {
		if entry.id == "" {
			continue
		}
		r, e := p.Get(entry.kind, entry.id)
		if e != nil || !Usable(r) {
			return o, errors.New("已分配资源不可用，请重新分配")
		}
		if entry.kind == "proxy" {
			o.ProxyURL = r.Secret
		} else {
			o.Cookie = r.Secret
		}
	}
	return o, nil
}
func (p *Service) Check(ctx context.Context, kind, id, proxy string) (model.Resource, error) {
	r, e := p.Get(kind, id)
	if e != nil {
		return r, e
	}
	if p.Budget.Cooldown().Active {
		return r, errors.New("节点冷却中，请到期后再检测")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := p.CheckFunc(ctx, r, proxy)
	if ctx.Err() != nil {
		return r, errors.New("检测已取消或超时，保留原状态")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	current, e := p.Get(kind, id)
	if e != nil {
		return r, e
	}
	if current.Version != r.Version {
		return current, errors.New("检测期间配置已修改，请重新检测")
	}
	r.State, r.Detail, r.LatencyMS, r.CheckedAt = result.State, result.Detail, result.LatencyMS, time.Now().UnixMilli()
	r.Version = time.Now().UnixNano()
	return r, p.Store.SaveResource(r)
}
