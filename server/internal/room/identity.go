package room

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	dl "github.com/jwwsjlm/douyinLive/v2"
	"github.com/jwwsjlm/req/v3"
	"github.com/tidwall/gjson"
)

var idPattern = regexp.MustCompile(`^[0-9]{9,20}$`)

func ParseID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "://") {
		u, e := url.Parse(input)
		if e != nil || u.Hostname() != "live.douyin.com" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return "", errors.New("请填写直播间号、抖音短号或 live.douyin.com 直播链接")
		}
		input = strings.Trim(u.Path, "/")
	}
	return dl.ValidateLiveID(input)
}
func Resolve(ctx context.Context, input string, b *engine.Budget) (string, error) {
	id, e := ParseID(input)
	if e != nil {
		return "", errors.New("直播间标识无效")
	}
	if idPattern.MatchString(id) {
		return id, nil
	}
	c := req.C().ImpersonateChrome().SetTimeout(20 * time.Second)
	defer c.CloseIdleConnections()
	c.GetTransport().WrapRoundTripFunc(func(next http.RoundTripper) req.HttpRoundTripFunc {
		return func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() != "live.douyin.com" {
				return nil, errors.New("直播间地址跳转不受支持")
			}
			if e := b.BeforeRequest(r); e != nil {
				return nil, e
			}
			response, err := next.RoundTrip(r)
			if why := engine.RestrictedReason(response); why != "" {
				b.RestrictBecause("resolve_live_id " + why)
				if response.Body != nil {
					response.Body.Close()
				}
				return nil, engine.ErrAccessRestricted
			}
			return response, err
		}
	})
	resp, e := c.R().SetContext(ctx).DisableAutoReadResponse().Get("https://live.douyin.com/" + id)
	if e != nil {
		return "", errors.New("短号暂时无法确认，请使用 live.douyin.com 后的直播间号")
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if e != nil {
		return "", errors.New("读取直播间信息失败")
	}
	canonical := canonicalFromPage(string(body))
	if canonical == "" {
		return "", errors.New("未能解析唯一直播间号，请改用完整直播链接")
	}
	return canonical, nil
}
func canonicalFromPage(body string) string {
	// Only roomStore owns this page's identity. Recommendations also contain
	// web_rid fields and must never be mistaken for the requested room.
	body = strings.ReplaceAll(body, `\"`, `"`)
	key := `"roomStore":`
	if at := strings.Index(body, key); at >= 0 {
		id := gjson.Get(strings.TrimSpace(body[at+len(key):]), "roomInfo.web_rid").String()
		if idPattern.MatchString(id) {
			return id
		}
	}
	return ""
}
