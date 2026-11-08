package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

type LogEntry struct {
	At      int64          `json:"at"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	LiveID  string         `json:"live_id,omitempty"`
	Fields  map[string]any `json:"fields"`
}
type Logs struct {
	mu        sync.Mutex
	dir       string
	file      *os.File
	size, max int64
	backups   int
	level     slog.Level
	entries   []LogEntry
	stdout    io.Writer
}

func NewLogs(dir string, maxBytes int64, backups int, level slog.Level) (*Logs, error) {
	if maxBytes < 1 || backups < 1 {
		return nil, fmt.Errorf("日志轮转参数无效")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	l := &Logs{dir: dir, max: maxBytes, backups: backups, level: level, stdout: os.Stdout}
	e := l.open()
	return l, e
}
func (l *Logs) open() error {
	f, e := os.OpenFile(filepath.Join(l.dir, "collector.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	l.file = f
	st, e := f.Stat()
	if e == nil {
		l.size = st.Size()
	}
	return e
}
func (l *Logs) Close() error          { l.mu.Lock(); defer l.mu.Unlock(); return l.file.Close() }
func (l *Logs) Handler() slog.Handler { return &logHandler{logs: l} }

type logHandler struct {
	logs  *Logs
	attrs []slog.Attr
	group string
}

func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.logs.level }

// Cookie headers contain several credentials separated by semicolons. Mask the
// whole header value before matching individual credential names (including suffixes).
var credentialHeader = regexp.MustCompile(`(?i)\b(set-cookie|cookie|authorization)["']?[ \t]*[:=][ \t]*(?:"[^"\r\n]*"?|'[^'\r\n]*'?|[^\r\n"']*)`)
var credentialText = regexp.MustCompile(`(?i)\b(password|token|secret|sessionid|ttwid|sid_guard|sid_tt|uid_tt|odin_tt|passport|mstoken|s_v_web_id)[a-z0-9_-]*["']?[ \t]*[:=][ \t]*(?:"[^"\r\n]*"?|'[^'\r\n]*'?|[^\s,;"']+)`)
var authenticatedURL = regexp.MustCompile(`(?i)(https?|socks5)://[^\s/@]+@`)
var apiKeyText = regexp.MustCompile(`lt_live_[0-9A-Za-z]{43}`)

func redactText(s string) string {
	s = apiKeyText.ReplaceAllString(s, "[REDACTED]")
	s = credentialHeader.ReplaceAllString(s, "$1=[REDACTED]")
	s = authenticatedURL.ReplaceAllString(s, "$1://***@")
	s = credentialText.ReplaceAllString(s, "$1=[REDACTED]")
	if len(s) > 1000 {
		end := 1000
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return s
}
func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	entry := LogEntry{At: r.Time.UnixMilli(), Level: r.Level.String(), Message: redactText(r.Message), Fields: map[string]any{}}
	attrs := append([]slog.Attr{}, h.attrs...)
	r.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
	for _, a := range attrs {
		switch a.Key {
		case "live_id", "state", "kind", "action", "count", "duration_ms", "cooldown_seconds", "strikes", "resource_id", "address", "cause", "trigger":
			// Group/LogValuer values are not expanded; unknown fields never enter disk.
			value := a.Value.Resolve()
			if value.Kind() == slog.KindGroup || value.Kind() == slog.KindAny {
				entry.Fields[a.Key] = "[REDACTED]"
			} else {
				entry.Fields[a.Key] = redactText(value.String())
			}
			if a.Key == "live_id" {
				entry.LiveID = redactText(value.String())
			}
		default:
			entry.Fields[a.Key] = "[REDACTED]"
		}
	}
	data, e := json.Marshal(entry)
	if e != nil {
		return e
	}
	data = append(data, '\n')
	l := h.logs
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(data)) > l.max && l.size > 0 {
		if e = l.file.Close(); e != nil {
			return e
		}
		for i := l.backups; i >= 1; i-- {
			old := filepath.Join(l.dir, fmt.Sprintf("collector.log.%d", i))
			if i == l.backups {
				if e = os.Remove(old); e != nil && !os.IsNotExist(e) {
					return e
				}
			} else {
				if e = os.Rename(old, filepath.Join(l.dir, fmt.Sprintf("collector.log.%d", i+1))); e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
		if e = os.Rename(filepath.Join(l.dir, "collector.log"), filepath.Join(l.dir, "collector.log.1")); e != nil {
			return e
		}
		if e = l.open(); e != nil {
			return e
		}
	}
	n, e := l.file.Write(data)
	l.size += int64(n)
	if e != nil {
		return e
	}
	if l.stdout != nil {
		_, _ = l.stdout.Write(data)
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > 1000 {
		l.entries = append([]LogEntry{}, l.entries[len(l.entries)-1000:]...)
	}
	return nil
}
func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logHandler{logs: h.logs, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), group: h.group}
}
func (h *logHandler) WithGroup(name string) slog.Handler {
	return &logHandler{logs: h.logs, attrs: h.attrs, group: name}
}
func (l *Logs) Query(level, liveID, q string) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []LogEntry{}
	for i := len(l.entries) - 1; i >= 0 && len(out) < 200; i-- {
		v := l.entries[i]
		if level != "" && v.Level != level {
			continue
		}
		if liveID != "" && v.LiveID != liveID {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Message), strings.ToLower(q)) {
			continue
		}
		out = append(out, v)
	}
	return out
}
