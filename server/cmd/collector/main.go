// Command collector runs one LiveTap collection node: it receives public Douyin
// live-room messages as a read-only viewer and streams them to integrators.
//
// LiveTap: https://github.com/Vz7797/LiveTap
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/api"
	"github.com/Vz7797/LiveTap/server/internal/api/admin"
	"github.com/Vz7797/LiveTap/server/internal/api/northbound"
	"github.com/Vz7797/LiveTap/server/internal/auth"
	"github.com/Vz7797/LiveTap/server/internal/buildinfo"
	"github.com/Vz7797/LiveTap/server/internal/config"
	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/engine/douyinlive"
	"github.com/Vz7797/LiveTap/server/internal/lab"
	"github.com/Vz7797/LiveTap/server/internal/observe"
	"github.com/Vz7797/LiveTap/server/internal/room"
	"github.com/Vz7797/LiveTap/server/internal/store"
	"github.com/Vz7797/LiveTap/server/internal/webui"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if healthcheck(os.Getenv("LIVETAP_LISTEN")) != nil {
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		// The detailed (redacted) cause is in the rotating log; the log is already
		// closed here, so stderr gets a pointer for `docker logs`.
		fmt.Fprintln(os.Stderr, "服务启动或运行失败，原因见数据目录 logs/collector.log")
		os.Exit(1)
	}
}
func run() (err error) {
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	var level slog.Level
	if e = level.UnmarshalText([]byte(cfg.LogLevel)); e != nil {
		return errors.New("LIVETAP_LOG_LEVEL 无效")
	}
	logs, e := observe.NewLogs(filepath.Join(cfg.DataDir, "logs"), 10<<20, 5, level)
	if e != nil {
		return errors.New("无法创建轮转日志")
	}
	defer logs.Close()
	slog.SetDefault(slog.New(logs.Handler()))
	slog.Info("LiveTap 启动", "version", buildinfo.Version, "homepage", buildinfo.Homepage)
	defer func() {
		if err != nil {
			slog.Error("服务启动或运行失败", "error", err)
		}
	}()
	// An empty per-room proxy means direct access. Never inherit a host-wide proxy.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		_ = os.Unsetenv(name)
	}
	s, e := store.Open(cfg.DataDir, cfg.Key)
	if e != nil {
		return e
	}
	defer s.Close()
	a, e := auth.New(s, cfg.Key, cfg.InitialPassword)
	if e != nil {
		return e
	}
	cfg.InitialPassword = ""
	_ = os.Unsetenv("LIVETAP_ADMIN_INITIAL_PASSWORD")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var background sync.WaitGroup
	defer func() { cancel(); background.Wait() }()
	background.Add(1)
	go func() { defer background.Done(); a.Maintain(ctx) }()
	settings, e := s.Settings()
	if e != nil {
		return e
	}
	budget := engine.NewBudget(settings.RequestsPerSecond)
	m, e := room.New(ctx, s, budget, douyinlive.Factory(budget), cfg.NodeName)
	if e != nil {
		return e
	}
	defer m.Close()
	north, e := northbound.New(ctx, m, northbound.Options{StatusHeartbeatInterval: cfg.StatusHeartbeatInterval})
	if e != nil {
		return e
	}
	defer north.Close()
	background.Add(1)
	go func() { defer background.Done(); m.MonitorResources(ctx) }()
	snap := observe.New(ctx, m)
	defer snap.Close()
	upstream := observe.NewUpstreamCheck(cfg.UpdateCheck)
	snap.SetUpstream(upstream)
	background.Add(1)
	go func() { defer background.Done(); upstream.Run(ctx) }()
	alerts, e := observe.NewAlerts(ctx, m)
	if e != nil {
		return e
	}
	defer alerts.Close()
	experiments, e := lab.New(ctx, m, snap.Get)
	if e != nil {
		return e
	}
	defer experiments.Close()
	adminServer := admin.New(m, a, snap, webui.Handler())
	adminServer.CookieSecure = cfg.CookieSecure
	adminServer.TrustedProxies = cfg.TrustedProxies
	adminServer.Logs = logs
	adminServer.Alerts = alerts
	if cfg.LabEnabled {
		adminServer.Lab = experiments
	}
	adminServer.LabEnabled = cfg.LabEnabled
	adminServer.Northbound = north
	handler := adminServer.Handler()
	mainHandler, managementHandler := api.Listeners(handler, (&northbound.Server{Service: north, TrustedProxies: cfg.TrustedProxies, PingInterval: cfg.WSPingInterval}).Handler(), cfg.AdminListen != "")
	servers := []*http.Server{{Addr: cfg.Listen, Handler: mainHandler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}}
	if cfg.AdminListen != "" {
		servers = append(servers, &http.Server{Addr: cfg.AdminListen, Handler: managementHandler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10})
	}
	errs := make(chan error, len(servers))
	for _, srv := range servers {
		go func() { slog.Info("服务开始监听", "address", srv.Addr); errs <- srv.ListenAndServe() }()
	}
	select {
	case <-ctx.Done():
	case e = <-errs:
		cancel()
		if !errors.Is(e, http.ErrServerClosed) {
			e = fmt.Errorf("HTTP 监听失败：%w", e)
		}
	}
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdown)
	}
	return e
}
