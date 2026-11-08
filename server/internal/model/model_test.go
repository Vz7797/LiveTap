package model

import (
	"encoding/json"
	"testing"
)

func TestOverridesDoNotMutateGlobalDefaults(t *testing.T) {
	base := DefaultSettings().Defaults
	next, err := Merge(base, Overrides{"event_types": json.RawMessage(`["gift"]`)})
	if err != nil || len(next.EventTypes) != 1 || next.EventTypes[0] != "gift" {
		t.Fatal("override failed")
	}
	if base.EventTypes[0] != "chat" {
		t.Fatal("room override changed shared global settings")
	}
}
