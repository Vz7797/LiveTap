// Package collector is the Go client for the northbound API of a LiveTap
// collection node.
//
// LiveTap: https://github.com/Vz7797/LiveTap
package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

type Client struct {
	base string
	key  string
	http *http.Client
}

// APIVersion is the northbound contract supported by this SDK, not the build version.
const APIVersion = "v1"

func New(endpoint, key string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid collector endpoint")
	}
	if key == "" {
		return nil, errors.New("API key is required")
	}
	return &Client{strings.TrimRight(endpoint, "/"), key, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type APIError struct {
	Status     int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
	RequestID  string `json:"-"`
	// Released explains each ID when a renewal could not extend any of them.
	Released []Released `json:"-"`
}

// Released is a subscription that has ended on the node, reported by Renew for
// IDs it could not extend and by Stream for subscription_released frames. Reason
// is live_ended, lease_expired, idle_release, unsubscribed, room_removed,
// client_disabled, client_deleted, or not_found when the node no longer knows it.
type Released struct {
	SubscriptionID string `json:"subscription_id"`
	LiveID         string `json:"live_id"`
	Reason         string `json:"reason"`
}

func (e *APIError) Error() string { return "collector API: " + e.Code }

type Node struct {
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	APIVersion string  `json:"api_version"`
	Engine     string  `json:"engine"`
	BootID     string  `json:"boot_id"`
	Seq        uint64  `json:"seq"`
	Health     int     `json:"health"`
	LoadScore  float64 `json:"load_score"`
	Capacity   struct {
		Max  int `json:"max"`
		Used int `json:"used"`
		Live int `json:"live"`
	} `json:"capacity"`
	Cooldown struct {
		Active           bool `json:"active"`
		RemainingSeconds int  `json:"remainingSeconds"`
	} `json:"cooldown"`
}
type Options struct {
	LeaseSeconds       int `json:"lease_seconds,omitempty"`
	IdleReleaseSeconds int `json:"idle_release_seconds,omitempty"`
}
type SubscribeRequest struct {
	LiveID      string   `json:"live_id"`
	ExternalRef string   `json:"external_ref"`
	EventTypes  []string `json:"event_types,omitempty"`
	Options     Options  `json:"options"`
}
type RoomStatus struct {
	LiveID string `json:"live_id"`
	RoomID string `json:"room_id"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}
type Subscription struct {
	ID          string     `json:"id"`
	LiveID      string     `json:"live_id"`
	ExternalRef string     `json:"external_ref"`
	EventTypes  []string   `json:"event_types"`
	Options     Options    `json:"options"`
	ExpiresAt   time.Time  `json:"expires_at"`
	Status      RoomStatus `json:"status"`
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return errors.New("invalid request")
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid request")
	}
	r.Header.Set("Authorization", "Bearer "+c.key)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("collector request failed")
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 4<<20)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Error     APIError   `json:"error"`
			RequestID string     `json:"request_id"`
			Missing   []Released `json:"missing"`
		}
		if json.NewDecoder(limited).Decode(&envelope) != nil {
			return &APIError{Status: resp.StatusCode, Code: "invalid_response"}
		}
		envelope.Error.Status = resp.StatusCode
		envelope.Error.RequestID = envelope.RequestID
		envelope.Error.Released = envelope.Missing
		return &envelope.Error
	}
	if output == nil {
		return nil
	}
	if json.NewDecoder(limited).Decode(output) != nil {
		return errors.New("invalid collector response")
	}
	return nil
}
func (c *Client) Node(ctx context.Context) (Node, error) {
	var v Node
	err := c.request(ctx, "GET", "/v1/node", nil, &v)
	return v, err
}
func (c *Client) Subscribe(ctx context.Context, r SubscribeRequest) (Subscription, error) {
	var v Subscription
	err := c.request(ctx, "POST", "/v1/subscriptions", r, &v)
	return v, err
}
func (c *Client) Subscriptions(ctx context.Context) ([]Subscription, error) {
	var v struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	err := c.request(ctx, "GET", "/v1/subscriptions", nil, &v)
	return v.Subscriptions, err
}
func (c *Client) Subscription(ctx context.Context, id string) (Subscription, error) {
	var v Subscription
	err := c.request(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(id), nil, &v)
	return v, err
}
func (c *Client) Unsubscribe(ctx context.Context, id string) error {
	return c.request(ctx, "DELETE", "/v1/subscriptions/"+url.PathEscape(id), nil, nil)
}

// Renew extends the leases it can and returns the ended ones in released rather
// than failing the batch. An *APIError with status 404 means none was renewed;
// released then still explains each ID.
func (c *Client) Renew(ctx context.Context, ids []string) ([]Subscription, []Released, error) {
	var v struct {
		Subscriptions []Subscription `json:"subscriptions"`
		Missing       []Released     `json:"missing"`
	}
	err := c.request(ctx, "POST", "/v1/subscriptions:renew", map[string]any{"subscription_ids": ids}, &v)
	var api *APIError
	if errors.As(err, &api) {
		return nil, api.Released, err
	}
	return v.Subscriptions, v.Missing, err
}
func (c *Client) StatusBatch(ctx context.Context, ids []string) ([]RoomStatus, error) {
	var v struct {
		Rooms []RoomStatus `json:"rooms"`
	}
	err := c.request(ctx, "POST", "/v1/rooms/status:batch", map[string]any{"live_ids": ids}, &v)
	return v.Rooms, err
}

// AutoRenew keeps leases alive while no stream is connected; run it in a
// goroutine. It renews every third of the shortest lease, drops subscriptions the
// node reports as ended and passes each to onReleased (may be nil), and retries
// network, rate-limit and server failures every few seconds while the lease still
// runs. It returns when ctx ends, when nothing is left to renew, or on another
// API error such as a disabled key.
func (c *Client) AutoRenew(ctx context.Context, subs []Subscription, onReleased func(Released)) error {
	if len(subs) == 0 {
		return errors.New("no subscriptions to renew")
	}
	interval := time.Hour
	ids := []string{}
	for _, sub := range subs {
		if sub.Options.LeaseSeconds < 1 {
			return errors.New("invalid lease")
		}
		interval = min(interval, time.Duration(sub.Options.LeaseSeconds)*time.Second/3)
		ids = append(ids, sub.ID)
	}
	wait := interval
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		_, released, err := c.Renew(ctx, ids)
		var api *APIError
		if errors.As(err, &api) && api.Status == 404 && len(released) == 0 {
			// A node without per-ID reasons: all of them are gone.
			for _, id := range ids {
				released = append(released, Released{SubscriptionID: id, Reason: "not_found"})
			}
		}
		for _, r := range released {
			ids = slices.DeleteFunc(ids, func(id string) bool { return id == r.SubscriptionID })
			if onReleased != nil {
				onReleased(r)
			}
		}
		switch {
		case len(ids) == 0:
			return nil
		case err == nil:
			wait = interval
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &api) && api.Status != 429 && api.Status < 500:
			return err
		default:
			wait = min(interval, 3*time.Second)
		}
	}
}
