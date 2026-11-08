import { useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { MSG_TYPES, type MsgType, type RoomStatus } from "@/lib/meta";
import { ACCOUNTS, CLIENTS, INCIDENTS, PROXIES, TIMELINE, seedRooms } from "./seed";
import { CHATS, ENTER_SOURCES, GIFTS, NICKNAMES, jitter, pick, randId, weighted } from "./text";
import type {
  Account,
  Client,
  Counts,
  FeedMsg,
  Incident,
  LogLine,
  NodeStats,
  Proxy,
  Room,
  RoomEvent,
  TimelineItem,
} from "./types";

/**
 * 原型用的模拟运行时：前台每 2.5 秒推进一次，后台暂停。
 * 不连接任何真实服务。
 */

export interface SimState {
  /** 已经过的模拟秒数，不等于刷新次数。 */
  tick: number;
  rooms: Room[];
  feed: FeedMsg[];
  seq: number;
  /** 节点消息速率（条/分钟），每 5 秒一个点，保留 72 个点 = 6 分钟 */
  series: { at: number; v: Counts }[];
  totals: Counts;
  node: NodeStats;
  incidents: Incident[];
  timeline: TimelineItem[];
  logs: LogLine[];
  proxies: Proxy[];
  accounts: Account[];
  clients: Client[];
}

export type SimNotice = {
  tone: "success" | "error" | "info";
  title: string;
  description?: string;
};

const zero = (): Counts => ({ chat: 0, member: 0, like: 0, social: 0, gift: 0 });
const SHARE: Record<MsgType, number> = { chat: 0.3, member: 0.34, like: 0.26, social: 0.05, gift: 0.05 };
const REFRESH_MS = 2500;
const STEP_SECONDS = REFRESH_MS / 1000;
const MAX_FEED = 100;
const MAX_SERIES_POINTS = 72;

function poisson(lambda: number) {
  if (lambda <= 0) return 0;
  const L = Math.exp(-lambda);
  let k = 0;
  let p = 1;
  do {
    k++;
    p *= Math.random();
  } while (p > L && k < 60);
  return k - 1;
}

function makeMsg(room: Room, seq: number, type: MsgType): FeedMsg {
  const base = {
    id: randId(19),
    seq,
    type,
    liveId: room.liveId,
    roomName: room.name,
    nickname: pick(NICKNAMES),
    ts: Date.now(),
  };
  switch (type) {
    case "chat":
      return { ...base, content: pick(CHATS) };
    case "member": {
      const via = pick(ENTER_SOURCES);
      return { ...base, content: `通过${via}进入直播间`, extra: { via } };
    }
    case "like": {
      const count = 1 + Math.floor(Math.random() * 14);
      return { ...base, content: `为主播点赞`, extra: { count } };
    }
    case "social": {
      const action = Math.random() > 0.3 ? "关注" : "分享";
      return { ...base, content: action === "关注" ? "关注了主播" : "分享了直播间", extra: { action } };
    }
    case "gift": {
      const g = Math.random() > 0.93 ? pick(GIFTS.slice(6)) : pick(GIFTS.slice(0, 7));
      const count = g.value > 100 ? 1 : 1 + Math.floor(Math.random() * 9);
      return { ...base, content: `送出 ${g.name}`, extra: { gift: g.name, count } };
    }
  }
}

function seedSeries(rooms: Room[]) {
  const now = Date.now();
  const total = rooms.filter((r) => r.status === "collecting").reduce((s, r) => s + r.heat * 60, 0);
  return Array.from({ length: 72 }, (_, i) => {
    const wave = 1 + Math.sin(i / 7) * 0.12 + (Math.random() - 0.5) * 0.18;
    const v = zero();
    for (const t of MSG_TYPES) v[t] = Math.round(total * SHARE[t] * wave * (0.85 + Math.random() * 0.3));
    return { at: now - (72 - i) * 5000, v };
  });
}

function seedLogs(rooms: Room[]): LogLine[] {
  const now = Date.now();
  const out: LogLine[] = [];
  for (let i = 0; i < 60; i++) {
    const r = pick(rooms.filter((x) => x.status === "collecting"));
    out.push(makeLog(i, now - (60 - i) * 1700, r));
  }
  return out.reverse();
}

const LOG_TEMPLATES: { level: LogLine["level"]; msg: string; fields?: (r: Room) => string; w: number }[] = [
  { level: "DEBUG", msg: "发送保活心跳", fields: () => `interval=10s`, w: 6 },
  { level: "DEBUG", msg: "复用已准备的 WebSocket 上下文", w: 1 },
  { level: "INFO", msg: "WebSocket 连接成功", fields: (r) => `room_id=76969${r.liveId.slice(0, 8)} cost=${(1 + Math.random() * 2).toFixed(1)}s`, w: 2 },
  { level: "INFO", msg: "直播页确认当前正在直播", w: 1 },
  { level: "INFO", msg: "分钟统计已写入", fields: (r) => `msgs=${Math.round(r.msgPerMin)} silence=0s`, w: 3 },
  { level: "INFO", msg: "推送队列水位", fields: () => `client=cl-01 depth=${Math.floor(Math.random() * 8)} dropped=0`, w: 2 },
  { level: "WARN", msg: "WebSocket 连接失败，准备重连", fields: () => `backoff=1.5s jitter=${(Math.random() * 1.2).toFixed(2)}s`, w: 0.6 },
  { level: "WARN", msg: "web/enter 返回空响应，使用直播间页面状态兜底", w: 0.4 },
  { level: "WARN", msg: "检测到需重连，将稍后尝试", w: 0.4 },
  { level: "ERROR", msg: "解析 PushFrame 失败", fields: () => `err="unexpected EOF"`, w: 0.15 },
];

function makeLog(id: number, at: number, r: Room): LogLine {
  const t = LOG_TEMPLATES[weightedIndex(LOG_TEMPLATES.map((x) => x.w))];
  return { id, at, level: t.level, liveId: r.liveId, msg: t.msg, fields: t.fields?.(r) };
}

function weightedIndex(ws: number[]) {
  const total = ws.reduce((a, b) => a + b, 0);
  let x = Math.random() * total;
  for (let i = 0; i < ws.length; i++) {
    x -= ws[i];
    if (x <= 0) return i;
  }
  return 0;
}

function initialState(): SimState {
  const rooms = seedRooms();
  const now = Date.now();
  return {
    tick: 0,
    rooms,
    feed: [],
    seq: 1_048_576,
    series: seedSeries(rooms),
    totals: { chat: 48_213, member: 61_570, like: 392_861, social: 3_402, gift: 2_917 },
    node: {
      name: "sh-node-01",
      version: "v0.1.0-proto",
      engineVersion: "v2.3.2 + request-hook",
      engine: { enabled: true, repo: "jwwsjlm/douyinLive", version: "v2.3.2", latest: "v2.3.3", latestUrl: "https://github.com/jwwsjlm/douyinLive/releases", publishedAt: Date.now() - 2 * 86400000, checkedAt: Date.now() - 20 * 60000, newer: true },
      bootId: "b7f3a2c9",
      startedAt: now - (3 * 24 + 7) * 3_600_000 - 26 * 60_000,
      cpu: 3.4,
      memMB: 168,
      memLimitMB: 2048,
      goroutines: 412,
      upKBs: 9.6,
      downKBs: 96,
      conns: 16,
      reqPerMin: 62,
      reqBudgetPerSec: 5,
      maxRooms: 200,
      health: 96,
      cpuHist: Array.from({ length: 40 }, () => jitter(3.2, 0.3)),
      memHist: Array.from({ length: 40 }, (_, i) => 150 + i * 0.4 + Math.random() * 6),
      netHist: Array.from({ length: 40 }, () => jitter(96, 0.2)),
      upHist: Array.from({ length: 40 }, () => jitter(9.6, 0.2)),
      bandwidthAvailable: true,
    },
    incidents: INCIDENTS,
    timeline: TIMELINE,
    logs: seedLogs(rooms),
    proxies: PROXIES,
    accounts: ACCOUNTS,
    clients: CLIENTS,
  };
}

type Listener = () => void;
type NoticeListener = (n: SimNotice) => void;

class Simulation {
  state: SimState = initialState();
  private listeners = new Set<Listener>();
  private noticeListeners = new Set<NoticeListener>();
  private timer: number | null = null;
  private started = false;
  private logId = 1000;
  private scheduled: { at: number; fn: () => void }[] = [];

  start() {
    if (this.started) return;
    this.started = true;
    // 预热：让消息流一打开就有内容
    if (this.state.feed.length === 0) {
      const next = { ...this.state };
      for (let i = 0; i < 14; i++) this.emitFeed(next, 1.6);
      this.commit(next);
    }
    document.addEventListener("visibilitychange", this.syncTimer);
    this.syncTimer();
  }

  stop() {
    this.started = false;
    document.removeEventListener("visibilitychange", this.syncTimer);
    this.syncTimer();
  }

  private syncTimer = () => {
    if (this.timer !== null) window.clearInterval(this.timer);
    this.timer = null;
    if (this.started && !document.hidden) {
      this.timer = window.setInterval(() => this.step(), REFRESH_MS);
    }
  };

  subscribe = (l: Listener) => {
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  };

  onNotice(l: NoticeListener) {
    this.noticeListeners.add(l);
    return () => {
      this.noticeListeners.delete(l);
    };
  }

  getSnapshot = () => this.state;

  private commit(next: SimState) {
    this.state = next;
    this.listeners.forEach((l) => l());
  }

  private notice(n: SimNotice) {
    this.noticeListeners.forEach((l) => l(n));
  }

  private later(seconds: number, fn: () => void) {
    this.scheduled.push({ at: this.state.tick + seconds, fn });
  }

  // ---------- 对外操作（原型中的“管理接口”） ----------

  updateRoom(liveId: string, patch: Partial<Room>, event?: Omit<RoomEvent, "at">) {
    const rooms = this.state.rooms.map((r) =>
      r.liveId === liveId
        ? { ...r, ...patch, events: event ? [{ ...event, at: Date.now() }, ...r.events] : r.events }
        : r,
    );
    this.commit({ ...this.state, rooms });
  }

  setStatus(liveId: string, status: RoomStatus, text?: string) {
    const kind: RoomEvent["kind"] =
      status === "collecting" ? "ready" : status === "reconnecting" ? "reconnect" : status === "risk" ? "risk" : "probe";
    const r = this.room(liveId);
    if (!r) return;
    const patch: Partial<Room> = { status };
    if (status === "collecting") {
      if (r.online === 0) patch.online = Math.round(r.heat * 900 + 20);
      patch.lastMsgAt = Date.now();
    } else {
      patch.msgPerMin = 0;
      if (status !== "reconnecting") patch.online = 0;
    }
    this.updateRoom(liveId, patch, text ? { kind, text } : undefined);
  }

  room(liveId: string) {
    return this.state.rooms.find((r) => r.liveId === liveId);
  }

  reconnect(liveId: string) {
    this.setStatus(liveId, "reconnecting", "手动触发重连：释放旧实例，创建新实例");
    this.later(3, () => {
      const r = this.room(liveId);
      if (r?.status === "reconnecting") {
        this.setStatus(liveId, "collecting", "重连成功，上游已就绪");
        this.updateRoom(liveId, { reconnects30m: r.reconnects30m + 1 });
      }
    });
  }

  pause(liveId: string) {
    this.setStatus(liveId, "stopped", "手动暂停采集");
  }

  resume(liveId: string) {
    this.setStatus(liveId, "probing", "恢复采集，开始探测");
    this.later(2, () => this.setStatus(liveId, "connecting", "已开播，开始建立连接"));
    this.later(4, () => this.setStatus(liveId, "collecting", "WebSocket 连接成功，上游已就绪"));
  }

  remove(liveIds: string[]) {
    const set = new Set(liveIds);
    this.commit({ ...this.state, rooms: this.state.rooms.filter((r) => !set.has(r.liveId)) });
  }

  addRooms(ids: string[], tags: string[] = []) {
    const existing = new Set(this.state.rooms.map((r) => r.liveId));
    const fresh = ids.filter((id) => !existing.has(id));
    const now = Date.now();
    const names = ["新店试播间", "城南小吃街", "周记面馆", "好运来饺子馆", "王姐卤味", "海鲜大排档", "麻辣香锅研究社", "糖水铺子"];
    const added: Room[] = fresh.map((liveId, i) => ({
      liveId,
      name: names[(i + this.state.rooms.length) % names.length],
      title: "直播标题获取中…",
      city: "—",
      category: "美食团购",
      status: "probing",
      source: "manual",
      tags,
      engine: "douyinlive",
      sign: "local",
      protocol: "web",
      proxyId: null,
      accountId: null,
      online: 0,
      totalPv: 0,
      heat: 0.3 + Math.random() * 1.2,
      msgPerMin: 0,
      reconnects30m: 0,
      lastMsgAt: now,
      since: now,
      spark: Array(30).fill(0),
      onlineSpark: Array(30).fill(0),
      counts: zero(),
      subscribers: 0,
      silenceThreshold: 120,
      events: [{ at: now, kind: "probe", text: "后台手动添加，开始探测" }],
      enabledTypes: [...MSG_TYPES],
      minutes: Array.from({ length: 30 }, zero),
    }));
    this.commit({ ...this.state, rooms: [...added, ...this.state.rooms] });
    // 全局启动限速：每秒最多启动 2 个（设计文档 5.4）
    added.forEach((r, i) => {
      const delay = 2 + Math.floor(i / 2);
      this.later(delay, () => {
        const live = Math.random() > 0.3;
        if (live) {
          this.setStatus(r.liveId, "connecting", "直播页确认当前正在直播");
          this.later(2, () => this.setStatus(r.liveId, "collecting", "WebSocket 连接成功，上游已就绪"));
        } else {
          this.setStatus(r.liveId, "waiting", "已确认账号有房间但当前未开播");
        }
      });
    });
    return fresh.length;
  }

  ackIncident(id: string) {
    this.commit({
      ...this.state,
      incidents: this.state.incidents.map((x) => (x.id === id ? { ...x, acked: true } : x)),
    });
  }

  ackAll() {
    this.commit({ ...this.state, incidents: this.state.incidents.map((x) => ({ ...x, acked: true })) });
  }

  setProxies(proxies: Proxy[]) {
    this.commit({ ...this.state, proxies });
  }
  setAccounts(accounts: Account[]) {
    this.commit({ ...this.state, accounts });
  }
  setClients(clients: Client[]) {
    this.commit({ ...this.state, clients });
  }

  // ---------- 推进 ----------

  private emitFeed(s: SimState, lambda: number) {
    const live = s.rooms.filter((r) => r.status === "collecting");
    if (live.length === 0) return;
    const n = poisson(lambda);
    const total = live.reduce((a, r) => a + r.heat, 0);
    for (let i = 0; i < n; i++) {
      let x = Math.random() * total;
      let room = live[0];
      for (const r of live) {
        x -= r.heat;
        if (x <= 0) {
          room = r;
          break;
        }
      }
      const type = weighted({ chat: 0.42, member: 0.28, like: 0.16, social: 0.07, gift: 0.07 });
      if (!room.enabledTypes.includes(type)) continue;
      s.seq += 1;
      s.feed = [makeMsg(room, s.seq, type), ...s.feed].slice(0, MAX_FEED);
    }
  }

  private step() {
    const prev = this.state;
    const s: SimState = { ...prev, tick: prev.tick + STEP_SECONDS, feed: prev.feed, totals: { ...prev.totals } };
    const now = Date.now();
    const pushPoint = Math.floor(s.tick / 5) > Math.floor(prev.tick / 5);
    const minuteRoll = Math.floor(s.tick / 60) > Math.floor(prev.tick / 60);

    s.rooms = prev.rooms.map((r) => {
      if (r.status !== "collecting") {
        if (pushPoint) return { ...r, spark: [...r.spark.slice(1), 0], onlineSpark: [...r.onlineSpark.slice(1), 0] };
        return r;
      }
      const counts = { ...r.counts };
      let arrivals = 0;
      for (const t of MSG_TYPES) {
        const k = poisson(r.heat * SHARE[t] * (t === "like" ? 2.2 : 1) * STEP_SECONDS);
        counts[t] += k;
        s.totals[t] += k;
        arrivals += k;
      }
      const target = r.heat * 60 * (1 + Math.sin((s.tick + r.liveId.length * 7) / 9) * 0.18);
      const previousWeight = Math.pow(0.8, STEP_SECONDS);
      const msgPerMin = r.msgPerMin * previousWeight + jitter(target, 0.15) * (1 - previousWeight);
      const online = Math.max(1, Math.round(r.online + (Math.random() - 0.48) * Math.max(2, r.online * 0.012)));
      const minutes = minuteRoll
        ? [
            ...r.minutes.slice(1),
            Object.fromEntries(MSG_TYPES.map((t) => [t, Math.round(msgPerMin * SHARE[t])])) as Counts,
          ]
        : r.minutes;
      return {
        ...r,
        counts,
        msgPerMin,
        online,
        totalPv: r.totalPv + (Math.random() > 0.6 ? 1 : 0),
        // 系统消息（在线人数、统计）持续推送，冷清直播间也会更新 lastMsgAt
        lastMsgAt: arrivals > 0 || Math.random() > 0.55 ? now : r.lastMsgAt,
        spark: pushPoint ? [...r.spark.slice(1), Math.round(msgPerMin)] : r.spark,
        onlineSpark: pushPoint ? [...r.onlineSpark.slice(1), online] : r.onlineSpark,
        minutes,
      };
    });

    this.emitFeed(s, 2.4 * STEP_SECONDS);

    if (pushPoint) {
      const v = zero();
      for (const r of s.rooms) {
        if (r.status !== "collecting") continue;
        for (const t of MSG_TYPES) v[t] += Math.round(r.msgPerMin * SHARE[t] * (t === "like" ? 2.2 : 1));
      }
      s.series = [...prev.series.slice(-(MAX_SERIES_POINTS - 1)), { at: now, v }];
    }

    // 节点资源
    const liveCount = s.rooms.filter((r) => r.status === "collecting").length;
    const heatSum = s.rooms.reduce((a, r) => a + (r.status === "collecting" ? r.heat : 0), 0);
    const n = prev.node;
    const cpu = Math.max(0.6, jitter(1.2 + heatSum * 0.18, 0.18));
    const memMB = 58 + s.rooms.filter((r) => r.status !== "stopped" && r.status !== "notfound").length * 6.1 + Math.random() * 4;
    const downKBs = jitter(6 * liveCount + heatSum * 4, 0.12);
    const upKBs = downKBs * 0.11;
    s.node = {
      ...n,
      cpu,
      memMB,
      downKBs,
      upKBs,
      goroutines: 180 + liveCount * 14 + Math.floor(Math.random() * 8),
      conns: liveCount + 3,
      reqPerMin: Math.round(jitter(58, 0.12)),
      cpuHist: pushPoint ? [...n.cpuHist.slice(1), cpu] : n.cpuHist,
      memHist: pushPoint ? [...n.memHist.slice(1), memMB] : n.memHist,
      netHist: pushPoint ? [...n.netHist.slice(1), downKBs] : n.netHist,
      upHist: pushPoint ? [...(n.upHist ?? []).slice(1), upKBs] : n.upHist,
    };

    // 接入方推送量
    s.clients = prev.clients.map((c) =>
      c.enabled && c.connections > 0
        ? {
            ...c,
            pushPerSec: Math.round(jitter(c.id === "cl-01" ? heatSum * 1.6 : 6, 0.2)),
            queue: Math.floor(Math.random() * (c.id === "cl-01" ? 9 : 3)),
            spark: pushPoint ? [...c.spark.slice(1), Math.round(jitter(c.id === "cl-01" ? heatSum * 1.6 : 6, 0.2))] : c.spark,
          }
        : c,
    );

    // 日志
    const logCount = poisson(1.3 * STEP_SECONDS);
    if (logCount > 0) {
      const live = s.rooms.filter((r) => r.status === "collecting");
      const lines: LogLine[] = [];
      for (let i = 0; i < logCount && live.length; i++) lines.push(makeLog(this.logId++, now, pick(live)));
      s.logs = [...lines, ...prev.logs].slice(0, 500);
    }

    this.commit(s);
    this.runScript(prev.tick, s.tick);
  }

  private runScript(previousTick: number, tick: number) {
    const due = this.scheduled.filter((x) => x.at <= tick);
    this.scheduled = this.scheduled.filter((x) => x.at > tick);
    due.forEach((x) => x.fn());

    const byName = (name: string) => this.state.rooms.find((r) => r.name.startsWith(name));
    const crossed = (seconds: number) => previousTick < seconds && tick >= seconds;

    if (crossed(6)) {
      const r = byName("鲜炖牛杂");
      if (r?.status === "reconnecting") {
        this.setStatus(r.liveId, "collecting", "重连成功，耗时 2.4 秒");
        this.pushTimeline("ok", `${r.name} 重连成功`, "耗时 2.4 秒，期间无消息缺口");
        this.notice({ tone: "success", title: `${r.name} 重连成功`, description: "耗时 2.4 秒，期间无消息缺口" });
      }
    }
    if (crossed(10)) {
      const r = byName("茶语轻饮");
      if (r?.status === "connecting") this.setStatus(r.liveId, "collecting", "WebSocket 连接成功，上游已就绪");
    }
    if (crossed(16)) {
      const r = byName("火锅研究所");
      if (r?.status === "probing") this.setStatus(r.liveId, "connecting", "直播页确认当前正在直播");
    }
    if (crossed(19)) {
      const r = byName("火锅研究所");
      if (r?.status === "connecting") this.setStatus(r.liveId, "collecting", "WebSocket 连接成功，上游已就绪");
    }
    if (crossed(45)) {
      const r = byName("早安包子铺");
      if (r?.status === "waiting") {
        this.setStatus(r.liveId, "probing", "轮询到期，开始探测");
        this.later(2, () => {
          this.setStatus(r.liveId, "connecting", "直播页确认当前正在直播");
          this.pushTimeline("info", `${r.name} 开播了`, "探测确认已开播，开始建立连接");
          this.notice({ tone: "info", title: `${r.name} 开播了`, description: "已开始采集，订阅方会收到 status 事件" });
        });
        this.later(4, () => this.setStatus(r.liveId, "collecting", "WebSocket 连接成功，上游已就绪"));
      }
    }
    // 周期性：随机一个直播间短暂重连
    if (tick > 30 && Math.floor(tick / 47) > Math.floor(previousTick / 47)) {
      const live = this.state.rooms.filter((r) => r.status === "collecting" && r.heat > 0.4);
      const r = live.length ? pick(live) : undefined;
      if (r) {
        this.setStatus(r.liveId, "reconnecting", "WebSocket 连接失败，准备重连（1.5 秒后）");
        this.pushLog("WARN", r.liveId, "WebSocket 连接失败，准备重连", "backoff=1.5s");
        this.later(3, () => {
          const cur = this.room(r.liveId);
          if (cur?.status === "reconnecting") {
            this.setStatus(r.liveId, "collecting", "重连成功，上游已就绪");
            this.updateRoom(r.liveId, { reconnects30m: cur.reconnects30m + 1 });
            this.pushTimeline("ok", `${r.name} 重连成功`, "引擎自动重连，耗时约 3 秒");
          }
        });
      }
    }
  }

  private pushTimeline(tone: TimelineItem["tone"], title: string, detail: string) {
    const item: TimelineItem = { id: `tl-${Date.now()}`, at: Date.now(), tone, title, detail };
    this.commit({ ...this.state, timeline: [item, ...this.state.timeline].slice(0, 30) });
  }

  private pushLog(level: LogLine["level"], liveId: string, msg: string, fields?: string) {
    const line: LogLine = { id: this.logId++, at: Date.now(), level, liveId, msg, fields };
    this.commit({ ...this.state, logs: [line, ...this.state.logs].slice(0, 500) });
  }
}

export const sim = new Simulation();

/** 数组元素或对象字段相同即复用旧结果，适合 ID 列表与轻量状态摘要。 */
export function shallowEqual<T>(a: T, b: T): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== "object" || a === null || typeof b !== "object" || b === null) return false;
  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);
  if (aKeys.length !== bKeys.length) return false;
  const aRecord = a as Record<string, unknown>;
  const bRecord = b as Record<string, unknown>;
  return aKeys.every((key) => Object.prototype.hasOwnProperty.call(bRecord, key) && Object.is(aRecord[key], bRecord[key]));
}

/** 缓存同一快照的派生结果；只有所选值改变时才通知 React 重渲染。 */
export function useSim<T>(selector: (s: SimState) => T, isEqual: (a: T, b: T) => boolean = Object.is): T {
  const committed = useRef<{ value: T } | null>(null);
  const getSelection = useMemo(() => {
    let snapshot: SimState | undefined;
    let selected: T;
    return () => {
      const nextSnapshot = sim.getSnapshot();
      if (snapshot === nextSnapshot) return selected;
      const nextSelected = selector(nextSnapshot);
      const previous = snapshot === undefined ? committed.current : { value: selected };
      snapshot = nextSnapshot;
      selected = previous && isEqual(previous.value, nextSelected) ? previous.value : nextSelected;
      return selected;
    };
  }, [selector, isEqual]);
  const value = useSyncExternalStore(sim.subscribe, getSelection);
  useEffect(() => { committed.current = { value }; }, [value]);
  return value;
}

// Vite 热更新时清掉旧模块的定时器及监听，避免叠加多个模拟运行时。
if (import.meta.hot) import.meta.hot.dispose(() => sim.stop());
