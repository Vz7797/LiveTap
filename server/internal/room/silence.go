package room

import (
	"math"
	"slices"
	"time"
)

type interval struct {
	at      time.Time
	seconds float64
}
type Silence struct {
	last time.Time
	gaps []interval
}

func (s *Silence) Observe(now time.Time) {
	if !s.last.IsZero() {
		gap := now.Sub(s.last).Seconds()
		if gap >= 1 {
			s.gaps = append(s.gaps, interval{now, gap})
		}
	}
	s.last = now
	s.prune(now)
}
func (s *Silence) prune(now time.Time) {
	for len(s.gaps) > 0 && (now.Sub(s.gaps[0].at) > 30*time.Minute || len(s.gaps) > 1800) {
		s.gaps = s.gaps[1:]
	}
}
func (s *Silence) Threshold(now time.Time) int {
	s.prune(now)
	if len(s.gaps) == 0 {
		return 90
	}
	v := make([]float64, len(s.gaps))
	for i, x := range s.gaps {
		v[i] = x.seconds
	}
	slices.Sort(v)
	return int(math.Ceil(math.Min(300, math.Max(90, v[int(math.Ceil(float64(len(v))*.99))-1]*4))))
}
func PollDelay(idle time.Duration, base int) time.Duration {
	seconds := base
	if idle >= 5*time.Minute {
		seconds = max(seconds, 30)
	}
	if idle >= 15*time.Minute {
		seconds = max(seconds, 60)
	}
	if idle >= 30*time.Minute {
		seconds = max(seconds, 120)
	}
	return time.Duration(seconds) * time.Second
}
