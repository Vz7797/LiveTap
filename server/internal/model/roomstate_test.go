package model

import (
	"regexp"
	"testing"
)

func TestRoomStateClassification(t *testing.T) {
	want := map[RoomState][3]bool{ // live, abnormal, final
		StateProbing:      {false, false, false},
		StateWaiting:      {false, false, false},
		StateConnecting:   {false, false, false},
		StateCollecting:   {true, false, false},
		StateReconnecting: {false, true, false},
		StateRisk:         {false, true, false},
		StateNotFound:     {false, true, true},
		StateStopped:      {false, false, true},
		StateEnded:        {false, false, true},
	}
	if len(want) != len(RoomStates) {
		t.Fatalf("RoomStates has %d states, the table %d", len(RoomStates), len(want))
	}
	for _, s := range RoomStates {
		w, ok := want[s]
		if !ok || !s.Known() {
			t.Fatalf("%q missing from the table", s)
		}
		if got := [3]bool{s.Live(), s.Abnormal(), s.Final()}; got != w {
			t.Errorf("%s: live/abnormal/final = %v, want %v", s, got, w)
		}
		if s.Active() == s.Final() {
			t.Errorf("%s: Active must be the opposite of Final", s)
		}
		if s.EventKind() == "" {
			t.Errorf("%s has no timeline kind", s)
		}
	}
	if StateCooling.Known() || RoomState("").Known() {
		t.Fatal("cooling is a heartbeat-only state, not a task state")
	}
}

func TestReasonCodesAreStable(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	seen := map[string]bool{}
	for _, c := range ReasonCodes {
		if !shape.MatchString(c) || seen[c] {
			t.Fatalf("reason code %q is malformed or repeated", c)
		}
		seen[c] = true
	}
}
