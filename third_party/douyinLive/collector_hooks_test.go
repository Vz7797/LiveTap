package douyinLive

import (
	"context"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollectorResponseHookRedirectRebuildAndWebSocket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", 302)
			return
		}
		if r.URL.Path == "/blocked" {
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(204)
	}))
	defer ts.Close()
	count := 0
	blocked := errors.New("synthetic restriction")
	dl, err := NewDouyinLiveWithOptions("123", nil, Options{AfterResponse: func(r *http.Response) error {
		count++
		if r.StatusCode == 403 {
			return blocked
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	if _, err = dl.client.R().Get(ts.URL + "/redirect"); err != nil || count != 2 {
		t.Fatal("redirect response skipped")
	}
	dl.rebuildHTTPClientAndHeaders()
	if _, err = dl.client.R().Get(ts.URL + "/blocked"); !errors.Is(err, blocked) || count != 3 {
		t.Fatal("response gate lost on rebuild")
	}
	_, resp, err := dl.dialUpstreamWebSocket(websocket.DefaultDialer, "ws"+strings.TrimPrefix(ts.URL, "http")+"/blocked", nil, 1)
	closeWebSocketHandshakeResponse(resp)
	if !errors.Is(err, blocked) || count != 4 {
		t.Fatal("websocket rejection did not reach response hook")
	}
}

func TestCollectorGuestProviderAndExplicitCookiePrecedence(t *testing.T) {
	calls := 0
	provider := func(ctx context.Context, fetch func(context.Context) (string, time.Time, error)) (string, error) {
		calls++
		return "synthetic-guest", nil
	}
	dl, err := NewDouyinLiveWithOptions("123", nil, Options{GuestTTWID: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	if err = dl.prepareRequestContextLocked(context.Background()); err != nil || calls != 1 || dl.cookieValue("ttwid") != "synthetic-guest" {
		t.Fatal("guest provider not used")
	}
	logged, err := NewDouyinLiveWithOptions("456", nil, Options{Cookie: "ttwid=synthetic-explicit", GuestTTWID: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer logged.Close()
	if err = logged.prepareRequestContextLocked(context.Background()); err != nil || calls != 1 || logged.cookieValue("ttwid") != "synthetic-explicit" {
		t.Fatal("explicit cookie was replaced")
	}
}
