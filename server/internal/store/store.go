package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/secret"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB    *sql.DB
	vault *secret.Vault
}

func Open(dir string, key []byte) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	v, e := secret.New(key)
	if e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "collector.db")
	db, e := openDB(path)
	if e != nil {
		return nil, e
	}
	// Conversion is an optimisation: a failure (e.g. too little free disk) must not stop the node.
	reopen, e := enableIncrementalVacuum(db, path)
	if e != nil {
		slog.Warn("数据库未切换为增量回收，继续按原方式运行", "error", e)
	}
	if reopen {
		if db, e = openDB(path); e != nil {
			return nil, e
		}
	}
	s := &Store{db, v}
	// Temporary b-trees stay in memory: containers mount a small tmpfs at /tmp.
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA temp_store=MEMORY;
 CREATE TABLE IF NOT EXISTS schema_version(version INTEGER PRIMARY KEY); INSERT OR IGNORE INTO schema_version VALUES(1);
 CREATE TABLE IF NOT EXISTS settings(id INTEGER PRIMARY KEY CHECK(id=1), data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS rooms(live_id TEXT PRIMARY KEY,name TEXT NOT NULL,title TEXT NOT NULL,room_id TEXT NOT NULL,source TEXT NOT NULL,tags TEXT NOT NULL,note TEXT NOT NULL,enabled INTEGER NOT NULL,created_at INTEGER NOT NULL,options TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS aliases(alias TEXT PRIMARY KEY,live_id TEXT NOT NULL REFERENCES rooms(live_id) ON DELETE CASCADE ON UPDATE CASCADE);
 CREATE TABLE IF NOT EXISTS admin(id INTEGER PRIMARY KEY CHECK(id=1),password_hash TEXT NOT NULL,must_change INTEGER NOT NULL,failures INTEGER NOT NULL DEFAULT 0,locked_until INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY,expires_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS auth_failures(ip TEXT PRIMARY KEY,failures INTEGER NOT NULL,expires_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS audit_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,at INTEGER NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,target TEXT NOT NULL,detail TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS room_stats_minute(live_id TEXT NOT NULL,minute INTEGER NOT NULL,counts TEXT NOT NULL,reconnects INTEGER NOT NULL,silence INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(live_id,minute));
 CREATE TABLE IF NOT EXISTS incidents(id INTEGER PRIMARY KEY AUTOINCREMENT,at INTEGER NOT NULL,live_id TEXT NOT NULL,kind TEXT NOT NULL,detail TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS stats_minute_time ON room_stats_minute(minute);
 CREATE TABLE IF NOT EXISTS room_stats_hour(live_id TEXT NOT NULL,hour INTEGER NOT NULL,counts TEXT NOT NULL,reconnects INTEGER NOT NULL,silence INTEGER NOT NULL,PRIMARY KEY(live_id,hour));
 CREATE INDEX IF NOT EXISTS stats_hour_time ON room_stats_hour(hour);
 `)
	if e == nil {
		e = addColumns(db, "rooms", map[string]string{"avatar": "TEXT NOT NULL DEFAULT ''", "ended_at": "INTEGER NOT NULL DEFAULT 0", "ended_reason": "TEXT NOT NULL DEFAULT ''", "last_live_at": "INTEGER NOT NULL DEFAULT 0"})
	}
	if e == nil {
		e = addColumns(db, "sessions", map[string]string{"created_at": "INTEGER NOT NULL DEFAULT 0"})
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	// A key check fails early even if no room has yet stored a credential.
	var stored string
	e = db.QueryRow("SELECT data FROM settings WHERE id=1").Scan(&stored)
	if e == sql.ErrNoRows {
		e = s.SaveSettings(model.DefaultSettings())
	} else if e == nil {
		_, e = v.Open(stored, "settings")
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Settings() (model.Settings, error) {
	var v string
	var out model.Settings
	e := s.DB.QueryRow("SELECT data FROM settings WHERE id=1").Scan(&v)
	if e != nil {
		return out, e
	}
	b, e := s.vault.Open(v, "settings")
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	return out, e
}
func (s *Store) SaveSettings(v model.Settings) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO settings VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", s.vault.Seal(b, "settings"))
	return e
}

const roomColumns = "live_id,name,title,room_id,source,tags,note,enabled,created_at,options,avatar,ended_at,ended_reason,last_live_at"

// addColumns adds columns introduced after a table was first created.
func addColumns(db *sql.DB, table string, defs map[string]string) error {
	rows, e := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if e != nil {
		return e
	}
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return e
		}
		have[name] = true
	}
	rows.Close()
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		if !have[name] {
			if _, e = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + defs[name]); e != nil {
				return e
			}
		}
	}
	return nil
}

func (s *Store) SaveRoom(r model.RoomRecord) error {
	tags, _ := json.Marshal(r.Tags)
	opts, _ := json.Marshal(r.Overrides)
	_, e := s.DB.Exec(`INSERT INTO rooms(`+roomColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(live_id) DO UPDATE SET name=excluded.name,title=excluded.title,room_id=excluded.room_id,tags=excluded.tags,note=excluded.note,enabled=excluded.enabled,options=excluded.options,avatar=excluded.avatar,ended_at=excluded.ended_at,ended_reason=excluded.ended_reason,last_live_at=excluded.last_live_at`, r.LiveID, r.Name, r.Title, r.RoomID, r.Source, string(tags), r.Note, r.Enabled, r.CreatedAt.UnixMilli(), s.vault.Seal(opts, "room:"+r.LiveID), r.Avatar, r.EndedAt, r.EndedReason, r.LastLiveAt)
	return e
}
func (s *Store) Rooms() ([]model.RoomRecord, error) {
	rows, e := s.DB.Query("SELECT " + roomColumns + " FROM rooms ORDER BY created_at,live_id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.RoomRecord{}
	for rows.Next() {
		var r model.RoomRecord
		var tags, opts string
		var at int64
		if e = rows.Scan(&r.LiveID, &r.Name, &r.Title, &r.RoomID, &r.Source, &tags, &r.Note, &r.Enabled, &at, &opts, &r.Avatar, &r.EndedAt, &r.EndedReason, &r.LastLiveAt); e != nil {
			return nil, e
		}
		r.CreatedAt = time.UnixMilli(at)
		if e = json.Unmarshal([]byte(tags), &r.Tags); e != nil {
			return nil, e
		}
		b, e := s.vault.Open(opts, "room:"+r.LiveID)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &r.Overrides); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) DeleteRoom(id string) error {
	_, e := s.DB.Exec("DELETE FROM rooms WHERE live_id=?", id)
	return e
}
func (s *Store) Audit(action, target, detail string) error {
	_, e := s.DB.Exec("INSERT INTO audit_logs(at,actor,action,target,detail) VALUES(?,?,?,?,?)", time.Now().UnixMilli(), "admin", action, target, detail)
	return e
}
func (s *Store) Incident(id, kind, detail string) error {
	_, e := s.DB.Exec("INSERT INTO incidents(at,live_id,kind,detail) VALUES(?,?,?,?)", time.Now().UnixMilli(), id, kind, detail)
	return e
}
func (s *Store) Minute(id string, at time.Time, c model.Counts, reconnects, silence int, state string) error {
	b, _ := json.Marshal(c)
	_, e := s.DB.Exec(`INSERT INTO room_stats_minute VALUES(?,?,?,?,?,?) ON CONFLICT(live_id,minute) DO UPDATE SET counts=excluded.counts,reconnects=excluded.reconnects,silence=excluded.silence,state=excluded.state`, id, at.Truncate(time.Minute).UnixMilli(), string(b), reconnects, silence, state)
	return e
}

type Minute struct {
	LiveID string       `json:"live_id"`
	At     int64        `json:"at"`
	Counts model.Counts `json:"counts"`
}

func (s *Store) Minutes(since time.Time) ([]Minute, error) {
	rows, e := s.DB.Query("SELECT live_id,minute,counts FROM room_stats_minute WHERE minute>=? ORDER BY minute", since.UnixMilli())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Minute{}
	for rows.Next() {
		var m Minute
		var b string
		if e = rows.Scan(&m.LiveID, &m.At, &b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &m.Counts); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Aggregate in SQLite so dashboard refreshes do not copy every room's history.
func (s *Store) MinuteTotals(since time.Time) ([]Minute, error) {
	rows, err := s.DB.Query(`SELECT minute, j.key, SUM(CAST(j.value AS INTEGER)) FROM room_stats_minute, json_each(counts) j WHERE minute>=? GROUP BY minute,j.key ORDER BY minute`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Minute{}
	for rows.Next() {
		var at, n int64
		var typ string
		if err = rows.Scan(&at, &typ, &n); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].At != at {
			out = append(out, Minute{At: at, Counts: model.Zero()})
		}
		out[len(out)-1].Counts[typ] = n
	}
	return out, rows.Err()
}

// Audits 返回最近 limit 条后台操作（新到旧），limit 限定在 1～500，默认 100。
func (s *Store) Audits(limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	rows, e := s.DB.Query("SELECT id,at,actor,action,target,detail FROM audit_logs ORDER BY id DESC LIMIT ?", limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, at int64
		var a, b, c, d string
		if e = rows.Scan(&id, &at, &a, &b, &c, &d); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "at": at, "actor": a, "action": b, "target": c, "detail": d})
	}
	return out, rows.Err()
}

// Storage budget (docs/design.md §9.2): message bodies are never stored;
// per-room minute stats are kept 48 h (enough for today/yesterday charts) and then
// rolled up into hourly rows kept for StatsDays; only the newest LabRunsKept lab
// reports are kept. Freed pages are returned incrementally instead of a full VACUUM,
// which rewrote the whole file and blocked every write while it ran.
const (
	MinuteRetention = 48 * time.Hour
	LabRunsKept     = 20
)

func openDB(path string) (*sql.DB, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// enableIncrementalVacuum switches the file to auto_vacuum=INCREMENTAL. A new file
// takes the setting directly. An existing one is rebuilt once with VACUUM INTO a
// sibling file (a plain VACUUM would stage the whole database in the temp directory,
// which is a small tmpfs in the container), verified, and swapped in. reopen reports
// that db was closed and the caller must open the path again.
func enableIncrementalVacuum(db *sql.DB, path string) (reopen bool, err error) {
	var mode, tables int
	if e := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); e != nil || mode == 2 {
		return false, e
	}
	if _, e := db.Exec("PRAGMA auto_vacuum=INCREMENTAL"); e != nil {
		return false, e
	}
	if e := db.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&tables); e != nil || tables == 0 {
		return false, e
	}
	next := path + ".incremental"
	_ = os.Remove(next)
	if _, e := db.Exec("VACUUM INTO '" + strings.ReplaceAll(next, "'", "''") + "'"); e != nil {
		_ = os.Remove(next)
		return false, e
	}
	if e := verifyIncremental(next); e != nil {
		_ = os.Remove(next)
		return false, e
	}
	// The copy already contains everything committed, including what was still in
	// the WAL, so the old WAL is discarded only after the swap succeeds.
	if e := db.Close(); e != nil {
		_ = os.Remove(next)
		return true, e
	}
	if e := os.Rename(next, path); e != nil {
		_ = os.Remove(next)
		return true, e
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	return true, nil
}

func verifyIncremental(path string) error {
	db, e := openDB(path)
	if e != nil {
		return e
	}
	defer db.Close()
	var mode int
	var check string
	if e = db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); e != nil {
		return e
	}
	if e = db.QueryRow("PRAGMA quick_check").Scan(&check); e != nil {
		return e
	}
	if mode != 2 || check != "ok" {
		return fmt.Errorf("转换后的数据库校验未通过：auto_vacuum=%d，%s", mode, check)
	}
	return nil
}

// rollupSQL sums one whole hour of minute rows into room_stats_hour. Each hour is
// rolled up in full and then its minute rows are deleted, so re-running is harmless.
const rollupSQL = `WITH m AS (SELECT live_id,counts,reconnects,silence FROM room_stats_minute WHERE minute >= ?1 AND minute < ?2),
k AS (SELECT m.live_id,j.key AS key,SUM(CAST(j.value AS INTEGER)) AS n FROM m, json_each(m.counts) j GROUP BY m.live_id,j.key),
r AS (SELECT live_id,SUM(reconnects) AS rc,SUM(silence) AS si FROM m GROUP BY live_id)
INSERT INTO room_stats_hour(live_id,hour,counts,reconnects,silence)
SELECT r.live_id,?1,COALESCE((SELECT json_group_object(k.key,k.n) FROM k WHERE k.live_id=r.live_id),'{}'),r.rc,r.si FROM r WHERE true
ON CONFLICT(live_id,hour) DO UPDATE SET counts=excluded.counts,reconnects=excluded.reconnects,silence=excluded.silence`

// rollup works one hour at a time so even a long backlog (an upgrade from a version
// that kept 30 days of minutes) never needs a large sort.
func (s *Store) rollup(ctx context.Context, cutoff int64) error {
	for ctx.Err() == nil {
		var oldest sql.NullInt64
		if e := s.DB.QueryRowContext(ctx, "SELECT MIN(minute) FROM room_stats_minute WHERE minute < ?", cutoff).Scan(&oldest); e != nil || !oldest.Valid {
			return e
		}
		start := oldest.Int64 / hourMillis * hourMillis
		end := min(start+hourMillis, cutoff)
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, rollupSQL, start, end); e == nil {
			_, e = tx.ExecContext(ctx, "DELETE FROM room_stats_minute WHERE minute >= ? AND minute < ?", start, end)
		}
		if e != nil {
			_ = tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return ctx.Err()
}

const hourMillis = int64(time.Hour / time.Millisecond)

func (s *Store) Clean(ctx context.Context, c model.Settings) error {
	now := time.Now()
	cutoff := now.Add(-MinuteRetention).Truncate(time.Hour).UnixMilli()
	steps := []struct {
		sql  string
		args []any
	}{
		{"DELETE FROM room_stats_hour WHERE hour < ?", []any{now.AddDate(0, 0, -c.StatsDays).UnixMilli()}},
		{"DELETE FROM audit_logs WHERE at < ?", []any{now.AddDate(0, 0, -c.EventDays).UnixMilli()}},
		{"DELETE FROM incidents WHERE at < ?", []any{now.AddDate(0, 0, -c.EventDays).UnixMilli()}},
		{"DELETE FROM sessions WHERE expires_at < ?", []any{now.Unix()}},
	}
	if e := s.rollup(ctx, cutoff); e != nil {
		return e
	}
	for _, step := range steps {
		if _, e := s.DB.ExecContext(ctx, step.sql, step.args...); e != nil {
			return e
		}
	}
	if e := s.pruneLab(ctx); e != nil {
		return e
	}
	// Alerts are created by the observe service; they follow EventDays too.
	var alerts int
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alerts); e != nil {
		return e
	}
	if alerts > 0 {
		if _, e := s.DB.ExecContext(ctx, "DELETE FROM alerts WHERE at < ?", now.AddDate(0, 0, -c.EventDays).UnixMilli()); e != nil {
			return e
		}
	}
	_, e := s.DB.ExecContext(ctx, "PRAGMA incremental_vacuum")
	return e
}

// pruneLab keeps the newest LabRunsKept reports (the lab tables are created by the
// lab service; they may not exist in tools and tests).
func (s *Store) pruneLab(ctx context.Context) error {
	var n int
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('lab_runs','lab_samples')").Scan(&n); e != nil || n < 2 {
		return e
	}
	old := fmt.Sprintf("SELECT id FROM lab_runs WHERE status <> 'running' ORDER BY at DESC LIMIT -1 OFFSET %d", LabRunsKept)
	if _, e := s.DB.ExecContext(ctx, "DELETE FROM lab_samples WHERE run_id IN ("+old+")"); e != nil {
		return e
	}
	_, e := s.DB.ExecContext(ctx, "DELETE FROM lab_runs WHERE id IN ("+old+")")
	return e
}

// SizeBytes is the database file size in use (pages × page size).
func (s *Store) SizeBytes() int64 {
	var pages, size, free int64
	_ = s.DB.QueryRow("PRAGMA page_count").Scan(&pages)
	_ = s.DB.QueryRow("PRAGMA page_size").Scan(&size)
	_ = s.DB.QueryRow("PRAGMA freelist_count").Scan(&free)
	return (pages - free) * size
}
