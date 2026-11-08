package douyinLive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRequestHookRedirectRebuildAndCancel(t *testing.T) {
	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		w.WriteHeader(204)
	}))
	defer ts.Close()
	dl, err := NewDouyinLiveWithOptions("123", nil, Options{BeforeRequest: func(r *http.Request) error { count.Add(1); return r.Context().Err() }})
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	if _, err = dl.client.R().Get(ts.URL + "/redirect"); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatalf("redirects: got %d", count.Load())
	}
	dl.rebuildHTTPClientAndHeaders()
	if _, err = dl.client.R().Get(ts.URL + "/ok"); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 3 {
		t.Fatalf("hook lost on rebuild: %d", count.Load())
	}
	dl.beforeRequest = func(r *http.Request) error { <-r.Context().Done(); return r.Context().Err() }
	dl.rebuildHTTPClientAndHeaders()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = dl.client.R().SetContext(ctx).Get(ts.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestRequestHookBlocksWebSocket(t *testing.T) {
	blocked := errors.New("budget cancelled")
	dl, err := NewDouyinLiveWithOptions("123", nil, Options{BeforeRequest: func(*http.Request) error { return blocked }})
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	_, _, err = dl.dialUpstreamWebSocket(websocket.DefaultDialer, "ws://127.0.0.1:1/", nil, 1)
	if !errors.Is(err, blocked) {
		t.Fatalf("handshake not gated: %v", err)
	}
}
