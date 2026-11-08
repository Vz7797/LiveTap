package observe

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// netDevPath lists the interfaces of the process's own network namespace. In the
// default bridge network that is the container's traffic only: Douyin downstream,
// northbound pushes and the admin page. With host networking it is the whole host.
var netDevPath = "/proc/self/net/dev"

// bandwidth turns the cumulative interface byte counters into KB/s.
type bandwidth struct {
	rx, tx   uint64
	at       time.Time
	down, up float64
	ok       bool
}

func (b *bandwidth) sample(now time.Time) {
	f, err := os.Open(netDevPath)
	if err != nil {
		b.ok = false
		return
	}
	defer f.Close()
	rx, tx, err := readNetDev(f)
	if err != nil {
		b.ok = false
		return
	}
	// A counter that went backwards (interface recreated) only restarts the baseline.
	if !b.at.IsZero() && rx >= b.rx && tx >= b.tx {
		if seconds := now.Sub(b.at).Seconds(); seconds > 0 {
			b.down = float64(rx-b.rx) / seconds / 1024
			b.up = float64(tx-b.tx) / seconds / 1024
			b.ok = true
		}
	}
	b.rx, b.tx, b.at = rx, tx, now
}

// readNetDev sums received and transmitted bytes of every interface except loopback.
func readNetDev(r io.Reader) (rx, tx uint64, err error) {
	scanner := bufio.NewScanner(r)
	found := false
	for scanner.Scan() {
		name, rest, ok := strings.Cut(scanner.Text(), ":")
		name = strings.TrimSpace(name)
		if !ok || name == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		in, e1 := strconv.ParseUint(fields[0], 10, 64)
		out, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		rx, tx, found = rx+in, tx+out, true
	}
	if err = scanner.Err(); err == nil && !found {
		err = errors.New("no network interface")
	}
	return rx, tx, err
}
