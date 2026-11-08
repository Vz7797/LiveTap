package room

import (
	"bytes"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestTodayIncludesPendingWithoutDoubleCounting(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().Truncate(time.Minute)
	at := now.UnixMilli()
	roomTask := &task{record: model.RoomRecord{LiveID: "123456789"}, minutes: map[int64]model.Counts{at: {"chat": 7}}, persistedMinutes: map[int64]model.Counts{at: {"chat": 4}}, dirtyMinutes: map[int64]bool{at: true}}
	if e = s.Minute("123456789", now, model.Counts{"chat": 4}, 0, 0, "collecting"); e != nil {
		t.Fatal(e)
	}
	m := &Manager{Store: s, tasks: map[string]*task{"123456789": roomTask}}
	check := func(want int64) {
		rows, err := m.MinuteTotals(now)
		if err != nil || len(rows) != 1 || rows[0].Counts["chat"] != want {
			t.Fatalf("wrong merged total: %v %v", rows, err)
		}
	}
	check(7)
	m.flush(roomTask)
	check(7)
	roomTask.minutes[at]["chat"] += 2
	roomTask.dirtyMinutes[at] = true
	check(9)
	m.flush(roomTask)
	check(9)
}

func TestMinuteRowsCountReconnectsPerMinute(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	roomTask := &task{record: model.RoomRecord{LiveID: "123456789"}, state: "collecting"}
	m := &Manager{Store: s, tasks: map[string]*task{"123456789": roomTask}}
	roomTask.setState("reconnecting", "test")
	roomTask.setState("collecting", "test")
	roomTask.setState("reconnecting", "test")
	roomTask.mu.Lock()
	roomTask.countMinute(time.Now(), 0, 1)
	roomTask.mu.Unlock()
	m.flush(roomTask)
	m.flush(roomTask) // flushing again must not add to the stored counts
	var reconnects, silence int
	if e = s.DB.QueryRow("SELECT SUM(reconnects),SUM(silence) FROM room_stats_minute WHERE live_id=?", "123456789").Scan(&reconnects, &silence); e != nil {
		t.Fatal(e)
	}
	if reconnects != 2 || silence != 1 {
		t.Fatalf("minute row stored reconnects=%d silence=%d, want 2 and 1", reconnects, silence)
	}
}

// While the database is busy (one SQLite connection), a flush must not keep the room
// locked: the room loop takes t.mu for every message and would stall until its entry
// queue overflowed.
func TestFlushDoesNotLockRoomWhileWaitingForDatabase(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	roomTask := &task{record: model.RoomRecord{LiveID: "123456789"}, state: "collecting"}
	m := &Manager{Store: s, tasks: map[string]*task{"123456789": roomTask}}
	roomTask.mu.Lock()
	roomTask.countMinute(time.Now(), 1, 0)
	roomTask.mu.Unlock()

	busy, e := s.DB.Begin() // holds the only connection
	if e != nil {
		t.Fatal(e)
	}
	flushed := make(chan struct{})
	go func() { m.flush(roomTask); close(flushed) }()
	time.Sleep(100 * time.Millisecond) // flush is now waiting for the connection
	locked := make(chan struct{})
	go func() {
		roomTask.mu.Lock()
		roomTask.countMinute(time.Now(), 1, 0)
		roomTask.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("room stayed locked while flush waited for the database")
	}
	_ = busy.Rollback()
	<-flushed
	m.flush(roomTask) // the message counted during the write is written next time
	var reconnects int
	if e = s.DB.QueryRow("SELECT SUM(reconnects) FROM room_stats_minute WHERE live_id=?", "123456789").Scan(&reconnects); e != nil {
		t.Fatal(e)
	}
	if reconnects != 2 {
		t.Fatalf("stored reconnects=%d, want 2", reconnects)
	}
}
