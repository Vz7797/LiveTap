package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

type offlineEngine struct{}

func (*offlineEngine) Name() string { return "offline-test" }
func (*offlineEngine) Close()       {}
func (*offlineEngine) Probe(context.Context, engine.RoomSpec) (engine.LiveStatus, error) {
	return engine.LiveStatus{Code: "offline"}, nil
}
func (*offlineEngine) Run(ctx context.Context, _ engine.RoomSpec, _ engine.Sink) error {
	<-ctx.Done()
	return ctx.Err()
}
func TestFullObservationWithNoConnectionsExportAndCleanup(t *testing.T) {
	db, e := store.Open(t.TempDir(), bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := room.New(ctx, db, engine.NewBudget(100), func() engine.Engine { return &offlineEngine{} }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	s, e := New(ctx, m, func() map[string]any {
		return map[string]any{"node": map[string]any{"cpu": float64(0), "memMB": float64(1)}}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	s.now = func() time.Time { return now }
	s.wait = func(ctx context.Context, d time.Duration) bool { now = now.Add(d); return ctx.Err() == nil }
	result, e := s.Start(Plan{Rooms: []string{"123456789"}, Stages: []int{1}, StartupSeconds: 2, DurationSeconds: 60, Sharing: []bool{false, true}, Modes: []string{"anonymous"}})
	if e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	<-done
	result, e = s.Get(result.ID)
	if e != nil || result.Status != "completed" || len(result.Stages) != 2 {
		t.Fatal("experiment failed", result.Status, e)
	}
	for _, stage := range result.Stages {
		if stage.Finished-stage.Started != 60000 || stage.Samples != 13 || stage.MinimumCollecting != 0 {
			t.Fatal("shortened zero-connection observation", stage)
		}
	}
	records, _ := db.Rooms()
	if len(records) != 0 {
		t.Fatal("test rooms retained")
	}
	var out bytes.Buffer
	if e = s.Export(result.ID, &out); e != nil || !json.Valid(out.Bytes()) {
		t.Fatal("invalid export", e)
	}
	if strings.Contains(out.String(), "sessionid=") {
		t.Fatal("credential in export")
	}
	var exported struct {
		Samples []Sample `json:"samples"`
	}
	if e = json.Unmarshal(out.Bytes(), &exported); e != nil {
		t.Fatal(e)
	}
	// 13 stored samples per stage (one per 5 s for 60 s); room detail only on the first and the last.
	detailed := 0
	for _, sample := range exported.Samples {
		if sample.Rooms != nil {
			detailed++
		}
	}
	if len(exported.Samples) != 26 || detailed != 4 {
		t.Fatal("unexpected sample storage", len(exported.Samples), detailed)
	}
	var raw string
	if e = db.DB.QueryRow("SELECT data FROM lab_samples LIMIT 1").Scan(&raw); e != nil || !strings.HasPrefix(raw, compressedPrefix) {
		t.Fatal("samples are stored uncompressed", e)
	}
	listed, e := s.Listing()
	if e != nil || len(listed) != 1 || len(listed[0].Recent) == 0 {
		t.Fatal("newest report lost its recent curve", e)
	}
	if e = s.Delete(result.ID); e != nil {
		t.Fatal(e)
	}
	var left int
	_ = db.DB.QueryRow("SELECT (SELECT count(*) FROM lab_runs)+(SELECT count(*) FROM lab_samples)").Scan(&left)
	if left != 0 {
		t.Fatal("report not deleted", left)
	}
}

func TestUnpackReadsOldPlainRows(t *testing.T) {
	packed, e := pack([]byte(`{"at":1}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{packed, `{"at":1}`} {
		if data, e := unpack(raw); e != nil || string(data) != `{"at":1}` {
			t.Fatal("unpack failed", raw, string(data), e)
		}
	}
}
func TestDiscoveryAndPlanValidation(t *testing.T) {
	found := ParseRooms(`{"web_rid":"123456789","recommended":{"web_rid":"123456789"},"other":{"web_rid":"223456789"}}`)
	if len(found) != 2 {
		t.Fatal(found)
	}
	p := Plan{Rooms: []string{"123456789"}, Stages: []int{1}, DurationSeconds: 1200, Sharing: []bool{false, true}, Modes: []string{"anonymous"}}
	if e := Validate(p, 200); e != nil {
		t.Fatal(e)
	}
	p.Stages = []int{2}
	if Validate(p, 200) == nil {
		t.Fatal("oversized stage accepted")
	}
}
func TestStageReportsRealCounterDifferences(t *testing.T) {
	first := Sample{At: 1000, Stage: 1, Collecting: 1, Rooms: []RoomSample{{ID: "1", State: "collecting", RequestsTotal: 7, Disconnects: 3, Recoveries: 3}}}
	stage := BeginStage(first)
	last := Sample{At: 5000, Stage: 1, Collecting: 1, Rooms: []RoomSample{{ID: "1", State: "collecting", RequestsTotal: 11, Disconnects: 4, Recoveries: 4, RecoveryMS: []int64{200}, FalsePositives: 1}}}
	stage.Add(last)
	stage.Finish(time.UnixMilli(6000))
	if stage.Rooms[0].Requests != 4 || stage.Rooms[0].Disconnects != 1 || stage.Rooms[0].FalsePositives != 1 || stage.Rooms[0].RecoveryMS[0] != 200 {
		t.Fatal(stage.Rooms)
	}
}

func TestRecoveryHistoryAcrossSamples(t *testing.T) {
	stage := BeginStage(Sample{Stage: 1, Rooms: []RoomSample{{ID: "1"}}})
	for i := 1; i <= 300; i++ {
		stage.Add(Sample{Collecting: 1, Rooms: []RoomSample{{ID: "1", Recoveries: uint64(i), RecoveryMS: []int64{int64(i)}}}})
	}
	stage.Finish(time.Now())
	if len(stage.Rooms[0].RecoveryMS) != 300 || stage.Rooms[0].RecoveryIncomplete {
		t.Fatal("rolling history was lost")
	}
	stage = BeginStage(Sample{Stage: 1, Rooms: []RoomSample{{ID: "1"}}})
	stage.Add(Sample{Rooms: []RoomSample{{ID: "1", Recoveries: 3, RecoveryMS: []int64{10}}}})
	stage.Finish(time.Now())
	if !stage.Rooms[0].RecoveryIncomplete {
		t.Fatal("missing history not disclosed")
	}
}

func TestCancelCleansOnlyOwnedRoomsAndRejectsConflicts(t *testing.T) {
	db, e := store.Open(t.TempDir(), bytes.Repeat([]byte{7}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := room.New(ctx, db, engine.NewBudget(100), func() engine.Engine { return &offlineEngine{} }, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	_, _, e = m.AddConfigured(ctx, "987654321", nil, false, "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(ctx, m, func() map[string]any { return map[string]any{"node": map[string]any{}} })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := Plan{Rooms: []string{"987654321"}, Stages: []int{1}, StartupSeconds: 10, DurationSeconds: 60, Sharing: []bool{false}, Modes: []string{"anonymous"}}
	if _, e = s.Start(p); e == nil {
		t.Fatal("existing task overwritten")
	}
	entered := make(chan struct{})
	s.wait = func(ctx context.Context, _ time.Duration) bool { close(entered); <-ctx.Done(); return false }
	p.Rooms = []string{"123456789"}
	r, e := s.Start(p)
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	if e = s.Stop(r.ID); e != nil {
		t.Fatal(e)
	}
	s.Close()
	r, e = s.Get(r.ID)
	if e != nil || r.Status != "cancelled" {
		t.Fatal("cancel status", e)
	}
	records, e := db.Rooms()
	if e != nil || len(records) != 1 || records[0].LiveID != "987654321" {
		t.Fatal("cleanup affected manual rooms", e)
	}
}

func TestGroupsUseObservedAudienceAndCounterDeltas(t *testing.T) {
	first := Sample{Stage: 2, Rooms: []RoomSample{{ID: "1", Messages: model.Counts{"chat": 10}}, {ID: "2"}}}
	stage := BeginStage(first)
	stage.Add(Sample{Collecting: 1, Rooms: []RoomSample{{ID: "1", State: "collecting", Group: "0-5", Messages: model.Counts{"chat": 14}, Disconnects: 1}, {ID: "2", State: "risk", Group: "unknown"}}})
	stage.Finish(time.Now())
	if stage.Groups["0-5"].Messages["chat"] != 4 || stage.Groups["0-5"].Disconnects != 1 || stage.Groups["unknown"].Collecting != 0 {
		t.Fatal(stage.Groups)
	}
}
