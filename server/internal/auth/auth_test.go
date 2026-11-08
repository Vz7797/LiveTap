package auth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestIPLockPasswordFailuresAndSessionCleanup(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := New(s, bytes.Repeat([]byte{1}, 32), "synthetic-initial-password")
	if e != nil {
		t.Fatal(e)
	}
	token, e := a.Login("good", "admin", "synthetic-initial-password")
	if e != nil {
		t.Fatal(e)
	}
	for range 3 {
		_, _ = a.Login("bad", "admin", "wrong")
	}
	for range 2 {
		if e = a.ChangePassword("bad", token, "wrong", "synthetic-new-password"); !errors.Is(e, ErrLogin) {
			t.Fatal(e)
		}
	}
	if _, e = a.Login("bad", "admin", "synthetic-initial-password"); !errors.Is(e, ErrLocked) {
		t.Fatal("source not locked")
	}
	if _, e = a.Login("good", "admin", "synthetic-initial-password"); e != nil {
		t.Fatal("another source was locked", e)
	}
	_, _ = s.DB.Exec("INSERT INTO sessions(token_hash,expires_at) VALUES('expired',?)", time.Now().Add(-time.Hour).Unix())
	if e = a.Cleanup(time.Now()); e != nil {
		t.Fatal(e)
	}
	var n int
	_ = s.DB.QueryRow("SELECT count(*) FROM sessions WHERE token_hash='expired'").Scan(&n)
	if n != 0 {
		t.Fatal("expired session retained")
	}
	if ok, _ := a.Session(token); !ok {
		t.Fatal("valid session deleted")
	}
	if e = a.Cleanup(time.Now().Add(16 * time.Minute)); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Login("bad", "admin", "synthetic-initial-password"); e != nil {
		t.Fatal("expired lock retained")
	}
}

func TestSessionSlidesWithUseAndStopsAtMax(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := New(s, bytes.Repeat([]byte{1}, 32), "synthetic-initial-password")
	if e != nil {
		t.Fatal(e)
	}
	token, e := a.Login("ip", "admin", "synthetic-initial-password")
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	// 每 6 天用一次：一直有效，直到登录满 SessionMax
	at := start
	for at.Before(start.Add(SessionMax - 7*24*time.Hour)) {
		at = at.Add(6 * 24 * time.Hour)
		if ok, _ := a.session(token, at); !ok {
			t.Fatalf("session expired at day %.0f despite use", at.Sub(start).Hours()/24)
		}
	}
	if ok, _ := a.session(token, start.Add(SessionMax+time.Minute)); ok {
		t.Fatal("session outlived SessionMax")
	}
	// 新会话连续 SessionIdle 未使用即失效
	idle, e := a.Login("ip", "admin", "synthetic-initial-password")
	if e != nil {
		t.Fatal(e)
	}
	if ok, _ := a.session(idle, time.Now().Add(SessionIdle+time.Minute)); ok {
		t.Fatal("idle session still valid")
	}
}
