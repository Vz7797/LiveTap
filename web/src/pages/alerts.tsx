import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertOctagon,
  BellRing,
  CheckCheck,
  ChevronDown,
  Download,
  FileText,
  Info,
  Pause,
  Play,
  Save,
  ScrollText,
  Settings2,
  ShieldAlert,
  Sparkles,
  Trash2,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { api, queryClient } from "@/data/api";
import { notifyError } from "@/data/runtime";
import { useToast } from "@/app/toast";
import { navigate, setParams, useRoute } from "@/app/router";
import { Card, CardHeader, IconTile, Num, PageHeader, PillButton, Tag } from "@/components/kit";
import { SearchInput, Segmented, TextInput } from "@/components/kit/form";
import { SelectBox } from "@/components/kit/select";
import { ConfirmDialog } from "@/components/kit/confirm";
import { Switch } from "@/components/motion/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { fmtAgo, fmtClock } from "@/lib/format";
import type { Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";

interface Alert { id: number; at: number; rule: string; level: string; live_id: string; detail: string; acked: boolean }
interface Summary { open: Record<string, number>; today: number; total: number; top_rules: { rule: string; count: number }[]; oldest: number }
interface Log { at: number; level: string; live_id?: string; message: string; fields: Record<string, unknown> }
interface LogStats { files: { name: string; size: number; modified: number }[]; total_bytes: number; max_bytes: number; backups: number; buffered: number; by_level: Record<string, number> }
interface Rules { enabled: Record<string, boolean>; mass_percent: number; capacity_percent: number; webhook: string; webhook_configured: boolean }

const RULES: Record<string, { name: string; desc: string; icon: LucideIcon }> = {
  mass_failure: { name: "大面积失败", desc: "5 分钟内失败的直播间占比超过阈值", icon: AlertOctagon },
  silence: { name: "静默异常", desc: "旁证连接确认主连接收不到消息", icon: ScrollText },
  risk: { name: "静默重建达到上限", desc: "30 分钟内自动重建 3 次后停止重建", icon: ShieldAlert },
  capacity: { name: "容量将满", desc: "启用任务数超过容量阈值", icon: TriangleAlert },
  proxy_invalid: { name: "代理失效", desc: "代理检测失败并被自动摘除", icon: Info },
  account_invalid: { name: "小号失效", desc: "小号登录态失效并被自动摘除", icon: Info },
  node_cooldown: { name: "节点受限冷却", desc: "上游明确受限，暂停新的探测与建连", icon: ShieldAlert },
};
const ruleName = (rule: string) => RULES[rule]?.name ?? rule;
const LEVELS: Record<string, { name: string; tone: Tone; icon: LucideIcon }> = {
  critical: { name: "严重", tone: "err", icon: AlertOctagon },
  warning: { name: "警告", tone: "warn", icon: TriangleAlert },
  info: { name: "提醒", tone: "info", icon: Info },
};
const level = (l: string) => LEVELS[l] ?? { name: l, tone: "idle" as Tone, icon: Info };

export function AlertsPage() {
  const { params } = useRoute();
  const rawTab = params.get("tab");
  const tab = rawTab === "rules" || rawTab === "logs" ? rawTab : "alerts";
  const summary = useSummary();
  const open = Object.values(summary?.open ?? {}).reduce((a, b) => a + (Number(b) || 0), 0);
  return (
    <>
      <PageHeader icon={BellRing} tone="warn" title="告警与日志" desc="告警持久保存并按“事件与审计保留天数”自动清理；日志脱敏写入文件，按大小轮转" />
      <Segmented
        value={tab}
        onChange={(value) => setParams({ tab: value === "alerts" ? undefined : value })}
        options={[
          { value: "alerts", label: open ? `告警 · ${open}` : "告警" },
          { value: "logs", label: "实时日志" },
          { value: "rules", label: "规则与通知" },
        ]}
      />
      <div className="mt-5">{tab === "rules" ? <RulesPanel /> : tab === "logs" ? <LogsPanel /> : <AlertsPanel summary={summary} />}</div>
    </>
  );
}

function useSummary() {
  const q = useQuery({ queryKey: ["alerts-summary"], queryFn: () => api<Summary>("/alerts/summary"), refetchInterval: 10_000 });
  return q.data;
}
const refreshAlerts = () => Promise.all([queryClient.invalidateQueries({ queryKey: ["alerts"] }), queryClient.invalidateQueries({ queryKey: ["alerts-summary"] })]);

/* ---------------- 告警 ---------------- */

/** 相邻且规则、级别、直播间、确认状态都相同的告警合并成一组，避免同一问题刷屏 */
interface Group { key: string; items: Alert[]; first: Alert }
function groupAlerts(list: Alert[]): Group[] {
  const out: Group[] = [];
  for (const a of list) {
    const last = out[out.length - 1];
    const same = last && last.first.rule === a.rule && last.first.level === a.level && last.first.live_id === a.live_id && last.first.acked === a.acked && dayKey(last.first.at) === dayKey(a.at);
    if (same) last.items.push(a);
    else out.push({ key: String(a.id), items: [a], first: a });
  }
  return out;
}
function dayKey(ts: number) {
  const d = new Date(ts);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}
function dayLabel(ts: number) {
  const d = new Date(ts);
  const today = new Date();
  const yesterday = new Date(Date.now() - 86_400_000);
  if (dayKey(ts) === dayKey(today.getTime())) return "今天";
  if (dayKey(ts) === dayKey(yesterday.getTime())) return "昨天";
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

function AlertsPanel({ summary }: { summary?: Summary }) {
  const toast = useToast();
  const [status, setStatus] = useState("0");
  const [lv, setLv] = useState("");
  const [rule, setRule] = useState("");
  const [liveID, setLiveID] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirm, setConfirm] = useState<null | { kind: "ack-all" } | { kind: "purge"; mode: "acked" | "before"; days: number }>(null);
  const filter = { level: lv, rule, live_id: liveID.trim() };
  const query = useQuery({
    queryKey: ["alerts", status, lv, rule, liveID.trim()],
    queryFn: () => api<Alert[]>(`/alerts?${new URLSearchParams({ ...filter, acked: status, limit: "300" })}`),
    refetchInterval: 5000,
  });
  const groups = useMemo(() => groupAlerts(Array.isArray(query.data) ? query.data : []), [query.data]);
  const openGroups = groups.filter((g) => !g.first.acked);
  const selectedIds = groups.filter((g) => selected.has(g.key)).flatMap((g) => g.items.map((a) => a.id));

  const ack = async (ids: number[]) => {
    try {
      const r = await api<{ acked: number }>("/alerts/ack", "POST", { ids });
      setSelected(new Set());
      await refreshAlerts();
      toast.show({ title: `已确认 ${r.acked} 条告警`, status: "success" });
    } catch (e) { notifyError(e); }
  };

  const tiles: { label: string; value: number; tone: Tone; icon: LucideIcon; hint: string; onClick?: () => void }[] = [
    { label: "严重 · 未处理", value: summary?.open?.critical ?? 0, tone: "err", icon: AlertOctagon, hint: "需要立即处理", onClick: () => { setStatus("0"); setLv("critical"); } },
    { label: "警告 · 未处理", value: summary?.open?.warning ?? 0, tone: "warn", icon: TriangleAlert, hint: "建议尽快查看", onClick: () => { setStatus("0"); setLv("warning"); } },
    { label: "提醒 · 未处理", value: summary?.open?.info ?? 0, tone: "info", icon: Info, hint: "了解即可", onClick: () => { setStatus("0"); setLv("info"); } },
    { label: "今日新增", value: summary?.today ?? 0, tone: "chat", icon: Sparkles, hint: summary?.top_rules?.[0] ? `最多：${ruleName(summary.top_rules[0].rule)} ×${summary.top_rules[0].count}` : "最近 24 小时没有告警" },
  ];

  return (
    <>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {tiles.map((t) => (
          <button key={t.label} type="button" onClick={t.onClick} disabled={!t.onClick} className="text-left disabled:cursor-default">
            <Card className={cn("flex h-full items-start gap-3 p-4", t.onClick && "transition-colors hover:border-border-strong")}>
              <IconTile icon={t.icon} tone={t.value > 0 || !t.onClick ? t.tone : "idle"} size={36} />
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground">{t.label}</div>
                <Num animate value={t.value} format={String} className={cn("mt-0.5 text-2xl font-semibold", t.value > 0 && t.tone === "err" && "text-ink-err")} />
                <div className="mt-0.5 truncate text-[11px] text-faint" title={t.hint}>{t.hint}</div>
              </div>
            </Card>
          </button>
        ))}
      </div>

      <Card className="mt-4 overflow-hidden">
        <div className="flex flex-wrap items-center gap-2.5 border-b p-3 sm:p-4">
          <Segmented
            value={status}
            onChange={(v) => { setStatus(v); setSelected(new Set()); }}
            size="sm"
            options={[
              { value: "0", label: "未处理" },
              { value: "1", label: "已确认" },
              { value: "", label: "全部" },
            ]}
          />
          <SelectBox
            ariaLabel="级别"
            className="w-36"
            value={lv}
            onChange={setLv}
            options={[{ value: "", label: "全部级别" }, ...Object.entries(LEVELS).map(([value, l]) => ({ value, label: l.name, tone: l.tone }))]}
          />
          <SelectBox
            ariaLabel="规则"
            className="w-48"
            value={rule}
            onChange={setRule}
            options={[{ value: "", label: "全部规则" }, ...Object.entries(RULES).map(([value, r]) => ({ value, label: r.name, icon: r.icon }))]}
          />
          <SearchInput value={liveID} onChange={setLiveID} placeholder="直播间号" className="w-full sm:w-44" />
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {selectedIds.length > 0 ? (
              <PillButton tone="primary" icon={CheckCheck} onClick={() => void ack(selectedIds)}>
                确认所选 {selectedIds.length}
              </PillButton>
            ) : (
              <PillButton icon={CheckCheck} disabled={!openGroups.length} onClick={() => setConfirm({ kind: "ack-all" })}>
                全部确认
              </PillButton>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button type="button" className="inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-full px-4 text-sm font-medium tint-err">
                  <Trash2 className="size-4" /> 清理 <ChevronDown className="size-3.5" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuLabel className="text-xs text-muted-foreground">删除后不可恢复</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setConfirm({ kind: "purge", mode: "acked", days: 0 })}>清理全部已确认的告警</DropdownMenuItem>
                <DropdownMenuSeparator />
                {[7, 30].map((d) => (
                  <DropdownMenuItem key={d} onSelect={() => setConfirm({ kind: "purge", mode: "before", days: d })}>
                    清理 {d} 天前的告警
                  </DropdownMenuItem>
                ))}
                <DropdownMenuItem className="text-ink-err" onSelect={() => setConfirm({ kind: "purge", mode: "before", days: 0 })}>
                  清空全部告警
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {query.error && <p role="alert" className="p-4 text-sm text-ink-err">读取告警失败：{query.error.message}</p>}
        {!query.isPending && groups.length === 0 && (
          <div className="flex flex-col items-center gap-2 px-6 py-14 text-center">
            <IconTile icon={CheckCheck} tone="ok" size={44} />
            <div className="font-medium">{status === "0" ? "没有待处理的告警" : "当前筛选下没有告警"}</div>
            <div className="text-xs text-muted-foreground">告警按“事件与审计保留天数”自动清理，也可以在右上角手动清理</div>
          </div>
        )}
        {groups.length > 0 && (
          <div className="scroll-area max-h-[calc(100dvh-360px)] min-h-[240px] pb-2">
            <Timeline
              groups={groups}
              selected={selected}
              onCheck={(key, v) => setSelected((cur) => { const next = new Set(cur); if (v) next.add(key); else next.delete(key); return next; })}
              onAck={(g) => void ack(g.items.map((a) => a.id))}
            />
            {query.data && query.data.length >= 300 && <p className="px-5 py-3 text-center text-xs text-muted-foreground">只显示最近 300 条，可用上方条件缩小范围</p>}
          </div>
        )}
      </Card>

      <ConfirmDialog
        open={confirm?.kind === "ack-all"}
        title="确认当前筛选下的全部告警？"
        description={<>将把 <b className="text-foreground">{[lv && level(lv).name, rule && ruleName(rule), liveID && `直播间 ${liveID}`].filter(Boolean).join(" · ") || "全部"}</b> 范围内所有未处理的告警标记为已确认。</>}
        confirmLabel="全部确认"
        tone="chat"
        icon={CheckCheck}
        onClose={() => setConfirm(null)}
        onConfirm={async () => {
          const r = await api<{ acked: number }>("/alerts/ack", "POST", filter);
          await refreshAlerts();
          toast.show({ title: `已确认 ${r.acked} 条告警`, status: "success" });
        }}
      />
      <ConfirmDialog
        open={confirm?.kind === "purge"}
        title={confirm?.kind === "purge" ? (confirm.mode === "acked" ? "清理已确认的告警？" : confirm.days === 0 ? "清空全部告警？" : `清理 ${confirm.days} 天前的告警？`) : ""}
        description={confirm?.kind === "purge" && confirm.mode === "before" && confirm.days === 0 ? <>包括尚未处理的告警在内，共 <b className="text-foreground">{summary?.total ?? 0}</b> 条将被删除，且不可恢复。</> : "删除后不可恢复。未处理的告警只有在早于所选时间点时才会被删除。"}
        confirmLabel="确认清理"
        onClose={() => setConfirm(null)}
        onConfirm={async () => {
          if (confirm?.kind !== "purge") return;
          const r = await api<{ deleted: number }>("/alerts/purge", "POST", { mode: confirm.mode, days: confirm.days });
          setSelected(new Set());
          await refreshAlerts();
          toast.show({ title: `已清理 ${r.deleted} 条告警`, status: "success" });
        }}
      />
    </>
  );
}

/* 时间轴：左侧时间，中间竖线与按级别着色的节点，右侧告警卡片；按天分段，日期标签挂在线上 */
const RAIL = "grid grid-cols-[28px_minmax(0,1fr)] gap-3 sm:grid-cols-[76px_32px_minmax(0,1fr)] sm:gap-4";

function Timeline({ groups, selected, onCheck, onAck }: { groups: Group[]; selected: Set<string>; onCheck: (key: string, v: boolean) => void; onAck: (g: Group) => void }) {
  const days: { key: string; at: number; groups: Group[] }[] = [];
  for (const g of groups) {
    const k = dayKey(g.first.at);
    if (days[days.length - 1]?.key !== k) days.push({ key: k, at: g.first.at, groups: [] });
    days[days.length - 1].groups.push(g);
  }
  return (
    <div className="px-3 pt-4 sm:px-5">
      {days.map((day, di) => (
        <section key={day.key}>
          <div className={RAIL}>
            <span className="hidden sm:block" />
            <Rail first={di === 0}>
              <span className="relative z-[1] mt-1 block size-3 rounded-full border-2 border-border-strong bg-card" />
            </Rail>
            <div className="pb-3">
              <span className="inline-flex items-center gap-2 rounded-full bg-muted px-3 py-1 text-xs font-medium">
                {dayLabel(day.at)}
                <span className="num text-muted-foreground">{fullDate(day.at)} · {day.groups.reduce((n, g) => n + g.items.length, 0)} 条</span>
              </span>
            </div>
          </div>
          {day.groups.map((g, gi) => (
            <TimelineItem
              key={g.key}
              group={g}
              last={di === days.length - 1 && gi === day.groups.length - 1}
              checked={selected.has(g.key)}
              onCheck={(v) => onCheck(g.key, v)}
              onAck={() => onAck(g)}
            />
          ))}
        </section>
      ))}
    </div>
  );
}

function fullDate(ts: number) {
  const d = new Date(ts);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

/** 竖线所在的一列：线贯穿整行，第一行从节点处开始，最后一行到节点处结束 */
function Rail({ children, first, last }: { children: ReactNode; first?: boolean; last?: boolean }) {
  return (
    <div className="relative flex justify-center">
      <span className={cn("absolute left-1/2 w-px -translate-x-1/2 bg-border", first ? "top-3" : "top-0", last ? "h-4" : "bottom-0")} />
      {children}
    </div>
  );
}

function TimelineItem({ group, last, checked, onCheck, onAck }: { group: Group; last: boolean; checked: boolean; onCheck: (v: boolean) => void; onAck: () => void }) {
  const [open, setOpen] = useState(false);
  const a = group.first;
  const lv = level(a.level);
  const count = group.items.length;
  const earliest = group.items[count - 1].at;
  const Icon = RULES[a.rule]?.icon ?? lv.icon;
  return (
    <div className={RAIL}>
      <div className="hidden pt-3 text-right sm:block">
        <div className={cn("num text-sm font-medium", a.acked && "text-muted-foreground")}>{fmtClock(a.at, false)}</div>
        {count > 1 && <div className="num text-[11px] text-faint">起 {fmtClock(earliest, false)}</div>}
        <div className="text-[11px] text-faint">{fmtAgo(a.at)}</div>
      </div>
      <Rail last={last}>
        <span
          className={cn(
            "t-tone relative z-[1] mt-2 inline-flex size-8 items-center justify-center rounded-full ring-4 ring-card",
            a.acked ? "border border-border-strong bg-card text-faint" : TONE[lv.tone].tint,
          )}
          title={lv.name}
        >
          <Icon className="size-4" />
          {!a.acked && count > 1 && <span className="num absolute -top-1 -right-1.5 rounded-full bg-foreground px-1 text-[9px] leading-[14px] font-semibold text-background">{count > 99 ? "99+" : count}</span>}
        </span>
      </Rail>
      <div className={cn("pb-4", last && "pb-2")}>
        <div className={cn("rounded-2xl border bg-background/40 p-3.5 transition-colors hover:border-border-strong sm:p-4", a.acked && "opacity-70", checked && "border-ring")}>
          <div className="flex items-start gap-2.5">
            <Checkbox className="mt-0.5" checked={checked} disabled={a.acked} onCheckedChange={(v) => onCheck(v === true)} aria-label="选择这条告警" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{ruleName(a.rule)}</span>
                <Tag tone={lv.tone}>{lv.name}</Tag>
                {count > 1 && (
                  <button type="button" onClick={() => setOpen((v) => !v)} aria-expanded={open} className="num inline-flex cursor-pointer items-center gap-0.5 rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium hover:bg-accent">
                    {count} 次
                    <ChevronDown className={cn("size-3 transition-transform duration-200", open && "rotate-180")} />
                  </button>
                )}
              </div>
              <p className="mt-1 text-sm break-words text-muted-foreground">{a.detail}</p>
              <div className="mt-1.5 flex flex-wrap items-center gap-x-2 text-xs text-faint">
                <span className="num sm:hidden">{count > 1 ? `${fmtClock(earliest, false)} – ${fmtClock(a.at, false)}` : fmtClock(a.at)}</span>
                <span className="sm:hidden">·</span>
                {a.live_id ? (
                  <button type="button" className="num cursor-pointer hover:text-foreground hover:underline" onClick={() => navigate("rooms", { room: a.live_id })}>直播间 {a.live_id}</button>
                ) : (
                  <span>节点</span>
                )}
              </div>
            </div>
            {a.acked ? (
              <span className="inline-flex shrink-0 items-center gap-1 text-xs text-faint"><CheckCheck className="size-3.5" /><span className="hidden sm:inline">已确认</span></span>
            ) : (
              <PillButton tone="ghost" icon={CheckCheck} className="-mt-1 h-8 shrink-0 px-2.5 text-xs sm:px-3" onClick={onAck} aria-label={`确认${count > 1 ? ` ${count} 条` : ""}`}>
                <span className="hidden sm:inline">确认{count > 1 ? ` ${count} 条` : ""}</span>
              </PillButton>
            )}
          </div>
          {open && count > 1 && (
            <ol className="relative mt-3 ml-1.5 space-y-2 border-l border-border-strong pl-4">
              {group.items.map((x) => (
                <li key={x.id} className="relative text-xs">
                  <span className={cn("absolute top-1.5 -left-[21px] size-2 rounded-full ring-2 ring-card", a.acked ? "bg-border-strong" : TONE[lv.tone].fill)} />
                  <span className="num mr-2 text-faint">{fmtClock(x.at)}</span>
                  <span className="break-words text-muted-foreground">{x.detail}</span>
                </li>
              ))}
            </ol>
          )}
        </div>
      </div>
    </div>
  );
}

/* ---------------- 实时日志 ---------------- */

const LOG_TONE: Record<string, Tone> = { ERROR: "err", WARN: "warn", INFO: "info", DEBUG: "idle" };
function fmtBytes(n: number) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(0)} KB`;
  return `${n} B`;
}

function LogsPanel() {
  const toast = useToast();
  const [lv, setLv] = useState("");
  const [liveID, setLiveID] = useState("");
  const [keyword, setKeyword] = useState("");
  const [paused, setPaused] = useState(false);
  const [purge, setPurge] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ["logs", lv, liveID.trim(), keyword.trim()],
    queryFn: () => api<Log[]>(`/logs?${new URLSearchParams({ level: lv, live_id: liveID.trim(), q: keyword.trim() })}`),
    refetchInterval: paused ? false : 2500,
  });
  const stats = useQuery({ queryKey: ["log-stats"], queryFn: () => api<LogStats>("/logs/stats"), refetchInterval: paused ? false : 10_000 });
  const s = stats.data && Array.isArray(stats.data.files) ? stats.data : undefined;
  const cap = s ? s.max_bytes * (s.backups + 1) : 0;
  const rows = Array.isArray(query.data) ? query.data : [];

  return (
    <>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Card className="flex items-start gap-3 p-4">
          <IconTile icon={FileText} tone="member" size={36} />
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">日志文件</div>
            <div className="num mt-0.5 text-2xl font-semibold">{s ? fmtBytes(s.total_bytes) : "—"}</div>
            <div className="truncate text-[11px] text-faint">{s ? `${s.files.length} 个文件 · 上限约 ${fmtBytes(cap)}` : "读取中"}</div>
          </div>
        </Card>
        {(["ERROR", "WARN", "INFO"] as const).map((k) => (
          <button key={k} type="button" className="text-left" onClick={() => setLv((cur) => (cur === k ? "" : k))}>
            <Card className={cn("flex h-full items-start gap-3 p-4 transition-colors hover:border-border-strong", lv === k && "border-ring")}>
              <IconTile icon={k === "ERROR" ? AlertOctagon : k === "WARN" ? TriangleAlert : Info} tone={(s?.by_level?.[k] ?? 0) > 0 ? LOG_TONE[k] : "idle"} size={36} />
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground">{{ ERROR: "错误", WARN: "警告", INFO: "信息" }[k]}</div>
                <Num animate value={s?.by_level?.[k] ?? 0} format={String} className="mt-0.5 text-2xl font-semibold" />
                <div className="text-[11px] text-faint">内存中最近 {s?.buffered ?? 0} 条里</div>
              </div>
            </Card>
          </button>
        ))}
      </div>

      <Card className="mt-4 overflow-hidden">
        <div className="flex flex-wrap items-center gap-2.5 border-b p-3 sm:p-4">
          <Segmented
            value={lv}
            onChange={setLv}
            size="sm"
            options={[{ value: "", label: "全部" }, ...["ERROR", "WARN", "INFO", "DEBUG"].map((v) => ({ value: v, label: v }))]}
          />
          <SearchInput value={keyword} onChange={setKeyword} placeholder="关键字" className="w-full sm:w-56" />
          <SearchInput value={liveID} onChange={setLiveID} placeholder="直播间号" className="w-full sm:w-40" />
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <PillButton icon={paused ? Play : Pause} onClick={() => setPaused(!paused)}>{paused ? "继续" : "暂停"}</PillButton>
            <PillButton icon={Download} onClick={() => { window.location.href = "/admin/api/logs/export"; }}>导出</PillButton>
            <button type="button" onClick={() => setPurge(true)} className="inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-full px-4 text-sm font-medium tint-err">
              <Trash2 className="size-4" /> 清理
            </button>
          </div>
        </div>
        {query.error && <p role="alert" className="p-4 text-sm text-ink-err">读取日志失败：{query.error.message}</p>}
        <div className="scroll-area max-h-[calc(100dvh-380px)] min-h-[260px] bg-muted/20 pb-3 font-mono text-xs">
          {rows.map((e, i) => {
            const id = `${e.at}-${i}`;
            const hasFields = e.fields && Object.keys(e.fields).length > 0;
            return (
              <div key={id} className="border-b border-border/60 last:border-0">
                <button type="button" onClick={() => hasFields && setExpanded(expanded === id ? null : id)} className={cn("grid w-full grid-cols-[64px_52px_minmax(0,1fr)] items-start gap-3 px-4 py-2 text-left sm:grid-cols-[72px_56px_112px_minmax(0,1fr)] sm:px-5", hasFields && "cursor-pointer hover:bg-accent/40")}>
                  <span className="num text-muted-foreground" title={new Date(e.at).toLocaleString()}>{fmtClock(e.at)}</span>
                  <Tag tone={LOG_TONE[e.level] ?? "idle"} className="justify-center">{e.level}</Tag>
                  <span className="num hidden truncate text-muted-foreground sm:block">{e.live_id || "节点"}</span>
                  <span className="min-w-0 font-sans break-words">{e.message}{hasFields && <span className="ml-2 text-faint">{expanded === id ? "收起" : "详情"}</span>}</span>
                </button>
                {expanded === id && hasFields && (
                  <pre className="mx-4 mb-2 overflow-x-auto rounded-xl bg-background/70 p-3 text-[11px] leading-relaxed text-muted-foreground sm:mx-5">{JSON.stringify(e.fields, null, 2)}</pre>
                )}
              </div>
            );
          })}
          {!query.isPending && rows.length === 0 && <p className="p-10 text-center font-sans text-sm text-muted-foreground">当前筛选下没有日志</p>}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-4 py-2.5 text-[11px] text-muted-foreground sm:px-5">
          <span>显示内存中最近 200 条匹配日志；完整记录在数据目录 logs/，单个文件 {s ? fmtBytes(s.max_bytes) : "10 MB"}，保留 {s?.backups ?? 5} 个轮转备份。</span>
          {s?.files.map((f) => (
            <span key={f.name} className="num rounded-md bg-muted px-1.5 py-0.5" title={`更新于 ${new Date(f.modified).toLocaleString()}`}>{f.name} · {fmtBytes(f.size)}</span>
          ))}
        </div>
      </Card>

      <ConfirmDialog
        open={purge}
        title="清理日志文件？"
        description={<>将删除全部轮转备份并清空当前日志文件（共 <b className="text-foreground">{s ? fmtBytes(s.total_bytes) : "—"}</b>），内存中的日志也会清空。建议先导出留档。此操作会记入审计日志。</>}
        confirmLabel="确认清理"
        onClose={() => setPurge(false)}
        onConfirm={async () => {
          const r = await api<{ freed: number }>("/logs/purge", "POST", {});
          await Promise.all([queryClient.invalidateQueries({ queryKey: ["logs"] }), queryClient.invalidateQueries({ queryKey: ["log-stats"] })]);
          toast.show({ title: `日志已清理，释放 ${fmtBytes(r.freed)}`, status: "success" });
        }}
      />
    </>
  );
}

/* ---------------- 规则与通知 ---------------- */

function RulesPanel() {
  const query = useQuery({ queryKey: ["alert-rules"], queryFn: () => api<Rules>("/alert-rules") });
  if (query.error) return <p role="alert" className="text-ink-err">读取规则失败：{query.error.message}</p>;
  return query.data ? <RulesForm key={query.dataUpdatedAt} initial={query.data} /> : <p role="status" className="text-sm text-muted-foreground">正在读取规则…</p>;
}

function RulesForm({ initial }: { initial: Rules }) {
  const [rules, setRules] = useState(initial);
  const [clearWebhook, setClearWebhook] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  const enabledCount = Object.keys(RULES).filter((k) => rules.enabled[k]).length;
  return (
    <form
      className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]"
      onSubmit={async (e) => {
        e.preventDefault();
        setPending(true);
        setError("");
        try {
          await api("/alert-rules", "PUT", { enabled: rules.enabled, mass_percent: rules.mass_percent, capacity_percent: rules.capacity_percent, webhook: rules.webhook, clear_webhook: clearWebhook });
          setRules({ ...rules, webhook: "" });
          await queryClient.invalidateQueries({ queryKey: ["alert-rules"] });
          toast.show({ title: "告警规则已保存", status: "success" });
        } catch (err) {
          setError(err instanceof Error ? err.message : "保存失败");
        } finally {
          setPending(false);
        }
      }}
    >
      <Card className="p-4 sm:p-5">
        <CardHeader icon={Settings2} tone="warn" title="告警规则" desc={`已启用 ${enabledCount} / ${Object.keys(RULES).length} 条；关闭只停止告警，保护机制照常生效`} />
        <div className="mt-4 divide-y rounded-2xl border">
          {Object.entries(RULES).map(([key, r]) => (
            <div key={key} className="flex items-center gap-3 px-4 py-3">
              <IconTile icon={r.icon} tone={rules.enabled[key] ? "warn" : "idle"} size={32} />
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{r.name}</div>
                <div className="text-xs text-muted-foreground">{r.desc}</div>
              </div>
              <Switch checked={rules.enabled[key] ?? false} onCheckedChange={(v) => setRules({ ...rules, enabled: { ...rules.enabled, [key]: v } })} ariaLabel={r.name} />
            </div>
          ))}
        </div>
      </Card>
      <div className="grid content-start gap-4">
        <Card className="p-4 sm:p-5">
          <CardHeader icon={TriangleAlert} tone="info" title="阈值" desc="超过阈值才产生告警" />
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <label className="space-y-2 text-sm">
              大面积失败（%）
              <TextInput type="number" min={1} max={100} value={rules.mass_percent} onChange={(e) => setRules({ ...rules, mass_percent: Number(e.target.value) })} />
            </label>
            <label className="space-y-2 text-sm">
              容量将满（%）
              <TextInput type="number" min={1} max={100} value={rules.capacity_percent} onChange={(e) => setRules({ ...rules, capacity_percent: Number(e.target.value) })} />
            </label>
          </div>
        </Card>
        <Card className="p-4 sm:p-5">
          <CardHeader icon={BellRing} tone="social" title="Webhook 通知" desc={rules.webhook_configured ? "已配置，告警会同步推送" : "未配置，只在后台查看"} />
          <div className="mt-4 space-y-3">
            <TextInput type="password" autoComplete="new-password" aria-label="Webhook 地址" value={rules.webhook} onChange={(e) => setRules({ ...rules, webhook: e.target.value })} placeholder={rules.webhook_configured ? "已配置；留空保留，填写替换" : "https:// 通知地址（可选）"} />
            {rules.webhook_configured && (
              <label className="flex items-center gap-2 text-sm">
                <Checkbox checked={clearWebhook} onCheckedChange={(v) => setClearWebhook(v === true)} />
                清除已有 Webhook
              </label>
            )}
            <p className="text-xs leading-relaxed text-muted-foreground">只发送脱敏后的告警，地址加密保存；投递失败最多重试 3 次。</p>
          </div>
        </Card>
        {error && <p role="alert" className="text-sm text-ink-err">{error}</p>}
        <div className="flex justify-end">
          <PillButton type="submit" tone="primary" icon={Save} disabled={pending}>{pending ? "保存中…" : "保存规则"}</PillButton>
        </div>
      </div>
    </form>
  );
}
