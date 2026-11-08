import { useState } from "react";
import { Check, Settings2 } from "lucide-react";
import { CollectorOptions, optionPatch, type PublicOptions } from "@/components/collector-options";
import { IconTile, PillButton } from "@/components/kit";
import { Field, TextInput } from "@/components/kit/form";
import type { Room } from "@/data/types";
import { api } from "@/data/api";
import { notifyError, refresh } from "@/data/runtime";
import { Assignment } from "@/components/resource-pool";
import { useToast } from "@/app/toast";

export function RoomSettings({ room }: { room: Room }) {
  const [dirty, setDirty] = useState<Partial<PublicOptions>>({}); const [inherit, setInherit] = useState(false); const [note, setNote] = useState(room.note ?? ""); const [tags, setTags] = useState(room.tags.join(" ")); const [pending, setPending] = useState(false); const toast = useToast();
  if (!room.options) return <p>正在读取采集配置…</p>;
  return <div className="space-y-5"><div className="flex items-start gap-3 rounded-2xl border border-info/25 bg-info/8 p-3 text-xs leading-relaxed"><IconTile icon={Settings2} tone="info" size={28} /><p className="text-muted-foreground">只修改你更改过的项目，其余继续继承全局默认。连接参数变更只重建本直播间；消息类型和静默阈值立即生效。</p></div><section className="space-y-3 rounded-2xl border p-3"><h3 className="text-sm font-semibold">为本房间分配资源</h3><Assignment kind="proxy" liveID={room.liveId} /><Assignment kind="account" liveID={room.liveId} /></section><div className="grid gap-4 sm:grid-cols-2"><Field label="备注"><TextInput value={note} onChange={(e) => setNote(e.target.value)} maxLength={2000} /></Field><Field label="标签" hint="用空格分隔"><TextInput value={tags} onChange={(e) => setTags(e.target.value)} /></Field></div><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={inherit} onChange={(e) => setInherit(e.target.checked)} />清除本直播间单独配置，全部恢复跟随全局</label>{!inherit && <CollectorOptions value={{ ...room.options, ...dirty }} onChange={(patch) => setDirty((v) => ({ ...v, ...patch }))} />}<div className="sticky bottom-0 -mx-5 -mb-4 flex flex-wrap items-center gap-3 border-t bg-card/95 px-5 py-3"><span className="text-xs text-muted-foreground">保存后以后台返回的采集状态为准</span><PillButton icon={Check} tone="primary" className="ml-auto" disabled={pending} onClick={async () => { setPending(true); try { await api(`/rooms/${encodeURIComponent(room.liveId)}`, "PATCH", { note, tags: tags.split(/\s+/).filter(Boolean), inherit_all: inherit, overrides: optionPatch(dirty) }); await refresh(); setDirty({}); setInherit(false); toast.show({ title: "直播间设置已保存", status: "success" }); } catch (e) { notifyError(e); } finally { setPending(false); } }}>保存设置</PillButton></div></div>;
}
