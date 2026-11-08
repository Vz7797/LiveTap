package room

import "time"

// onlineWindow 是卡片走势线覆盖的分钟数，与消息分钟统计保持一致。
const onlineWindow = 30

// onlineCarry 是某分钟没有收到在线人数上报时，沿用上一分钟数值的最长分钟数；
// 超过后视为没有数据（下播或断流），走势线落到 0。
const onlineCarry = 3

// recordOnline 记下该分钟最后一次上报的在线人数，并丢弃窗口外的旧分钟。调用方持有 t.mu。
func (t *task) recordOnline(at time.Time, online int64) {
	if t.onlineMinutes == nil {
		t.onlineMinutes = map[int64]int64{}
	}
	minute := at.Truncate(time.Minute)
	t.onlineMinutes[minute.UnixMilli()] = online
	cutoff := minute.Add(-onlineWindow * time.Minute).UnixMilli()
	for k := range t.onlineMinutes {
		if k <= cutoff {
			delete(t.onlineMinutes, k)
		}
	}
}

// onlineSpark 返回最近 onlineWindow 分钟每分钟的在线人数（旧到新）。调用方持有 t.mu。
func (t *task) onlineSpark(now time.Time) []int64 {
	out := make([]int64, 0, onlineWindow)
	var last int64
	gap := onlineCarry + 1
	for i := onlineWindow - 1; i >= 0; i-- {
		at := now.Truncate(time.Minute).Add(-time.Duration(i) * time.Minute).UnixMilli()
		if v, ok := t.onlineMinutes[at]; ok {
			last, gap = v, 0
		} else {
			gap++
		}
		if gap > onlineCarry {
			out = append(out, 0)
		} else {
			out = append(out, last)
		}
	}
	return out
}
