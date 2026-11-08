import type { CodeTab } from "./code";

/* 接入文档的示例代码。直播间号、Key、ID 都是占位示例；接口地址取自当前后台地址。 */

const wsOf = (base: string) => base.replace(/^http/, "ws");

export const examples = {
  quickstart: (base: string): CodeTab[] => [
    {
      label: "cURL",
      code: `# Key 从环境变量读取，不要写进脚本或命令历史
export LIVETAP_API_KEY="<API_KEY>"

# 1. 节点信息
curl -s ${base}/node -H "Authorization: Bearer $LIVETAP_API_KEY"

# 2. 订阅直播间（商家开播时）
curl -s -X POST ${base}/subscriptions \\
  -H "Authorization: Bearer $LIVETAP_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"live_id":"123456789","external_ref":"merchant:1001:session:20261017","event_types":["chat","member","social","status"]}'

# 3. 退订（商家主动结束时）
curl -s -X DELETE ${base}/subscriptions/sub_example -H "Authorization: Bearer $LIVETAP_API_KEY"`,
    },
    {
      label: "Go SDK",
      code: `package main

import (
	"context"
	"fmt"
	"os"

	collector "github.com/Vz7797/LiveTap/sdk/go"
)

func main() {
	c, _ := collector.New("${base.replace(/\/v1$/, "")}", os.Getenv("LIVETAP_API_KEY"))
	ctx := context.Background()

	sub, err := c.Subscribe(ctx, collector.SubscribeRequest{
		LiveID:      "123456789",
		ExternalRef: "merchant:1001:session:20261017",
		EventTypes:  []string{"chat", "member", "social", "status"},
	})
	if err != nil {
		panic(err)
	}
	go c.AutoRenew(ctx, []collector.Subscription{sub}) // 断流期间也保持续租

	_ = c.Stream(ctx, collector.StreamOptions{
		Deduper: collector.NewDeduper(), // 按消息 ID 去重
		OnEvent: func(e collector.Event) error {
			if e.Type == "chat" {
				fmt.Println(e.User.Nickname, "：", e.Content)
			}
			return nil
		},
		OnGap: func(collector.Event) { fmt.Println("期间可能漏消息") },
	})
}`,
    },
  ],

  node: (base: string): CodeTab[] => [
    { label: "cURL", code: `curl -s ${base}/node -H "Authorization: Bearer $LIVETAP_API_KEY"` },
    {
      label: "响应 200",
      code: `{
  "name": "collector-01",
  "version": "0.3.2",
  "api_version": "v1",
  "engine": "douyinlive",
  "boot_id": "node-start-id",
  "seq": 210,
  "capacity": {"max": 200, "used": 20, "live": 15},
  "health": 100,
  "load_score": 0.1,
  "cooldown": {"active": false, "until": 0, "remainingSeconds": 0, "strikes": 0}
}`,
    },
  ],

  subscribe: (base: string): CodeTab[] => [
    {
      label: "cURL",
      code: `curl -s -X POST ${base}/subscriptions \\
  -H "Authorization: Bearer $LIVETAP_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "live_id": "https://live.douyin.com/123456789",
    "external_ref": "merchant:1001:session:20261017",
    "event_types": ["chat", "member", "social", "status"],
    "options": {"lease_seconds": 120, "idle_release_seconds": 3600}
  }'`,
    },
    {
      label: "响应 200",
      code: `{
  "id": "sub_example",
  "live_id": "123456789",
  "external_ref": "merchant:1001:session:20261017",
  "event_types": ["chat", "member", "social", "status"],
  "options": {"lease_seconds": 120, "idle_release_seconds": 3600},
  "created_at": "2026-10-17T12:00:00Z",
  "expires_at": "2026-10-17T12:02:00Z",
  "status": {"live_id": "123456789", "room_id": "", "state": "probing", "reason": "排队等待启动", "last_message_at": null}
}`,
    },
  ],

  manage: (base: string): CodeTab[] => [
    {
      label: "cURL",
      code: `# 本接入方的全部订阅
curl -s ${base}/subscriptions -H "Authorization: Bearer $LIVETAP_API_KEY"

# 单个订阅（含当前状态）
curl -s ${base}/subscriptions/sub_example -H "Authorization: Bearer $LIVETAP_API_KEY"

# 退订
curl -s -X DELETE ${base}/subscriptions/sub_example -H "Authorization: Bearer $LIVETAP_API_KEY"`,
    },
  ],

  renew: (base: string): CodeTab[] => [
    {
      label: "cURL",
      code: `curl -s -X POST ${base}/subscriptions:renew \\
  -H "Authorization: Bearer $LIVETAP_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"subscription_ids":["sub_example"]}'`,
    },
  ],

  status: (base: string): CodeTab[] => [
    {
      label: "cURL",
      code: `curl -s -X POST ${base}/rooms/status:batch \\
  -H "Authorization: Bearer $LIVETAP_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"live_ids":["123456789","https://live.douyin.com/234567890"]}'`,
    },
    {
      label: "响应 200",
      code: `{"rooms":[
  {"live_id":"123456789","room_id":"7696945466596018971","state":"collecting","reason":"已连接"},
  {"live_id":"234567890","room_id":"","state":"waiting","reason":"直播已结束，10 分钟内未重新开播将停止检测"}
]}`,
    },
  ],

  events: (): CodeTab[] => [
    {
      label: "弹幕",
      code: `{
  "id": "7701234567890123456", "seq": 211, "type": "chat",
  "subscription_id": "sub_example", "external_ref": "merchant:1001:session:20261017",
  "live_id": "123456789", "room_id": "7696945466596018971",
  "ts": "2026-10-17T12:00:01.234Z", "received_at": "2026-10-17T12:00:02.310Z",
  "user": {"id": "user-id", "sec_uid": "user-sec-id", "nickname": "示例用户", "avatar": "https://…"},
  "content": "这个多少钱？", "extra": {}
}`,
    },
    {
      label: "状态心跳",
      code: `{
  "id": "", "seq": 212, "type": "status",
  "subscription_id": "sub_example", "external_ref": "merchant:1001:session:20261017",
  "live_id": "123456789",
  "extra": {
    "code": "heartbeat", "state": "collecting", "reason": "已连接",
    "last_message_at": "2026-10-17T12:00:58Z", "heartbeat_interval_seconds": 60,
    "node_cooldown": false, "cooldown_remaining_seconds": 0
  }
}`,
    },
    {
      label: "订阅结束",
      code: `{
  "id": "", "seq": 980, "type": "status",
  "live_id": "123456789",
  "extra": {"code": "subscription_released", "subscription_id": "sub_example", "live_id": "123456789", "reason": "live_ended"}
}`,
    },
  ],

  full: (base: string): CodeTab[] => [
    {
      label: "Python",
      code: `# pip install requests websocket-client
import json, os, requests, websocket

BASE = "${base}"
KEY = os.environ["LIVETAP_API_KEY"]
H = {"Authorization": f"Bearer {KEY}"}

sub = requests.post(f"{BASE}/subscriptions", headers=H, json={
    "live_id": "123456789",
    "external_ref": "merchant:1001:session:20261017",
    "event_types": ["chat", "member", "social", "status"],
}).json()

seen = set()  # 生产环境请换成持久化存储，键为 (live_id, id)

def on_message(ws, raw):
    e = json.loads(raw)
    extra = e.get("extra") or {}
    if e["type"] == "status" and not e.get("subscription_id"):
        if extra.get("code") == "subscription_released" and extra.get("reason") == "live_ended":
            print("本场已下播：", extra.get("live_id"))
        return
    key = (e["live_id"], e["id"])
    if e["id"] and key in seen:
        return
    seen.add(key)
    if e["type"] == "chat":
        print(e["user"]["nickname"], "：", e["content"])

# 节点每 15 秒 Ping，websocket-client 会自动回 Pong，从而续租
websocket.WebSocketApp("${wsOf(base)}/stream", header=[f"Authorization: Bearer {KEY}"],
                       on_message=on_message).run_forever(reconnect=5)`,
    },
    {
      label: "Node.js",
      code: `// npm install ws
import WebSocket from "ws";

const BASE = "${base}";
const KEY = process.env.LIVETAP_API_KEY;
const headers = { Authorization: \`Bearer \${KEY}\`, "Content-Type": "application/json" };

const sub = await fetch(\`\${BASE}/subscriptions\`, {
  method: "POST",
  headers,
  body: JSON.stringify({ live_id: "123456789", external_ref: "merchant:1001:session:20261017", event_types: ["chat", "member", "social", "status"] }),
}).then((r) => r.json());

const seen = new Set(); // 生产环境请换成持久化去重
const ws = new WebSocket("${wsOf(base)}/stream", { headers: { Authorization: \`Bearer \${KEY}\` } });
ws.on("message", (raw) => {
  const e = JSON.parse(raw);
  if (e.type === "status" && !e.subscription_id) {
    if (e.extra?.code === "subscription_released" && e.extra?.reason === "live_ended") console.log("本场已下播", e.extra.live_id);
    return;
  }
  const key = \`\${e.live_id}:\${e.id}\`;
  if (e.id && seen.has(key)) return;
  seen.add(key);
  if (e.type === "chat") console.log(e.user.nickname, "：", e.content);
});
// ws 库会自动回 Pong；断线后请带 boot_id 与 since_seq 重连补拉`,
    },
    {
      label: "Go SDK",
      code: `// SDK 自带自动续租、断线重连、去重与缺口回调
c, _ := collector.New("${base.replace(/\/v1$/, "")}", os.Getenv("LIVETAP_API_KEY"))
sub, _ := c.Subscribe(ctx, collector.SubscribeRequest{LiveID: "123456789", ExternalRef: "merchant:1001:session:20261017"})
go c.AutoRenew(ctx, []collector.Subscription{sub})
_ = c.Stream(ctx, collector.StreamOptions{
	Deduper: collector.NewDeduper(),
	OnEvent: func(e collector.Event) error {
		if e.Type == "status" && e.SubscriptionID == "" && e.StringField("code") == "subscription_released" {
			fmt.Println("订阅结束：", e.StringField("reason")) // live_ended = 本场已下播
			return nil
		}
		if e.Type == "chat" {
			fmt.Println(e.User.Nickname, "：", e.Content)
		}
		return nil
	},
	OnGap: func(collector.Event) { fmt.Println("期间可能漏消息") },
})`,
    },
  ],
};
