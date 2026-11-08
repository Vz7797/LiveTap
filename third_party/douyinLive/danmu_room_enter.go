package douyinLive

// LiveTap patch: try an unsigned web/enter request before the live page.
//
// 2026-10-16 server test: once a datacenter IP is flagged, every live page
// (live.douyin.com/<web_rid>) returns a 6 KB captcha page, and the signed
// web/enter request returns an empty body. An unsigned web/enter request that
// carries the guest ttwid still returns full room data (room id, owner id,
// status). web/enter alone provides everything the WebSocket needs (room id and
// owner id as user_unique_id), so the live page is only a fallback. Skipping it
// also saves about 1.3 MB per room start.

import (
	"context"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func (dl *DouyinLive) buildUnsignedRoomEnterParams() string {
	screenWidth, screenHeight := dl.fingerprint.screenSize()
	parts := []string{
		"aid=" + webcastAid,
		"app_name=" + webcastAppName,
		"live_id=" + webcastLiveID,
		"device_platform=" + webcastDevice,
		"language=zh-CN",
		"enter_from=web_live",
		"cookie_enabled=true",
		fmt.Sprintf("screen_width=%d", screenWidth),
		fmt.Sprintf("screen_height=%d", screenHeight),
		"browser_language=zh-CN",
		"browser_platform=Win32",
		"browser_name=Chrome",
		"browser_version=" + queryEscapeValue(chromeVersionFromUserAgent(dl.userAgent)),
		"web_rid=" + queryEscapeValue(dl.liveID),
	}
	return strings.Join(parts, "&")
}

// tryUnsignedRoomEnter returns a usable web/enter body, or ok=false so the caller
// keeps the original live-page + signed web/enter flow unchanged.
func (dl *DouyinLive) tryUnsignedRoomEnter(ctx context.Context) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dl.cookieValue("ttwid") == "" {
		return "", false
	}
	headers := map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Encoding": "identity",
		"Cookie":          dl.getCookieString(),
		"Referer":         "https://live.douyin.com/" + dl.liveID,
		"User-Agent":      dl.userAgent,
	}
	dl.applyProtocolHeaders(headers)
	resp, err := dl.client.R().
		SetContext(ctx).
		SetHeaders(headers).
		Get("https://live.douyin.com/webcast/room/web/enter/?" + dl.buildUnsignedRoomEnterParams())
	if err != nil || resp == nil {
		dl.logger.Debug("未签名 web/enter 不可用，回到直播页流程", "live_id", dl.liveID, "err", err)
		return "", false
	}
	body, err := responseString(resp)
	if err != nil || strings.TrimSpace(body) == "" || gjson.Get(body, "status_code").Int() != 0 {
		dl.logger.Debug("未签名 web/enter 未返回可用数据，回到直播页流程", "live_id", dl.liveID, "status", resp.GetStatusCode(), "raw_len", len(body))
		return "", false
	}
	if _, err = parseRoomInfo(body); err != nil {
		return "", false
	}
	if _, known := parseLiveStatusFromRoomEnter(body); !known {
		return "", false
	}
	return body, true
}

// applyUnsignedRoomEnter records room data and status exactly like the tail of
// refreshRoomEnterDataAfterLivePage does for a successful web/enter body.
func (dl *DouyinLive) applyUnsignedRoomEnter(body string) bool {
	roomInfo, err := parseRoomInfo(body)
	if err != nil {
		return false
	}
	isLive, known := parseLiveStatusFromRoomEnter(body)
	if !known {
		return false
	}
	dl.setLiveStatus(isLive)
	if strings.TrimSpace(roomInfo.liveName) == "" {
		dl.logMissingLiveName("web_enter_unsigned", roomInfo)
	}
	dl.updateRoomInfoFromEnter(roomInfo)
	dl.storeRoomEnterData(body)
	dl.logger.Debug("未签名 web/enter 获取房间信息成功", "live_id", dl.liveID, "room_id", roomInfo.roomID, "is_live", isLive)
	return true
}
