package northbound

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/Vz7797/LiveTap/sdk/go"
	"github.com/shirou/gopsutil/v4/process"
)

// Opt-in, offline only. The report records process scope explicitly because SDK
// clients share the same test process; this is not a server capacity verdict.
func TestNorthboundStress(t *testing.T) {
	if os.Getenv("LIVETAP_STRESS") != "1" {
		t.Skip("opt-in fake-engine stress")
	}
	f := setup(t, false)
	var clients []*sdk.Client
	owners := []string{}
	ids := []string{}
	for i := 0; i < 200; i++ {
		ids = append(ids, fmt.Sprint(400000000+i))
	}
	for range 4 {
		owner, c, _ := f.client(t)
		owners = append(owners, owner.ID)
		clients = append(clients, c)
		for _, id := range ids {
			if _, err := c.Subscribe(f.ctx, sdk.SubscribeRequest{LiveID: id, EventTypes: []string{"chat"}}); err != nil {
				t.Fatal("stress subscription failed")
			}
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for f.source.starts.Load() != 200 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if f.source.starts.Load() != 200 {
		t.Fatal("not all fake rooms started")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var connected, received, gaps atomic.Int64
	var wg sync.WaitGroup
	var latMu sync.Mutex
	latencies := []float64{}
	for _, c := range clients[:3] {
		wg.Add(1)
		go func(c *sdk.Client) {
			defer wg.Done()
			_ = c.Stream(ctx, sdk.StreamOptions{OnConnect: func() { connected.Add(1) }, OnGap: func(sdk.Event) { gaps.Add(1) }, OnEvent: func(e sdk.Event) error {
				if e.Type == "chat" {
					received.Add(1)
					latMu.Lock()
					latencies = append(latencies, float64(time.Since(e.ReceivedAt).Microseconds())/1000)
					latMu.Unlock()
				}
				return nil
			}})
		}(c)
	}
	waitFor(t, func() bool { return connected.Load() == 3 })
	slow, _, err := f.s.Open(owners[3], "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.CloseStream(slow)
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	previous, err := proc.Times()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	lastSample := started
	maxCPU, maxRSS := float64(0), uint64(0)
	samples := []map[string]any{}
	emitted := 0
	emit := time.NewTicker(200 * time.Millisecond)
	defer emit.Stop()
	sample := time.NewTicker(time.Second)
	defer sample.Stop()
	end := time.NewTimer(30 * time.Second)
	defer end.Stop()
loop:
	for {
		select {
		case <-emit.C:
			for _, id := range ids {
				emitted++
				f.source.emit(id, fmt.Sprint(8000000000000000+emitted))
			}
		case <-sample.C:
			now := time.Now()
			cpu, err := proc.Times()
			if err != nil {
				t.Fatal(err)
			}
			rss, err := proc.MemoryInfo()
			if err != nil {
				t.Fatal(err)
			}
			percent := (cpu.User + cpu.System - previous.User - previous.System) / now.Sub(lastSample).Seconds() * 100
			previous = cpu
			lastSample = now
			maxCPU = max(maxCPU, percent)
			maxRSS = max(maxRSS, rss.RSS)
			samples = append(samples, map[string]any{"elapsed_seconds": now.Sub(started).Seconds(), "cpu_one_core_percent": percent, "rss_bytes": rss.RSS, "received": received.Load()})
		case <-end.C:
			break loop
		}
	}
	time.Sleep(500 * time.Millisecond)
	views, total, _ := f.s.Metrics()
	cancel()
	wg.Wait()
	latMu.Lock()
	slices.Sort(latencies)
	percentile := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[min(len(latencies)-1, int(float64(len(latencies)-1)*p))]
	}
	lat := map[string]float64{"p50_ms": percentile(.5), "p95_ms": percentile(.95), "p99_ms": percentile(.99), "max_ms": percentile(1)}
	latMu.Unlock()
	fastDrops, slowDrops := uint64(0), uint64(0)
	for _, v := range views {
		if v.ID == owners[3] {
			slowDrops = v.Dropped
		} else {
			fastDrops += v.Dropped
		}
	}
	slowClosed := false
	select {
	case <-slow.Done:
		slowClosed = true
	default:
	}
	passed := received.Load() == int64(emitted*3) && fastDrops == 0 && gaps.Load() == 0 && f.source.starts.Load() == 200 && f.m.Budget.Count() == 0 && slowDrops > 0 && slowClosed
	report := map[string]any{"mode": "fake-engine-no-upstream", "at": time.Now().Format(time.RFC3339), "duration_seconds": 30, "rooms": 200, "clients": 4, "fast_websocket_clients": 3, "slow_client": "unconsumed in-process outbox, deliberately exercises queue overflow", "upstream_connections": f.source.starts.Load(), "events_emitted": emitted, "fast_events_received": received.Load(), "fast_expected": emitted * 3, "fast_drops": fastDrops, "slow_drops": slowDrops, "gap_callbacks": gaps.Load(), "latency": lat, "peak_cpu_one_core_percent": maxCPU, "peak_rss_bytes": maxRSS, "logical_cpus": runtime.NumCPU(), "process_scope": "collector core plus 3 Go SDK consumers in one Go test process; includes SDK CPU/memory", "northbound": total, "samples": samples, "upstream_requests": f.m.Budget.Count(), "real_collection_verified": false}
	report["passed"] = passed
	report["subscriptions"] = len(ids) * len(clients)
	report["slow_disconnected"] = slowClosed
	output, err := filepath.Abs("../../../../test-results/stress")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "stress.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if !passed {
		t.Fatal("fake stress contract failed; see statistics-only report")
	}
	t.Logf("fake rooms=200, consumers=3, received=%d, fast_drops=%d, slow_drops=%d, p95=%.2fms", received.Load(), fastDrops, slowDrops, lat["p95_ms"])
}
