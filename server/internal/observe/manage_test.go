package observe

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestAlertSummaryBulkAckAndPurge(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{8}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := room.New(ctx, s, engine.NewBudget(100), func() engine.Engine { return nil }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	a, e := NewAlerts(ctx, m)
	if e != nil {
		t.Fatal(e)
	}
	a.Close()
	now := time.Now()
	old := now.AddDate(0, 0, -10).UnixMilli()
	for _, row := range []struct {
		at          int64
		rule, level string
	}{{old, "silence", "warning"}, {now.UnixMilli(), "silence", "warning"}, {now.UnixMilli(), "node_cooldown", "warning"}, {now.UnixMilli(), "capacity", "critical"}} {
		if _, e = s.DB.Exec("INSERT INTO alerts(at,rule,level,live_id,detail) VALUES(?,?,?,?,?)", row.at, row.rule, row.level, "", "x"); e != nil {
			t.Fatal(e)
		}
	}
	sum, e := a.Summary(now)
	if e != nil || sum.Total != 4 || sum.Open["warning"] != 3 || sum.Open["critical"] != 1 || sum.Today < 3 || len(sum.TopRules) == 0 {
		t.Fatalf("unexpected summary %+v %v", sum, e)
	}
	n, e := a.AckMatching(AlertFilter{Rule: "silence"})
	if e != nil || n != 2 {
		t.Fatal("bulk ack by filter", n, e)
	}
	open, _ := a.Filtered(AlertFilter{Acked: "0"}, 0)
	if len(open) != 2 {
		t.Fatal("open filter", len(open))
	}
	if n, e = a.Purge(true, 0); e != nil || n != 2 {
		t.Fatal("purge acknowledged", n, e)
	}
	if n, e = a.Purge(false, now.Add(time.Hour).UnixMilli()); e != nil || n != 2 {
		t.Fatal("purge before", n, e)
	}
	if _, e = a.Purge(false, 0); e == nil {
		t.Fatal("purge without a time point accepted")
	}
}

func TestLogsStatsExportAndPurge(t *testing.T) {
	logs, e := NewLogs(t.TempDir(), 400, 2, slog.LevelInfo)
	if e != nil {
		t.Fatal(e)
	}
	defer logs.Close()
	logs.stdout = io.Discard
	logger := slog.New(logs.Handler())
	for range 20 {
		logger.Info("检测完成", "live_id", "123")
	}
	st := logs.Stats()
	if len(st.Files) < 2 || st.TotalBytes == 0 || st.ByLevel["INFO"] == 0 {
		t.Fatalf("unexpected stats %+v", st)
	}
	var out strings.Builder
	if e = logs.Export(&out); e != nil || !strings.Contains(out.String(), "检测完成") {
		t.Fatal("export", e)
	}
	freed, e := logs.Purge()
	if e != nil || freed == 0 {
		t.Fatal("purge", freed, e)
	}
	st = logs.Stats()
	if len(st.Files) != 1 || st.TotalBytes != 0 || st.Buffered != 0 {
		t.Fatalf("logs not purged %+v", st)
	}
	logger.Info("清理后继续写入")
	if logs.Stats().TotalBytes == 0 {
		t.Fatal("writing after purge failed")
	}
}
