package model

import "time"

type Client struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Env       string   `json:"env"`
	Enabled   bool     `json:"enabled"`
	Quota     int      `json:"quota"`
	Whitelist []string `json:"whitelist"`
	CreatedAt int64    `json:"createdAt"`
	LastSeen  int64    `json:"lastSeen"`
	KeyHash   string   `json:"-"`
}
type SubscriptionOptions struct {
	LeaseSeconds       int `json:"lease_seconds"`
	IdleReleaseSeconds int `json:"idle_release_seconds"`
}
type Subscription struct {
	ID          string              `json:"id"`
	ClientID    string              `json:"-"`
	LiveID      string              `json:"live_id"`
	ExternalRef string              `json:"external_ref"`
	EventTypes  []string            `json:"event_types"`
	Options     SubscriptionOptions `json:"options"`
	ExpiresAt   time.Time           `json:"expires_at"`
	CreatedAt   time.Time           `json:"created_at"`
	StartSeq    uint64              `json:"-"`
}
