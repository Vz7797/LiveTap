package collector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestStreamDedupeGapCallbackAndCursor(t *testing.T) {
	for _, handleGap := range []bool{false, true} {
		t.Run(map[bool]string{false: "gap-stops", true: "gap-callback"}[handleGap], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/node" {
					_ = json.NewEncoder(w).Encode(Node{BootID: "boot"})
					return
				}
				if r.URL.Query().Get("boot_id") != "boot" || r.URL.Query().Get("since_seq") != "0" {
					t.Error("resume cursor missing")
				}
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				for _, e := range []Event{{Type: "status", Seq: 99, Extra: map[string]json.RawMessage{"code": json.RawMessage(`"hello"`), "boot_id": json.RawMessage(`"boot"`)}}, {ID: "synthetic-message", Seq: 1, Type: "chat", LiveID: "one", SubscriptionID: "sub"}, {ID: "synthetic-message", Seq: 2, Type: "chat", LiveID: "one", SubscriptionID: "sub"}, {Type: "status", Seq: 100, Extra: map[string]json.RawMessage{"code": json.RawMessage(`"gap"`)}}} {
					data, _ := json.Marshal(e)
					if c.Write(ctx, websocket.MessageText, data) != nil {
						return
					}
				}
				<-ctx.Done()
			}))
			defer server.Close()
			c, err := New(server.URL, "in-memory-test-only")
			if err != nil {
				t.Fatal(err)
			}
			cursor := &Cursor{}
			events, gaps := 0, 0
			o := StreamOptions{Cursor: cursor, OnEvent: func(e Event) error { events++; return nil }}
			if handleGap {
				o.OnGap = func(Event) { gaps++; cancel() }
			}
			err = c.Stream(ctx, o)
			cancel()
			if events != 1 || cursor.Seq != 2 {
				t.Fatal("dedupe failed or control seq incorrectly advanced cursor")
			}
			if handleGap {
				if gaps != 1 {
					t.Fatal("gap callback missing")
				}
			} else {
				var gap *GapError
				if !errors.As(err, &gap) {
					t.Fatal("gap was silently ignored")
				}
			}
		})
	}
}
func TestFailedConsumerDoesNotAdvanceCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		data, _ := json.Marshal(Event{ID: "fixture", Type: "chat", SubscriptionID: "sub", Seq: 8})
		_ = c.Write(ctx, websocket.MessageText, data)
		<-ctx.Done()
	}))
	defer server.Close()
	client, _ := New(server.URL, "in-memory-test-only")
	cursor := &Cursor{BootID: "boot", Seq: 7}
	wanted := errors.New("consumer failed")
	err := client.Stream(ctx, StreamOptions{Cursor: cursor, OnEvent: func(Event) error { return wanted }})
	cancel()
	if !errors.Is(err, wanted) || cursor.Seq != 7 {
		t.Fatal("failed callback acknowledged event")
	}
}

func TestSubscriptionHeartbeatsAreDeliveredAndAdvanceCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for _, seq := range []uint64{8, 9} {
			data, _ := json.Marshal(Event{Type: "status", LiveID: "one", SubscriptionID: "sub", Seq: seq,
				Extra: map[string]json.RawMessage{"code": json.RawMessage(`"heartbeat"`), "state": json.RawMessage(`"waiting"`), "last_message_at": json.RawMessage(`null`)}})
			if c.Write(ctx, websocket.MessageText, data) != nil {
				return
			}
		}
		<-ctx.Done()
	}))
	defer server.Close()
	client, _ := New(server.URL, "in-memory-test-only")
	cursor := &Cursor{BootID: "boot", Seq: 7}
	count := 0
	_ = client.Stream(ctx, StreamOptions{Cursor: cursor, OnEvent: func(e Event) error {
		if e.ID != "" || e.StringField("code") != "heartbeat" || e.StringField("state") != "waiting" || string(e.Extra["last_message_at"]) != "null" {
			t.Error("heartbeat payload changed")
		}
		count++
		if count == 2 {
			cancel()
		}
		return nil
	}})
	if count != 2 || cursor.Seq != 9 {
		t.Fatal("empty-ID heartbeat deduped or treated as a control frame")
	}
}

func TestInvalidCursorResetsBeforeReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for _, event := range []Event{
			{Type: "status", Seq: 99, Extra: map[string]json.RawMessage{"code": json.RawMessage(`"gap"`), "reason": json.RawMessage(`"buffer_exceeded"`), "reset_cursor": json.RawMessage(`true`)}},
			{ID: "recovered", Type: "chat", Seq: 12, SubscriptionID: "sub"},
		} {
			data, _ := json.Marshal(event)
			if c.Write(ctx, websocket.MessageText, data) != nil {
				return
			}
		}
		<-ctx.Done()
	}))
	defer server.Close()
	client, _ := New(server.URL, "in-memory-test-only")
	cursor := &Cursor{BootID: "boot", Seq: 9999}
	gaps := 0
	_ = client.Stream(ctx, StreamOptions{Cursor: cursor, OnGap: func(Event) { gaps++ }, OnEvent: func(Event) error { cancel(); return nil }})
	if gaps != 1 || cursor.Seq != 12 {
		t.Fatal("invalid cursor prevented future replay progress")
	}
}
