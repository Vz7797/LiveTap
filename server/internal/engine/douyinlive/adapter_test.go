package douyinlive

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	dl "github.com/jwwsjlm/douyinLive/v2"
	p "github.com/jwwsjlm/douyinlive-proto/generated/new_douyin"
	"google.golang.org/protobuf/proto"
)

func TestAccessRestrictionSurvivesPinnedProbeFormatting(t *testing.T) {
	flattened := fmt.Errorf("%w: live_id=123: %v: verification", dl.ErrLiveStatusUnknown, dl.ErrUpstreamAccessRestricted)
	if !accessRestricted(flattened) || !accessRestricted(dl.ErrUpstreamAccessRestricted) {
		t.Fatal("access restriction was lost")
	}
	if accessRestricted(nil) || accessRestricted(errors.New("connection timeout")) {
		t.Fatal("unrelated error classified as restriction")
	}
}

// Regression (2026-10-16 server test): the engine's ambiguous "may be restricted"
// result appeared right after streams ended and tripped the whole node. It must stay
// room-level; only concrete evidence (engine.ErrAccessRestricted) is node-level.
func TestAmbiguousRestrictionIsRoomLevelOnly(t *testing.T) {
	flattened := fmt.Errorf("%w: live_id=123: %v: verification", dl.ErrLiveStatusUnknown, dl.ErrUpstreamAccessRestricted)
	if concrete, uncertain := classifyRestriction(flattened); concrete || !uncertain {
		t.Fatalf("ambiguous engine result: concrete=%v uncertain=%v", concrete, uncertain)
	}
	wrapped := fmt.Errorf("请求直播间信息失败: %v", engine.ErrAccessRestricted)
	if concrete, uncertain := classifyRestriction(wrapped); !concrete || uncertain {
		t.Fatalf("concrete evidence: concrete=%v uncertain=%v", concrete, uncertain)
	}
	if concrete, uncertain := classifyRestriction(errors.New("connection timeout")); concrete || uncertain {
		t.Fatal("unrelated error classified as restriction")
	}
}

func TestPinnedLogContract(t *testing.T) {
	for msg, state := range map[string]string{"WebSocket 连接成功": "collecting", "重连成功": "collecting", "WebSocket 连接失败，准备重连": "reconnecting", "检测到需重连，将稍后尝试": "reconnecting", "连接最终失败": "risk", "WS 读错后 HTTP 兜底确认直播已下播，不再重连": "waiting"} {
		if ClassifyLog(msg) != state {
			t.Fatalf("log contract changed: %s", msg)
		}
	}
	if ClassifyLog("unrelated cookie=secret") != "" {
		t.Fatal("unrecognized log used as state")
	}
}
func TestNormalizeUpstreamTimestampUnits(t *testing.T) {
	now := time.Now()
	for _, ts := range []uint64{uint64(now.Unix()), uint64(now.UnixMilli()), ^uint64(0)} {
		e := normalize(&dl.LiveMessage{ReceivedAt: now, Raw: &p.Webcast_Im_Message{}, Parsed: &p.Webcast_Im_ChatMessage{Common: &p.Webcast_Im_Common{CreateTime: ts}}})
		if _, err := json.Marshal(e); err != nil {
			t.Fatal(err)
		}
		if e.TS.Year() != now.Year() {
			t.Fatal("invalid event timestamp")
		}
	}
}
func TestNormalizeRawPayloadContract(t *testing.T) {
	payload, err := proto.Marshal(&p.Webcast_Im_ChatMessage{Content: "真实协议消息", User: &p.Webcast_Data_User{Id: 9223372036854775000, Nickname: "观众"}})
	if err != nil {
		t.Fatal(err)
	}
	e := normalize(&dl.LiveMessage{LiveID: "123", ReceivedAt: time.Now(), Raw: &p.Webcast_Im_Message{Method: "WebcastChatMessage", MsgId: 7701234567890123456, Payload: payload}})
	if e.Type != "chat" || e.Content != "真实协议消息" || e.User.ID != "9223372036854775000" {
		t.Fatal("raw payload was not decoded")
	}
	e = normalize(&dl.LiveMessage{Raw: &p.Webcast_Im_Message{Method: "WebcastChatMessage", Payload: []byte{255}}})
	if e.Type != "" {
		t.Fatal("corrupt payload accepted")
	}
}
func TestNormalizeMessageIDAndUser(t *testing.T) {
	e := normalize(&dl.LiveMessage{LiveID: "123", RoomID: "456", ReceivedAt: time.Now(), Raw: &p.Webcast_Im_Message{MsgId: 7701234567890123456}, Parsed: &p.Webcast_Im_ChatMessage{Content: "你好", User: &p.Webcast_Data_User{Id: 9223372036854775000, Nickname: "观众"}}})
	if e.ID != "7701234567890123456" || e.User.ID != "9223372036854775000" || e.Type != "chat" || e.Content != "你好" {
		t.Fatalf("normalization corrupted ID or text")
	}
}

func TestUpstreamStatusCodeIsKeptAndRegionBlockIsRecognised(t *testing.T) {
	flattened := fmt.Errorf("%w: live_id=416229594796: retry: all attempts failed:\n\t#1: 直播间信息接口返回异常 status_code=4003073", dl.ErrLiveStatusUnknown)
	if got := upstreamStatusCode(flattened); got != RegionBlockedCode {
		t.Fatalf("status code = %q", got)
	}
	if upstreamStatusCode(errors.New("直播间信息接口返回异常 status_code=30003")) != EndedCode {
		t.Fatal("ended stream code not recognised")
	}
	if upstreamStatusCode(errors.New("直播间信息接口返回异常 status_code=10011")) != "status_code=10011" {
		t.Fatal("other upstream codes must keep their number for the log")
	}
	if upstreamStatusCode(nil) != "" || upstreamStatusCode(errors.New("timeout")) != "" {
		t.Fatal("no code expected")
	}
}
