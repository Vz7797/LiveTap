import { useMemo, useSyncExternalStore } from "react";
import { api } from "./api";
import { sim } from "./runtime";
import type { FeedMsg } from "./types";

/**
 * 实时消息的本地缓冲。推送里只带全站最新 50 条，进场、点赞往往占大半，按类型筛选时
 * 剩下的弹幕很少。这里把每次推送的消息按类型分别累积（每类最多 PER_TYPE 条），
 * 筛选时再从节点内存补齐（GET /feed，每个直播间保留最近 200 条），列表就能填满。
 */
const PER_TYPE = 300;
const buckets = new Map<string, FeedMsg[]>();
const seen = new Set<string>();
let version = 0;
const listeners = new Set<() => void>();

const keyOf = (m: FeedMsg) => `${m.liveId}:${m.id || m.seq}`;

export function mergeFeed(items: FeedMsg[]) {
  let changed = false;
  for (const m of items) {
    if (!m || !m.type) continue;
    const k = keyOf(m);
    if (seen.has(k)) continue;
    seen.add(k);
    const list = buckets.get(m.type) ?? [];
    list.push(m);
    buckets.set(m.type, list);
    changed = true;
  }
  if (!changed) return;
  for (const [type, list] of buckets) {
    list.sort((a, b) => b.seq - a.seq);
    if (list.length > PER_TYPE) {
      for (const m of list.splice(PER_TYPE)) seen.delete(keyOf(m));
    }
    buckets.set(type, list);
  }
  version++;
  listeners.forEach((fn) => fn());
}

/** 从节点内存补齐某些类型最近的消息（筛选切换时调用） */
export async function seedFeed(types: string[]) {
  try {
    const items = await api<FeedMsg[]>(`/feed?${new URLSearchParams({ types: types.join(","), limit: String(PER_TYPE) })}`);
    if (Array.isArray(items)) mergeFeed(items);
  } catch {
    // 补齐失败不影响实时推送，下次推送仍会累积
  }
}

// 消息序号是节点内存计数，重启后从头开始。页面开着时节点重启，旧消息序号都比新消息大，
// 按序号截断会把新消息全部挤掉、列表停住；所以启动标识一变就清空缓冲。
let bootId = "";
function onPush() {
  const boot = sim.state.node.bootId;
  if (boot && boot !== bootId) {
    if (bootId) {
      buckets.clear();
      seen.clear();
      version++;
      listeners.forEach((fn) => fn());
    }
    bootId = boot;
  }
  mergeFeed(sim.state.feed);
}

// 每次推送都并入缓冲（模块加载后即开始累积）
onPush();
sim.subscribe(onPush);

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

/** 缓冲中的全部消息，按时间倒序 */
export function useFeedBuffer(): FeedMsg[] {
  const v = useSyncExternalStore(subscribe, () => version);
  return useMemo(() => [...buckets.values()].flat().sort((a, b) => b.seq - a.seq), [v]);
}
