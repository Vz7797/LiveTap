package lab

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
)

var categoryPattern = regexp.MustCompile(`^[0-9]{1,3}_[0-9]{1,3}$`)
var roomPattern = regexp.MustCompile(`"web_rid"\s*:\s*"([0-9]{9,20})"`)

func ParseRooms(body string) []string {
	out := []string{}
	seen := map[string]bool{}
	body = strings.ReplaceAll(body, `\"`, `"`)
	for _, match := range roomPattern.FindAllStringSubmatch(body, -1) {
		id := match[1]
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

type gatedTransport struct {
	base   http.RoundTripper
	budget *engine.Budget
}

func (g gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if e := g.budget.BeforeRequest(req); e != nil {
		return nil, e
	}
	resp, e := g.base.RoundTrip(req)
	if why := engine.RestrictedReason(resp); why != "" {
		g.budget.RestrictBecause("lab_discovery " + why)
		if resp.Body != nil {
			resp.Body.Close()
		}
		return nil, engine.ErrAccessRestricted
	}
	return resp, e
}
func (s *Service) Discover(ctx context.Context, categories []string) ([]string, error) {
	if len(categories) < 1 || len(categories) > 10 {
		return nil, errors.New("请选择 1～10 个分类")
	}
	for _, c := range categories {
		if !categoryPattern.MatchString(c) {
			return nil, errors.New("分类编号无效")
		}
	}
	if s.m.Budget.Cooldown().Active {
		return nil, errors.New("节点冷却中，请稍后再发现直播间")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: gatedTransport{transport, s.m.Budget}, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	out := []string{}
	seen := map[string]bool{}
	for _, category := range categories {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://live.douyin.com/category/"+category, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/133.0.0.0 Safari/537.36")
		response, e := client.Do(req)
		if e != nil {
			return nil, errors.New("分类发现失败或上游受限，已停止请求")
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if e != nil || response.StatusCode != 200 {
			return nil, errors.New("分类页未返回可用内容")
		}
		for _, id := range ParseRooms(string(body)) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("分类页没有可解析房间，请手动填写样本，不反复请求")
	}
	return out, nil
}
