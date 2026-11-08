package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func TestCleanRollsOldMinutesIntoHours(t *testing.T) {
	s, e := Open(t.TempDir(), bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var mode int
	if e = s.DB.QueryRow("PRAGMA auto_vacuum").Scan(&mode); e != nil || mode != 2 {
		t.Fatal("database is not in incremental auto_vacuum mode", mode, e)
	}
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Hour)
	for i := range 3 {
		if e = s.Minute("1", old.Add(time.Duration(i)*time.Minute), model.Counts{"chat": 2, "gift": int64(i)}, 1, 10, "collecting"); e != nil {
			t.Fatal(e)
		}
	}
	recent := time.Now().Add(-time.Hour)
	if e = s.Minute("1", recent, model.Counts{"chat": 5}, 0, 0, "collecting"); e != nil {
		t.Fatal(e)
	}
	settings := model.DefaultSettings()
	for range 2 { // a second run must not double-count
		if e = s.Clean(context.Background(), settings); e != nil {
			t.Fatal(e)
		}
	}
	var counts string
	var reconnects, silence int
	if e = s.DB.QueryRow("SELECT counts,reconnects,silence FROM room_stats_hour WHERE live_id='1' AND hour=?", old.UnixMilli()).Scan(&counts, &reconnects, &silence); e != nil {
		t.Fatal("hour row missing", e)
	}
	if counts != `{"chat":6,"gift":3}` || reconnects != 3 || silence != 30 {
		t.Fatal("wrong rollup", counts, reconnects, silence)
	}
	var left int
	_ = s.DB.QueryRow("SELECT count(*) FROM room_stats_minute").Scan(&left)
	if left != 1 {
		t.Fatal("expected only the recent minute row to remain", left)
	}
	settings.StatsDays = 1
	if e = s.Clean(context.Background(), settings); e != nil {
		t.Fatal(e)
	}
	_ = s.DB.QueryRow("SELECT count(*) FROM room_stats_hour").Scan(&left)
	if left != 0 {
		t.Fatal("hour rows beyond StatsDays kept", left)
	}
}

func TestCleanKeepsNewestLabRuns(t *testing.T) {
	s, e := Open(t.TempDir(), bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.DB.Exec(`CREATE TABLE lab_runs(id TEXT PRIMARY KEY,at INTEGER NOT NULL,status TEXT NOT NULL,report TEXT NOT NULL);CREATE TABLE lab_samples(id INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL,data TEXT NOT NULL);`); e != nil {
		t.Fatal(e)
	}
	total := LabRunsKept + 5
	for i := range total {
		status := "completed"
		if i == 0 {
			status = "running" // the oldest, but still running: never pruned
		}
		id := string(rune('a' + i))
		if _, e = s.DB.Exec("INSERT INTO lab_runs VALUES(?,?,?,'{}')", id, i, status); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.Exec("INSERT INTO lab_samples(run_id,data) VALUES(?,'{}')", id); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Clean(context.Background(), model.DefaultSettings()); e != nil {
		t.Fatal(e)
	}
	var runs, samples, running int
	_ = s.DB.QueryRow("SELECT count(*) FROM lab_runs").Scan(&runs)
	_ = s.DB.QueryRow("SELECT count(*) FROM lab_samples").Scan(&samples)
	_ = s.DB.QueryRow("SELECT count(*) FROM lab_runs WHERE status='running'").Scan(&running)
	if runs != LabRunsKept+1 || samples != runs || running != 1 {
		t.Fatal("unexpected lab retention", runs, samples, running)
	}
	if s.SizeBytes() <= 0 {
		t.Fatal("size not reported")
	}
}

func TestOpenConvertsExistingDatabaseKeepingWALData(t *testing.T) {
	dir := t.TempDir()
	old, e := openDB(filepath.Join(dir, "collector.db"))
	if e != nil {
		t.Fatal(e)
	}
	// An existing database from an older version: full auto_vacuum off, WAL mode, and
	// a row that may still sit in the WAL when the new version starts.
	if _, e = old.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE legacy(v TEXT); INSERT INTO legacy VALUES('kept');"); e != nil {
		t.Fatal(e)
	}
	old.Close()
	s, e := Open(dir, bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var mode int
	var v string
	if e = s.DB.QueryRow("PRAGMA auto_vacuum").Scan(&mode); e != nil || mode != 2 {
		t.Fatal("existing database not converted", mode, e)
	}
	if e = s.DB.QueryRow("SELECT v FROM legacy").Scan(&v); e != nil || v != "kept" {
		t.Fatal("data lost during conversion", v, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "collector.db.incremental")); !os.IsNotExist(e) {
		t.Fatal("conversion file left behind", e)
	}
}

func TestOpenAddsRoomColumnsToOldDatabase(t *testing.T) {
	dir := t.TempDir()
	old, e := openDB(filepath.Join(dir, "collector.db"))
	if e != nil {
		t.Fatal(e)
	}
	// rooms as created by 0.3.x, with one row
	if _, e = old.Exec(`CREATE TABLE rooms(live_id TEXT PRIMARY KEY,name TEXT NOT NULL,title TEXT NOT NULL,room_id TEXT NOT NULL,source TEXT NOT NULL,tags TEXT NOT NULL,note TEXT NOT NULL,enabled INTEGER NOT NULL,created_at INTEGER NOT NULL,options TEXT NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	old.Close()
	key := bytes.Repeat([]byte{7}, 32)
	s, e := Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	r := model.RoomRecord{LiveID: "123456789", Name: "n", Source: "manual", Tags: []string{}, Enabled: true, CreatedAt: time.Now(), Avatar: "https://example.invalid/a.jpeg", EndedAt: 5, EndedReason: "live_ended", LastLiveAt: 4}
	if e = s.SaveRoom(r); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rooms, e := s.Rooms()
	if e != nil || len(rooms) != 1 {
		t.Fatal(e, len(rooms))
	}
	got := rooms[0]
	if got.Avatar != r.Avatar || got.EndedAt != 5 || got.EndedReason != "live_ended" || got.LastLiveAt != 4 || !got.Dormant() {
		t.Fatalf("room fields not persisted: %+v", got)
	}
}
