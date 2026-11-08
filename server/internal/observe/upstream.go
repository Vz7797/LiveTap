package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
)

// UpstreamCheck asks GitHub a few times a day whether the collector kernel's author
// has published a release newer than the vendored one. It only feeds a hint in the
// admin page: nothing is downloaded or changed. LIVETAP_UPDATE_CHECK=false turns it off.
type UpstreamCheck struct {
	enabled bool
	url     string
	client  *http.Client
	every   time.Duration
	mu      sync.RWMutex
	status  UpstreamStatus
}

// UpstreamStatus is what the admin page shows next to the version badge.
type UpstreamStatus struct {
	Enabled     bool   `json:"enabled"`
	Repo        string `json:"repo"`
	Version     string `json:"version"`     // vendored kernel version
	Latest      string `json:"latest"`      // author's latest release tag; empty until checked
	LatestURL   string `json:"latestUrl"`   // release page
	PublishedAt int64  `json:"publishedAt"` // Unix ms
	CheckedAt   int64  `json:"checkedAt"`   // Unix ms of the last attempt
	Newer       bool   `json:"newer"`       // Latest is a higher version than Version
	Error       string `json:"error,omitempty"`
}

func NewUpstreamCheck(enabled bool) *UpstreamCheck {
	return &UpstreamCheck{
		enabled: enabled,
		url:     "https://api.github.com/repos/" + buildinfo.EngineRepo + "/releases/latest",
		client:  &http.Client{Timeout: 15 * time.Second},
		every:   6 * time.Hour,
		status:  UpstreamStatus{Enabled: enabled, Repo: buildinfo.EngineRepo, Version: buildinfo.EngineVersion},
	}
}

// Status is safe on a nil check (tests and tools build a snapshotter without one).
func (u *UpstreamCheck) Status() UpstreamStatus {
	if u == nil {
		return UpstreamStatus{Repo: buildinfo.EngineRepo, Version: buildinfo.EngineVersion}
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.status
}

// Run checks shortly after start, then every six hours, until ctx ends.
func (u *UpstreamCheck) Run(ctx context.Context) {
	if u == nil || !u.enabled {
		return
	}
	wait := 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		u.check(ctx)
		wait = u.every
	}
}

func (u *UpstreamCheck) check(ctx context.Context) {
	latest, err := u.fetch(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.CheckedAt = time.Now().UnixMilli()
	if err != nil {
		// Keep the last good answer; only report that this attempt failed.
		u.status.Error = err.Error()
		return
	}
	u.status.Error = ""
	u.status.Latest = latest.Tag
	u.status.LatestURL = latest.URL
	u.status.PublishedAt = latest.Published.UnixMilli()
	u.status.Newer = newerVersion(latest.Tag, u.status.Version)
}

type release struct {
	Tag       string    `json:"tag_name"`
	URL       string    `json:"html_url"`
	Published time.Time `json:"published_at"`
}

func (u *UpstreamCheck) fetch(ctx context.Context) (release, error) {
	var r release
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.url, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LiveTap/"+buildinfo.Version+" (+"+buildinfo.Homepage+")")
	resp, err := u.client.Do(req)
	if err != nil {
		return r, fmt.Errorf("无法连接 GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil || r.Tag == "" {
		return r, fmt.Errorf("GitHub 返回内容无法识别")
	}
	return r, nil
}

// newerVersion reports whether tag a is a higher vMAJOR.MINOR.PATCH than b.
// Anything that does not parse is never reported as newer.
func newerVersion(a, b string) bool {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
