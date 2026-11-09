import {
  Server,
  Bell,
  ChevronsLeft,

  LogOut,
  Menu,
  Moon,
  Plus,
  Search,
  ShieldCheck,
  Sun,
  KeyRound as KeyIcon,
  Palette,
  Zap,
  CircleUserRound,
  ArrowUpRight,
} from "lucide-react";
import { AnimatePresence, LayoutGroup, motion } from "motion/react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMotionSettings } from "@/app/motion";
import { prefetchPage } from "@/app/pages";
import { CommandPalette, type CommandItem } from "@/components/motion/command-palette";
import { ActionSwapIcon } from "@/components/motion/action-swap";
import { Avatar, IconButton, Kbd, Meter, StatusDot } from "@/components/kit";
import { RollingNumber } from "@/components/motion/rolling-number";
import { Logo } from "@/components/illustrations";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { fmtAgo, fmtDuration } from "@/lib/format";
import { STATUS_META, isAbnormal, isLive, type Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";
import { useSim } from "@/data/runtime";
import { NAV, NAV_BY_KEY, isLabEnabled, navigate, useRoute, visibleNav, type PageKey } from "./router";
import { useTheme } from "./theme";
import { logout } from "@/data/api";
import { notifyError } from "@/data/runtime";

const COLLAPSE_KEY = "livetap-sidebar-collapsed";

export function Shell({ children }: { children: ReactNode }) {
  const { page } = useRoute();
  const { minimal } = useMotionSettings();
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === "1";
    } catch {
      return false;
    }
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSE_KEY, collapsed ? "1" : "0");
    } catch {
      /* ignore */
    }
  }, [collapsed]);

  useEffect(() => setMobileOpen(false), [page]);

  return (
    <div className="relative min-h-dvh">
      <aside
        className={cn(
          "fixed inset-y-3 left-3 z-40 hidden flex-col rounded-3xl border bg-card shadow-[var(--shadow-card)] transition-[width] duration-200 ease-out lg:flex",
          collapsed ? "w-[76px]" : "w-[244px]",
        )}
      >
        <SidebarBody collapsed={collapsed} onCollapse={() => setCollapsed((c) => !c)} />
      </aside>

      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-[280px] gap-0 border-r bg-card p-0 sm:max-w-[280px]">
          <SheetTitle className="sr-only">导航</SheetTitle>
          <SidebarBody collapsed={false} mobile />
        </SheetContent>
      </Sheet>

      <div className={cn("transition-[padding] duration-200 ease-out", collapsed ? "lg:pl-[92px]" : "lg:pl-[260px]")}>
        <Topbar onMenu={() => setMobileOpen(true)} onSearch={() => setPaletteOpen(true)} />
        <main className="w-full px-4 pt-2 pb-16 sm:px-6">
          {/* 切换页面不再等旧页面退场动画：精简模式直接替换；标准模式只给新页面一个
              140 ms 的纯透明度淡入（由合成层完成，不占主线程），新页面立即开始渲染。 */}
          <div key={page} className={minimal ? undefined : "page-enter"}>
            {children}
          </div>
        </main>
      </div>

      <CommandMenu open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

/* ---------------- 侧边栏 ---------------- */

function SidebarBody({ collapsed, onCollapse, mobile }: { collapsed: boolean; onCollapse?: () => void; mobile?: boolean }) {
  const { page } = useRoute();
  const collecting = useSim((s) => s.rooms.filter((r) => isLive(r.status)).length);
  const incidents = useSim((s) => s.incidents);
  const badges = useMemo<Partial<Record<PageKey, { n: number; tone: "ok" | "err" | "warn" }>>>(() => {
    const open = incidents.filter((i) => !i.acked).length;
    return {
      rooms: { n: collecting, tone: "ok" },
      alerts: open ? { n: open, tone: "err" } : undefined,
    };
  }, [collecting, incidents]);

  const groups = useMemo(() => {
    const g = new Map<string, typeof NAV>();
    visibleNav().forEach((n) => g.set(n.group, [...(g.get(n.group) ?? []), n]));
    return [...g.entries()];
  }, []);

  return (
    <div className="flex h-full flex-col">
      <div className={cn("flex h-[68px] shrink-0 items-center gap-2.5 px-4", collapsed && "justify-center px-0")}>
        <Logo size={36} />
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="text-[15px] font-semibold tracking-tight">
              Live<span className="text-ink-chat">Tap</span>
            </div>
            <div className="truncate text-[11px] text-muted-foreground">直播采集</div>
          </div>
        )}
      </div>

      <nav className="scrollbar-none flex-1 overflow-y-auto px-3 pb-3">
        <LayoutGroup id={mobile ? "nav-m" : "nav"}>
          {groups.map(([group, items]) => (
            <div key={group} className="mt-3 first:mt-1">
              {!collapsed ? (
                <div className="px-3 pb-1.5 text-[11px] font-medium text-faint">{group}</div>
              ) : (
                <div className="mx-auto mb-2 h-px w-6 bg-border" />
              )}
              <div className="space-y-0.5">
                {items.map((n) => {
                  const active = page === n.key;
                  const Icon = n.icon;
                  const badge = badges[n.key];
                  return (
                    <button
                      key={n.key}
                      type="button"
                      onClick={() => navigate(n.key)}
                      // 鼠标移上或键盘聚焦时预取该页面脚本，点击后无需等待下载
                      onPointerEnter={() => prefetchPage(n.key)}
                      onFocus={() => prefetchPage(n.key)}
                      title={collapsed ? n.label : undefined}
                      className={cn(
                        "group relative flex h-10 w-full cursor-pointer items-center gap-3 rounded-xl px-2.5 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
                        active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                        collapsed && "justify-center px-0",
                      )}
                    >
                      {active && (
                        <motion.span
                          layoutId="nav-active"
                          className="absolute inset-0 rounded-xl bg-primary/10"
                          transition={{ type: "tween", duration: 0.18 }}
                        >
                          <span className="absolute top-1/2 -left-3 h-5 w-1 -translate-y-1/2 rounded-r-full bg-primary" />
                        </motion.span>
                      )}
                      <span
                        className={cn(
                          "relative inline-flex size-7 shrink-0 items-center justify-center rounded-lg transition-colors",
                          active ? "tint-chat" : "group-hover:bg-accent",
                        )}
                      >
                        <Icon className="size-[17px]" strokeWidth={2} />
                      </span>
                      {!collapsed && <span className="relative flex-1 truncate text-left font-medium">{n.label}</span>}
                      {badge && !collapsed && (
                        <span className={cn("num relative rounded-full px-1.5 py-px text-[10px] font-semibold", TONE[badge.tone].tint)}>
                          {badge.n}
                        </span>
                      )}
                      {badge && collapsed && badge.tone !== "ok" && (
                        <span className={cn("absolute top-1.5 right-3 size-2 rounded-full ring-2 ring-card", TONE[badge.tone].fill)} />
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
        </LayoutGroup>
      </nav>

      <div className="shrink-0 p-3 pt-0">
        {!collapsed ? (
          <SidebarNode />
        ) : (
          <div className="flex justify-center py-2">
            <span className="size-2 rounded-full bg-ok" />
          </div>
        )}
        {onCollapse && (
          <button
            type="button"
            onClick={onCollapse}
            className={cn(
              "mt-2 flex h-9 w-full cursor-pointer items-center gap-2 rounded-xl px-3 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              collapsed && "justify-center px-0",
            )}
            aria-label={collapsed ? "展开侧边栏" : "收起侧边栏"}
          >
            <ChevronsLeft className={cn("size-4 transition-transform duration-200", collapsed && "rotate-180")} />
            {!collapsed && "收起侧边栏"}
          </button>
        )}
      </div>
    </div>
  );
}

function SidebarNode() {
  const node = useSim((s) => s.node);
  const collecting = useSim((s) => s.rooms.filter((r) => isLive(r.status)).length);
  const cooling = !!node.cooldown?.active;
  const health = node.health ?? 100;
  const state = cooling
    ? { label: "节点冷却", tone: "warn" as const }
    : health >= 80
      ? { label: "运行良好", tone: "ok" as const }
      : { label: "需要关注", tone: "warn" as const };
  const memPct = node.memLimitMB > 0 ? (node.memMB / node.memLimitMB) * 100 : 0;
  return (
    <div className="rounded-2xl border bg-background/50 p-3">
      <div className="flex items-center gap-2">
        <span className={cn("t-tone inline-flex size-7 shrink-0 items-center justify-center rounded-lg", TONE[state.tone].tint)}>
          <Server className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1 truncate text-xs font-semibold">{node.name || "采集节点"}</div>
        <span className={cn("t-tone inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium", TONE[state.tone].tint)}>
          <span className={cn("size-1.5 rounded-full", TONE[state.tone].fill)} />
          {state.label}
        </span>
      </div>
      <div className="mt-3 space-y-2">
        {[
          { label: "CPU", value: `${node.cpu.toFixed(1)}%`, pct: node.cpu, tone: (node.cpu >= 85 ? "warn" : "info") as Tone },
          { label: "内存", value: `${Math.round(node.memMB)} MB`, pct: memPct, tone: (memPct >= 85 ? "warn" : "social") as Tone },
        ].map((m) => (
          <div key={m.label}>
            <div className="flex items-baseline justify-between text-[11px]">
              <span className="text-muted-foreground">{m.label}</span>
              <RollingNumber className="num font-medium" text={m.value} />
            </div>
            <Meter value={m.pct} max={100} tone={m.tone} className="mt-1 h-1" />
          </div>
        ))}
      </div>
      <div className="mt-2.5 flex items-center justify-between gap-2 border-t pt-2 text-[11px] text-muted-foreground">
        <span className="truncate">已运行 {fmtDuration(Date.now() - node.startedAt)}</span>
        <span className="shrink-0">
          采集中 <RollingNumber className="num font-semibold text-foreground" text={String(collecting)} /> 间
        </span>
      </div>
    </div>
  );
}

/* ---------------- 版本标识 ---------------- */

function fmtDay(ms: number) {
  const d = new Date(ms);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** 顶栏版本标识：我们的版本 + 采集内核版本；内核作者发了新版本时亮一个提醒点 */
function VersionBadge() {
  const version = useSim((s) => s.node.version);
  const revision = useSim((s) => s.node.revision);
  const engine = useSim((s) => s.node.engine);
  const label = useSim((s) => s.node.engineVersion);
  const engineVersion = engine?.version ?? label;
  const newer = !!engine?.newer;
  const latestText = !engine?.enabled ? "未开启检查" : engine.latest ? engine.latest + (engine.publishedAt ? `（${fmtDay(engine.publishedAt)} 发布）` : "") : "检查中…";
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={newer ? `内核有新版本 ${engine?.latest}` : "版本信息"}
          className="hidden h-7 shrink-0 cursor-pointer items-center gap-1.5 rounded-full border bg-card px-2.5 text-[11px] whitespace-nowrap text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground lg:inline-flex"
        >
          <span className="num">
            <span className="hidden xl:inline">LiveTap </span>
            {version}
          </span>
          <span className="text-faint">·</span>
          <span className="num">内核 {engineVersion}</span>
          {newer && <span className="size-1.5 rounded-full bg-warn" />}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[320px] rounded-2xl border-border-strong bg-popover p-4 text-sm">
        <dl className="space-y-2.5">
          {(
            [
              ["LiveTap", revision ? `${version} · ${revision}` : version],
              ["项目主页", <a href="https://github.com/Vz7797/LiveTap" target="_blank" rel="noreferrer" className="hover:underline">Vz7797/LiveTap</a>],
            ["采集内核", engineVersion],
            ["内核作者", <a href={`https://github.com/${engine?.repo ?? "jwwsjlm/douyinLive"}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 hover:underline">{engine?.repo ?? "jwwsjlm/douyinLive"}<ArrowUpRight className="size-3" /></a>],
            ["作者最新版", latestText],
              ["上次检查", engine?.checkedAt ? fmtAgo(engine.checkedAt) : "—"],
            ] as [string, ReactNode][]
          ).map(([k, v]) => (
            <div key={k} className="flex justify-between gap-3">
              <dt className="shrink-0 text-muted-foreground">{k}</dt>
              <dd className="num text-right break-all">{v}</dd>
            </div>
          ))}
        </dl>
        {engine?.error && <p className="mt-3 text-xs text-muted-foreground">最近一次检查失败：{engine.error}，保留上次结果</p>}
        {newer ? (
          <div className="mt-3 rounded-xl tint-warn p-3 text-xs leading-5">
            内核作者发布了 {engine?.latest}。升级要重新核对我们的补丁、跑完测试再部署，不会自动更新。
            {engine?.latestUrl && (
              <a href={engine.latestUrl} target="_blank" rel="noreferrer" className="mt-1 inline-flex items-center gap-0.5 font-medium underline-offset-2 hover:underline">
                查看更新说明 <ArrowUpRight className="size-3.5" />
              </a>
            )}
          </div>
        ) : (
          engine?.latest && <p className="mt-3 text-xs text-muted-foreground">内核已是作者最新版本。</p>
        )}
      </PopoverContent>
    </Popover>
  );
}

/* ---------------- 顶栏 ---------------- */

function Topbar({ onMenu, onSearch }: { onMenu: () => void; onSearch: () => void }) {
  const { page } = useRoute();
  const nav = NAV_BY_KEY[page];
  const { theme, toggle } = useTheme();
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
  const scrolled = useScrolled();

  return (
    // 顶栏实底：滚动后内容不再从顶栏下透出与面包屑、搜索框重叠；滚动时显示细分隔线
    <header className={cn("sticky top-0 z-30 border-b bg-background px-4 pt-3 pb-2 transition-[border-color] duration-150 sm:px-6", scrolled ? "border-border" : "border-transparent")}>
      <div className="flex h-14 w-full items-center gap-2 rounded-2xl border bg-card px-2 shadow-[var(--shadow-card)] sm:gap-3 sm:px-3 lg:border-transparent lg:bg-transparent lg:shadow-none">
        <IconButton icon={Menu} label="打开导航" onClick={onMenu} className="lg:hidden" />
        <div className="flex min-w-0 items-center gap-2 lg:hidden">
          <Logo size={28} />
        </div>
        <div className="hidden min-w-0 items-center gap-2 text-sm whitespace-nowrap lg:flex">
          <span className="text-muted-foreground">{nav.group}</span>
          <span className="text-faint">/</span>
          <span className="font-medium">{nav.label}</span>
        </div>

        <button
          type="button"
          onClick={onSearch}
          className="group ml-auto flex h-10 min-w-0 cursor-pointer items-center gap-2.5 rounded-full border bg-card px-3 text-sm text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground sm:w-[300px] lg:ml-6 lg:bg-card"
        >
          <Search className="size-4 shrink-0" />
          <span className="hidden flex-1 truncate text-left sm:inline">搜索直播间、页面或操作…</span>
          <span className="hidden items-center gap-1 sm:flex">
            <Kbd>{isMac ? "⌘" : "Ctrl"}</Kbd>
            <Kbd>K</Kbd>
          </span>
        </button>

        <div className="flex items-center gap-1 lg:ml-auto">
          <VersionBadge />
          <Notifications />
          <button
            type="button"
            aria-label={theme === "dark" ? "切换到浅色" : "切换到深色"}
            title={theme === "dark" ? "切换到浅色" : "切换到深色"}
            onClick={(e) => toggle({ x: e.clientX, y: e.clientY })}
            className="inline-flex size-9 cursor-pointer items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ActionSwapIcon value={theme} animation="blur">
              {theme === "dark" ? <Sun className="size-[18px]" /> : <Moon className="size-[18px]" />}
            </ActionSwapIcon>
          </button>
          <AdminMenu />
        </div>
      </div>
    </header>
  );
}

function Notifications() {
  const incidents = useSim((s) => s.incidents);
  const open = incidents.filter((i) => !i.acked);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="告警通知"
          className="relative inline-flex size-9 cursor-pointer items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Bell className="size-[18px]" />
          <AnimatePresence>
            {open.length > 0 && (
              <motion.span
                initial={{ scale: 0 }}
                animate={{ scale: 1 }}
                exit={{ scale: 0 }}
                className="num absolute top-1 right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-err px-1 text-[10px] font-semibold text-white ring-2 ring-background"
              >
                {open.length}
              </motion.span>
            )}
          </AnimatePresence>
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[340px] rounded-2xl border-border-strong bg-popover p-0">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="text-sm font-semibold">当前异常房间</div>
          <span className="text-xs text-muted-foreground">点击查看采集状态</span>
        </div>
        <div className="scroll-area max-h-[360px] overscroll-contain p-2 pb-3">
          {incidents.slice(0, 6).map((i) => {
            const tone = i.level === "critical" ? "err" : i.level === "warning" ? "warn" : "info";
            return (
              <button
                key={i.id}
                type="button"
                onClick={() => {
                  navigate("rooms", { room: i.liveId ?? i.id });
                }}
                className="flex w-full cursor-pointer gap-3 rounded-xl p-2.5 text-left transition-colors hover:bg-accent"
              >
                <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", i.acked ? "bg-idle/50" : TONE[tone].fill)} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className={cn("text-sm font-medium", i.acked && "text-muted-foreground")}>{i.title}</span>
                    <span className="ml-auto shrink-0 text-[11px] text-faint">{fmtAgo(i.at)}</span>
                  </div>
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{i.detail}</p>
                </div>
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function AdminMenu() {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="ml-1 cursor-pointer rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring/60" aria-label="管理员菜单">
          <Avatar name="管理员" seed="admin-x" size={34} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52 rounded-xl border-border-strong bg-popover">
        <DropdownMenuLabel className="flex items-center gap-2.5 py-2">
          <Avatar name="管理员" seed="admin-x" size={32} />
          <div className="min-w-0">
            <div className="text-sm font-medium">admin</div>
            <div className="text-[11px] font-normal text-muted-foreground">单管理员 · 7 天未使用自动退出</div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => navigate("settings", { tab: "admin" })}>
          <KeyIcon /> 修改密码
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigate("settings", { tab: "audit" })}>
          <ShieldCheck /> 审计日志
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onSelect={() => void logout().catch(notifyError)}
        >
          <LogOut /> 退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/* ---------------- 命令面板 ---------------- */

function CommandMenu({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const rooms = useSim((s) => s.rooms);
  const { toggle, theme } = useTheme();

  const items = useMemo<CommandItem[]>(() => {
    const pages: CommandItem[] = visibleNav().map((n) => ({
      id: `page-${n.key}`,
      label: n.label,
      group: "跳转页面",
      keywords: [n.desc, n.key],
      icon: n.icon,
      tone: n.tone,
      hint: "页面",
      onSelect: () => navigate(n.key),
    }));
    const actions: CommandItem[] = [
      { id: "act-add", label: "添加直播间", group: "快捷操作", icon: Plus, tone: "chat", keywords: ["新增", "采集", "add"], onSelect: () => navigate("rooms", { add: "1" }) },
      { id: "act-key", label: "创建 API Key", group: "快捷操作", icon: KeyIcon, tone: "gift", keywords: ["接入方", "密钥"], onSelect: () => navigate("clients", { create: "1" }) },
      ...(isLabEnabled() ? [{ id: "act-lab", label: "开始容量测试", group: "快捷操作", icon: Zap, tone: "like" as const, keywords: ["实验室", "压测"], onSelect: () => navigate("lab") }] : []),
      {
        id: "act-theme",
        label: theme === "dark" ? "切换到浅色主题" : "切换到深色主题",
        group: "快捷操作",
        icon: Palette,
        tone: "social",
        keywords: ["主题", "深色", "浅色", "theme"],
        onSelect: () => toggle({ x: window.innerWidth / 2, y: window.innerHeight * 0.3 }),
      },
      { id: "act-pw", label: "修改管理员密码", group: "快捷操作", icon: CircleUserRound, tone: "idle", onSelect: () => navigate("settings", { tab: "admin" }) },
    ];
    const roomItems: CommandItem[] = rooms.map((r) => ({
      id: `room-${r.liveId}`,
      label: r.name,
      group: "直播间",
      keywords: [r.liveId, r.city, ...r.tags, STATUS_META[r.status].label],
      icon: STATUS_META[r.status].icon,
      tone: STATUS_META[r.status].tone,
      badge: (
        <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <StatusDot status={r.status} />
          <span className="num">{r.liveId}</span>
        </span>
      ),
      onSelect: () => navigate("rooms", { room: r.liveId }),
    }));
    return [...actions, ...pages, ...roomItems];
  }, [rooms, theme, toggle]);

  return (
    <CommandPalette
      items={items}
      open={open}
      onOpenChange={onOpenChange}
      placeholder="搜索直播间号、名称、页面或操作…"
      emptyMessage="没有找到匹配的内容"
    />
  );
}

export function useAbnormalCount() {
  const rooms = useSim((s) => s.rooms);
  return useMemo(() => rooms.filter((r) => isAbnormal(r.status)).length, [rooms]);
}

/** 页面是否已向下滚动（只在越过阈值时更新状态，滚动过程中不重复渲染） */
function useScrolled(threshold = 4) {
  const [scrolled, setScrolled] = useState(() => typeof window !== "undefined" && window.scrollY > threshold);
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > threshold);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [threshold]);
  return scrolled;
}
