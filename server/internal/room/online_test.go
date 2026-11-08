package room

import (
	"testing"
	"time"
)

func TestOnlineSpark(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 30, 20, 0, time.UTC)
	task := &task{}
	task.recordOnline(now.Add(-40*time.Minute), 999) // 窗口外，应被丢弃
	task.recordOnline(now.Add(-10*time.Minute), 120)
	task.recordOnline(now.Add(-10*time.Minute+20*time.Second), 150) // 同一分钟取最后一次
	task.recordOnline(now, 180)
	spark := task.onlineSpark(now)
	if len(spark) != onlineWindow {
		t.Fatalf("len = %d", len(spark))
	}
	if len(task.onlineMinutes) != 2 {
		t.Fatalf("old minutes kept: %v", task.onlineMinutes)
	}
	at := func(minutesAgo int) int64 { return spark[onlineWindow-1-minutesAgo] }
	if at(11) != 0 || at(10) != 150 || at(9) != 150 || at(7) != 150 {
		t.Fatalf("carry forward wrong: %v", spark)
	}
	if at(6) != 0 || at(1) != 0 {
		t.Fatalf("gap should drop to 0: %v", spark)
	}
	if at(0) != 180 {
		t.Fatalf("latest = %d", at(0))
	}
}
