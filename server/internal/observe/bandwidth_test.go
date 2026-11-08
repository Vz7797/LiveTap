package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const netDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 9000000     100    0    0    0     0          0         0  9000000     100    0    0    0     0       0          0
  eth0: %RX%    2000    0    0    0     0          0         0   %TX%    1500    0    0    0     0       0          0
`

func writeNetDev(t *testing.T, path string, rx, tx string) {
	t.Helper()
	body := strings.NewReplacer("%RX%", rx, "%TX%", tx).Replace(netDevFixture)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBandwidthFromInterfaceCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev")
	previous := netDevPath
	netDevPath = path
	defer func() { netDevPath = previous }()

	var b bandwidth
	at := time.Now()
	writeNetDev(t, path, "1000000", "200000")
	b.sample(at)
	if b.ok {
		t.Fatal("a single reading has no rate yet")
	}
	// 2.5 s later: 512000 B down and 25600 B up, loopback ignored.
	writeNetDev(t, path, "1512000", "225600")
	b.sample(at.Add(2500 * time.Millisecond))
	if !b.ok || b.down != 200 || b.up != 10 {
		t.Fatalf("rate = %.2f / %.2f KB/s, ok=%v", b.down, b.up, b.ok)
	}
	// Counters reset: keep the last rate and restart the baseline instead of going negative.
	writeNetDev(t, path, "100", "100")
	b.sample(at.Add(5 * time.Second))
	if b.down != 200 || b.up != 10 || b.rx != 100 {
		t.Fatalf("reset handled wrongly: %+v", b)
	}
	os.Remove(path)
	b.sample(at.Add(7500 * time.Millisecond))
	if b.ok {
		t.Fatal("missing counters must report bandwidth as unavailable")
	}
}
