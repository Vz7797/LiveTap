import {
  BellRing,
  BookOpen,
  FlaskConical,
  Globe2,
  KeyRound,
  LayoutDashboard,
  type LucideIcon,
  MessagesSquare,
  Settings2,
  Tv,
  UserRoundCog,
} from "lucide-react";
import { useSyncExternalStore } from "react";
import type { Tone } from "@/lib/meta";

export type PageKey =
  | "overview"
  | "rooms"
  | "messages"
  | "proxies"
  | "accounts"
  | "clients"
  | "docs"
  | "lab"
  | "alerts"
  | "settings";

export interface NavItem {
  key: PageKey;
  label: string;
  icon: LucideIcon;
  tone: Tone;
  group: string;
  desc: string;
}

export const NAV: NavItem[] = [
  { key: "overview", label: "总览", icon: LayoutDashboard, tone: "chat", group: "监控", desc: "节点健康、消息速率与最近事件" },
  { key: "rooms", label: "直播间", icon: Tv, tone: "ok", group: "监控", desc: "采集任务的状态、设置与操作" },
  { key: "messages", label: "实时消息", icon: MessagesSquare, tone: "member", group: "监控", desc: "全节点消息流" },
  { key: "proxies", label: "代理池", icon: Globe2, tone: "info", group: "资源", desc: "代理连通性与占用" },
  { key: "accounts", label: "账号池", icon: UserRoundCog, tone: "social", group: "资源", desc: "小号 Cookie 与分配" },
  { key: "clients", label: "接入方", icon: KeyRound, tone: "gift", group: "资源", desc: "业务系统与 API Key" },
  { key: "docs", label: "接入文档", icon: BookOpen, tone: "info", group: "资源", desc: "北向接口与接入指南" },
  { key: "lab", label: "测试实验室", icon: FlaskConical, tone: "like", group: "工具", desc: "容量与风控实测" },
  { key: "alerts", label: "告警与日志", icon: BellRing, tone: "warn", group: "工具", desc: "告警、规则与实时日志" },
  { key: "settings", label: "系统设置", icon: Settings2, tone: "idle", group: "系统", desc: "全局默认、数据保留与版本" },
];

export const NAV_BY_KEY = Object.fromEntries(NAV.map((n) => [n.key, n])) as Record<PageKey, NavItem>;

/* 节点按配置开关的功能。测试实验室会真实连接抖音直播间，正式环境默认关闭，关闭时不显示入口。 */
let labEnabled = true;
export function setFeatures(features?: { lab?: boolean }) {
  const next = features?.lab !== false;
  if (next === labEnabled) return;
  labEnabled = next;
  snapshot = parse();
  listeners.forEach((l) => l());
}
export const isLabEnabled = () => labEnabled;
/** 当前可见的导航项 */
export function visibleNav() {
  return labEnabled ? NAV : NAV.filter((n) => n.key !== "lab");
}


// 地址用普通路径（/overview、/rooms?id=…），不再用 #/。节点对未知的页面路径一律返回后台页面。
// 旧书签里的 #/页面?参数 在加载时换成对应的路径。
function upgradeHashURL() {
  const legacy = window.location.hash.match(/^#\/?([a-z]+)(\?.*)?$/);
  if (legacy) history.replaceState(null, "", `/${legacy[1]}${legacy[2] ?? ""}`);
}
upgradeHashURL();

function parse() {
  const path = window.location.pathname.replace(/^\/+|\/+$/g, "");
  const page = (visibleNav().some((n) => n.key === path) ? path : "overview") as PageKey;
  return { page, params: new URLSearchParams(window.location.search), raw: `${path}${window.location.search}` };
}

let snapshot = parse();
const listeners = new Set<() => void>();
function changed() {
  snapshot = parse();
  listeners.forEach((l) => l());
}
window.addEventListener("popstate", changed);

export function useRoute() {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => snapshot,
  );
}

const urlOf = (page: PageKey, query: URLSearchParams) => {
  const s = query.toString();
  return `/${page}${s ? `?${s}` : ""}`;
};

export function navigate(page: PageKey, params?: Record<string, string | undefined>) {
  const q = new URLSearchParams();
  Object.entries(params ?? {}).forEach(([k, v]) => v !== undefined && q.set(k, v));
  const next = urlOf(page, q);
  if (window.location.pathname + window.location.search !== next) {
    history.pushState(null, "", next);
    changed();
  }
}

/** 只改查询参数，不新增历史记录 */
export function setParams(params: Record<string, string | undefined>) {
  const { page, params: cur } = parse();
  Object.entries(params).forEach(([k, v]) => (v === undefined ? cur.delete(k) : cur.set(k, v)));
  history.replaceState(null, "", urlOf(page, cur));
  changed();
}
