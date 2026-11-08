import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import type { Counts, Room } from "./types";

/**
 * 按需数据（docs/design.md 修订记录 v0.18）：2.5 秒一次的推送只带卡片需要的精简字段，
 * 房间详情面板和分钟历史在用到时才单独获取，避免慢线路被推送占满带宽。
 */

const DETAIL_KEYS = ["minutes", "options", "overrides", "enabledTypes", "diagnostics", "counts", "events", "subscriptions"] as const;

/** 打开详情面板期间每 5 秒刷新一次；其余字段始终以推送里的最新值为准。 */
export function useRoomDetail(room: Room): Room {
  const detail = useQuery({
    queryKey: ["room-detail", room.liveId],
    queryFn: () => api<Partial<Room>>(`/rooms/${encodeURIComponent(room.liveId)}/detail`),
    refetchInterval: 5000,
    staleTime: 4000,
  });
  const d = detail.data as Record<string, unknown> | undefined;
  if (!d) return { ...room, minutes: room.minutes ?? [] };
  const merged = { ...room } as unknown as Record<string, unknown>;
  for (const key of DETAIL_KEYS) {
    const value = d[key];
    if (value !== undefined && value !== null) merged[key] = value;
  }
  return normalizeDetail(merged as unknown as Room);
}

function normalizeDetail(merged: Room): Room {
  merged.minutes = Array.isArray(merged.minutes) ? merged.minutes : [];
  merged.events = Array.isArray(merged.events) ? merged.events : [];
  return merged;
}

/** 全节点逐分钟历史，只在查看 1 小时 / 24 小时图表时获取，每分钟最多刷新一次。 */
export function useHistory(enabled: boolean) {
  const history = useQuery({
    queryKey: ["history"],
    queryFn: () => api<{ at: number; v: Counts }[]>("/history"),
    enabled,
    staleTime: 55_000,
    refetchInterval: enabled ? 60_000 : false,
  });
  return Array.isArray(history.data) ? history.data : [];
}
