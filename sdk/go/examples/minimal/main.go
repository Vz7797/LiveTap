package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	collector "github.com/Vz7797/LiveTap/sdk/go"
)

func main() {
	// Inject values into the current process environment; never put a key in code.
	c, err := collector.New(os.Getenv("LIVETAP_NODE_URL"), os.Getenv("LIVETAP_API_KEY"))
	if err != nil {
		fmt.Println("请配置节点地址与接入密钥")
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	node, err := c.Node(ctx)
	if err != nil {
		fmt.Println("节点查询失败")
		return
	}
	fmt.Printf("节点 %s，容量 %d/%d\n", node.Name, node.Capacity.Used, node.Capacity.Max)
	sub, err := c.Subscribe(ctx, collector.SubscribeRequest{LiveID: os.Getenv("LIVETAP_LIVE_ID"), ExternalRef: "merchant:session", EventTypes: []string{"chat", "status"}})
	if err != nil {
		fmt.Println("订阅失败")
		return
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = c.Unsubscribe(stop, sub.ID)
	}()
	go func() {
		released := func(r collector.Released) { fmt.Printf("订阅已结束：%s\n", r.Reason) }
		if c.AutoRenew(ctx, []collector.Subscription{sub}, released) != nil && ctx.Err() == nil {
			fmt.Println("续租失败，请检查订阅")
			cancel()
		}
	}()
	_ = c.Stream(ctx, collector.StreamOptions{OnEvent: func(e collector.Event) error { fmt.Printf("收到 %s 事件，seq=%d\n", e.Type, e.Seq); return nil }, OnGap: func(collector.Event) { fmt.Println("事件存在缺口，请按业务策略处理") }})
}
