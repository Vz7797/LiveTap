import {
  type ColumnDef,
  type RowSelectionState,
  type SortingState,
  createSortedRowModel,
  flexRender,
  rowSelectionFeature,
  rowSortingFeature,
  sortFns,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown, Globe2, UserRound } from "lucide-react";
import { useMemo, useState } from "react";
import { Sparkline } from "@/components/charts";
import { Avatar, Num, StatusPill, Tag } from "@/components/kit";
import { Checkbox } from "@/components/ui/checkbox";
import { fmtAgo } from "@/lib/format";
import { SOURCE_META, STATUS_META, STATUS_ORDER, isFinal, isLive } from "@/lib/meta";
import { cn } from "@/lib/utils";
import { usePool } from "@/components/resource-pool";
import type { Room } from "@/data/types";
import { DouyinLink } from "@/components/kit/douyin-link";

// Table v9 只装用到的功能：排序和多选。放在模块级，避免每次渲染重建行模型。
const features = tableFeatures({
  rowSortingFeature,
  rowSelectionFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns,
});

export function RoomTable({
  rooms,
  onOpen,
  selection,
  onSelectionChange,
}: {
  rooms: Room[];
  onOpen: (id: string) => void;
  selection: RowSelectionState;
  onSelectionChange: (s: RowSelectionState) => void;
}) {
  const [sorting, setSorting] = useState<SortingState>([]);
  // 快照不带资源池，名称从代理 / 小号接口取；“custom” 表示房间单独填写的代理或 Cookie
  const proxies = usePool("proxy").data;
  const accounts = usePool("account").data;
  const proxyNames = useMemo<Record<string, string>>(() => ({ custom: "单独指定的代理", ...Object.fromEntries((proxies ?? []).map((p) => [p.id, p.name])) }), [proxies]);
  const accountNames = useMemo<Record<string, string>>(() => ({ custom: "单独指定的 Cookie", ...Object.fromEntries((accounts ?? []).map((a) => [a.id, a.name])) }), [accounts]);

  const columns = useMemo<ColumnDef<typeof features, Room>[]>(
    () => [
      {
        id: "select",
        header: ({ table }) => (
          <Checkbox
            aria-label="全选"
            checked={table.getIsAllRowsSelected() ? true : table.getIsSomeRowsSelected() ? "indeterminate" : false}
            onCheckedChange={(v) => table.toggleAllRowsSelected(!!v)}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            aria-label="选择"
            checked={row.getIsSelected()}
            onCheckedChange={(v) => row.toggleSelected(!!v)}
            onClick={(e) => e.stopPropagation()}
          />
        ),
      },
      {
        id: "name",
        header: "直播间",
        accessorFn: (r) => r.name,
        cell: ({ row: { original: r } }) => (
          <div className="flex min-w-[200px] items-center gap-3">
            <Avatar name={r.name} src={r.avatar} seed={r.liveId} size={34} live={isLive(r.status)} dim={isFinal(r.status)} />
            <div className="min-w-0">
              <div className="truncate font-medium">{r.name}</div>
              <div className="num text-[11px] text-muted-foreground">{r.liveId}</div>
            </div>
            <DouyinLink liveId={r.liveId} className="ml-auto" />
          </div>
        ),
      },
      {
        id: "status",
        header: "状态",
        accessorFn: (r) => STATUS_ORDER.indexOf(r.status),
        cell: ({ row: { original: r } }) => <StatusPill status={r.status} size="sm" />,
      },
      {
        id: "online",
        header: "在线",
        accessorFn: (r) => r.online,
        cell: ({ row: { original: r } }) => (r.online > 0 ? <Num value={r.online} className="font-medium" /> : <span className="text-faint">—</span>),
      },
      {
        id: "rate",
        header: "条/分",
        accessorFn: (r) => r.msgPerMin,
        cell: ({ row: { original: r } }) => (
          <div className="flex items-center gap-3">
            <span className="w-12 text-right">
              {isLive(r.status) ? <Num value={r.msgPerMin} className="font-medium" /> : <span className="text-faint">—</span>}
            </span>
            <Sparkline data={r.spark} tone={isLive(r.status) ? "idle" : STATUS_META[r.status].tone} height={26} className="w-24" strokeWidth={1.5} />
          </div>
        ),
      },
      {
        id: "reconnects",
        header: "重连",
        accessorFn: (r) => r.reconnects30m,
        cell: ({ row: { original: r } }) => (
          <span className={cn("num font-medium", r.reconnects30m >= 3 && "text-ink-warn")}>{r.reconnects30m}</span>
        ),
      },
      {
        id: "source",
        header: "来源",
        accessorFn: (r) => r.source,
        cell: ({ row: { original: r } }) => <Tag tone={SOURCE_META[r.source].tone}>{SOURCE_META[r.source].label}</Tag>,
      },
      {
        id: "route",
        header: "代理 / 账号",
        enableSorting: false,
        cell: ({ row: { original: r } }) => (
          <div className="flex max-w-[180px] flex-col gap-0.5 text-xs text-muted-foreground">
            {r.proxyId && (
              <span className="inline-flex items-center gap-1 truncate">
                <Globe2 className="size-3.5 shrink-0 text-ink-info" />
                {proxyNames[r.proxyId] ?? r.proxyId}
              </span>
            )}
            {r.accountId && (
              <span className="inline-flex items-center gap-1 truncate">
                <UserRound className="size-3.5 shrink-0 text-ink-social" />
                {accountNames[r.accountId] ?? r.accountId}
              </span>
            )}
            {!r.proxyId && !r.accountId && "直连 · 游客"}
          </div>
        ),
      },
      {
        id: "last",
        header: "最近消息",
        accessorFn: (r) => r.lastMsgAt,
        cell: ({ row: { original: r } }) => (
          <span className="text-xs text-muted-foreground">{isLive(r.status) ? fmtAgo(r.lastMsgAt) : "—"}</span>
        ),
      },
    ],
    [proxyNames, accountNames],
  );

  const table = useTable({
    features,
    data: rooms,
    columns,
    state: { sorting, rowSelection: selection },
    getRowId: (r) => r.liveId,
    onSortingChange: setSorting,
    onRowSelectionChange: (u) => onSelectionChange(typeof u === "function" ? u(selection) : u),
  });

  return (
    <div className="overflow-hidden rounded-3xl border bg-card shadow-[var(--shadow-card)]">
      <div className="scroll-area overflow-y-hidden pb-1">
        <table className="w-full min-w-[960px] text-sm">
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b">
                {hg.headers.map((h) => {
                  const sortable = h.column.getCanSort() && h.id !== "select";
                  const dir = h.column.getIsSorted();
                  return (
                    <th key={h.id} className="h-11 px-3 text-left text-xs font-medium whitespace-nowrap text-muted-foreground first:pl-5" style={{ width: h.id === "select" ? 44 : undefined }}>
                      {sortable ? (
                        <button
                          type="button"
                          onClick={h.column.getToggleSortingHandler()}
                          className={cn("inline-flex cursor-pointer items-center gap-1 hover:text-foreground", dir && "text-foreground")}
                        >
                          {flexRender(h.column.columnDef.header, h.getContext())}
                          {dir === "asc" ? <ArrowUp className="size-3" /> : dir === "desc" ? <ArrowDown className="size-3" /> : <ChevronsUpDown className="size-3 opacity-40" />}
                        </button>
                      ) : (
                        flexRender(h.column.columnDef.header, h.getContext())
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                onClick={() => onOpen(row.id)}
                className={cn(
                  "cursor-pointer border-b transition-colors last:border-0 hover:bg-accent/50",
                  row.getIsSelected() && "bg-chat/[0.06] hover:bg-chat/10",
                )}
              >
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="px-3 py-2.5 first:pl-5">
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
