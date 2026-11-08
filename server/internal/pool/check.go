package pool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	req "github.com/jwwsjlm/req/v3"
)

// A probe never logs response bodies, Cookie headers or proxy URLs.
func (p *Service) check(ctx context.Context, r model.Resource, proxy string) Result {
	address := "https://live.douyin.com/"
	if r.Kind == "proxy" {
		proxy = r.Secret
	} else {
		address = "https://www.douyin.com/aweme/v1/web/user/profile/self/?aid=6383&device_platform=webapp"
	}
	client, e := newCheckClient(proxy, p.Budget)
	if e != nil {
		return Result{State: "invalid", Detail: "代理配置无效"}
	}
	defer client.GetTransport().CloseIdleConnections()
	request := client.R().SetContext(ctx).SetHeader("Referer", "https://www.douyin.com/")
	if r.Kind == "account" {
		request.SetHeader("Cookie", r.Secret)
	}
	at := time.Now()
	resp, e := request.Get(address)
	if e != nil {
		if p.Budget.Cooldown().Active {
			return Result{State: "restricted", Detail: "上游要求验证，不能据此判定资源失效"}
		}
		state := "invalid"
		if r.Kind == "account" {
			state = "unknown"
		}
		return Result{State: state, Detail: "连接检测失败，未回传上游错误内容"}
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return Result{State: "unknown", Detail: "响应读取失败，暂不能确认"}
	}
	result := Classify(r.Kind, resp.StatusCode, body)
	result.LatencyMS = time.Since(at).Milliseconds()
	return result
}

// Keep this profile aligned with third_party/douyinLive/session_profile.go:
// the pinned req client supplies Chrome 133 Windows UA, TLS and HTTP/2 settings.
func newCheckClient(proxy string, budget *engine.Budget) (*req.Client, error) {
	client := req.C().ImpersonateChromeWithOS(req.BrowserOSWindows).
		SetProxy(nil).SetTimeout(15 * time.Second).
		SetRedirectPolicy(req.NoRedirectPolicy()).DisableAutoReadResponse()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "socks5") {
			return nil, errors.New("代理配置无效")
		}
		client.SetProxy(http.ProxyURL(u)).DisableHTTP3()
	} else {
		client.EnableHTTP3().EnableHTTP3FallbackOnError()
	}
	client.GetTransport().WrapRoundTripFunc(func(next http.RoundTripper) req.HttpRoundTripFunc {
		return (&checkedTransport{base: next, budget: budget}).RoundTrip
	})
	return client, nil
}

type checkedTransport struct {
	base   http.RoundTripper
	budget *engine.Budget
}

func (t *checkedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if e := t.budget.BeforeRequest(r); e != nil {
		return nil, e
	}
	resp, e := t.base.RoundTrip(r)
	if why := engine.RestrictedReason(resp); why != "" {
		t.budget.RestrictBecause("pool_check " + why)
		if resp.Body != nil {
			resp.Body.Close()
		}
		return nil, engine.ErrAccessRestricted
	}
	return resp, e
}
func Classify(kind string, status int, body []byte) Result {
	if status == 401 {
		return Result{State: "invalid", Detail: "上游明确拒绝登录态"}
	}
	if status < 200 || status >= 300 {
		return Result{State: "unknown", Detail: "上游未返回可确认结果"}
	}
	if kind == "proxy" {
		return Result{State: "healthy", Detail: "已通过代理完成 HTTPS 访问"}
	}
	var value struct {
		Code *int `json:"status_code"`
		User struct {
			UID string `json:"uid"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &value) != nil || value.Code == nil {
		return Result{State: "unknown", Detail: "登录态未确认，可能需要更新检测接口"}
	}
	if *value.Code == 0 && value.User.UID != "" && value.User.UID != "0" {
		return Result{State: "healthy", Detail: "上游已确认登录用户"}
	}
	if *value.Code == 8 || *value.Code == 2483 {
		return Result{State: "invalid", Detail: "上游提示登录态失效"}
	}
	return Result{State: "unknown", Detail: "未确认登录态，不视为 Cookie 有效"}
}
