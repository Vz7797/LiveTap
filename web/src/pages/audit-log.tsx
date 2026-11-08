import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bell,
  ChevronDown,
  FlaskConical,
  History,
  KeyRound,
  LogIn,
  LogOut,
  MinusCircle,
  Network,
  Pause,
  Play,
  Plug,
  PlusCircle,
  RefreshCw,
  ScrollText,
  Settings2,
  ShieldAlert,
  SlidersHorizontal,
  Trash2,
  Tv,
  type LucideIcon,
} from "lucide-react";
import { api } from "@/data/api";
import { Card, CardHeader, PillButton } from "@/components/kit";
import { SearchInput, ToggleChip } from "@/components/kit/form";
import { fmtAgo, fmtClock } from "@/lib/format";
import type { Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";

/*
 * 审计日志：按日期分时间轴，相邻的同一操作（例如批量移除 40 个直播间）合并成一行，可展开看明细；
 * 按类别筛选、按关键字搜索；列表在卡片内滚动，不把整页拉得很长。
 */

interface AuditRow {
  id?: number;
  at: number;
  actor: string;
  action: string;
  target: string;
  detail: string;
}

type Category = "account" | "room" | "client" | "config" | "ops";
const CATEGORIES: { key: Category; label: string; tone: Tone; icon: LucideIcon }[] = [
  { key: "account", label: "登录与账号", tone: "social", icon: KeyRound },
  { key: "room", label: "直播间", tone: "chat", icon: Tv },
  { key: "client", label: "接入方", tone: "info", icon: Plug },
  { key: "config", label: "设置与规则", tone: "member", icon: SlidersHorizontal },
  { key: "ops", label: "运维操作", tone: "like", icon: ScrollText },
];

const ACTIONS: Record<string, { label: string; icon: LucideIcon; tone?: Tone }> = {
  login: { label: "登录", icon: LogIn },
  logout: { label: "退出登录", icon: LogOut },
  login_failed: { label: "登录失败", icon: ShieldAlert, tone: "err" },
  password_changed: { label: "修改密码", icon: KeyRound },
  password_change_failed: { label: "修改密码失败", icon: ShieldAlert, tone: "err" },
  room_add: { label: "添加直播间", icon: PlusCircle },
  room_remove: { label: "移除直播间", icon: MinusCircle, tone: "warn" },
  room_prune: { label: "自动清理直播间", icon: Trash2 },
  room_update: { label: "修改直播间设置", icon: SlidersHorizontal },
  room_pause: { label: "暂停采集", icon: Pause },
  room_resume: { label: "开始采集", icon: Play },
  room_reconnect: { label: "重新连接", icon: RefreshCw },
  client_create: { label: "新建接入方", icon: PlusCircle },
  client_update: { label: "修改接入方", icon: Plug },
  client_delete: { label: "删除接入方", icon: Trash2, tone: "warn" },
  settings_update: { label: "修改系统设置", icon: Settings2 },
  alert_rules: { label: "修改告警规则", icon: Bell },
  alert_ack: { label: "确认告警", icon: Bell },
  alert_ack_bulk: { label: "批量确认告警", icon: Bell },
  alert_purge: { label: "清理告警", icon: Trash2 },
  logs_purge: { label: "清理日志", icon: Trash2 },
  lab_start: { label: "开始实验", icon: FlaskConical },
  lab_stop: { label: "停止实验", icon: FlaskConical },
  lab_delete: { label: "删除实验记录", icon: Trash2 },
  pool_save: { label: "保存代理或账号", icon: Network },
  pool_delete: { label: "删除代理或账号", icon: Trash2 },
  pool_check: { label: "检测代理或账号", icon: Network },
};

function categoryOf(action: string): Category {
  if (action.startsWith("login") || action.startsWith("logout") || action.startsWith("password")) return "account";
  if (action.startsWith("room_")) return "room";
  if (action.startsWith("client_")) return "client";
  if (action === "settings_update" || action === "alert_rules" || action.startsWith("rollout_")) return "config";
  return "ops";
}

function actionMeta(action: string) {
  const cat = CATEGORIES.find((c) => c.key === categoryOf(action))!;
  const known = ACTIONS[action];
  if (known) return { ...known, tone: known.tone ?? cat.tone };
  if (action.startsWith("rollout_")) return { label: "滚动应用设置", icon: RefreshCw, tone: cat.tone };
  return { label: action, icon: cat.icon, tone: cat.tone };
}

/**
 * 同一天、同一操作人、同一类别，且相邻两条间隔不超过 10 分钟的记录合并为一组。
 * 按类别而不是按具体操作合并：批量添加直播间时“添加”和“开始采集”是交替写入的。
 */
interface Group {
  key: string;
  items: AuditRow[];
  first: AuditRow;
}
const MERGE_GAP = 10 * 60_000;
function groupRows(rows: AuditRow[]): Group[] {
  const out: Group[] = [];
  for (const r of rows) {
    const last = out[out.length - 1];
    const prev = last?.items[last.items.length - 1];
    if (last && prev && categoryOf(last.first.action) === categoryOf(r.action) && last.first.actor === r.actor && dayKey(prev.at) === dayKey(r.at) && prev.at - r.at <= MERGE_GAP) last.items.push(r);
    else out.push({ key: `${r.id ?? r.at}-${out.length}`, items: [r], first: r });
  }
  return out;
}
function dayKey(ts: number) {
  const d = new Date(ts);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}
function dayLabel(ts: number) {
  const d = new Date(ts);
  if (dayKey(ts) === dayKey(Date.now())) return "今天";
  if (dayKey(ts) === dayKey(Date.now() - 86_400_000)) return "昨天";
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

const PAGE = 100;
const MAX = 500;

export function AuditLog() {
  const [limit, setLimit] = useState(PAGE);
  const [cats, setCats] = useState<Set<Category>>(new Set());
  const [keyword, setKeyword] = useState("");
  const rows = useQuery({
    queryKey: ["audit", limit],
    queryFn: () => api<AuditRow[]>(`/audit?limit=${limit}`),
    refetchInterval: 10_000,
    placeholderData: (prev) => prev,
  });
  const all = rows.data ?? [];
  const counts = useMemo(() => {
    const c = {} as Record<Category, number>;
    for (const r of all) c[categoryOf(r.action)] = (c[categoryOf(r.action)] ?? 0) + 1;
    return c;
  }, [all]);
  const filtered = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    return all.filter((r) => {
      if (cats.size && !cats.has(categoryOf(r.action))) return false;
      if (!k) return true;
      return [r.action, actionMeta(r.action).label, r.target, r.detail, r.actor].some((v) => v?.toLowerCase().includes(k));
    });
  }, [all, cats, keyword]);
  const days = useMemo(() => {
    const out: { key: string; at: number; count: number; groups: Group[] }[] = [];
    for (const g of groupRows(filtered)) {
      const k = dayKey(g.first.at);
      if (out[out.length - 1]?.key !== k) out.push({ key: k, at: g.first.at, count: 0, groups: [] });
      const day = out[out.length - 1];
      day.groups.push(g);
      day.count += g.items.length;
    }
    return out;
  }, [filtered]);
  const toggle = (c: Category) =>
    setCats((prev) => {
      const next = new Set(prev);
      if (next.has(c)) next.delete(c);
      else next.add(c);
      return next;
    });
  const more = all.length >= limit && limit < MAX;

  return (
    <Card className="overflow-hidden">
      <div className="p-4 sm:p-6">
        <CardHeader icon={History} tone="like" title="审计日志" desc={`后台操作记录，相邻的同类操作已合并；敏感值不记录 · 已载入最近 ${all.length} 条`} />
        <div className="mt-5 flex flex-wrap items-center gap-2">
          <ToggleChip active={cats.size === 0} onClick={() => setCats(new Set())} tone="idle">
            全部 <span className="num opacity-70">{all.length}</span>
          </ToggleChip>
          {CATEGORIES.map((c) => (
            <ToggleChip key={c.key} active={cats.has(c.key)} onClick={() => toggle(c.key)} tone={c.tone} icon={c.icon}>
              {c.label} <span className="num opacity-70">{counts[c.key] ?? 0}</span>
            </ToggleChip>
          ))}
          <SearchInput value={keyword} onChange={setKeyword} placeholder="搜索操作、直播间号、说明" className="w-full sm:ml-auto sm:w-64" />
        </div>
      </div>
      {rows.error && <p className="border-t px-4 py-3 text-sm text-ink-err sm:px-6">读取审计日志失败</p>}
      <div className="scroll-area max-h-[min(640px,62dvh)] border-t px-3 pt-4 sm:px-5">
        {days.length === 0 && !rows.isLoading && <p className="py-10 text-center text-sm text-muted-foreground">{all.length ? "没有符合条件的记录" : "暂无后台操作记录"}</p>}
        {days.map((day, di) => (
          <section key={day.key}>
            <div className="sticky top-0 z-[2] -mx-3 bg-card/95 px-3 pb-3 backdrop-blur-sm sm:-mx-5 sm:px-5">
              <div className={RAIL}>
                <span className="hidden sm:block" />
                <Rail first={di === 0}>
                  <span className="relative z-[1] mt-1 block size-3 rounded-full border-2 border-border-strong bg-card" />
                </Rail>
                <div>
                  <span className="inline-flex items-center gap-2 rounded-full bg-muted px-3 py-1 text-xs font-medium">
                    {dayLabel(day.at)}
                    <span className="num text-muted-foreground">{day.count} 条</span>
                  </span>
                </div>
              </div>
            </div>
            {day.groups.map((g, gi) => (
              <AuditItem key={g.key} group={g} last={di === days.length - 1 && gi === day.groups.length - 1} />
            ))}
          </section>
        ))}
        <div className="flex justify-center pt-1 pb-5">
          {more ? (
            <PillButton tone="ghost" icon={ChevronDown} disabled={rows.isFetching} onClick={() => setLimit((n) => Math.min(MAX, n + PAGE))}>
              {rows.isFetching ? "正在载入…" : "载入更早的记录"}
            </PillButton>
          ) : (
            all.length > 0 && <span className="text-xs text-faint">{limit >= MAX && all.length >= MAX ? `最多显示最近 ${MAX} 条` : "没有更早的记录了"}</span>
          )}
        </div>
      </div>
    </Card>
  );
}

const RAIL = "grid grid-cols-[28px_minmax(0,1fr)] gap-3 sm:grid-cols-[64px_28px_minmax(0,1fr)] sm:gap-4";

function Rail({ children, first, last }: { children: ReactNode; first?: boolean; last?: boolean }) {
  return (
    <div className="relative flex justify-center">
      <span className={cn("absolute left-1/2 w-px -translate-x-1/2 bg-border", first ? "top-3" : "top-0", last ? "h-4" : "-bottom-3")} />
      {children}
    </div>
  );
}

/** 后端写入的通用说明（与操作名重复），不再单独显示 */
const GENERIC = new Set(["更新采集任务", "添加手动采集任务", "移除采集任务", "登录成功", "登录失败"]);

/** 组内各操作按出现次数排序，如 [添加直播间 30, 开始采集 30] */
function actionSummary(items: AuditRow[]) {
  const n = new Map<string, number>();
  for (const x of items) n.set(x.action, (n.get(x.action) ?? 0) + 1);
  return [...n.entries()].sort((a, b) => b[1] - a[1]).map(([action, count]) => ({ action, count, meta: actionMeta(action) }));
}

function AuditItem({ group, last }: { group: Group; last: boolean }) {
  const [open, setOpen] = useState(false);
  const r = group.first;
  const kinds = actionSummary(group.items);
  const m = kinds[0].meta;
  const count = group.items.length;
  const earliest = group.items[count - 1].at;
  const targets = [...new Set(group.items.map((x) => x.target).filter(Boolean))];
  const details = [...new Set(group.items.map((x) => x.detail).filter((d) => d && !GENERIC.has(d)))];
  const Icon = m.icon;
  return (
    <div className={RAIL}>
      <div className="hidden pt-1.5 text-right sm:block">
        <div className="num text-sm font-medium">{fmtClock(r.at, false)}</div>
        <div className="text-[11px] text-faint">{fmtAgo(r.at)}</div>
      </div>
      <Rail last={last}>
        <span className={cn("relative z-[1] mt-1 inline-flex size-7 items-center justify-center rounded-full ring-4 ring-card", TONE[m.tone].tint)}>
          <Icon className="size-3.5" />
        </span>
      </Rail>
      <div className={cn("min-w-0 pb-4", last && "pb-2")}>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 pt-1">
          <span className="text-sm font-medium">
            {kinds.length === 1
              ? m.label
              : kinds.map((k, i) => (
                  <span key={k.action}>
                    {i > 0 && <span className="mx-1 text-faint">·</span>}
                    {k.meta.label}
                    <span className="num ml-1 text-muted-foreground">{k.count}</span>
                  </span>
                ))}
          </span>
          {count > 1 && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              className="num inline-flex cursor-pointer items-center gap-0.5 rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium hover:bg-accent"
            >
              {count} 次
              <ChevronDown className={cn("size-3 transition-transform duration-200", open && "rotate-180")} />
            </button>
          )}
          {count === 1 && r.target && r.target !== r.actor && r.target !== "node" && <span className="num truncate text-sm text-muted-foreground">{r.target}</span>}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-faint">
          <span className="num sm:hidden">{count > 1 ? `${fmtClock(earliest, false)} – ${fmtClock(r.at, false)}` : fmtClock(r.at, false)}</span>
          <span className="sm:hidden">·</span>
          {count > 1 && <span className="num hidden sm:inline">{fmtClock(earliest, false)} 起 ·</span>}
          <span>{r.actor}</span>
          {details.length === 1 && kinds.length === 1 && details[0] !== m.label && (
            <>
              <span>·</span>
              <span className="break-words">{details[0]}</span>
            </>
          )}
          {count > 1 && targets.length > 1 && !open && (
            <>
              <span>·</span>
              <span className="num truncate">
                {targets.slice(0, 2).join("、")}
                {targets.length > 2 && ` 等 ${targets.length} 个${categoryOf(r.action) === "room" ? "直播间" : ""}`}
              </span>
            </>
          )}
        </div>
        {open && count > 1 && (
          <ol className="mt-2 grid gap-x-4 gap-y-1 rounded-xl border bg-background/40 p-2.5 text-xs sm:grid-cols-2 xl:grid-cols-3">
            {group.items.map((x, i) => (
              <li key={x.id ?? i} className="flex min-w-0 items-baseline gap-2">
                <span className="num shrink-0 text-faint">{fmtClock(x.at)}</span>
                {kinds.length > 1 && <span className={cn("shrink-0", TONE[actionMeta(x.action).tone].ink)}>{actionMeta(x.action).label}</span>}
                <span className="num truncate text-muted-foreground" title={x.detail}>
                  {x.target || x.detail}
                </span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
}
