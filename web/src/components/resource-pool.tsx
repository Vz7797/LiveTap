import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, Check, Globe2, Plus, Trash2, UserRoundCog, X, Pencil } from "lucide-react";
import { api, queryClient } from "@/data/api";
import { notifyError, refresh, useSim } from "@/data/runtime";
import { useToast } from "@/app/toast";
import { Card, IconTile, PageHeader, PillButton, Tag } from "@/components/kit";
import { TextInput } from "@/components/kit/form";
import { SelectBox } from "@/components/kit/select";

export interface PoolResource {
  id: string; kind: "proxy" | "account"; name: string; tags: string[]; enabled: boolean;
  limit: number; state: string; checked_at: number; latency_ms: number; detail: string;
  mask: string; rooms: string[];
}
export const selectClass = "h-10 w-full min-w-0 rounded-xl border bg-background px-3 text-sm outline-none focus:border-ring";
const stateNames: Record<string, string> = { unknown: "未确认", healthy: "检测通过", invalid: "不可用", restricted: "上游受限" };
const endpoint = (kind: "proxy" | "account") => kind === "proxy" ? "/proxies" : "/accounts";
export function usePool(kind: "proxy" | "account") {
  return useQuery({ queryKey: ["pool", kind], queryFn: () => api<PoolResource[]>(endpoint(kind)), refetchInterval: 5000 });
}
async function reloadPools() {
  await queryClient.invalidateQueries({ queryKey: ["pool"] });
  await refresh();
}

export function PoolPage({ kind }: { kind: "proxy" | "account" }) {
  const query = usePool(kind);
  const proxyQuery = usePool("proxy");
  const [editing, setEditing] = useState<PoolResource | "new" | null>(null);
  const [busy, setBusy] = useState("");
  const [checkProxy, setCheckProxy] = useState("");
  const [removing, setRemoving] = useState("");
  const toast = useToast();
  const isProxy = kind === "proxy";
  const rows = query.data ?? [];
  async function perform(id: string, operation: () => Promise<unknown>, title: string) {
    setBusy(id);
    try { await operation(); await reloadPools(); toast.show({ title, status: "success" }); }
    catch (error) { notifyError(error); }
    finally { setBusy(""); }
  }
  return <>
    <PageHeader icon={isProxy ? Globe2 : UserRoundCog} tone={isProxy ? "info" : "social"}
      title={isProxy ? "代理池" : "账号池"}
      desc={isProxy ? "管理采集出口，检测连通性并按直播间分配" : "只使用专用小号；Cookie 加密保存，默认仍为游客模式"}
      actions={<PillButton tone="primary" icon={Plus} onClick={() => setEditing("new")}>{isProxy ? "添加代理" : "添加小号"}</PillButton>} />
    <Card className="mb-4 grid gap-3 p-4 text-sm sm:flex sm:flex-wrap sm:items-center">
      <span className="text-muted-foreground">共 {rows.length} 个 · 可分配 {rows.filter(r => r.enabled && r.state !== "invalid" && r.state !== "restricted").length} 个</span>
      {!isProxy && <label className="flex min-w-0 flex-1 flex-col items-start gap-2 sm:flex-row sm:items-center">检测出口
        <SelectBox ariaLabel="小号检测出口" className="max-w-xs" value={checkProxy} onChange={setCheckProxy} options={[{ value: "", label: "直连" }, ...(proxyQuery.data?.filter(r => r.enabled).map(r => ({ value: r.id, label: r.name })) ?? [])]} />
      </label>}
      <PillButton icon={Activity} disabled={!!busy || !rows.length} onClick={() => void perform("all", async () => {
        for (const r of rows.filter(r => r.enabled)) await api(`${endpoint(kind)}/${r.id}/check`, "POST", { proxy_id: checkProxy });
      }, "资源检测已完成")}>{busy === "all" ? "检测中…" : "检测全部启用项"}</PillButton>
    </Card>
    {editing && <ResourceEditor key={editing === "new" ? "new" : editing.id} kind={kind} value={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
    {query.isPending && <p role="status">正在读取资源…</p>}
    {query.error && <p role="alert" className="text-ink-err">读取失败：{query.error.message}</p>}
    {!query.isPending && !rows.length && <Card className="mb-4 p-8 text-center text-muted-foreground">尚未登记{isProxy ? "代理" : "小号"}。添加后可以先检测，再分配房间。</Card>}
    <div className="grid gap-4 lg:grid-cols-2 min-[1920px]:grid-cols-3">
      {rows.map(r => <Card key={r.id} className="min-w-0 p-5">
        <div className="flex items-start gap-3">
          <IconTile icon={isProxy ? Globe2 : UserRoundCog} tone={isProxy ? "info" : "social"} />
          <div className="min-w-0 flex-1"><h2 className="truncate font-semibold">{r.name}</h2><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{r.mask}</p></div>
          <Tag tone={!r.enabled ? "idle" : r.state === "healthy" ? "ok" : r.state === "invalid" ? "err" : "warn"}>{!r.enabled ? "已停用" : stateNames[r.state] ?? r.state}</Tag>
        </div>
        <div className="my-4 flex flex-wrap gap-2">{(r.tags ?? []).map(tag => <Tag key={tag}>{tag}</Tag>)}</div>
        <div className="grid grid-cols-2 gap-4 rounded-2xl bg-muted/50 p-3">
          <div><p className="text-xs text-muted-foreground">已分配 / 上限</p><p className="num mt-1 text-xl">{r.rooms.length} / {r.limit}</p></div>
          <div><p className="text-xs text-muted-foreground">检测耗时</p><p className="num mt-1 text-xl">{r.checked_at ? `${r.latency_ms} ms` : "—"}</p></div>
        </div>
        <p className="mt-3 min-h-8 text-xs text-muted-foreground">{busy === r.id ? "操作中…" : r.detail}</p>
        {r.rooms.length > 0 && <p className="mt-2 break-all text-xs text-muted-foreground">房间：{r.rooms.join("、")}</p>}
        <p className="mt-2 text-xs text-faint">{r.checked_at ? `上次检测 ${new Date(r.checked_at).toLocaleString()}` : "尚未检测"}</p>
        <div className="mt-4 flex flex-wrap gap-2 border-t pt-3">
          <PillButton icon={Activity} disabled={!!busy} onClick={() => void perform(r.id, () => api(`${endpoint(kind)}/${r.id}/check`, "POST", { proxy_id: checkProxy }), "检测结果已更新")}>检测</PillButton>
          <PillButton icon={Pencil} tone="ghost" onClick={() => setEditing(r)}>编辑</PillButton>
          <PillButton tone="ghost" disabled={!!busy} onClick={() => void perform(r.id, () => api(`${endpoint(kind)}/${r.id}`, "PUT", { name: r.name, tags: r.tags, limit: r.limit, enabled: !r.enabled, secret: "" }), "资源状态已更新")}>{r.enabled ? "停用" : "启用"}</PillButton>
          <PillButton icon={Trash2} tone="ghost" onClick={() => setRemoving(r.id)}>移除</PillButton>
        </div>
        {removing === r.id && <div role="alert" className="mt-3 rounded-2xl border border-warn/30 p-3 text-xs">
          <p>{isProxy ? "移除后，使用该代理的房间会暂停，请重新分配出口。" : "移除后，使用该小号的房间会退回游客模式。"}</p>
          <div className="mt-3 flex gap-2"><PillButton tone="danger" disabled={!!busy} onClick={() => void perform(r.id, async () => { await api(`${endpoint(kind)}/${r.id}`, "DELETE"); setRemoving(""); }, "资源已移除")}>确认移除</PillButton><PillButton onClick={() => setRemoving("")}>取消</PillButton></div>
        </div>}
      </Card>)}
    </div>
    <Card className="mt-5 p-5"><h2 className="mb-3 font-semibold">给房间分配{isProxy ? "代理" : "小号"}</h2><Assignment kind={kind} /></Card>
  </>;
}

function ResourceEditor({ kind, value, onClose }: { kind: "proxy" | "account"; value?: PoolResource; onClose: () => void }) {
  const [name, setName] = useState(value?.name ?? "");
  const [tags, setTags] = useState(value?.tags?.join(" ") ?? "");
  const [secret, setSecret] = useState("");
  const [limit, setLimit] = useState(value?.limit ?? (kind === "account" ? 5 : 20));
  const [confirmed, setConfirmed] = useState(!!value);
  const [batch, setBatch] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  return <Card className="mb-5 p-5">
    <div className="mb-4 flex items-center justify-between"><h2 className="font-semibold">{value ? "编辑" : "添加"}{kind === "proxy" ? "代理" : "小号"}</h2><PillButton icon={X} tone="ghost" onClick={onClose} disabled={pending}>关闭</PillButton></div>
    <form className="grid gap-4 sm:grid-cols-2" onSubmit={async e => {
      e.preventDefault(); setPending(true); setError("");
      try {
        const entries = batch ? secret.split(/\s+/).filter(Boolean) : [secret];
        if (entries.length > 50) throw new Error("一次最多导入 50 个代理");
        for (const [i, entry] of entries.entries()) await api(`${endpoint(kind)}${value ? `/${value.id}` : ""}`, value ? "PUT" : "POST", { name: batch ? `${name} ${i + 1}` : name, tags: tags.split(/\s+/).filter(Boolean), enabled: value?.enabled ?? true, limit, secret: entry, small_account: confirmed });
        setSecret(""); await reloadPools(); toast.show({ title: "资源已保存", status: "success" }); onClose();
      } catch (err) { setError(err instanceof Error ? err.message : "保存失败"); await reloadPools(); }
      finally { setPending(false); }
    }}>
      <label className="space-y-2 text-sm">名称 / 备注<TextInput required value={name} maxLength={100} onChange={e => setName(e.target.value)} /></label>
      <label className="space-y-2 text-sm">分配上限<TextInput type="number" min={1} max={2000} required value={limit} onChange={e => setLimit(Number(e.target.value))} /></label>
      <label className="space-y-2 text-sm sm:col-span-2">{kind === "proxy" ? "代理地址（HTTP / SOCKS5，可带认证）" : "小号 Cookie"}
        <TextInput type="password" autoComplete="new-password" value={secret} onChange={e => setSecret(e.target.value)} required={!value} placeholder={value ? "留空保留原凭据，填写则替换" : "在此粘贴，输入内容始终隐藏"} />
      </label>
      <label className="space-y-2 text-sm">标签（空格分隔）<TextInput value={tags} onChange={e => setTags(e.target.value)} /></label>
      {!value && kind === "proxy" && <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={batch} onChange={e => setBatch(e.target.checked)} />批量导入：多个地址用空格分隔</label>}
      {kind === "account" && <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={confirmed} required onChange={e => setConfirmed(e.target.checked)} />确认是专用小号，不是商家主账号</label>}
      {error && <p role="alert" className="text-sm text-ink-err sm:col-span-2">{error}</p>}
      <div className="flex flex-wrap items-center justify-between gap-3 sm:col-span-2"><p className="text-xs text-muted-foreground">保存后仅显示脱敏信息；检测会产生真实网络请求。</p><PillButton type="submit" tone="primary" icon={Check} disabled={pending || (kind === "account" && !confirmed)}>{pending ? "保存中…" : "保存资源"}</PillButton></div>
    </form>
  </Card>;
}

export function Assignment({ kind, liveID }: { kind: "proxy" | "account"; liveID?: string }) {
  const resources = usePool(kind);
  const rooms = useSim(s => s.rooms);
  const [roomID, setRoomID] = useState(liveID ?? "");
  const [selection, setSelection] = useState("");
  const [pending, setPending] = useState(false);
  const toast = useToast();
  return <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
    {!liveID && <label className="space-y-2 text-sm">直播间<SelectBox ariaLabel="直播间" value={roomID} onChange={setRoomID} placeholder="选择已添加的房间" options={[{ value: "", label: "选择已添加的房间" }, ...rooms.map(r => ({ value: r.liveId, label: r.name || r.liveId, hint: r.liveId }))]} /></label>}
    <label className="space-y-2 text-sm">{kind === "proxy" ? "代理资源" : "小号资源"}<SelectBox ariaLabel={kind === "proxy" ? "代理资源" : "小号资源"} value={selection} onChange={setSelection} options={[{ value: "", label: `解除分配（${kind === "proxy" ? "直连" : "游客"}）` }, { value: "auto:least_used", label: "自动分配：最少占用" }, { value: "auto:round_robin", label: "自动分配：轮询" }, ...(resources.data?.filter(r => r.enabled && r.state !== "invalid" && r.state !== "restricted").map(r => ({ value: r.id, label: r.name, hint: `${r.rooms.length}/${r.limit}` })) ?? [])]} /></label>
    <PillButton className="self-end" disabled={!roomID || pending} onClick={async () => { setPending(true); try { await api(`/rooms/${roomID}/assignment`, "POST", { kind, resource_id: selection.startsWith("auto:") ? "" : selection, policy: selection.startsWith("auto:") ? selection.slice(5) : "" }); await reloadPools(); toast.show({ title: "房间资源分配已更新", status: "success" }); } catch (error) { notifyError(error); } finally { setPending(false); } }}>{pending ? "应用中…" : "应用分配"}</PillButton>
  </div>;
}
