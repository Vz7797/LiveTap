import { ChevronDown, Database, Download, Flame, MessagesSquare, Pause, Play, Tv } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useToast } from "@/app/toast";
import { LiveFeed } from "@/components/feed";
import { Avatar, Card, CardHeader, Num, PageHeader } from "@/components/kit";
import { SearchInput, ToggleChip } from "@/components/kit/form";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { fmtNum } from "@/lib/format";
import { MSG_TYPES, TYPE_META, isLive, type MsgType } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";
import { useSim } from "@/data/runtime";
import { seedFeed, useFeedBuffer } from "@/data/feed-buffer";
import type { FeedMsg } from "@/data/types";

/** 记在 localStorage 的筛选状态；读不到、格式不对或为空时用默认值（隐私模式下只在本次有效） */
function useRemembered<T extends unknown[]>(key: string, fallback: T, clean: (v: unknown[]) => T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const raw = JSON.parse(localStorage.getItem(key) ?? "null");
      const v = Array.isArray(raw) ? clean(raw) : null;
      return v && v.length ? v : fallback;
    } catch {
      return fallback;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // 存不下时只在本次有效
    }
  }, [key, value]);
  return [value, setValue] as const;
}

export function MessagesPage() {
  const toast = useToast();
  // 本地按类型累积的消息（每类最多 300 条），筛选时从节点补齐，见 data/feed-buffer.ts
  const feed = useFeedBuffer();
  const rooms = useSim((s) => s.rooms);
  // 类型与直播间筛选记在本机：切换页面或刷新后保持上次的选择
  const [types, setTypes] = useRemembered<MsgType[]>("livetap-messages-types", [...MSG_TYPES], (v) => v.filter((t): t is MsgType => MSG_TYPES.includes(t as MsgType)));
  const typesKey = types.join(",");
  // 进入页面或切换类型时从节点补齐；补齐完成后换一个 resetKey，让列表直接显示最新结果，而不是把补来的消息当新消息逐条放出
  const [seeded, setSeeded] = useState(0);
  useEffect(() => {
    let alive = true;
    void seedFeed(typesKey.split(",")).then(() => alive && setSeeded((n) => n + 1));
    return () => {
      alive = false;
    };
  }, [typesKey]);
  const [roomIds, setRoomIds] = useRemembered<string[]>("livetap-messages-rooms", [], (v) => v.filter((x): x is string => typeof x === "string"));
  const [kw, setKw] = useState("");
  const [paused, setPaused] = useState(false);

  const filtered = useMemo(() => {
    const k = kw.trim().toLowerCase();
    return feed.filter(
      (m) =>
        types.includes(m.type) &&
        (roomIds.length === 0 || roomIds.includes(m.liveId)) &&
        (!k || m.content.toLowerCase().includes(k) || m.nickname.toLowerCase().includes(k)),
    );
  }, [feed, types, roomIds, kw]);

  const typeCounts = useMemo(() => {
    const c = Object.fromEntries(MSG_TYPES.map((t) => [t, 0])) as Record<MsgType, number>;
    feed.forEach((m) => c[m.type]++);
    return c;
  }, [feed]);

  const liveRooms = rooms.filter((r) => isLive(r.status));

  const exportAs = (fmt: "json" | "csv") => {
    const rows = filtered.slice(0, 100);
    const body =
      fmt === "json"
        ? JSON.stringify(
            rows.map((m) => ({ id: m.id, seq: m.seq, type: m.type, live_id: m.liveId, ts: new Date(m.ts).toISOString(), user: { nickname: m.nickname }, content: m.content, extra: m.extra ?? {} })),
            null,
            2,
          )
        : ["seq,type,live_id,time,nickname,content", ...rows.map((m) => [m.seq, m.type, m.liveId, new Date(m.ts).toISOString(), m.nickname, `"${m.content.replace(/"/g, '""')}"`].join(","))].join("\n");
    const blob = new Blob([fmt === "csv" ? "﻿" + body : body], { type: fmt === "json" ? "application/json" : "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `messages-${Date.now()}.${fmt}`;
    a.click();
    URL.revokeObjectURL(a.href);
    toast.show({ title: `已导出 ${rows.length} 条记录`, description: `${fmt.toUpperCase()} · 统一事件格式`, status: "success" });
  };

  return (
    <>
      <PageHeader
        icon={MessagesSquare}
        tone="member"
        title="实时消息"
        desc="全节点消息流，新消息逐条出现；需要细看时点“暂停”"
        actions={
          <>
            <motion.button
              type="button"
              onClick={() => setPaused((p) => !p)}
              transition={{ type: "tween", duration: 0.18, ease: "easeOut" }}
              className={cn(
                "inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-full px-4 text-sm font-medium",
                paused ? "bg-primary text-primary-foreground" : "border bg-card hover:bg-accent",
              )}
            >
              <AnimatePresence mode="popLayout" initial={false}>
                <motion.span key={String(paused)} initial={{ opacity: 0, scale: 0.6 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.6 }} className="inline-flex items-center gap-1.5">
                  {paused ? <Play className="size-4" /> : <Pause className="size-4" />}
                  {paused ? "继续" : "暂停"}
                </motion.span>
              </AnimatePresence>
            </motion.button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button type="button" className="inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-full border bg-card px-4 text-sm font-medium hover:bg-accent">
                  <Download className="size-4" /> 导出最近记录 <ChevronDown className="size-3.5 opacity-60" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel className="text-xs text-muted-foreground">按当前筛选，最多 100 条</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => exportAs("json")}>JSON（统一事件格式）</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => exportAs("csv")}>CSV（Excel 可打开）</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
        <Card className="min-w-0 overflow-hidden">
          {/* 过滤条 */}
          <div className="flex flex-col gap-3 border-b p-3 sm:p-4">
            <div className="flex flex-wrap items-center gap-1.5">
              {MSG_TYPES.map((t) => {
                const meta = TYPE_META[t];
                return (
                  <ToggleChip
                    key={t}
                    tone={meta.tone}
                    icon={meta.icon}
                    active={types.includes(t)}
                    onClick={() => setTypes((cur) => (cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t]))}
                  >
                    {meta.label}
                    <span className="num inline-block min-w-[3ch] text-right opacity-60">{typeCounts[t]}</span>
                  </ToggleChip>
                );
              })}
              <button
                type="button"
                onClick={() => setTypes(types.length === 1 && types[0] === "chat" ? [...MSG_TYPES] : ["chat"])}
                className="ml-1 cursor-pointer text-xs text-muted-foreground hover:text-foreground"
              >
                {types.length === 1 && types[0] === "chat" ? "显示全部类型" : "只看弹幕"}
              </button>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button
                    type="button"
                    className={cn(
                      "inline-flex h-9 shrink-0 cursor-pointer items-center gap-2 rounded-full border px-3 text-sm hover:bg-accent",
                      roomIds.length > 0 && "tint-member border-transparent",
                    )}
                  >
                    <Tv className="size-4" />
                    {roomIds.length === 0 ? "全部直播间" : `已选 ${roomIds.length} 个直播间`}
                    <ChevronDown className="size-3.5 opacity-60" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="max-h-80 w-64">
                  <DropdownMenuLabel className="text-xs text-muted-foreground">采集中的直播间</DropdownMenuLabel>
                  {liveRooms.map((r) => (
                    <DropdownMenuCheckboxItem
                      key={r.liveId}
                      checked={roomIds.includes(r.liveId)}
                      onSelect={(e) => e.preventDefault()}
                      onCheckedChange={(v) => setRoomIds((cur) => (v ? [...cur, r.liveId] : cur.filter((x) => x !== r.liveId)))}
                    >
                      <Avatar name={r.name} src={r.avatar} seed={r.liveId} size={20} />
                      <span className="truncate">{r.name}</span>
                    </DropdownMenuCheckboxItem>
                  ))}
                  {roomIds.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onSelect={() => setRoomIds([])}>清除选择</DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
              <SearchInput value={kw} onChange={setKw} placeholder="关键字：内容或昵称，如“停车”“套餐”" className="flex-1" />
            </div>
          </div>
          <div className="flex items-center justify-between px-4 pt-3 text-xs text-muted-foreground sm:px-5">
            <span>
              显示 <span className="num text-foreground">{Math.min(filtered.length, 80)}</span> / 匹配 <span className="num text-foreground">{filtered.length}</span> 条 · 每类最多保留最近 300 条
            </span>
            <span className="hidden items-center gap-1.5 sm:inline-flex">
              <span className={cn("size-1.5 rounded-full", paused ? "bg-chat" : "bg-ok")} />
              {paused ? "已暂停" : "实时推送中"}
            </span>
          </div>
          <div className="scroll-area h-[calc(100dvh-330px)] min-h-[420px] px-3 pb-4 sm:px-4">
            <LiveFeed messages={filtered} showRoom kw={kw.trim()} limit={80} paused={paused} resetKey={`${types.join()}|${roomIds.join()}|${kw.trim()}|${seeded}`} emptyTitle="没有匹配的消息" emptyDesc="调整类型、直播间或关键字筛选试试" />
          </div>
        </Card>

        <div className="grid min-w-0 gap-4 md:grid-cols-2 xl:grid-cols-1 xl:content-start">
          <RateCard />
          <HotWords feed={feed} onPick={setKw} />
          <ActiveRooms feed={feed} />
          <Card className="p-4">
            <CardHeader icon={Database} tone="info" title="数据保留" desc="弹幕正文默认不落库" />
            <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
              消息仅在内存中短期保留，用于后台预览和断线补拉；节点重启后清空。需要复盘请由接入方保存。
            </p>
          </Card>
        </div>
      </div>
    </>
  );
}

/**
 * 速率取节点按各房间计数算出的实时速率（每 2.5 秒一个点，单位条/分钟），取最近 4 个点约 10 秒。
 * 不能从推送下来的消息里数：推送只带最新 50 条，房间多时远低于实际。
 */
function RateCard() {
  const series = useSim((s) => s.series);
  const recent = series.slice(-4);
  const perSecond = (t: MsgType) => recent.reduce((a, p) => a + ((p.v as Record<string, number>)[t] ?? 0), 0) / Math.max(1, recent.length) / 60;
  const per = MSG_TYPES.map((t) => ({ t, n: perSecond(t) }));
  const total = per.reduce((a, p) => a + p.n, 0);
  const max = Math.max(0.1, ...per.map((p) => p.n));
  return (
    <Card className="p-4">
      <div className="flex items-end justify-between">
        <div>
          <div className="text-xs text-muted-foreground">最近 10 秒</div>
          <Num value={total} format={(n) => n.toFixed(1)} className="text-3xl font-semibold" suffix="条/秒" />
        </div>
        <div className="flex h-12 items-end gap-1.5">
          {per.map(({ t, n }) => (
            <span
              key={t}
              title={`${TYPE_META[t].label} ${n.toFixed(1)} 条/秒`}
              className={cn("w-3 rounded-t-[4px]", TONE[TYPE_META[t].tone].fill)}
              style={{ height: `${Math.max(6, (n / max) * 100)}%` }}
            />
          ))}
        </div>
      </div>
      <div className="mt-3 grid grid-cols-5 gap-1 text-center text-[10px] text-muted-foreground">
        {per.map(({ t, n }) => (
          <div key={t}>
            <div className={cn("font-medium", TONE[TYPE_META[t].tone].ink)}>{TYPE_META[t].label}</div>
            <div className="num text-foreground">{n.toFixed(1)}</div>
          </div>
        ))}
      </div>
    </Card>
  );
}

/**
 * 从最近的弹幕里统计热词：表情（[捂脸]）、英文与数字（666、gg）、反复出现的短句，以及 2～4 字的词组。
 * 每条弹幕里同一个词只算一次；去掉以常见虚词开头或结尾的片段，较长的词组覆盖与它次数相近的子串。
 */
const STOP = new Set("的了是我你他她它这那就都也还在有和与吗呢吧啊呀哦嗯哈么个们说要会能去来上下不没很太把被给让到对".split(""));
function hotWords(chats: FeedMsg[]) {
  const counts = new Map<string, number>();
  for (const m of chats) {
    const text = (m.content || "").trim();
    if (!text) continue;
    const found = new Set<string>();
    for (const e of text.match(/\[[^\[\]]{1,6}\]/g) ?? []) found.add(e);
    for (const w of text.match(/[A-Za-z0-9]{2,12}/g) ?? []) found.add(w.toLowerCase());
    const plain = text.replace(/\[[^\[\]]{1,6}\]/g, "").replace(/\s+/g, "");
    if (/^[一-龥]{2,8}$/.test(plain)) found.add(plain);
    for (const run of plain.match(/[一-龥]{2,}/g) ?? []) {
      for (let n = 2; n <= 4; n++) {
        for (let i = 0; i + n <= run.length; i++) {
          const g = run.slice(i, i + n);
          if (!STOP.has(g[0]) && !STOP.has(g[g.length - 1])) found.add(g);
        }
      }
    }
    for (const w of found) counts.set(w, (counts.get(w) ?? 0) + 1);
  }
  const ranked = [...counts].filter(([, n]) => n >= 2).sort((a, b) => b[1] - a[1] || b[0].length - a[0].length);
  let out: { k: string; n: number }[] = [];
  for (const [k, n] of ranked) {
    // 已有更长的词组包含它：跳过；它包含已选的片段且次数不太少（≥ 40%）：用整句替换片段（“确实可加油”替换“确实可”“加油”）
    if (out.some((o) => o.k.includes(k))) continue;
    const parts = out.filter((o) => k.includes(o.k));
    if (parts.length && parts.some((o) => n < o.n * 0.4)) continue;
    out = out.filter((o) => !k.includes(o.k));
    out.push({ k, n });
  }
  return out.sort((a, b) => b.n - a.n).slice(0, 12);
}

function HotWords({ feed, onPick }: { feed: FeedMsg[]; onPick: (w: string) => void }) {
  const words = useMemo(() => hotWords(feed.filter((m) => m.type === "chat").slice(0, 300)), [feed]);
  return (
    <Card className="p-4">
      <CardHeader icon={Flame} tone="gift" title="弹幕热词" desc="点一下按关键字过滤" />
      {/* 固定两行高度：热词个数变化时卡片不再忽高忽低 */}
      <div className="mt-3 flex max-h-[60px] min-h-[60px] flex-wrap content-start gap-1.5 overflow-hidden">
          {words.map((w) => (
            <button
              key={w.k}
              type="button"
              onClick={() => onPick(w.k)}
              className="inline-flex cursor-pointer items-center gap-1 rounded-full tint-chat px-2.5 py-1 text-xs font-medium transition-colors hover:bg-chat/20"
            >
              {w.k}
              <span className="num text-[10px] opacity-70">{w.n}</span>
            </button>
          ))}
        {words.length === 0 && <span className="text-xs text-muted-foreground">等待弹幕…</span>}
      </div>
    </Card>
  );
}

function ActiveRooms({ feed }: { feed: FeedMsg[] }) {
  const rooms = useSim((s) => s.rooms);
  const avatars = useMemo(() => new Map(rooms.map((r) => [r.liveId, r.avatar])), [rooms]);
  const top = useMemo(() => {
    const map = new Map<string, { name: string; n: number }>();
    feed.slice(0, 100).forEach((m) => map.set(m.liveId, { name: m.roomName, n: (map.get(m.liveId)?.n ?? 0) + 1 }));
    return [...map.entries()].sort((a, b) => b[1].n - a[1].n).slice(0, 5);
  }, [feed]);
  const max = Math.max(1, ...top.map(([, v]) => v.n));
  return (
    <Card className="p-4">
      <CardHeader icon={Tv} tone="ok" title="消息最多的直播间" desc="最近 100 条消息" />
      {/* 固定 5 行高度：上榜房间数变化时卡片高度不变 */}
      <div className="mt-3 min-h-[198px] space-y-2.5">
        {top.length === 0 && <p className="pt-2 text-xs text-muted-foreground">等待消息…</p>}
        {top.map(([id, v]) => (
          <div key={id} className="flex items-center gap-2.5">
            <Avatar name={v.name} src={avatars.get(id)} seed={id} size={26} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <span className="truncate text-sm">{v.name}</span>
                <span className="num text-xs text-muted-foreground">{fmtNum(v.n)}</span>
              </div>
              <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-info"
                  style={{ width: `${(v.n / max) * 100}%` }}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
