package douyinlive

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Vz7797/LiveTap/server/internal/model"
	dl "github.com/jwwsjlm/douyinLive/v2"
	p "github.com/jwwsjlm/douyinlive-proto/generated/new_douyin"
	"google.golang.org/protobuf/proto"
)

type modelEvent = model.Event

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func normalize(m *dl.LiveMessage) model.Event {
	if m == nil || m.Raw == nil {
		return model.Event{}
	}
	// v2.3.x only pre-decodes control messages. Decode ordinary payloads here,
	// on the room worker, so the upstream callback stays non-blocking.
	if m.Parsed == nil {
		var decoded proto.Message
		switch m.GetMethod() {
		case "WebcastChatMessage":
			decoded = &p.Webcast_Im_ChatMessage{}
		case "WebcastMemberMessage":
			decoded = &p.Webcast_Im_MemberMessage{}
		case "WebcastLikeMessage":
			decoded = &p.Webcast_Im_LikeMessage{}
		case "WebcastSocialMessage":
			decoded = &p.Webcast_Im_SocialMessage{}
		case "WebcastGiftMessage":
			decoded = &p.Webcast_Im_GiftMessage{}
		case "WebcastRoomUserSeqMessage":
			decoded = &p.Webcast_Im_RoomUserSeqMessage{}
		default:
			return model.Event{}
		}
		if err := proto.Unmarshal(m.Raw.GetPayload(), decoded); err != nil {
			return model.Event{}
		}
		m.Parsed = decoded
	}
	e := model.Event{ID: strconv.FormatUint(m.Raw.GetMsgId(), 10), LiveID: m.LiveID, RoomID: m.RoomID, TS: m.ReceivedAt, ReceivedAt: m.ReceivedAt, Extra: map[string]any{}}
	if e.ID == "0" {
		e.ID = ""
	}
	if c, ok := m.Parsed.(interface{ GetCommon() *p.Webcast_Im_Common }); ok {
		if ts := c.GetCommon().GetCreateTime(); ts > 0 {
			candidate := time.Unix(int64(ts), 0)
			if ts >= 1_000_000_000_000 {
				candidate = time.UnixMilli(int64(ts))
			}
			// Upstream messages use both seconds and milliseconds. Invalid values
			// must not make the entire JSON response fail to serialize.
			if candidate.Year() >= 2000 && !candidate.After(m.ReceivedAt.Add(24*time.Hour)) {
				e.TS = candidate
			}
		}
	}
	if u, ok := m.Parsed.(interface{ GetUser() *p.Webcast_Data_User }); ok {
		user := u.GetUser()
		e.User = model.User{ID: strconv.FormatUint(user.GetId(), 10), SecUID: trim(user.GetSecUid(), 200), Nickname: trim(user.GetNickname(), 200)}
		if urls := user.GetAvatarThumb().GetUrlList(); len(urls) > 0 {
			e.User.Avatar = trim(urls[0], 600)
		}
	}
	switch v := m.Parsed.(type) {
	case *p.Webcast_Im_ChatMessage:
		e.Type = "chat"
		e.Content = trim(v.GetContent(), 2000)
	case *p.Webcast_Im_MemberMessage:
		e.Type = "member"
		e.Content = "进入直播间"
	case *p.Webcast_Im_LikeMessage:
		e.Type = "like"
		e.Content = "为主播点赞"
		e.Extra["count"] = v.GetCount()
		e.Extra["total"] = v.GetTotal()
	case *p.Webcast_Im_SocialMessage:
		e.Type = "social"
		e.Extra["action"] = v.GetAction()
		e.Content = "关注或分享直播间"
	case *p.Webcast_Im_GiftMessage:
		e.Type = "gift"
		e.Extra["gift_name"] = trim(v.GetGift().GetName(), 200)
		e.Extra["repeat_count"] = v.GetRepeatCount()
		e.Content = "送出 " + trim(v.GetGift().GetName(), 200)
	case *p.Webcast_Im_RoomUserSeqMessage:
		e.Type = "room_stats"
		e.Extra["online"] = v.GetTotal()
		e.Extra["total_pv"] = v.GetTotalPvForAnchor()
	}
	return e
}
