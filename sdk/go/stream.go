package collector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Event struct {
	ID             string    `json:"id"`
	Seq            uint64    `json:"seq"`
	Type           string    `json:"type"`
	SubscriptionID string    `json:"subscription_id"`
	ExternalRef    string    `json:"external_ref"`
	LiveID         string    `json:"live_id"`
	RoomID         string    `json:"room_id"`
	TS             time.Time `json:"ts"`
	ReceivedAt     time.Time `json:"received_at"`
	User           struct {
		ID       string `json:"id"`
		SecUID   string `json:"sec_uid"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	} `json:"user"`
	Content string                     `json:"content"`
	Extra   map[string]json.RawMessage `json:"extra"`
}

func (e Event) StringField(key string) string {
	var v string
	_ = json.Unmarshal(e.Extra[key], &v)
	return v
}

type Cursor struct {
	BootID string
	Seq    uint64
}
type GapError struct{ Reason string }

func (e *GapError) Error() string { return "collector stream has a gap: " + e.Reason }

type StreamOptions struct {
	SubscriptionID string
	Cursor         *Cursor
	Deduper        *Deduper
	OnEvent        func(Event) error
	OnGap          func(Event)
	// OnReleased receives subscription_released frames, including those the node
	// replays on reconnect for releases that happened while the stream was away.
	// Without it they reach OnEvent as status events, as before.
	OnReleased func(Released)
	OnConnect  func()
	OnRetry    func(error)
}
type seenEvent struct {
	id string
	at time.Time
}
type seenRoom struct {
	ids   map[string]bool
	order []seenEvent
}
type Deduper struct {
	mu    sync.Mutex
	rooms map[string]*seenRoom
}

func NewDeduper() *Deduper { return &Deduper{rooms: map[string]*seenRoom{}} }
func (d *Deduper) seen(e Event, mark bool) bool {
	if e.ID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for id, r := range d.rooms {
		if len(r.order) > 0 && now.Sub(r.order[len(r.order)-1].at) > 10*time.Minute {
			delete(d.rooms, id)
		}
	}
	r := d.rooms[e.LiveID]
	if r == nil {
		r = &seenRoom{ids: map[string]bool{}}
		d.rooms[e.LiveID] = r
	}
	for len(r.order) > 0 && now.Sub(r.order[0].at) > 10*time.Minute {
		delete(r.ids, r.order[0].id)
		r.order[0] = seenEvent{}
		r.order = r.order[1:]
	}
	if r.ids[e.ID] {
		return true
	}
	if mark {
		for len(r.order) >= 2000 {
			delete(r.ids, r.order[0].id)
			r.order[0] = seenEvent{}
			r.order = r.order[1:]
		}
		r.ids[e.ID] = true
		r.order = append(r.order, seenEvent{e.ID, now})
	}
	return false
}

// Stream reconnects with the last successfully delivered data cursor. Control
// status frames have their own seq, but must never advance the replay cursor.
// A missing OnGap handler stops with GapError instead of silently losing data.
func (c *Client) Stream(ctx context.Context, o StreamOptions) error {
	if o.OnEvent == nil {
		return errors.New("OnEvent is required")
	}
	cursor := o.Cursor
	if cursor == nil {
		cursor = &Cursor{}
	}
	dedupe := o.Deduper
	if dedupe == nil {
		dedupe = NewDeduper()
	}
	if cursor.BootID == "" {
		node, err := c.Node(ctx)
		if err != nil {
			return err
		}
		cursor.BootID = node.BootID
	}
	delay := 100 * time.Millisecond
	for ctx.Err() == nil {
		u, _ := url.Parse(c.base + "/v1/stream")
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
		query := u.Query()
		query.Set("since_seq", strconv.FormatUint(cursor.Seq, 10))
		query.Set("boot_id", cursor.BootID)
		if o.SubscriptionID != "" {
			query.Set("subscription", o.SubscriptionID)
		}
		u.RawQuery = query.Encode()
		conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.key}}, CompressionMode: websocket.CompressionDisabled})
		if err == nil {
			conn.SetReadLimit(64 << 10)
			if o.OnConnect != nil {
				o.OnConnect()
			}
			delay = 100 * time.Millisecond
			terminal := false
			for {
				_, data, readErr := conn.Read(ctx)
				if readErr != nil {
					break
				}
				var e Event
				if json.Unmarshal(data, &e) != nil {
					err = errors.New("invalid collector event")
					terminal = true
					break
				}
				if e.Type == "status" && e.SubscriptionID == "" {
					code := e.StringField("code")
					if code == "hello" {
						boot := e.StringField("boot_id")
						if cursor.BootID != boot {
							cursor.BootID = boot
							cursor.Seq = 0
						}
						continue
					}
					if code == "gap" || code == "backpressure" {
						var reset bool
						_ = json.Unmarshal(e.Extra["reset_cursor"], &reset)
						if reset {
							cursor.Seq = 0
						}
						if o.OnGap == nil {
							err = &GapError{Reason: code}
							terminal = true
							break
						}
						o.OnGap(e)
						continue
					}
					if code == "subscription_released" && o.OnReleased != nil {
						o.OnReleased(Released{SubscriptionID: e.StringField("subscription_id"), LiveID: e.StringField("live_id"), Reason: e.StringField("reason")})
						continue
					}
					if err = o.OnEvent(e); err != nil {
						terminal = true
						break
					}
					continue
				}
				if !dedupe.seen(e, false) {
					if err = o.OnEvent(e); err != nil {
						terminal = true
						break
					}
					dedupe.seen(e, true)
				}
				cursor.Seq = max(cursor.Seq, e.Seq)
			}
			_ = conn.CloseNow()
			if terminal {
				return err
			}
		} else if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404) {
			return &APIError{Status: resp.StatusCode, Code: "stream_rejected"}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Never expose the websocket library's error string or handshake headers.
		if o.OnRetry != nil {
			o.OnRetry(errors.New("collector stream disconnected; retrying"))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(10*time.Second, delay*2)
	}
	return ctx.Err()
}
