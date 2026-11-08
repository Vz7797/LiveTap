package model

type Resource struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Tags      []string `json:"tags"`
	Enabled   bool     `json:"enabled"`
	Limit     int      `json:"limit"`
	State     string   `json:"state"`
	CheckedAt int64    `json:"checked_at"`
	LatencyMS int64    `json:"latency_ms"`
	Detail    string   `json:"detail"`
	Mask      string   `json:"mask"`
	Version   int64    `json:"version"`
	Secret    string   `json:"-"`
	Rooms     []string `json:"rooms"`
}
