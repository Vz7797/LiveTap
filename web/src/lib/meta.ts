import {
  AlertOctagon,
  CircleHelp,
  CircleSlash,
  Gift,
  Heart,
  type LucideIcon,
  MessageSquareText,
  MoonStar,
  Power,
  Radar,
  RefreshCw,
  ShieldAlert,
  Unplug,
  UserPlus,
  Waves,
  DoorOpen,
} from "lucide-react";

/**
 * 后端取值以后端为准。真实数据里出现前端表中没有的取值时（例如 2026-10-16 实验室房间的
 * 来源是 "test"，而前端写成了 "lab"），直接查表会得到 undefined 并让整页白屏。
 * 所有对照表都包一层兜底：未知取值显示为灰色原文，不再崩溃。
 */
export function withFallback<K extends string, V>(table: Record<K, V>, fallback: (key: string) => V): Record<K, V> {
  return new Proxy(table, {
    get(target, key, receiver) {
      if (typeof key === "string" && !Object.prototype.hasOwnProperty.call(target, key)) return fallback(key);
      return Reflect.get(target, key, receiver);
    },
  });
}

/** 设计文档 11.2 的功能色名称，对应 index.css 中的 --c-* / --ink-* 令牌 */
export type Tone =
  | "chat"
  | "member"
  | "like"
  | "social"
  | "gift"
  | "info"
  | "ok"
  | "warn"
  | "err"
  | "idle";

/** 统一事件格式中的 type（设计文档 7.3） */
export type MsgType = "chat" | "member" | "like" | "social" | "gift";

export const MSG_TYPES: MsgType[] = ["chat", "member", "like", "social", "gift"];

export const TYPE_META: Record<MsgType, { label: string; tone: Tone; icon: LucideIcon }> = withFallback<MsgType, { label: string; tone: Tone; icon: LucideIcon }>({
  chat: { label: "弹幕", tone: "chat", icon: MessageSquareText },
  member: { label: "进场", tone: "member", icon: DoorOpen },
  like: { label: "点赞", tone: "like", icon: Heart },
  social: { label: "关注", tone: "social", icon: UserPlus },
  gift: { label: "礼物", tone: "gift", icon: Gift },
}, (key) => ({ label: key || "其他", tone: "idle", icon: MessageSquareText }));

/** 采集任务状态（设计文档 5.2） */
export type RoomStatus =
  | "probing"
  | "waiting"
  | "connecting"
  | "reconnecting"
  | "collecting"
  | "risk"
  | "notfound"
  | "stopped"
  | "ended";

export const STATUS_META: Record<
  RoomStatus,
  { label: string; tone: Tone; icon: LucideIcon; breathe?: boolean; hint: string }
> = withFallback<RoomStatus, { label: string; tone: Tone; icon: LucideIcon; breathe?: boolean; hint: string }>({
  collecting: { label: "采集中", tone: "ok", icon: Waves, breathe: true, hint: "正常接收消息" },
  waiting: { label: "等待开播", tone: "idle", icon: MoonStar, hint: "按轮询间隔检查是否开播" },
  probing: { label: "探测中", tone: "info", icon: Radar, hint: "确认是否开播" },
  connecting: { label: "连接中", tone: "chat", icon: Unplug, hint: "正在建立连接" },
  reconnecting: { label: "重连中", tone: "chat", icon: RefreshCw, hint: "引擎内部指数退避重连" },
  risk: { label: "状态待确认", tone: "warn", icon: ShieldAlert, hint: "上游暂时没给出明确状态（多见于刚下播）或连续失败，退避重试；节点被限制时另有冷却提示" },
  notfound: { label: "不存在", tone: "err", icon: AlertOctagon, hint: "直播间标识无效，任务已终止" },
  stopped: { label: "已停止", tone: "idle", icon: CircleSlash, hint: "已主动停止" },
  ended: { label: "已下播", tone: "idle", icon: Power, hint: "确认下播后已停止检测，不再向抖音发请求" },
}, (key) => ({ label: key || "未知状态", tone: "idle", icon: CircleHelp, hint: "后台返回了界面暂不认识的状态" }));

export const STATUS_ORDER: RoomStatus[] = [
  "collecting",
  "connecting",
  "reconnecting",
  "probing",
  "waiting",
  "risk",
  "notfound",
  "ended",
  "stopped",
];

export const isAbnormal = (s: RoomStatus) => s === "risk" || s === "reconnecting" || s === "notfound";

/** 与后端一致：manual 手动添加、subscription 接入方订阅、test 测试实验室创建 */
export type Source = "subscription" | "manual" | "test";
export const SOURCE_META: Record<Source, { label: string; tone: Tone }> = withFallback<Source, { label: string; tone: Tone }>({
  subscription: { label: "订阅", tone: "info" },
  manual: { label: "手动", tone: "social" },
  test: { label: "测试", tone: "gift" },
}, (key) => ({ label: key || "未知来源", tone: "idle" }));

/** tone → CSS 变量，用于 SVG 填充等需要原始颜色的地方 */
export const toneVar = (tone: Tone) => `var(--c-${tone})`;
export const inkVar = (tone: Tone) => `var(--ink-${tone})`;

/** 给每个直播间一个稳定的点缀色，用于迷你曲线等，让列表不再单一色调 */
const ROOM_TONES: Tone[] = ["chat", "member", "like", "social", "gift", "info", "ok"];
export function roomTone(liveId: string): Tone {
  let h = 0;
  for (let i = 0; i < liveId.length; i++) h = (h * 31 + liveId.charCodeAt(i)) | 0;
  return ROOM_TONES[Math.abs(h) % ROOM_TONES.length];
}
