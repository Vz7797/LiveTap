import { useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { api, queryClient } from "./api";
import type { SimState, SimNotice } from "@/mock/sim";
import type { Counts, Room, Account, Client, Proxy, RoomEvent } from "./types";

export interface RuntimeState extends SimState {
  history: { at: number; v: Counts }[];
  rhythm: { today: number[]; yesterday: number[] };
  rollout: { running: boolean; paused: boolean; done: number; total: number; error?: string };
}
const zero = (): Counts => ({ chat: 0, member: 0, like: 0, social: 0, gift: 0 });
const empty: RuntimeState = {
  tick: 0, rooms: [], feed: [], seq: 0, series: [{ at: Date.now(), v: zero() }], totals: zero(), incidents: [], timeline: [], logs: [], proxies: [], accounts: [], clients: [],
  history: [], rhythm: { today: Array(48).fill(0), yesterday: Array(48).fill(0) }, rollout: { running: false, paused: false, done: 0, total: 0 },
  node: { name: "", version: "", engineVersion: "", bootId: "", startedAt: Date.now(), cpu: 0, memMB: 0, memLimitMB: 0, goroutines: 0, upKBs: 0, downKBs: 0, conns: 0, reqPerMin: 0, reqBudgetPerSec: 5, maxRooms: 200, health: 100, cpuHist: [], memHist: [], netHist: [] },
};
/**
 * 统一入口校正：后端或旧版本可能把空列表编码成 null（2026-10-16 发现订阅添加的房间
 * "tags": null 导致整个后台白屏）。所有列表字段在进入界面前补成 []，界面代码可以放心遍历。
 */
const arr = <T,>(v: T[] | null | undefined): T[] => (Array.isArray(v) ? v : []);
function normalize(value: RuntimeState): RuntimeState {
  const v = { ...empty, ...value };
  return {
    ...v,
    rooms: arr(v.rooms).map((r) => ({ ...r, tags: arr(r.tags), events: arr(r.events), spark: arr(r.spark), onlineSpark: arr(r.onlineSpark), minutes: arr(r.minutes), enabledTypes: arr(r.enabledTypes), subscriberClients: arr(r.subscriberClients) })),
    feed: arr(v.feed), series: arr(v.series).length ? v.series : empty.series, incidents: arr(v.incidents), timeline: arr(v.timeline), logs: arr(v.logs),
    proxies: arr(v.proxies), accounts: arr(v.accounts), clients: arr(v.clients), history: arr(v.history),
    node: { ...empty.node, ...v.node, cpuHist: arr(v.node?.cpuHist), memHist: arr(v.node?.memHist), netHist: arr(v.node?.netHist), upHist: arr(v.node?.upHist) },
  };
}
const listeners = new Set<() => void>();
const notices = new Set<(notice: SimNotice) => void>();
export function notifyError(error: unknown) { for (const fn of notices) fn({ tone: "error", title: error instanceof Error ? error.message : "操作失败，请重试" }); }
export async function refresh() { const snapshot = await api<RuntimeState>("/snapshot"); queryClient.setQueryData(["snapshot"], snapshot); sim.commit(snapshot); }
async function action(path: string, method: string, body?: unknown) { try { await api(path, method, body); await refresh(); return true; } catch (e) { notifyError(e); return false; } }
const unavailable = () => notifyError(new Error("此功能将在后续阶段接入"));
// The existing presentation components retain their selector API. Every write
// here goes through authenticated server endpoints; no simulated ticks run.
export const sim = {
  state: empty,
  commit(value: RuntimeState) { this.state = normalize(value); listeners.forEach((fn) => fn()); },
  getSnapshot: () => sim.state,
  subscribe(fn: () => void) { listeners.add(fn); return () => { listeners.delete(fn); }; },
  onNotice(fn: (n: SimNotice) => void) { notices.add(fn); return () => { notices.delete(fn); }; },
  room: (id: string) => sim.state.rooms.find((r) => r.liveId === id),
  reconnect: (id: string) => action(`/rooms/${encodeURIComponent(id)}/actions`, "POST", { action: "reconnect" }),
  pause: (id: string) => action(`/rooms/${encodeURIComponent(id)}/actions`, "POST", { action: "pause" }),
  resume: (id: string) => action(`/rooms/${encodeURIComponent(id)}/actions`, "POST", { action: "resume" }),
  async remove(ids: string[]) { for (const id of ids) if (!await action(`/rooms/${encodeURIComponent(id)}`, "DELETE")) return false; return true; },
  async addRooms(ids: string[], tags: string[] = []) {
    try {
      const result = await api<{ results: { ok: boolean; created?: boolean; error?: string }[] }>("/rooms", "POST", { live_ids: ids, tags });
      await refresh(); const failed = result.results.filter((x) => !x.ok); if (failed.length) notifyError(new Error(`${failed.length} 个未能添加：${failed[0].error}`));
      return result.results.filter((x) => x.ok && x.created).length;
    } catch (e) { notifyError(e); return 0; }
  },
  updateRoom(id: string, patch: Partial<Room>, _event?: Omit<RoomEvent, "at">) { return action(`/rooms/${encodeURIComponent(id)}`, "PATCH", { note: patch.note, tags: patch.tags }); },
  ackIncident: (_id: string) => unavailable(), ackAll: unavailable,
  setProxies: (_v: Proxy[]) => unavailable(), setAccounts: (_v: Account[]) => unavailable(), setClients: (_v: Client[]) => unavailable(),
};
export function shallowEqual<T>(a: T, b: T): boolean { if (Object.is(a, b)) return true; if (typeof a !== "object" || !a || typeof b !== "object" || !b) return false; const keys = Object.keys(a); return keys.length === Object.keys(b).length && keys.every((k) => Object.is((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k])); }
export function useSim<T>(selector: (state: RuntimeState) => T, isEqual: (a: T, b: T) => boolean = Object.is): T {
  const committed = useRef<{ value: T } | null>(null);
  const getSelection = useMemo(() => { let snapshot: RuntimeState | undefined; let selected: T; return () => { const next = sim.getSnapshot(); if (snapshot === next) return selected; const value = selector(next); const previous = snapshot ? { value: selected } : committed.current; snapshot = next; selected = previous && isEqual(previous.value, value) ? previous.value : value; return selected; }; }, [selector, isEqual]);
  const value = useSyncExternalStore(sim.subscribe, getSelection); useEffect(() => { committed.current = { value }; }, [value]); return value;
}
