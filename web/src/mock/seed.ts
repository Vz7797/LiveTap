import type { MsgType, RoomStatus, Source } from "@/lib/meta";
import type { Account, Client, Counts, Incident, Proxy, Room, RoomEvent, TimelineItem } from "./types";

const now = Date.now();
const MIN = 60_000;

type RoomSeed = [
  liveId: string,
  name: string,
  title: string,
  city: string,
  status: RoomStatus,
  source: Source,
  heat: number,
  online: number,
  tags: string[],
  extra?: Partial<Room>,
];

// 虚构的商家直播间。heat 为每秒模拟消息数基准。
const SEEDS: RoomSeed[] = [
  ["403593241280", "川味小馆·春熙路店", "双人火锅套餐 138 元，今晚限量 200 份", "成都", "collecting", "subscription", 2.6, 3412, ["火锅", "重点客户"]],
  ["516466932480", "老街坊烧烤", "周末烧烤自助 59.9 元起，全城通用", "重庆", "collecting", "subscription", 1.8, 1876, ["烧烤"]],
  ["728193746501", "阿婆手作甜品", "新品芋泥杯 9.9 元秒杀", "杭州", "collecting", "subscription", 0.9, 642, ["甜品"]],
  ["639201847362", "小满咖啡福利社", "咖啡 2 杯 19.9，工作日通用", "上海", "collecting", "subscription", 1.4, 1220, ["咖啡", "连锁"]],
  ["801736492015", "山城串串香", "串串 0.5 元一串，今晚直播间专属", "重庆", "collecting", "subscription", 3.2, 5893, ["火锅", "重点客户"], { proxyId: "px-02" }],
  ["372819465023", "一碗好面", "招牌牛肉面 2 碗 29.9", "兰州", "collecting", "manual", 0.15, 23, ["面馆", "冷清样本"]],
  ["918273645012", "云朵蛋糕旗舰店", "生日蛋糕 6 寸 88 元，同城配送", "广州", "collecting", "subscription", 0.7, 418, ["甜品"], { accountId: "ac-01" }],
  ["264819375026", "夜猫子小龙虾", "麻辣小龙虾 3 斤 168，宵夜档开播", "武汉", "collecting", "subscription", 2.1, 2654, ["夜宵"]],
  ["573920184637", "湘里人家", "家常湘菜 4 人餐 199", "长沙", "collecting", "subscription", 0.5, 287, ["湘菜"]],
  ["690213847561", "鲜炖牛杂·团购专场", "牛杂锅 2-3 人份 99 元", "深圳", "reconnecting", "subscription", 1.1, 905, ["火锅"], { reconnects30m: 2 }],
  ["135792468024", "日料·鮨匠", "午市寿司定食 68 元", "北京", "collecting", "subscription", 0.35, 156, ["日料"]],
  ["246813579035", "茶语轻饮", "果茶第二杯半价", "厦门", "connecting", "subscription", 0.6, 0, ["饮品"]],
  ["357924681046", "早安包子铺", "早餐套餐 6.9 元", "西安", "waiting", "subscription", 0.3, 0, ["早餐"]],
  ["468035792157", "椰林鸡火锅", "椰子鸡 3-4 人餐 218", "海口", "waiting", "subscription", 0.9, 0, ["火锅"]],
  ["579146803268", "阿杰烤肉", "韩式烤肉自助 79.9", "沈阳", "waiting", "manual", 0.8, 0, ["烧烤"]],
  ["680257914379", "江湖酸菜鱼", "酸菜鱼双人餐 88", "南京", "risk", "subscription", 1.2, 0, ["鱼"], { reconnects30m: 4, note: "30 分钟内重建 4 次，已停止自动重建" }],
  ["791368025480", "火锅研究所", "实验室风味锅底上新", "成都", "probing", "test", 1.5, 0, ["火锅", "压测"]],
  ["10293847", "dy_wrongid_99", "—", "—", "notfound", "manual", 0, 0, ["待核对"]],
  ["802479136591", "胖妞麻辣烫", "麻辣烫自选 25 元券", "哈尔滨", "stopped", "manual", 0.4, 0, ["小吃"]],
  ["913580247602", "粤式早茶·茶点小馆", "早茶 4 人套餐 168", "广州", "collecting", "test", 1.0, 733, ["粤菜", "压测"]],
];

const TYPE_SHARE: Record<MsgType, number> = { chat: 0.3, member: 0.34, like: 0.26, social: 0.05, gift: 0.05 };

function seedCounts(perMin: number): Counts {
  return {
    chat: Math.round(perMin * TYPE_SHARE.chat),
    member: Math.round(perMin * TYPE_SHARE.member),
    like: Math.round(perMin * TYPE_SHARE.like),
    social: Math.round(perMin * TYPE_SHARE.social),
    gift: Math.round(perMin * TYPE_SHARE.gift),
  };
}

function seedOnline(online: number, spark: number[]): number[] {
  let v = online * (0.8 + Math.random() * 0.2);
  return spark.map((m, i) => {
    if (m === 0) return 0;
    v = i === spark.length - 1 ? online : Math.max(1, v + (online - v) * 0.12 + (Math.random() - 0.5) * online * 0.05);
    return Math.round(v);
  });
}

function seedSpark(heat: number, status: RoomStatus): number[] {
  const base = heat * 60;
  const out: number[] = [];
  let v = base;
  for (let i = 0; i < 30; i++) {
    v = Math.max(0, v + (Math.random() - 0.5) * base * 0.35);
    v = v * 0.85 + base * 0.15;
    if (status === "reconnecting" && i > 24) v = v * 0.3;
    if (status === "risk") v = i < 12 ? base : i < 18 ? base * 0.2 : 0;
    if (status === "stopped" || status === "notfound" || status === "waiting" || status === "probing" || status === "connecting")
      v = 0;
    out.push(Math.round(v));
  }
  return out;
}

function seedEvents(status: RoomStatus, since: number): RoomEvent[] {
  const ev: RoomEvent[] = [];
  if (status === "notfound") {
    return [{ at: now - 8 * MIN, kind: "probe", text: "直播间不存在或页面未返回有效房间状态" }];
  }
  ev.push({ at: since, kind: "online", text: "直播页确认当前正在直播" });
  ev.push({ at: since + 2400, kind: "ready", text: "WebSocket 连接成功，上游已就绪" });
  if (status === "reconnecting" || status === "risk") {
    ev.push({ at: now - 22 * MIN, kind: "reconnect", text: "WebSocket 连接失败，准备重连（1.5 秒后）" });
    ev.push({ at: now - 22 * MIN + 1800, kind: "ready", text: "重连成功" });
    ev.push({ at: now - 4 * MIN, kind: "handshake_fail", text: "握手失败：上游返回空响应" });
  }
  if (status === "risk") {
    ev.push({ at: now - 3 * MIN, kind: "rebuild", text: "静默超过阈值，旁证连接也收不到消息" });
    ev.push({ at: now - 2 * MIN, kind: "risk", text: "30 分钟内重建 4 次，转为疑似风控，退避 5 分钟" });
  }
  if (status === "collecting" && Math.random() > 0.5) {
    ev.push({ at: now - 41 * MIN, kind: "reconnect", text: "检测到需重连，将稍后尝试" });
    ev.push({ at: now - 41 * MIN + 2600, kind: "ready", text: "重连成功，耗时 2.6 秒" });
  }
  return ev.sort((a, b) => b.at - a.at);
}

const ALL_TYPES: MsgType[] = ["chat", "member", "like", "social", "gift"];

export function seedRooms(): Room[] {
  return SEEDS.map(([liveId, name, title, city, status, source, heat, online, tags, extra], i) => {
    const live = status === "collecting" || status === "reconnecting";
    const since = now - (18 + i * 7) * MIN;
    const perMin = live ? Math.round(heat * 60) : 0;
    const spark = seedSpark(heat, status);
    const minutes: Counts[] = Array.from({ length: 30 }, (_, k) => {
      const f = live ? 0.6 + Math.random() * 0.8 : status === "risk" && k < 14 ? 0.8 : 0;
      return seedCounts(perMin * f);
    });
    return {
      liveId,
      name,
      title,
      city,
      category: "美食团购",
      status,
      source,
      tags,
      engine: "douyinlive",
      sign: "local",
      protocol: "web",
      proxyId: null,
      accountId: null,
      online,
      totalPv: online * (8 + Math.round(Math.random() * 10)),
      heat,
      msgPerMin: perMin,
      reconnects30m: status === "collecting" && Math.random() > 0.7 ? 1 : 0,
      lastMsgAt: live ? now - 1000 : now - 30 * MIN,
      since,
      spark,
      onlineSpark: seedOnline(online, spark),
      counts: {
        chat: Math.round(perMin * 0.3 * 25),
        member: Math.round(perMin * 0.34 * 25),
        like: Math.round(perMin * 0.26 * 25),
        social: Math.round(perMin * 0.05 * 25),
        gift: Math.round(perMin * 0.05 * 25),
      },
      subscribers: source === "subscription" ? (i % 4 === 0 ? 2 : 1) : 0,
      silenceThreshold: Math.max(90, Math.round(240 / Math.max(heat, 0.6))),
      events: seedEvents(status, since),
      enabledTypes: ALL_TYPES,
      minutes,
      ...extra,
    };
  });
}

export const PROXIES: Proxy[] = [
  { id: "px-01", name: "上海电信-01", url: "http://u***@10.8.1.21:3128", protocol: "HTTP", region: "上海", isp: "电信", enabled: true, health: "ok", latency: 38, history: [42, 40, 39, 41, 38, 37, 39, 38], rooms: 3, lastTest: now - 4 * MIN },
  { id: "px-02", name: "重庆联通-住宅", url: "socks5://u***@172.16.4.9:1080", protocol: "SOCKS5", region: "重庆", isp: "联通", enabled: true, health: "ok", latency: 61, history: [70, 64, 66, 59, 62, 60, 63, 61], rooms: 5, lastTest: now - 4 * MIN },
  { id: "px-03", name: "广州移动-02", url: "http://u***@10.8.3.7:8080", protocol: "HTTP", region: "广州", isp: "移动", enabled: true, health: "slow", latency: 412, history: [180, 220, 310, 290, 350, 380, 400, 412], rooms: 1, lastTest: now - 9 * MIN },
  { id: "px-04", name: "北京BGP-备用", url: "socks5://172.16.9.3:1080", protocol: "SOCKS5", region: "北京", isp: "BGP", enabled: false, health: "down", latency: null, history: [55, 58, 0, 0, 0, 0, 0, 0], rooms: 0, lastTest: now - 26 * MIN },
  { id: "px-05", name: "杭州电信-住宅", url: "http://u***@10.8.5.40:3128", protocol: "HTTP", region: "杭州", isp: "电信", enabled: true, health: "ok", latency: 44, history: [48, 46, 45, 44, 47, 45, 43, 44], rooms: 2, lastTest: now - 4 * MIN },
  { id: "px-06", name: "成都移动-03", url: "socks5://u***@172.16.6.18:1080", protocol: "SOCKS5", region: "成都", isp: "移动", enabled: true, health: "unknown", latency: null, history: [], rooms: 0, lastTest: 0 },
];

export const ACCOUNTS: Account[] = [
  { id: "ac-01", note: "小号 A · 美食探店", cookieMask: "sessionid=7f3c****9a1e; ttwid=1%7C****Ab3f", validity: "valid", rooms: ["918273645012"], limit: 5, lastCheck: now - 12 * MIN, addedAt: now - 9 * 24 * 60 * MIN },
  { id: "ac-02", note: "小号 B · 同城生活", cookieMask: "sessionid=a01d****44c2; ttwid=1%7C****Qm8z", validity: "valid", rooms: [], limit: 5, lastCheck: now - 12 * MIN, addedAt: now - 6 * 24 * 60 * MIN },
  { id: "ac-03", note: "小号 C · 备用", cookieMask: "sessionid=5bb2****0f7d; ttwid=1%7C****Lp2w", validity: "limited", rooms: ["680257914379"], limit: 3, lastCheck: now - 40 * MIN, addedAt: now - 20 * 24 * 60 * MIN },
  { id: "ac-04", note: "小号 D · 已过期", cookieMask: "sessionid=c9e4****b210; ttwid=1%7C****Xx01", validity: "expired", rooms: [], limit: 5, lastCheck: now - 3 * 60 * MIN, addedAt: now - 45 * 24 * 60 * MIN },
];

function clientSpark(base: number) {
  return Array.from({ length: 24 }, () => Math.max(0, Math.round(base * (0.6 + Math.random() * 0.8))));
}

export const CLIENTS: Client[] = [
  { id: "cl-01", name: "助播平台 · 生产主站", env: "生产", keyPrefix: "lt_live_8fK2", createdAt: now - 30 * 24 * 60 * MIN, lastSeen: now - 2000, enabled: true, quota: 150, used: 14, whitelist: ["10.0.12.8", "10.0.12.9"], connections: 2, pushPerSec: 38, dropped: 0, queue: 3, spark: clientSpark(38) },
  { id: "cl-02", name: "助播平台 · 预发环境", env: "预发", keyPrefix: "lt_live_Q7vd", createdAt: now - 12 * 24 * 60 * MIN, lastSeen: now - 8000, enabled: true, quota: 20, used: 3, whitelist: ["10.0.20.0/24"], connections: 1, pushPerSec: 6, dropped: 12, queue: 0, spark: clientSpark(6) },
  { id: "cl-03", name: "联调测试 · 本机", env: "测试", keyPrefix: "lt_live_3nTx", createdAt: now - 2 * 24 * 60 * MIN, lastSeen: now - 5 * 60 * 60 * 1000, enabled: false, quota: 5, used: 0, whitelist: [], connections: 0, pushPerSec: 0, dropped: 0, queue: 0, spark: Array(24).fill(0) },
];

export const INCIDENTS: Incident[] = [
  { id: "in-1", at: now - 2 * MIN, level: "warning", title: "风控升级", detail: "江湖酸菜鱼 30 分钟内静默重建 4 次，已转为疑似风控", liveId: "680257914379", acked: false },
  { id: "in-2", at: now - 4 * MIN, level: "warning", title: "单直播间静默", detail: "鲜炖牛杂·团购专场 112 秒无任何消息，旁证连接正常，已重建主连接", liveId: "690213847561", acked: false },
  { id: "in-3", at: now - 26 * MIN, level: "warning", title: "代理失效", detail: "北京BGP-备用 连通性检测失败（连接超时），已自动停用", acked: true },
  { id: "in-4", at: now - 58 * MIN, level: "notice", title: "直播间标识无效", detail: "dy_wrongid_99 解析失败：直播间不存在", liveId: "10293847", acked: true },
  { id: "in-5", at: now - 3 * 60 * MIN, level: "notice", title: "小号失效", detail: "小号 D · 已过期 有效性检测未通过，已从账号池摘除", acked: true },
];

export const TIMELINE: TimelineItem[] = [
  { id: "tl-1", at: now - 1 * MIN, tone: "ok", title: "山城串串香 重连成功", detail: "耗时 2.1 秒，未丢失消息" },
  { id: "tl-2", at: now - 2 * MIN, tone: "warn", title: "江湖酸菜鱼 转为疑似风控", detail: "退避 5 分钟后重新探测" },
  { id: "tl-3", at: now - 4 * MIN, tone: "chat", title: "鲜炖牛杂 静默重建", detail: "112 秒无消息，旁证确认主连接异常" },
  { id: "tl-4", at: now - 9 * MIN, tone: "info", title: "助播平台 · 生产主站 新增订阅", detail: "夜猫子小龙虾（264819375026）" },
  { id: "tl-5", at: now - 17 * MIN, tone: "member", title: "早安包子铺 下播", detail: "转入等待开播，轮询间隔 15 秒" },
  { id: "tl-6", at: now - 31 * MIN, tone: "social", title: "全局设置已滚动应用", detail: "协议画像 web，18 个直播间逐个重建完成" },
];
