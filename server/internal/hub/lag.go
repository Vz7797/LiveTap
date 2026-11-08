package hub

import (
	"slices"
	"sync"
	"time"
)

// Lag keeps the most recent latency samples and reports quantiles over a time
// window. It is cheap enough to observe every message on the hot path.
type Lag struct {
	mu      sync.Mutex
	at      [lagSize]int64
	value   [lagSize]int64
	precise [lagSize]bool
	next    int
}

const lagSize = 4096

// LagStats summarises the samples of the last window. Precise is false when most
// samples were derived from second-resolution timestamps (error up to 1 s).
type LagStats struct {
	P50     int64 `json:"p50_ms"`
	P95     int64 `json:"p95_ms"`
	Samples int   `json:"samples"`
	Precise bool  `json:"precise"`
}

func (l *Lag) Observe(now time.Time, d time.Duration, precise bool) {
	if d < 0 {
		d = 0
	}
	l.mu.Lock()
	l.at[l.next] = now.UnixMilli()
	l.value[l.next] = d.Milliseconds()
	l.precise[l.next] = precise
	l.next = (l.next + 1) % lagSize
	l.mu.Unlock()
}

func (l *Lag) Stats(now time.Time, window time.Duration) LagStats {
	since := now.Add(-window).UnixMilli()
	values := make([]int64, 0, 256)
	precise := 0
	l.mu.Lock()
	for i := range lagSize {
		if l.at[i] != 0 && l.at[i] >= since {
			values = append(values, l.value[i])
			if l.precise[i] {
				precise++
			}
		}
	}
	l.mu.Unlock()
	if len(values) == 0 {
		return LagStats{}
	}
	slices.Sort(values)
	q := func(p float64) int64 { return values[min(len(values)-1, int(p*float64(len(values))))] }
	return LagStats{P50: q(0.5), P95: q(0.95), Samples: len(values), Precise: precise*2 > len(values)}
}
