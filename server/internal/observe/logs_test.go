package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLogsRotationFilteringAndRedaction(t *testing.T) {
	dir := t.TempDir()
	logs, e := NewLogs(dir, 400, 2, slog.LevelInfo)
	if e != nil {
		t.Fatal(e)
	}
	defer logs.Close()
	logs.stdout = io.Discard
	logger := slog.New(logs.Handler())
	for range 12 {
		logger.Info("检测完成", "live_id", "123", "cookie", "synthetic-secret", "extra", map[string]string{"password": "nested-secret"})
	}
	logger.WarnContext(context.Background(), "请求 password=embedded-secret http://user:url-secret@host/", "live_id", "456")
	if len(logs.Query("WARN", "456", "请求")) != 1 || len(logs.Query("ERROR", "", "")) != 0 {
		t.Fatal("log filters failed")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "collector.log*"))
	if len(files) != 3 {
		t.Fatal("rotation not bounded", len(files))
	}
	for _, name := range files {
		raw, _ := os.ReadFile(name)
		for _, secret := range []string{"synthetic-secret", "nested-secret", "embedded-secret", "url-secret"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("secret leaked")
			}
		}
	}
}

func TestCredentialHeadersAndPrefixesRedactedInEverySink(t *testing.T) {
	dir := t.TempDir()
	logs, err := NewLogs(dir, 1<<20, 1, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	var stdout bytes.Buffer
	logs.stdout = &stdout
	logger := slog.New(logs.Handler())
	messages := []string{
		"request key lt_live_" + strings.Repeat("A", 43),
		"Cookie: sessionid=fixture-secret-a; sid_tt=fixture-secret-b; sessionid_ss=fixture-secret-c; uid_tt=fixture-secret-d; arbitrary=fixture-secret-e",
		"Set-Cookie: arbitrary=fixture-secret-f; Path=/; HttpOnly\nnext line",
		`request Cookie="arbitrary=fixture-secret-g; sid_tt=fixture-secret-h" status=failed`,
		`{"Cookie":"arbitrary=fixture-secret-i; sessionid_ss=fixture-secret-j","status":"failed"}`,
		"set-cookie='arbitrary=fixture-secret-k; other=fixture-secret-l' status=failed",
		"Authorization: Bearer fixture-secret-m\r\nnext line",
		"https://user:fixture-secret-n@localhost/",
	}
	for _, key := range []string{"sid_tt", "sessionid_ss", "uid_tt", "odin_tt", "passport_csrf_token", "passport_auth_status", "msToken", "s_v_web_id", "SESSIONID_SUFFIX", "ttwid_extra", "password", "token", "secret"} {
		messages = append(messages, key+"=fixture-secret-value", `"`+key+`":"fixture-secret-value with spaces"`)
	}
	for _, message := range messages {
		if got := redactText(message); strings.Contains(got, "fixture-secret") || strings.Contains(got, "lt_live_") || !strings.Contains(got, "[REDACTED]") && !strings.Contains(got, "://***@") {
			t.Fatal("credential was not fully redacted")
		}
		logger.Warn(message, "state", message)
	}
	file, err := os.ReadFile(filepath.Join(dir, "collector.log"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := json.Marshal(logs.Query("", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{file, stdout.Bytes(), entries} {
		if bytes.Contains(output, []byte("fixture-secret")) {
			t.Fatal("credential leaked to log sink")
		}
	}
	if got := redactText("Cookie: arbitrary=fixture-secret\r\nnext line"); !strings.HasSuffix(got, "\r\nnext line") {
		t.Fatal("lost following line")
	}
	if got := redactText(`Cookie="arbitrary=fixture-secret" status=failed`); !strings.HasSuffix(got, " status=failed") {
		t.Fatal("lost text after quote")
	}
}

func TestLogTruncationPreservesUTF8(t *testing.T) {
	for _, input := range []string{strings.Repeat("中文", 200), strings.Repeat("a", 999) + "中", strings.Repeat("a", 998) + "😀", strings.Repeat("a", 1001)} {
		got := redactText(input)
		if len(got) > 1000 || len(got) < 997 || !utf8.ValidString(got) || !strings.HasPrefix(input, got) {
			t.Fatal("truncation split a character or exceeded byte limit")
		}
	}
	if got := redactText(strings.Repeat("中", 333) + "a"); len(got) != 1000 {
		t.Fatal("exact limit was truncated")
	}
}

// Regression: fields added to the probe-failure and startup logs were dropped
// because they were not on the allow list. Unknown fields stay redacted.
func TestDiagnosticFieldsKept(t *testing.T) {
	logs, e := NewLogs(t.TempDir(), 1<<20, 1, slog.LevelInfo)
	if e != nil {
		t.Fatal(e)
	}
	defer logs.Close()
	logs.stdout = io.Discard
	logger := slog.New(logs.Handler())
	logger.Warn("直播状态探测未成功", "live_id", "123", "cause", "upstream_uncertain", "code", "status_code=4003099", "extra", "hidden")
	logger.Info("LiveTap 启动", "version", "1.0.1", "homepage", "https://github.com/Vz7797/LiveTap")
	probe := logs.Query("WARN", "123", "探测")
	start := logs.Query("INFO", "", "启动")
	if len(probe) != 1 || len(start) != 1 {
		t.Fatalf("entries not found: %d %d", len(probe), len(start))
	}
	if probe[0].Fields["code"] != "status_code=4003099" || probe[0].Fields["extra"] != "[REDACTED]" {
		t.Fatalf("probe fields: %v", probe[0].Fields)
	}
	if start[0].Fields["version"] != "1.0.1" || start[0].Fields["homepage"] != "https://github.com/Vz7797/LiveTap" {
		t.Fatalf("startup fields: %v", start[0].Fields)
	}
}
