package room

import (
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

// Every status event carries the machine-readable reason_code next to the text.
func TestStatusEventCarriesReasonCode(t *testing.T) {
	var got model.Event
	roomTask := &task{record: model.RoomRecord{LiveID: "123456789"}, state: model.StateCollecting}
	roomTask.statusEvent = func(e model.Event) { got = e }
	roomTask.setState(model.StateReconnecting, model.ReasonConnectionDropped, "上游连接中断，15 秒后重新连接")
	if got.Extra["state"] != "reconnecting" || got.Extra["reason_code"] != model.ReasonConnectionDropped {
		t.Fatalf("status event: %v", got.Extra)
	}
	if roomTask.events[len(roomTask.events)-1].Kind != "reconnect" {
		t.Fatal("timeline kind changed")
	}
}

// Regression: the “no data” reason stayed after messages came back.
func TestNoDataReasonClearsWhenMessagesReturn(t *testing.T) {
	roomTask := &task{record: model.RoomRecord{LiveID: "123456789"}, state: model.StateCollecting, noData: true,
		reason: "无数据：主连接与旁证连接均未收到消息", reasonCode: model.ReasonNoData}
	(&Manager{}).consume(roomTask, engine.RawEvent{Decode: func() model.Event { return model.Event{} }})
	if roomTask.noData || roomTask.reasonCode != model.ReasonCollecting || roomTask.reason != "上游连接已就绪" {
		t.Fatalf("stale reason kept: %v %q %q", roomTask.noData, roomTask.reasonCode, roomTask.reason)
	}
}
