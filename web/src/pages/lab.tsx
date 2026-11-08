import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, FlaskConical, Play, Search, Square, Trash2 } from "lucide-react";
import { api, queryClient } from "@/data/api";
import { notifyError } from "@/data/runtime";
import { Card, PageHeader, PillButton, Tag } from "@/components/kit";
import { TextInput } from "@/components/kit/form";
import { usePool } from "@/components/resource-pool";
import { Sparkline } from "@/components/charts";
import { SelectBox } from "@/components/kit/select";
import { fmtBytesPerSec } from "@/lib/format";

const fmtRate = (kb = 0) => { const v = fmtBytesPerSec(kb); return `${v.value} ${v.unit}`; };

interface Stage {
  mode: string; sharing: boolean; size: number; started: number; finished: number;
  minimum_collecting: number; max_memory_mb: number; max_cpu: number; bandwidth_available?: boolean; max_down_kb_s?: number; max_up_kb_s?: number;
  groups?: Record<string, { rooms: number; collecting: number; disconnects: number; false_positives: number }>;
  startup_unconnected: { id: string; reason: string }[];
  rooms: { id: string; disconnects: number; recovery_ms: number[]; recovery_incomplete?: boolean; false_positives: number; requests: number; state: string; reason: string }[];
}
interface Run {
  id: string; status: string; started: number; finished: number; stages: Stage[];
  detail: string; conclusion: string; current_mode: string; current_sharing: boolean;
  current_stage: number; phase: string;
  recent: { stage: number; cooldown: { active: boolean; remainingSeconds: number }; at: number; collecting: number; risk: number; silent: number; memory_mb: number; cpu: number; homepage_requests_per_minute: number }[];
}
const modes: Record<string, string> = { anonymous: "匿名直连", proxy: "匿名 + 代理", account: "小号直连", proxy_account: "小号 + 代理" };
const states: Record<string, string> = { running: "运行中", completed: "观察完成", cancelled: "已停止", interrupted: "重启中断", failed: "未完成" };

export function LabPage() {
  const runs = useQuery({ queryKey: ["lab-runs"], queryFn: () => api<Run[]>("/lab/runs"), refetchInterval: 2500 });
  const proxies = usePool("proxy");
  const accounts = usePool("account");
  const [rooms, setRooms] = useState("");
  const [categories, setCategories] = useState("1_1");
  const [stages, setStages] = useState("10,30,50,100,150");
  const [minutes, setMinutes] = useState(20);
  const [startup, setStartup] = useState(120);
  const [sharing, setSharing] = useState("both");
  const [selectedModes, setSelectedModes] = useState(["anonymous"]);
  const [proxyID, setProxyID] = useState("");
  const [accountID, setAccountID] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // 删除需要二次确认：第一次点击变成"确认删除"，4 秒内不再点就恢复
  const [confirmDelete, setConfirmDelete] = useState("");
  useEffect(() => { if (!confirmDelete) return; const t = window.setTimeout(() => setConfirmDelete(""), 4000); return () => window.clearTimeout(t); }, [confirmDelete]);
  const active = runs.data?.find(r => r.status === "running");
  const ids = [...new Set(rooms.split(/[\s,，]+/).filter(Boolean))];
  async function operate(key: string, fn: () => Promise<unknown>) {
    setBusy(key); setError("");
    try { await fn(); await queryClient.invalidateQueries({ queryKey: ["lab-runs"] }); }
    catch (err) { setError(err instanceof Error ? err.message : "操作失败"); }
    finally { setBusy(""); }
  }
  return <>
    <PageHeader icon={FlaskConical} tone="like" title="测试实验室" desc="按同一计划做对照；完整观察、独立报告、结束自动清理测试房间" />
    <Card className="mb-4 border-info/25 p-4 text-sm text-muted-foreground">点击“开始实验”会发起真实采集，请在准备好出口与样本后运行。</Card>
    <Card className="mb-5 p-5">
      <h2 className="mb-4 font-semibold">实验计划</h2>
      <div className="grid gap-4 lg:grid-cols-2">
        <label className="space-y-2 text-sm">直播间样本（数字 ID，空格或换行分隔）
          <textarea className="h-32 w-full rounded-2xl border bg-background p-3 font-mono text-sm" value={rooms} onChange={e => setRooms(e.target.value)} placeholder="混合热门房间与自己的 0～5 人冷清房间" />
          <span className="text-xs text-muted-foreground">已填写 {ids.length} 个；已有采集任务不会被实验覆盖。</span>
        </label>
        <div className="space-y-3">
          <label className="block space-y-2 text-sm">分类编号（可选，逗号分隔）<TextInput value={categories} onChange={e => setCategories(e.target.value)} /></label>
          <PillButton icon={Search} disabled={!!busy || !!active} onClick={() => void operate("discover", async () => {
            const result = await api<{ rooms: string[] }>("/lab/discover", "POST", { categories: categories.split(/[,，\s]+/).filter(Boolean) });
            setRooms([...new Set([...ids, ...result.rooms])].join("\n"));
          })}>{busy === "discover" ? "发现中…" : "从分类页发现"}</PillButton>
          <p className="text-xs text-muted-foreground">发现会访问抖音分类页；上游要求验证时停止。推荐页多为热门房间，请另补冷清样本。</p>
        </div>
        <label className="space-y-2 text-sm">加量档位<TextInput value={stages} onChange={e => setStages(e.target.value)} /><span className="text-xs text-muted-foreground">例如 10,30,50；单档 + 1440 分钟可观察 24 小时。</span></label>
        <div className="grid grid-cols-2 gap-3">
          <label className="space-y-2 text-sm">每档观察（分钟）<TextInput type="number" min={1} max={1440} value={minutes} onChange={e => setMinutes(Number(e.target.value))} /></label>
          <label className="space-y-2 text-sm">启动期（秒）<TextInput type="number" min={0} max={600} value={startup} onChange={e => setStartup(Number(e.target.value))} /></label>
        </div>
        <label className="space-y-2 text-sm">共享游客身份<SelectBox ariaLabel="共享游客身份" value={sharing} onChange={setSharing} options={[{ value: "both", label: "关闭 / 开启，依次对照" }, { value: "off", label: "仅关闭" }, { value: "on", label: "仅开启" }]} /></label>
        <fieldset className="rounded-2xl border p-3"><legend className="px-1 text-sm">接入方式对照</legend><div className="flex flex-wrap gap-4">{Object.entries(modes).map(([key, label]) => <label key={key} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={selectedModes.includes(key)} onChange={e => setSelectedModes(current => e.target.checked ? [...current, key] : current.filter(v => v !== key))} />{label}</label>)}</div></fieldset>
        <label className="space-y-2 text-sm">代理选择<SelectBox ariaLabel="代理选择" value={proxyID} onChange={setProxyID} options={[{ value: "", label: "自动分配：最少占用" }, ...(proxies.data?.filter(r => r.enabled).map(r => ({ value: r.id, label: r.name })) ?? [])]} /></label>
        <label className="space-y-2 text-sm">小号选择<SelectBox ariaLabel="小号选择" value={accountID} onChange={setAccountID} options={[{ value: "", label: "自动分配：最少占用" }, ...(accounts.data?.filter(r => r.enabled).map(r => ({ value: r.id, label: r.name })) ?? [])]} /></label>
      </div>
      <p className="mt-4 text-xs text-muted-foreground">仅覆盖测试房间的配置，不改变节点默认值。各组顺序运行，出口已有流量及冷却会影响结果；报告保留每房诊断，带宽未计量时明确标注。</p>
      {error && <p role="alert" className="mt-3 text-sm text-ink-err">{error}</p>}
      <div className="mt-4 flex flex-wrap justify-end gap-2"><PillButton tone="primary" icon={Play} disabled={!!busy || !!active || !ids.length || !selectedModes.length} onClick={() => void operate("start", () => api("/lab/runs", "POST", { rooms: ids, stages: stages.split(/[,，\s]+/).filter(Boolean).map(Number), startup_seconds: startup, duration_seconds: Math.round(minutes * 60), sharing: sharing === "both" ? [false, true] : [sharing === "on"], modes: selectedModes, proxy_id: proxyID, account_id: accountID }))}>{busy === "start" ? "创建中…" : "开始实验"}</PillButton></div>
    </Card>
    {runs.error && <p role="alert" className="text-ink-err">报告读取失败：{runs.error.message}</p>}
    {!runs.isPending && !runs.data?.length && <Card className="p-8 text-center text-muted-foreground">还没有实验报告，页面不会生成模拟结果。</Card>}
    <div className="space-y-4">{runs.data?.map(run => <Card key={run.id} className="min-w-0 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">{new Date(run.started).toLocaleString()}</h2><p className="num mt-1 text-xs text-muted-foreground">{run.id}</p></div><Tag tone={run.status === "running" ? "info" : run.status === "completed" ? "ok" : "warn"}>{states[run.status] ?? run.status}</Tag>
        <div className="flex flex-wrap gap-2">{run.status === "running" && <PillButton icon={Square} disabled={!!busy} onClick={() => void operate("stop", () => api(`/lab/runs/${run.id}/stop`, "POST", {}))}>停止并清理</PillButton>}
          <PillButton icon={Download} onClick={async () => { try { const response = await fetch(`/admin/api/lab/runs/${run.id}/export`); if (!response.ok) throw new Error("报告导出失败"); const url = URL.createObjectURL(await response.blob()); const link = document.createElement("a"); link.href = url; link.download = `lab-${run.id}.json`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); } catch (err) { notifyError(err); } }}>导出 JSON</PillButton>
          {run.status !== "running" && <PillButton icon={Trash2} tone={confirmDelete === run.id ? "danger" : "ghost"} disabled={!!busy} aria-label="删除这份报告" onClick={() => { if (confirmDelete !== run.id) { setConfirmDelete(run.id); return; } setConfirmDelete(""); void operate("delete", () => api(`/lab/runs/${run.id}`, "DELETE")); }}>{confirmDelete === run.id ? "确认删除" : "删除"}</PillButton>}
        </div>
      </div>
      {run.status === "running" && <p role="status" className="mt-4 text-sm text-ink-info">{modes[run.current_mode]} · 共享{run.current_sharing ? "开" : "关"} · {run.current_stage} 间 · {run.phase === "starting" ? "启动期" : "固定时长观察中"}</p>}
      {(run.recent?.length ?? 0) > 0 && <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">{[
        { label: "采集中", values: run.recent.map(s => s.collecting), unit: "间" },
        { label: "状态待确认 / 重连", values: run.recent.map(s => s.risk), unit: "间" },
        { label: "CPU", values: run.recent.map(s => s.cpu), unit: "%" },
        { label: "静默房间", values: run.recent.map(s => s.silent), unit: "间" },
        { label: "连接成功率", values: run.recent.map(s => s.stage ? s.collecting / s.stage * 100 : 0), unit: "%" },
        { label: "冷却剩余", values: run.recent.map(s => s.cooldown?.remainingSeconds ?? 0), unit: "秒" },
        { label: "内存", values: run.recent.map(s => s.memory_mb), unit: "MB" },
        { label: "首页请求", values: run.recent.map(s => s.homepage_requests_per_minute), unit: "次/分钟" },
      ].map(item => <div key={item.label} className="rounded-2xl bg-muted/40 p-3"><p className="text-xs text-muted-foreground">{item.label}</p><p className="num mt-1 text-xl">{Number(item.values.at(-1)).toFixed(0)} <span className="text-xs">{item.unit}</span></p><Sparkline data={item.values} tone="info" height={36} /></div>)}</div>}
      <p className="mt-4 text-sm">{run.detail}</p><p className="mt-2 text-sm text-muted-foreground">{run.conclusion}</p>
      <div className="mt-4 space-y-3">{run.stages.map((stage, i) => <details key={i} className="rounded-2xl border p-3"><summary className="cursor-pointer text-sm">{modes[stage.mode]} · 共享{stage.sharing ? "开" : "关"} · {stage.size} 间：最低在线 {stage.minimum_collecting} 间，观察 {Math.round((stage.finished - stage.started) / 1000)} 秒</summary>
        <p className="mt-3 text-xs text-muted-foreground">CPU 峰值 {stage.max_cpu.toFixed(1)}% · 内存峰值 {stage.max_memory_mb.toFixed(1)} MB · {stage.bandwidth_available ? `下行峰值 ${fmtRate(stage.max_down_kb_s)} · 上行峰值 ${fmtRate(stage.max_up_kb_s)}` : "带宽尚未计量"}</p>
        <div className="mt-3 flex flex-wrap gap-2">{Object.entries(stage.groups ?? {}).map(([group, value]) => <Tag key={group}>{group === "unknown" ? "未确认热度" : group === "hot" ? "100 人以上" : group + " 人"}：{value.collecting}/{value.rooms} 在线 · 断线 {value.disconnects}</Tag>)}</div><div className="mt-3 grid gap-2 sm:grid-cols-2">{stage.rooms.map(room => <div key={room.id} className="min-w-0 rounded-xl bg-muted/40 p-3 text-xs"><p className="font-mono">{room.id}</p><p className="mt-1">断线 {room.disconnects} · 静默误判 {room.false_positives} · 请求 {room.requests}</p><p className="mt-1 break-words">重连耗时：{room.recovery_ms.length ? room.recovery_ms.join("、") + " ms" : "无已完成重连"}</p>{room.recovery_incomplete && <p className="mt-1 text-ink-warn">重连耗时记录存在缺口</p>}<p className="mt-1 text-muted-foreground">{room.reason}</p></div>)}</div>
        {stage.startup_unconnected.length > 0 && <div className="mt-3 text-xs text-ink-warn"><p>启动期未连通：</p>{stage.startup_unconnected.map(room => <p key={room.id}>{room.id}：{room.reason}</p>)}</div>}
      </details>)}</div>
    </Card>)}</div>
  </>;
}
