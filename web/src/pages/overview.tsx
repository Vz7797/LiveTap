import { describeDrops } from "@/lib/drops";
import {
  Activity,
  ArrowUp,
  ArrowUpRight,
  Cpu,
  Gauge as GaugeIcon,
  HardDrive,
  History,
  KeyRound,
  LayoutDashboard,
  MemoryStick,
  MoonStar,
  Network,
  PieChart,
  Radar,
  ShieldAlert,
  ShieldCheck,
  RadioTower,
  Trophy,
  Waves,
} from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { navigate } from "@/app/router";
import { Donut, Legend, Sparkline, StackedArea, type SeriesDef } from "@/components/charts";
import { RadarScope } from "@/components/illustrations";
import { Avatar, Card, CardHeader, IconTile, Meter, Num, PageHeader, StatusPill, Tag } from "@/components/kit";
import { Tabs, TabsList, TabsTrigger } from "@/components/motion/tabs";
import { Gauge } from "@/components/charts";
import { fmtAgo, fmtBytesPerSec, fmtClock, fmtDuration, fmtNum } from "@/lib/format";
import { MSG_TYPES, PRE_LIVE, STATUS_META, TYPE_META, isAbnormal, isActive, isLive, type MsgType, type RoomStatus } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";
import { useHistory } from "@/data/detail";
import { useSim } from "@/data/runtime";
import { RollingNumber } from "@/components/motion/rolling-number";
import type { LagStats } from "@/data/types";

/* 堆叠顺序（自下而上）：弹幕在最底用品牌黄，点赞在最上用柔和玫瑰色。 */
const STACK_ORDER: MsgType[] = ["chat", "member", "gift", "social", "like"];
const SERIES: SeriesDef[] = STACK_ORDER.map((t) => ({ key: t, label: TYPE_META[t].label, tone: TYPE_META[t].tone }));

const area = (name: string) => ({ gridArea: name });

export function OverviewPage() {
  return (
    <>
      <PageHeader
        icon={LayoutDashboard}
        tone="chat"
        title="总览"
        desc="节点健康、消息速率、异常与资源，一屏看全"
        actions={
          <Tag tone="ok" className="h-7 rounded-full px-3 text-xs">
            <span className="size-1.5 rounded-full bg-ok" /> 实时 · 2.5 秒刷新
          </Tag>
        }
      />
      <NodeCooldown />
      <div className="overview-grid grid gap-3 sm:gap-4">
        <div style={area("hero")}><HeroCard /></div>
        <div style={area("c1")}><CountCard kind="collecting" /></div>
        <div style={area("c2")}><CountCard kind="waiting" /></div>
        <div style={area("c3")}><CountCard kind="abnormal" /></div>
        <div style={area("cap")}><CapacityCard /></div>
        <div style={area("bud")}><BudgetCard /></div>
        <div style={area("rate")}><RateCard /></div>
        <div style={area("comp")}><CompositionCard /></div>
        <div style={area("radar")}><RadarCard /></div>
        <div style={area("res")}><ResourceCard /></div>
        <div style={area("time")}><TimelineCard /></div>
        <div style={area("top")}><TopRoomsCard /></div>
        <div style={area("cli")}><ClientsCard /></div>
      </div>
    </>
  );
}

function NodeCooldown() {
  const cooldown = useSim((s) => s.node.cooldown);
  if (!cooldown?.active) return null;
  return (
    <div role="status" aria-atomic="true" className="mb-4 flex items-start gap-3 rounded-2xl border border-warn/25 bg-warn/10 p-4">
      <IconTile icon={ShieldAlert} tone="warn" size={36} />
      <div className="min-w-0">
        <p className="font-medium text-ink-warn">节点冷却中，剩余 <span className="num">{cooldown.remainingSeconds}</span> 秒</p>
        <p className="mt-1 text-sm text-muted-foreground">上游访问受限，新的探测与建连已暂停。正在采集的连接继续接收消息。</p>
      </div>
    </div>
  );
}

/* ---------------- 主卡片：节点健康 + 今日节奏 ---------------- */

function HeroCard() {
  const node = useSim((s) => s.node);
  const today = useSim((s) => MSG_TYPES.reduce((sum, type) => sum + s.totals[type], 0));
  const live = useSim((s) => s.rooms.filter((r) => isLive(r.status)).length);
  const perMin = useSim((s) => {
    const last = s.series[s.series.length - 1]?.v;
    return last ? MSG_TYPES.reduce((a, t) => a + last[t], 0) : 0;
  });
  return (
    <Card className="flex h-full flex-col p-5 sm:p-6">
      <div className="flex items-center gap-3">
        <IconTile icon={RadioTower} tone="chat" accent />
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">采集节点</div>
          <h2 className="truncate text-base font-semibold">{node.name}</h2>
        </div>
        <Tag tone={node.health === 100 ? "ok" : "warn"} className="ml-auto h-6 rounded-full px-2.5 text-xs">
          <span className={`size-1.5 rounded-full ${node.health === 100 ? "bg-ok" : "bg-warn"}`} /> {node.health === 100 ? "运行良好" : "需关注"}
        </Tag>
      </div>

      <div className="mt-6 flex items-center gap-5">
        <div className="min-w-0 flex-1">
          <div className="text-sm text-muted-foreground">今日已采集消息</div>
          <Num animate value={today} format={(n) => fmtNum(n, false)} className="mt-2 text-[34px] leading-none font-semibold tracking-tight sm:text-[44px]" />
          <div className="mt-3 text-xs text-muted-foreground">
            当前 <RollingNumber className="num font-medium text-foreground" text={fmtNum(perMin)} /> 条/分钟 · 实际采集速率
          </div>
        </div>
        <Gauge value={node.health} size={96} thickness={8} colors={["var(--c-ok)", "var(--c-ok)"]}>
          <Num animate value={node.health} className="text-[24px] leading-none font-semibold" format={(n) => String(Math.round(n))} />
          <span className="mt-1 text-[10px] text-muted-foreground">健康度</span>
        </Gauge>
      </div>

      <DayRhythm total={today} className="mt-6 min-h-[112px] flex-1" />

      <div className="mt-5 grid grid-cols-3 gap-2 border-t pt-4 text-xs">
        <HeroStat label="直播中" value={`${live} 间`} />
        <HeroStat label="已运行" value={fmtDuration(Date.now() - node.startedAt).replace(/ 小时$/, "时")} />
        <HeroStat label="采集内核" value={node.engineVersion} />
      </div>
    </Card>
  );
}

/* 今日每 30 分钟的消息量，背后叠一层昨日同时段做参照：
   昨日为浅蓝底柱，今日已过时段为蓝色实柱，当前时段为品牌黄。
   原型按直播带货常见的日内曲线（午间、晚间两个高峰）把今日总量分摊到各时段。 */
const SLOTS = 48;

const clock = (m: number) => `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;

function DayRhythm({ className }: { total: number; className?: string }) {
  const [hover, setHover] = useState<number | null>(null);
  const d = new Date();
  const minutes = d.getHours() * 60 + d.getMinutes();
  const cur = Math.min(SLOTS - 1, Math.floor(minutes / 30));
  const { today, yesterday } = useSim((s) => s.rhythm);
  const peak = Math.max(1, ...today, ...yesterday);
  const empty = peak <= 1 && today.every((v) => !v) && yesterday.every((v) => !v);
  // 截至当前时段，与昨天同一时间段累计比较
  const todaySoFar = today.slice(0, cur + 1).reduce((a, v) => a + v, 0);
  const yesterdaySoFar = yesterday.slice(0, cur + 1).reduce((a, v) => a + v, 0);
  const change = yesterdaySoFar > 0 ? ((todaySoFar - yesterdaySoFar) / yesterdaySoFar) * 100 : null;
  const shown = hover;
  return (
    <div className={cn("flex flex-col", className)}>
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <span className="text-xs font-medium">今日消息量 · 每半小时</span>
        <span className="num text-[11px] text-muted-foreground">
          {shown !== null ? (
            <>
              {clock(shown * 30)}–{clock(shown * 30 + 30)} · 今天{" "}
              <span className="font-medium text-foreground">{shown <= cur ? fmtNum(today[shown]) : "—"}</span> · 昨天{" "}
              <span className="font-medium text-foreground">{fmtNum(yesterday[shown])}</span>
            </>
          ) : change === null ? (
            "昨天同期没有数据"
          ) : (
            <>
              比昨天同一时间{" "}
              <span className={cn("font-medium", change >= 0 ? "text-ink-ok" : "text-ink-warn")}>
                {change >= 0 ? "+" : ""}
                {change.toFixed(0)}%
              </span>
            </>
          )}
        </span>
      </div>
      <div className="mb-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-[2px] bg-graph" />今天</span>
        <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-[2px] bg-bar" />昨天同时段</span>
        <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-[2px] bg-primary" />当前半小时</span>
        <span className="num ml-auto text-faint">{empty ? "" : `顶部虚线 = 峰值 ${fmtNum(peak)} 条 · 空白时段未采集`}</span>
      </div>
      <div className="relative flex min-h-0 flex-1 items-end gap-[3px]" onPointerLeave={() => setHover(null)}>
        {!empty && (
          <div className="pointer-events-none absolute inset-x-0 top-0 border-t border-dashed border-border" />
        )}
        {empty && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center text-xs text-muted-foreground">
            今天还没有消息记录，开始采集后每 30 分钟累计一格
          </div>
        )}
        {today.map((v, i) => (
          <div key={i} className="relative h-full min-w-0 flex-1 overflow-hidden" onPointerEnter={() => setHover(i)}>
            {/* 柱高用平移表示（整根柱子下沉），只需合成，不触发重排 */}
            <span
              className={cn("t-move absolute inset-0 rounded-[3px] bg-bar", hover === i && "bg-border-strong")}
              style={{ transform: `translateY(${yesterday[i] > 0 ? 100 - Math.max(2, (yesterday[i] / peak) * 100) : 100}%)` }}
            />
            {i <= cur && (
              <span
                className={cn("t-move t-tone absolute inset-0 rounded-[3px]", i === cur ? "bg-primary" : hover === i ? "bg-foreground/60" : "bg-graph")}
                style={{ transform: `translateY(${v > 0 ? 100 - Math.max(3, (v / peak) * 100) : 100}%)` }}
              />
            )}
          </div>
        ))}
      </div>
      <div className="num mt-1.5 flex justify-between text-[10px] text-faint">
        <span>00:00</span>
        <span>06:00</span>
        <span>12:00</span>
        <span>18:00</span>
        <span>24:00</span>
      </div>
    </div>
  );
}

function HeroStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-muted-foreground">{label}</div>
      <div className="num mt-1 truncate text-sm font-medium">{value}</div>
    </div>
  );
}

/* ---------------- 数量卡片 ---------------- */

function CountCard({ kind }: { kind: "collecting" | "waiting" | "abnormal" }) {
  const rooms = useSim((s) => s.rooms);
  const data = useMemo(() => {
    const by = (s: RoomStatus[]) => rooms.filter((r) => s.includes(r.status));
    if (kind === "collecting") {
      const list = by(["collecting"]);
      const online = list.reduce((a, r) => a + r.online, 0);
      return {
        label: "采集中",
        icon: Waves,
        tone: "ok" as const,
        n: list.length,
        foot: (
          <>
            在线观众 <RollingNumber className="num font-medium text-foreground" text={fmtNum(online)} />
          </>
        ),
        filter: "collecting",
      };
    }
    if (kind === "waiting") {
      const list = by(PRE_LIVE);
      return {
        label: "等待开播",
        icon: MoonStar,
        tone: "member" as const,
        n: list.length,
        foot: (
          <>
            按配置轮询 · 等待越久，间隔逐步放宽
          </>
        ),
        filter: "waiting",
      };
    }
    const list = rooms.filter((r) => isAbnormal(r.status));
    const rc = list.filter((r) => r.status === "reconnecting").length;
    const rk = list.filter((r) => r.status === "risk").length;
    const nf = list.filter((r) => r.status === "notfound").length;
    return {
      label: "异常",
      icon: ShieldAlert,
      tone: "warn" as const,
      n: list.length,
      foot: (
        <span className="flex gap-2">
          <span className="text-ink-chat">重连 {rc}</span>
          <span className="text-ink-warn">风控 {rk}</span>
          <span className="text-ink-err">无效 {nf}</span>
        </span>
      ),
      filter: "abnormal",
    };
  }, [rooms, kind]);

  return (
    <Card interactive className="group h-full cursor-pointer p-3 sm:p-5" onClick={() => navigate("rooms", { status: data.filter })}>
      <div className="flex items-start justify-between">
        <IconTile icon={data.icon} tone={data.tone} accent />
        <ArrowUpRight className="hidden size-4 text-faint transition-transform sm:block group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground" />
      </div>
      <div className="mt-3 text-xs text-muted-foreground sm:mt-4">{data.label}</div>
      <Num animate value={data.n} className="mt-1 text-[26px] leading-none font-semibold sm:text-[32px]" format={(n) => String(n)} />
      <div className="mt-3 hidden truncate text-[11px] text-muted-foreground sm:block">{data.foot}</div>
    </Card>
  );
}

/* ---------------- 容量 ---------------- */

function CapacityCard() {
  const rooms = useSim((s) => s.rooms);
  const node = useSim((s) => s.node);
  const active = rooms.filter((r) => isActive(r.status));
  const groups: { status: RoomStatus; n: number }[] = (["collecting", "reconnecting", "connecting", "probing", "waiting", "risk"] as RoomStatus[])
    .map((s) => ({ status: s, n: active.filter((r) => r.status === s).length }))
    .filter((g) => g.n > 0);
  const pct = (active.length / node.maxRooms) * 100;
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader icon={HardDrive} tone="info" title="节点容量" desc="未停止的任务都计入容量" />
      <div className="mt-4 flex items-baseline gap-1.5">
        <Num animate value={active.length} className="text-[28px] leading-none font-semibold" format={String} />
        <span className="num text-sm text-muted-foreground">/ {node.maxRooms}</span>
        <span className="num ml-auto text-xs text-muted-foreground"><RollingNumber text={pct.toFixed(1)} />%</span>
      </div>
      {/* 分段条：按状态着色，放大显示 */}
      <div className="mt-3 flex h-2.5 gap-[2px] overflow-hidden rounded-full bg-muted">
        {groups.map((g) => (
          <div
            key={g.status}
            className={cn("t-flex h-full first:rounded-l-full last:rounded-r-full", TONE[STATUS_META[g.status].tone].fill)}
            style={{ width: `${Math.max(2, (g.n / active.length) * 100)}%` }}
            title={`${STATUS_META[g.status].label} ${g.n}`}
          />
        ))}
      </div>
      <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        {groups.map((g) => (
          <span key={g.status} className="inline-flex items-center gap-1">
            <span className={cn("size-1.5 rounded-full", TONE[STATUS_META[g.status].tone].fill)} />
            {STATUS_META[g.status].label}
            <span className="num text-foreground">{g.n}</span>
          </span>
        ))}
      </div>
    </Card>
  );
}

/* ---------------- 请求预算（设计文档 5.5） ---------------- */

function BudgetCard() {
  const node = useSim((s) => s.node);
  const cap = node.reqBudgetPerSec * 60;
  const hist = node.reqHist?.slice(-24) ?? [];
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader icon={GaugeIcon} tone="social" title="发往抖音的请求" desc={`节点令牌桶 ${node.reqBudgetPerSec} 次/秒`} />
      <div className="mt-4 flex items-baseline gap-1.5">
        <Num animate value={node.reqPerMin} className="text-[28px] leading-none font-semibold" format={String} />
        <span className="text-sm text-muted-foreground">次/分钟</span>
        <span className="num ml-auto text-xs text-muted-foreground">上限 {cap}</span>
      </div>
      <div className="mt-3 flex h-10 items-end gap-[3px]">
        {hist.map((v, i) => (
          <span
            key={i}
            className={cn("flex-1 rounded-t-[3px]", i === hist.length - 1 ? "bg-social" : "bg-social/25")}
            style={{ height: `${Math.min(100, (v / cap) * 100)}%` }}
          />
        ))}
      </div>
      <div className="mt-2 text-[11px] text-muted-foreground">
        已用预算 <span className="num text-foreground"><RollingNumber text={((node.reqPerMin / cap) * 100).toFixed(0)} />%</span> · 开播轮询已自适应放宽
      </div>
      <p className="mt-2 text-xs text-muted-foreground">首页请求 <span className="num text-foreground">{node.homepageRequestsPerMinute ?? 0}</span> 次/分钟 · 游客身份{node.shareGuestIdentity ? "共享" : "独立获取"}</p>
    </Card>
  );
}

/* ---------------- 实时消息速率 ---------------- */

function RateCard() {
  const series = useSim((s) => s.series);
  const [range, setRange] = useState("live");
  // 分钟历史不随推送下发，只在查看 1 小时 / 24 小时时获取
  const history = useHistory(range !== "live");
  const [focus, setFocus] = useState<string | null>(null);
  const data = useMemo(() => {
    if (range === "live") return series.map((p) => ({ at: p.at, v: p.v as Record<string, number> }));
    const step = range === "1h" ? 60_000 : 30 * 60_000;
    const start = Date.now() - (range === "1h" ? 3600_000 : 86400_000);
    const buckets = new Map<number, Record<string, number>>();
    for (const point of history) { if (point.at < start) continue; const at = Math.floor(point.at / step) * step; const v = buckets.get(at) ?? Object.fromEntries(MSG_TYPES.map((t) => [t, 0])); for (const t of MSG_TYPES) v[t] += point.v[t] / (step / 60_000); buckets.set(at, v); }
    return [...buckets].map(([at, v]) => ({ at, v }));
  }, [series, history, range]);
  const last = series[series.length - 1].v;
  const total = MSG_TYPES.reduce((a, t) => a + last[t], 0);

  return (
    <Card className="flex h-full flex-col p-4 sm:p-5">
      <CardHeader
        icon={Activity}
        tone="member"
        title="实时消息速率"
        desc="全节点，按消息类型堆叠"
        action={
          <Tabs value={range} onValueChange={setRange} variant="pill">
            <TabsList className="border bg-background/60" wrapperClassName="w-auto">
              <TabsTrigger value="live" className="px-3 py-1 text-xs">实时</TabsTrigger>
              <TabsTrigger value="1h" className="px-3 py-1 text-xs">1 小时</TabsTrigger>
              <TabsTrigger value="24h" className="px-3 py-1 text-xs">24 小时</TabsTrigger>
            </TabsList>
          </Tabs>
        }
      />
      <div className="mt-4 flex flex-wrap items-end gap-x-6 gap-y-2">
        <div>
          <Num animate value={total} className="text-[28px] leading-none font-semibold" suffix="条/分钟" />
        </div>
        <Legend className="pb-1" items={SERIES.map((s) => ({ key: s.key, label: s.label, tone: s.tone }))} onHover={setFocus} />
      </div>
      <StackedArea
        className="mt-3 min-h-0 flex-1"
        fill
        series={SERIES}
        highlight={focus}
        data={data}
        stream={range === "live"}
        slots={range === "live" ? 72 : undefined}
        height={292}
        formatX={(at) => (range === "24h" ? fmtClock(at, false) : range === "1h" ? fmtClock(at, false) : fmtClock(at))}
      />
    </Card>
  );
}

/* ---------------- 消息类型构成 ---------------- */

function CompositionCard() {
  const totals = useSim((s) => s.totals);
  const sum = MSG_TYPES.reduce((a, t) => a + totals[t], 0);
  const segments = STACK_ORDER.map((t) => ({ key: t, label: TYPE_META[t].label, tone: TYPE_META[t].tone, value: totals[t] }));
  return (
    <Card className="flex h-full flex-col p-4 sm:p-5">
      <CardHeader icon={PieChart} tone="like" title="消息类型构成" desc="今日累计，悬停查看占比" />
      <div className="mt-4 flex flex-1 flex-col items-center justify-center gap-5 sm:flex-row xl:flex-col 2xl:flex-row">
        <Donut
          segments={segments}
          size={168}
          thickness={16}
          center={(a) =>
            a ? (
              <>
                <span className="text-xs text-muted-foreground">{a.label}</span>
                <span className="num text-2xl font-semibold">{(a.pct * 100).toFixed(1)}%</span>
                <span className="num text-[11px] text-muted-foreground">{fmtNum(a.value)}</span>
              </>
            ) : (
              <>
                <span className="text-xs text-muted-foreground">今日合计</span>
                <Num animate value={sum} className="text-2xl font-semibold" />
              </>
            )
          }
        />
        {/* 每类一行：数量、占比，下方一条占比条（与圆环同色），一眼看出谁占大头 */}
        <div className="w-full flex-1 space-y-3">
          {STACK_ORDER.map((t) => {
            const m = TYPE_META[t];
            const Icon = m.icon;
            const pct = sum ? (totals[t] / sum) * 100 : 0;
            return (
              <div key={t}>
                <div className="flex items-center gap-2.5 text-sm">
                  <span className={cn("inline-flex size-6 items-center justify-center rounded-lg", TONE[m.tone].tint)}>
                    <Icon className="size-3.5" />
                  </span>
                  <span className="text-muted-foreground">{m.label}</span>
                  <RollingNumber className="num ml-auto font-medium" text={fmtNum(totals[t])} />
                  <span className="num w-12 text-right text-xs text-muted-foreground"><RollingNumber text={pct.toFixed(1)} />%</span>
                </div>
                <div className="mt-1.5 ml-[34px] h-1 overflow-hidden rounded-full bg-muted">
                  <div className="t-move h-full w-full rounded-full" style={{ background: `var(--c-${m.tone})`, transform: `translateX(${pct - 100}%)` }} />
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </Card>
  );
}

/* ---------------- 风控雷达 ---------------- */

function RadarCard() {
  const rooms = useSim((s) => s.rooms);
  const incidents = useSim((s) => s.incidents);
  const abnormal = rooms.filter((r) => isAbnormal(r.status));
  const blips = abnormal.map((r, i) => ({
    key: r.liveId,
    angle: (Number(r.liveId.slice(-3)) * 37 + i * 80) % 360,
    dist: 0.35 + ((Number(r.liveId.slice(-2)) % 60) / 100),
    tone: STATUS_META[r.status].tone,
  }));
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader
        icon={Radar}
        tone="warn"
        title="风控雷达"
        desc="最近的异常直播间"
        action={
          <button type="button" onClick={() => navigate("rooms",{status:"abnormal"})} className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
            异常房间
          </button>
        }
      />
      <div className="mt-4 flex items-center gap-4">
        <RadarScope blips={blips} size={124} />
        <div className="min-w-0 flex-1 space-y-2.5">
          <div>
            <div className="text-xs text-muted-foreground">当前异常占比</div>
            <div className="num mt-0.5 text-2xl font-semibold">
              <RollingNumber text={((abnormal.length / Math.max(1, rooms.length)) * 100).toFixed(0)} />%
              <span className="ml-1.5 text-xs font-normal text-muted-foreground">{abnormal.length ? "请检查异常任务" : "暂无异常"}</span>
            </div>
          </div>
          <div className="text-[11px] text-muted-foreground">
            当前异常 <RollingNumber className={cn("num font-medium t-tone", abnormal.length ? "text-ink-err" : "text-foreground")} text={String(abnormal.length)} /> 间 · 未处理告警{" "}
            <span className="num font-medium text-foreground">{incidents.length}</span> 条
          </div>
        </div>
      </div>
      {/* 列表区固定三行高度：有无异常、异常多少都不改变卡片高度 */}
      <div className="mt-4 min-h-[120px] space-y-1">
        {!abnormal.length && (
          <div className="flex h-[120px] flex-col items-center justify-center gap-2 rounded-2xl border border-dashed text-center">
            <IconTile icon={ShieldCheck} tone="ok" size={32} />
            <div className="text-sm">所有直播间运行正常</div>
            <div className="text-[11px] text-muted-foreground">出现重连、状态待确认或失效时会在这里列出</div>
          </div>
        )}
        {abnormal.slice(0, 3).map((r) => (
          <button
            key={r.liveId}
            type="button"
            onClick={() => navigate("rooms", { room: r.liveId })}
            className="flex w-full cursor-pointer items-center gap-2.5 rounded-xl px-2 py-1.5 text-left transition-colors hover:bg-accent"
          >
            <Avatar name={r.name} src={r.avatar} seed={r.liveId} size={28} dim={r.status === "notfound"} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm">{r.name}</div>
              <div className="truncate text-[11px] text-muted-foreground">{r.note ?? r.events[0]?.text ?? STATUS_META[r.status].hint}</div>
            </div>
            <StatusPill status={r.status} size="sm" />
          </button>
        ))}
      </div>
    </Card>
  );
}

/* ---------------- 资源 ---------------- */

function ResourceCard() {
  const node = useSim((s) => s.node);
  const rooms = useSim((s) => s.rooms);
  const activeRooms = rooms.filter((r) => isActive(r.status)).length;
  const down = fmtBytesPerSec(node.downKBs);
  const up = fmtBytesPerSec(node.upKBs);
  const upHist = node.upHist ?? [];
  // 带宽没有固定上限：进度条按最近峰值放缩，留两成余量。
  const scale = (hist: number[], floor: number) => Math.max(floor, ...hist) * 1.2;
  const rows = [
    { icon: Cpu, tone: "info" as const, label: "CPU", value: `${node.cpu.toFixed(1)}`, unit: "%", meter: node.cpu, max: 100, hist: node.cpuHist },
    { icon: MemoryStick, tone: "social" as const, label: "进程内存", value: `${Math.round(node.memMB)}`, unit: `MB / ${(node.memLimitMB / 1024).toFixed(1)}GB 主机`, meter: node.memMB, max: node.memLimitMB, hist: node.memHist },
    { icon: Network, tone: "member" as const, label: "下行带宽", value: node.bandwidthAvailable ? down.value : "—", unit: node.bandwidthAvailable ? down.unit : "尚未计量", meter: node.downKBs, max: scale(node.netHist, 64), hist: node.netHist },
    { icon: ArrowUpRight, tone: "gift" as const, label: "上行带宽", value: node.bandwidthAvailable ? up.value : "—", unit: node.bandwidthAvailable ? up.unit : "尚未计量", meter: node.upKBs, max: scale(upHist, 64), hist: upHist },
  ];
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader icon={HardDrive} tone="ok" title="资源" desc={`协程 ${node.goroutines} · 连接 ${node.conns}`} />
      <div className="mt-4 space-y-4">
        {rows.map((r) => (
          <div key={r.label} className="flex items-center gap-3">
            <IconTile icon={r.icon} tone={r.tone} size={32} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline gap-1">
                <span className="text-xs text-muted-foreground">{r.label}</span>
                <RollingNumber className="num ml-auto text-sm font-semibold" text={r.value} />
                <span className="text-[11px] text-muted-foreground">{r.unit}</span>
              </div>
              <Meter value={r.meter} max={r.max} tone={r.tone} className="mt-1.5" />
            </div>
            <Sparkline animate data={r.hist} tone={r.tone} height={30} className="w-16 shrink-0 sm:w-20" strokeWidth={1.5} />
          </div>
        ))}
      </div>
      <div className="mt-5 grid grid-cols-3 gap-2 border-t pt-4 text-center">
        {[
          ["协程", String(node.goroutines)],
          ["连接", String(node.conns)],
          ["活跃任务", String(activeRooms)],
        ].map(([k, v]) => (
          <div key={k} className="rounded-xl bg-muted/60 py-2">
            <RollingNumber className="num justify-center text-sm font-semibold" text={v} />
            <div className="text-[10px] text-muted-foreground">{k}</div>
          </div>
        ))}
      </div>
    </Card>
  );
}

/* ---------------- 最近事件 ---------------- */

function TimelineCard() {
  const timeline = useSim((s) => s.timeline);
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader icon={History} tone="info" title="最近事件" desc="状态变化与运维操作" />
      <ol className="relative mt-4 space-y-3.5 before:absolute before:top-2 before:bottom-2 before:left-[5px] before:w-px before:bg-border">
        {timeline.slice(0, 5).map((t) => (
          <li key={t.id} className="relative flex gap-3 pl-0">
            <span className={cn("relative mt-1.5 size-[11px] shrink-0 rounded-full ring-4 ring-card", TONE[t.tone].fill)} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline gap-2">
                <span className="truncate text-sm font-medium">{t.title}</span>
                <span className="ml-auto shrink-0 text-[11px] text-faint">{fmtAgo(t.at)}</span>
              </div>
              <p className="truncate text-xs text-muted-foreground">{t.detail}</p>
            </div>
          </li>
        ))}
      </ol>
    </Card>
  );
}

/* ---------------- 最活跃直播间 ---------------- */

function TopRoomsCard() {
  const rooms = useSim((s) => s.rooms);
  const top = useMemo(
    // 按在线人数排：比每分钟消息数稳定得多，名次不会每次刷新都乱跳
    () => [...rooms].filter((r) => isLive(r.status)).sort((a, b) => b.online - a.online || b.msgPerMin - a.msgPerMin).slice(0, 5),
    [rooms],
  );
  const max = Math.max(1, ...top.map((r) => r.online));
  const order = top.map((r) => r.liveId);
  const risen = useRankRise(order);
  const listRef = useRankSwap(order.join(","));
  return (
    <Card className="h-full p-4 sm:p-5">
      <CardHeader
        icon={Trophy}
        tone="gift"
        title="人气最高的直播间"
        desc="按在线人数排序，名次变化时位置互换"
        action={
          <button type="button" onClick={() => navigate("rooms")} className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
            全部直播间
          </button>
        }
      />
      {/* 名次变化时行互换位置：反超的一行从下往上顶上去。做法是 FLIP——记下旧位置，换序后先反向平移回旧位置，
          再用 CSS 过渡滑到新位置。只在名次变化时测量一次，滑动交给合成线程 */}
      <div ref={listRef} className="relative mt-3">
        {top.map((r, i) => (
          <button
            key={r.liveId}
            data-row={r.liveId}
            type="button"
            onClick={() => navigate("rooms", { room: r.liveId })}
            className={cn("rank-row relative flex w-full cursor-pointer items-center gap-3 rounded-xl bg-card py-2.5 text-left", i > 0 && "border-t", risen.has(r.liveId) && "z-[1]")}
          >
            <span
              className={cn(
                "t-tone num inline-flex size-6 shrink-0 items-center justify-center rounded-lg text-xs font-semibold",
                i === 0 ? "bg-primary text-primary-foreground" : i < 3 ? "tint-chat" : "bg-muted text-muted-foreground",
              )}
            >
              {i + 1}
            </span>
            <Avatar name={r.name} src={r.avatar} seed={r.liveId} size={34} live />
            <div className="w-28 min-w-0 sm:w-44">
              <div className="flex items-center gap-1.5">
                <span className="truncate text-sm font-medium">{r.name}</span>
                {risen.has(r.liveId) && (
                  <span className="rank-up inline-flex shrink-0 items-center rounded-full tint-ok px-1.5 text-[10px] font-medium">
                    <ArrowUp className="size-3" />
                    上升
                  </span>
                )}
              </div>
              <div className="num truncate text-[11px] text-muted-foreground">{r.liveId}</div>
            </div>
            <div className="hidden flex-1 sm:block">
              <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                <div
                  className={cn("t-move t-tone h-full w-full rounded-full", i === 0 ? "bg-primary" : "bg-graph")}
                  style={{ transform: `translateX(${(r.online / max) * 100 - 100}%)` }}
                />
              </div>
            </div>
            <div className="ml-auto w-20 text-right">
              <Num animate value={r.online} className="text-sm font-semibold" />
              <div className="text-[11px] text-muted-foreground">在线</div>
            </div>
            <div className="hidden w-20 text-right md:block">
              <Num animate value={r.msgPerMin} className="text-sm font-medium text-muted-foreground" />
              <div className="text-[11px] text-muted-foreground">条/分</div>
            </div>
          </button>
        ))}
      </div>
    </Card>
  );
}

/** 名次变化时的互换动画（FLIP）：新出现的行淡入，已有的行从旧位置滑到新位置 */
function useRankSwap(key: string) {
  const ref = useRef<HTMLDivElement>(null);
  const tops = useRef(new Map<string, number>());
  useLayoutEffect(() => {
    const list = ref.current;
    if (!list) return;
    const rows = [...list.querySelectorAll<HTMLElement>("[data-row]")];
    const next = new Map(rows.map((el) => [el.dataset.row!, el.offsetTop]));
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (tops.current.size && !reduce) {
      const moved: HTMLElement[] = [];
      for (const el of rows) {
        const before = tops.current.get(el.dataset.row!);
        const now = next.get(el.dataset.row!)!;
        if (before === undefined) {
          el.classList.remove("rank-enter");
          void el.offsetWidth;
          el.classList.add("rank-enter");
        } else if (before !== now) {
          el.style.transition = "none";
          el.style.transform = `translateY(${before - now}px)`;
          moved.push(el);
        }
      }
      if (moved.length) {
        void list.offsetHeight;
        for (const el of moved) {
          el.style.transition = "transform 600ms cubic-bezier(0.22, 1, 0.36, 1)";
          el.style.transform = "";
        }
      }
    }
    tops.current = next;
  }, [key]);
  return ref;
}

/** 本次推送中名次上升的直播间（与上一次相比），标记保留 3 秒 */
function useRankRise(ids: string[]) {
  const prev = useRef<string[]>([]);
  const [risen, setRisen] = useState<Set<string>>(() => new Set());
  const key = ids.join(",");
  useEffect(() => {
    const before = prev.current;
    prev.current = ids;
    if (!before.length) return;
    const up = ids.filter((id, i) => {
      const old = before.indexOf(id);
      return old === -1 ? false : i < old;
    });
    if (!up.length) return;
    setRisen(new Set(up));
    const t = window.setTimeout(() => setRisen(new Set()), 3000);
    return () => window.clearTimeout(t);
    // ids 的内容由 key 表示
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return risen;
}

/* ---------------- 接入方推送 ---------------- */

/** 中位数 / 95% 分位，统一单位：都不到 1 秒用毫秒，否则用秒 */
function lagPair(p50: number, p95: number) {
  return p95 < 1000 ? `${p50} / ${p95} 毫秒` : `${(p50 / 1000).toFixed(1)} / ${(p95 / 1000).toFixed(1)} 秒`;
}

/** 消息延迟：抖音发出到本节点收到、本节点收到到推给接入方（最近 1 分钟） */
function LatencyStrip({ latency }: { latency?: { upstream?: LagStats; push?: LagStats } }) {
  const up = latency?.upstream;
  const push = latency?.push;
  return (
    <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
      <div className="rounded-xl border px-3 py-2" title="抖音消息自带时间到本节点收到；中位数 / 95% 的消息不超过">
        <div className="text-muted-foreground">抖音 → 本节点</div>
        <div className="num mt-0.5 truncate font-medium">{up?.samples ? `${up.precise ? "" : "约 "}${lagPair(up.p50_ms, up.p95_ms)}` : "—"}</div>
      </div>
      <div className="rounded-xl border px-3 py-2" title="本节点收到到成功推送给接入方；中位数 / 95% 的消息不超过">
        <div className="text-muted-foreground">本节点 → 接入方</div>
        <div className="num mt-0.5 truncate font-medium">{push?.samples ? lagPair(push.p50_ms, push.p95_ms) : "暂无推送"}</div>
      </div>
    </div>
  );
}

function ClientsCard() {
  const clients = useSim((s) => s.clients);
  const traffic = useSim((s) => s.node.northbound);
  const latency = useSim((s) => s.node.latency);
  const drops = useSim((s) => s.node.entryDrops);
  return (
    <Card className="flex h-full flex-col p-4 sm:p-5">
      <CardHeader
        icon={KeyRound}
        tone="gift"
        title="接入方推送"
        desc="北向推送 · 最近 60 秒平均速率"
        action={
          <button type="button" onClick={() => navigate("clients")} className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
            管理
          </button>
        }
      />
      <LatencyStrip latency={latency} />
      <div className="mt-2 rounded-xl tint-info p-3 text-xs">节点出站 <span className="num font-semibold">{((traffic?.bytes_per_sec ?? 0) / 1024).toFixed(1)} KB/s</span> · 推送 <span className="num">{(traffic?.push_per_sec ?? 0).toFixed(1)} 条/秒</span>{!!drops?.total && <span className="text-ink-warn" title={describeDrops(drops)}> · 入口丢弃 <span className="num">{drops.total}</span></span>}</div>
      {!clients.length && (
        <div className="mt-4 flex flex-1 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed p-5 text-center">
          <IconTile icon={KeyRound} tone="gift" size={36} />
          <div className="text-sm font-medium">还没有接入方</div>
          <div className="max-w-[240px] text-xs text-muted-foreground">创建 API Key 后，业务系统可以按直播间订阅实时消息</div>
          <button type="button" onClick={() => navigate("clients")} className="mt-1 inline-flex cursor-pointer items-center gap-1 rounded-full tint-gift px-3 py-1.5 text-xs font-medium">
            去创建 API Key <ArrowUpRight className="size-3.5" />
          </button>
        </div>
      )}
      <div className={cn("space-y-3", clients.length && "mt-4")}>
        {clients.map((c) => (
          <div key={c.id} className="rounded-2xl border bg-background/40 p-3">
            <div className="flex items-center gap-2">
              <span className={cn("size-2 rounded-full", c.connections > 0 ? "bg-ok" : "bg-idle")} />
              <span className="truncate text-sm font-medium">{c.name}</span>
              <Tag tone={c.env === "生产" ? "chat" : c.env === "预发" ? "info" : "idle"} className="ml-auto">
                {c.env}
              </Tag>
            </div>
            <div className="mt-2 flex items-end gap-3">
              <div>
                <Num animate value={c.pushPerSec} className="text-lg font-semibold" suffix="条/秒" />
              </div>
            </div>
            <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-muted-foreground">
              <span>连接 {c.connections}</span>
              <span>订阅 {c.used}</span>
              <span>队列 {c.queue}</span>
              <span className={c.dropped ? "text-ink-warn" : ""}>丢弃 {c.dropped}</span>
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
