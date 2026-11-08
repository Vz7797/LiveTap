import type { RowSelectionState } from "@tanstack/react-table";
import { Filter, LayoutGrid, Pause, RefreshCw, Rows3, Tag as TagIcon, Trash2, Tv, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { memo, useCallback, useMemo, useState } from "react";
import { useProgressiveCount } from "@/lib/hooks/use-progressive-count";
import { setParams, useRoute } from "@/app/router";
import { useToast } from "@/app/toast";
import { EmptyState } from "@/components/illustrations";
import { PageHeader, PillButton, Portal } from "@/components/kit";
import { SearchInput, Segmented } from "@/components/kit/form";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SOURCE_META, isAbnormal, type RoomStatus, type Source } from "@/lib/meta";
import { cn } from "@/lib/utils";
import { sim, useSim } from "@/data/runtime";
import type { Room } from "@/data/types";
import { AddRoomButton } from "./AddRoomDialog";
import { RoomCard } from "./RoomCard";
import { RoomPanel } from "./RoomPanel";
import { RoomTable } from "./RoomTable";

type StatusFilter = "all" | "collecting" | "waiting" | "abnormal" | "ended" | "stopped";

const STATUS_GROUPS: Record<StatusFilter, (s: RoomStatus) => boolean> = {
  all: () => true,
  collecting: (s) => s === "collecting" || s === "connecting",
  waiting: (s) => s === "waiting" || s === "probing",
  abnormal: (s) => isAbnormal(s),
  ended: (s) => s === "ended",
  stopped: (s) => s === "stopped",
};

type DirectoryRoom = Pick<Room, "liveId" | "name" | "city" | "status" | "source" | "tags">;
function sameDirectory(a: DirectoryRoom[], b: DirectoryRoom[]) {
  return a.length === b.length && a.every((room, i) => {
    const next = b[i];
    return room.liveId === next.liveId && room.name === next.name && room.city === next.city &&
      room.status === next.status && room.source === next.source && room.tags === next.tags;
  });
}

const ConnectedRoomCard = memo(function ConnectedRoomCard({ liveId, onOpen }: { liveId: string; onOpen: (id: string) => void }) {
  const room = useSim((s) => s.rooms.find((r) => r.liveId === liveId));
  return room ? <RoomCard room={room} onOpen={onOpen} /> : null;
});

function ConnectedRoomPanel({ liveId, onClose }: { liveId: string; onClose: () => void }) {
  const room = useSim((s) => s.rooms.find((r) => r.liveId === liveId));
  return room ? <RoomPanel room={room} onClose={onClose} fromCard={false} /> : null;
}

function ConnectedRoomTable({ ids, onOpen, selection, onSelectionChange }: {
  ids: string[]; onOpen: (id: string) => void;
  selection: RowSelectionState; onSelectionChange: (selection: RowSelectionState) => void;
}) {
  const rooms = useSim((s) => s.rooms.filter((r) => ids.includes(r.liveId)));
  return <RoomTable rooms={rooms} onOpen={onOpen} selection={selection} onSelectionChange={onSelectionChange} />;
}

export function RoomsPage() {
  const { params } = useRoute();
  const toast = useToast();
  const rooms = useSim((s) => s.rooms.map(({ liveId, name, city, status, source, tags }) => ({ liveId, name, city, status, source, tags })), sameDirectory);

  const status = (params.get("status") as StatusFilter) || "all";
  const view = params.get("view") === "table" ? "table" : "card";
  const openId = params.get("room");
  const addOpen = params.get("add") === "1";

  const [q, setQ] = useState("");
  const [sources, setSources] = useState<Source[]>([]);
  const [tags, setTags] = useState<string[]>([]);
  const [selection, setSelection] = useState<RowSelectionState>({});

  const allTags = useMemo(() => [...new Set(rooms.flatMap((r) => r.tags))], [rooms]);

  const counts = useMemo(() => {
    const c = {} as Record<StatusFilter, number>;
    (Object.keys(STATUS_GROUPS) as StatusFilter[]).forEach((k) => (c[k] = rooms.filter((r) => STATUS_GROUPS[k](r.status)).length));
    return c;
  }, [rooms]);

  const filtered = useMemo(() => {
    const kw = q.trim().toLowerCase();
    return rooms.filter(
      (r) =>
        STATUS_GROUPS[status](r.status) &&
        (sources.length === 0 || sources.includes(r.source)) &&
        (tags.length === 0 || tags.some((t) => r.tags.includes(t))) &&
        (!kw || r.name.toLowerCase().includes(kw) || r.liveId.includes(kw) || r.city.includes(kw)),
    );
  }, [rooms, status, sources, tags, q]);
  const shownCount = useProgressiveCount(filtered.length);

  const openRoom = rooms.find((r) => r.liveId === openId) ?? null;
  const open = useCallback((id: string) => setParams({ room: id }), []);
  const close = useCallback(() => setParams({ room: undefined }), []);

  const selectedIds = Object.keys(selection).filter((k) => selection[k]);
  const activeFilters = sources.length + tags.length;

  return (
    <>
      <PageHeader
        icon={Tv}
        tone="ok"
        title="直播间"
        desc={
          <>
            共 <span className="num text-foreground">{rooms.length}</span> 个采集任务，
            <span className="num text-ink-ok">{counts.collecting}</span> 个正在采集
          </>
        }
        actions={<AddRoomButton open={addOpen} onOpenChange={(v) => setParams({ add: v ? "1" : undefined })} />}
      />

      {/* 工具栏 */}
      <div className="mb-4 flex flex-col gap-3 rounded-3xl border bg-card p-2 lg:flex-row lg:items-center">
        <Segmented<StatusFilter>
          value={status}
          onChange={(v) => setParams({ status: v === "all" ? undefined : v })}
          className="min-w-0"
          options={[
            { value: "all", label: "全部", count: counts.all },
            { value: "collecting", label: "采集中", count: counts.collecting },
            { value: "waiting", label: "等待开播", count: counts.waiting },
            { value: "abnormal", label: "异常", count: counts.abnormal },
            { value: "ended", label: "已下播", count: counts.ended },
            { value: "stopped", label: "已停止", count: counts.stopped },
          ]}
        />
        <div className="flex min-w-0 flex-1 items-center gap-2 lg:justify-end">
          <SearchInput value={q} onChange={setQ} placeholder="名称、直播间号、城市" className="min-w-0 flex-1 lg:max-w-[260px]" />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className={cn(
                  "inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-full border px-3 text-sm transition-colors hover:bg-accent",
                  activeFilters > 0 && "tint-chat border-transparent",
                )}
              >
                <Filter className="size-4" />
                <span className="hidden sm:inline">筛选</span>
                {activeFilters > 0 && <span className="num text-xs">{activeFilters}</span>}
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              <DropdownMenuLabel className="text-xs text-muted-foreground">来源</DropdownMenuLabel>
              {(Object.keys(SOURCE_META) as Source[]).map((s) => (
                <DropdownMenuCheckboxItem
                  key={s}
                  checked={sources.includes(s)}
                  onCheckedChange={(v) => setSources((cur) => (v ? [...cur, s] : cur.filter((x) => x !== s)))}
                  onSelect={(e) => e.preventDefault()}
                >
                  {SOURCE_META[s].label}
                </DropdownMenuCheckboxItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuLabel className="text-xs text-muted-foreground">引擎</DropdownMenuLabel>
              <DropdownMenuCheckboxItem checked onSelect={(e) => e.preventDefault()}>
                A · douyinLive
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem disabled>B · 浏览器（二期）</DropdownMenuCheckboxItem>
              <DropdownMenuSeparator />
              <DropdownMenuLabel className="text-xs text-muted-foreground">标签</DropdownMenuLabel>
              {allTags.map((t) => (
                <DropdownMenuCheckboxItem
                  key={t}
                  checked={tags.includes(t)}
                  onCheckedChange={(v) => setTags((cur) => (v ? [...cur, t] : cur.filter((x) => x !== t)))}
                  onSelect={(e) => e.preventDefault()}
                >
                  {t}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Segmented<"card" | "table">
            value={view}
            onChange={(v) => setParams({ view: v === "card" ? undefined : v })}
            options={[
              { value: "card", label: <span className="sr-only sm:not-sr-only">卡片</span>, icon: LayoutGrid },
              { value: "table", label: <span className="sr-only sm:not-sr-only">表格</span>, icon: Rows3 },
            ]}
          />
        </div>
      </div>

      <AnimatePresence initial={false}>
        {activeFilters > 0 && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="overflow-hidden"
          >
            <div className="mb-4 flex flex-wrap items-center gap-2 px-1">
              {sources.map((s) => (
                <FilterChip key={s} onRemove={() => setSources((c) => c.filter((x) => x !== s))}>
                  来源：{SOURCE_META[s].label}
                </FilterChip>
              ))}
              {tags.map((t) => (
                <FilterChip key={t} onRemove={() => setTags((c) => c.filter((x) => x !== t))}>
                  <TagIcon className="size-3" />
                  {t}
                </FilterChip>
              ))}
              <button
                type="button"
                onClick={() => {
                  setSources([]);
                  setTags([]);
                }}
                className="cursor-pointer text-xs text-muted-foreground hover:text-foreground"
              >
                清除全部
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {filtered.length === 0 ? (
        <div className="rounded-3xl border bg-card">
          <EmptyState title="没有符合条件的直播间" desc="换个筛选条件试试，或者点右上角「添加直播间」" tone="ok" />
        </div>
      ) : view === "card" ? (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 min-[1920px]:grid-cols-5 min-[2240px]:grid-cols-6">
            {filtered.slice(0, shownCount).map((r) => (
              <div key={r.liveId} className={cn("transition-opacity duration-150", openId === r.liveId && "opacity-30")} inert={openId === r.liveId} aria-hidden={openId === r.liveId || undefined}>
                <ConnectedRoomCard liveId={r.liveId} onOpen={open} />
              </div>
            ))}
          </div>
          <Portal>
            <AnimatePresence>{openRoom && <ConnectedRoomPanel key={openRoom.liveId} liveId={openRoom.liveId} onClose={close} />}</AnimatePresence>
          </Portal>
        </>
      ) : (
        <>
          <ConnectedRoomTable ids={filtered.map((r) => r.liveId)} onOpen={open} selection={selection} onSelectionChange={setSelection} />
          <Portal>
            <AnimatePresence>{openRoom && <ConnectedRoomPanel key={openRoom.liveId} liveId={openRoom.liveId} onClose={close} />}</AnimatePresence>
          </Portal>
        </>
      )}

      {/* 批量操作条 */}
      <Portal>
      <AnimatePresence>
        {view === "table" && selectedIds.length > 0 && (
          <motion.div
            initial={{ y: 80, opacity: 0, x: "-50%" }}
            animate={{ y: 0, opacity: 1, x: "-50%" }}
            exit={{ y: 80, opacity: 0, x: "-50%" }}
            transition={{ type: "tween", duration: 0.18 }}
            className="fixed bottom-6 left-1/2 z-50 flex items-center gap-1 rounded-full border border-border-strong bg-popover/95 p-1.5 pl-4 shadow-2xl"
          >
            <span className="mr-2 text-sm whitespace-nowrap">
              已选 <span className="num font-semibold text-ink-brand">{selectedIds.length}</span> 个
            </span>
            <PillButton
              tone="ghost"
              icon={RefreshCw}
              className="h-8 px-3"
              onClick={async () => {
                const results = await Promise.all(selectedIds.map((id) => sim.reconnect(id)));
                toast.show({ title: `已提交 ${results.filter(Boolean).length} 个重连任务`, description: "实际结果请查看直播间状态", status: "info" });
                setSelection({});
              }}
            >
              <span className="hidden sm:inline">重连</span>
            </PillButton>
            <PillButton
              tone="ghost"
              icon={Pause}
              className="h-8 px-3"
              onClick={async () => {
                const results = await Promise.all(selectedIds.map((id) => sim.pause(id)));
                toast.show({ title: `已提交 ${results.filter(Boolean).length} 个暂停任务`, status: "info" });
                setSelection({});
              }}
            >
              <span className="hidden sm:inline">暂停</span>
            </PillButton>
            <PillButton
              tone="danger"
              icon={Trash2}
              className="h-8 px-3"
              onClick={async () => {
                if (!await sim.remove(selectedIds)) return;
                toast.show({ title: `已移除 ${selectedIds.length} 个直播间`, status: "success" });
                setSelection({});
              }}
            >
              <span className="hidden sm:inline">移除</span>
            </PillButton>
            <button
              type="button"
              aria-label="取消选择"
              onClick={() => setSelection({})}
              className="ml-1 inline-flex size-8 cursor-pointer items-center justify-center rounded-full text-muted-foreground hover:bg-accent"
            >
              <X className="size-4" />
            </button>
          </motion.div>
        )}
      </AnimatePresence>
      </Portal>
    </>
  );
}

function FilterChip({ children, onRemove }: { children: React.ReactNode; onRemove: () => void }) {
  return (
    <motion.span layout className="inline-flex h-7 items-center gap-1 rounded-full tint-chat pr-1 pl-2.5 text-xs font-medium">
      {children}
      <button type="button" onClick={onRemove} className="inline-flex size-5 cursor-pointer items-center justify-center rounded-full hover:bg-black/10" aria-label="移除筛选">
        <X className="size-3" />
      </button>
    </motion.span>
  );
}

