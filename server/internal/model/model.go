package model

import (
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

var MessageTypes = []string{"chat", "member", "like", "social", "gift", "room_stats"}

type Counts map[string]int64

func Zero() Counts {
	return Counts{"chat": 0, "member": 0, "like": 0, "social": 0, "gift": 0, "room_stats": 0}
}

type Options struct {
	GuestSharing   *bool    `json:"share_guest_identity,omitempty"`
	ProxyID        string   `json:"proxy_id"`
	AccountID      string   `json:"account_id"`
	Engine         string   `json:"engine"`
	ProxyURL       string   `json:"proxy_url"`
	Cookie         string   `json:"cookie"`
	SignProvider   string   `json:"sign_provider"`
	TikHubToken    string   `json:"tikhub_token"`
	ProtocolMode   string   `json:"protocol_mode"`
	PollSeconds    int      `json:"poll_seconds"`
	SilenceSeconds int      `json:"silence_seconds"` // 0 = adaptive
	EventTypes     []string `json:"event_types"`
}
type Overrides map[string]json.RawMessage
type Settings struct {
	ShareGuestIdentity bool    `json:"share_guest_identity"`
	Defaults           Options `json:"defaults"`
	MaxRooms           int     `json:"max_rooms"`
	StartsPerSecond    float64 `json:"starts_per_second"`
	RequestsPerSecond  float64 `json:"requests_per_second"`
	RollingSeconds     int     `json:"rolling_seconds"`
	StatsDays          int     `json:"stats_days"`
	EventDays          int     `json:"event_days"`
}

func DefaultSettings() Settings {
	return Settings{Defaults: Options{Engine: "douyinlive", SignProvider: "local", ProtocolMode: "web", PollSeconds: 15, EventTypes: slices.Clone(MessageTypes)}, MaxRooms: 200, StartsPerSecond: 2, RequestsPerSecond: 5, RollingSeconds: 10, StatsDays: 30, EventDays: 90}
}
func Merge(base Options, o Overrides) (Options, error) {
	base.EventTypes = slices.Clone(base.EventTypes)
	b, _ := json.Marshal(o)
	err := json.Unmarshal(b, &base)
	return base, err
}
func (o Options) Validate() error {
	if o.Engine != "douyinlive" {
		return errors.New("目前仅支持 douyinlive 引擎")
	}
	if o.SignProvider != "local" && o.SignProvider != "tikhub" {
		return errors.New("签名方式无效")
	}
	if o.SignProvider == "tikhub" && o.TikHubToken == "" {
		return errors.New("请先配置 TikHub 密钥")
	}
	if o.ProtocolMode != "web" && o.ProtocolMode != "pc" {
		return errors.New("协议画像无效")
	}
	if o.PollSeconds < 15 || o.PollSeconds > 120 {
		return errors.New("轮询间隔应为 15～120 秒")
	}
	if o.SilenceSeconds != 0 && (o.SilenceSeconds < 90 || o.SilenceSeconds > 300) {
		return errors.New("静默阈值应为自适应或 90～300 秒")
	}
	for _, t := range o.EventTypes {
		if !slices.Contains(MessageTypes, t) {
			return errors.New("消息类型无效")
		}
	}
	if len(o.Cookie) > 32768 || len(o.TikHubToken) > 4096 {
		return errors.New("凭据过长")
	}
	if o.ProxyURL != "" {
		u, e := url.Parse(o.ProxyURL)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "socks5") || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("代理须为 HTTP 或 SOCKS5 地址")
		}
	}
	return nil
}
func (s Settings) Validate() error {
	if s.Defaults.ProxyID != "" || s.Defaults.AccountID != "" || s.Defaults.GuestSharing != nil {
		return errors.New("池资源请在直播间单独分配")
	}
	if s.MaxRooms < 1 || s.MaxRooms > 2000 || s.StartsPerSecond < 0.1 || s.StartsPerSecond > 20 || s.RequestsPerSecond < 0.1 || s.RequestsPerSecond > 100 || s.RollingSeconds < 1 || s.RollingSeconds > 300 || s.StatsDays < 1 || s.StatsDays > 3650 || s.EventDays < 1 || s.EventDays > 3650 {
		return errors.New("全局设置超出允许范围")
	}
	return s.Defaults.Validate()
}
func ValidateOverrides(o Overrides) error {
	for k, v := range o {
		if strings.TrimSpace(string(v)) == "null" {
			return errors.New("继承默认值请删除覆盖项")
		}
		switch k {
		case "engine", "proxy_url", "cookie", "proxy_id", "account_id", "share_guest_identity", "sign_provider", "tikhub_token", "protocol_mode", "poll_seconds", "silence_seconds", "event_types":
		default:
			return errors.New("未知的直播间配置项")
		}
	}
	return nil
}
func PublicOptions(o Options) map[string]any {
	b, _ := json.Marshal(o)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	for _, k := range []string{"cookie", "tikhub_token", "proxy_url"} {
		v := out[k].(string)
		out[k] = ""
		out[k+"_configured"] = v != ""
	}
	return out
}
func ConnectionEqual(a, b Options) bool {
	if (a.GuestSharing == nil) != (b.GuestSharing == nil) || (a.GuestSharing != nil && *a.GuestSharing != *b.GuestSharing) {
		return false
	}
	return a.Engine == b.Engine && a.ProxyID == b.ProxyID && a.AccountID == b.AccountID && a.ProxyURL == b.ProxyURL && a.Cookie == b.Cookie && a.SignProvider == b.SignProvider && a.TikHubToken == b.TikHubToken && a.ProtocolMode == b.ProtocolMode
}

type RoomRecord struct {
	LiveID    string    `json:"live_id"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	RoomID    string    `json:"room_id"`
	Source    string    `json:"source"`
	Tags      []string  `json:"tags"`
	Note      string    `json:"note"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	Overrides Overrides `json:"-"`
	// Avatar is the anchor's avatar thumbnail URL reported by the last probe.
	Avatar string `json:"avatar"`
	// EndedAt (Unix ms) marks a task that stopped probing on its own because the
	// stream ended or never started; EndedReason is "live_ended" or "never_live".
	// The task stays enabled but sends no upstream request until resumed.
	EndedAt     int64  `json:"ended_at"`
	EndedReason string `json:"ended_reason"`
	// LastLiveAt (Unix ms) is the last time the task started collecting.
	LastLiveAt int64 `json:"last_live_at"`
}

// SubscriberRef names one external subscription that references a room.
type SubscriberRef struct {
	Client      string `json:"client"`
	ExternalRef string `json:"externalRef"`
}

// Dormant reports whether the task stopped probing on its own.
func (r RoomRecord) Dormant() bool { return r.EndedAt != 0 }

type User struct {
	ID       string `json:"id"`
	SecUID   string `json:"sec_uid,omitempty"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
}
type Event struct {
	ID         string         `json:"id"`
	Seq        uint64         `json:"seq"`
	Type       string         `json:"type"`
	LiveID     string         `json:"live_id"`
	RoomID     string         `json:"room_id"`
	TS         time.Time      `json:"ts"`
	ReceivedAt time.Time      `json:"received_at"`
	User       User           `json:"user"`
	Content    string         `json:"content"`
	Extra      map[string]any `json:"extra"`
}
type StateEvent struct {
	At   int64  `json:"at"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}
