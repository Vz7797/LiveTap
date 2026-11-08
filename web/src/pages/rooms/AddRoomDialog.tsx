import { AlertCircle, CheckCircle2, ClipboardPaste, Hash, Link2, ListPlus, Plus, Sparkles, Tag as TagIcon, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useToast } from "@/app/toast";
import { IconButton, PillButton, Portal } from "@/components/kit";
import { Field, Segmented, TextInput } from "@/components/kit/form";
import { cn } from "@/lib/utils";
import { sim, useSim } from "@/data/runtime";

type Mode = "id" | "link" | "batch";

interface Parsed {
  raw: string;
  liveId: string | null;
  kind: "web_rid" | "link" | "short" | "douyin_id" | "invalid" | "dup";
  note: string;
}

const SPRING = { type: "tween" as const, duration: 0.18 };

function parseOne(raw: string, existing: Set<string>): Parsed {
  const s = raw.trim();
  let m = s.match(/live\.douyin\.com\/(\d{6,20})/);
  if (m) return mark({ raw: s, liveId: m[1], kind: "link", note: "直播间链接 → web_rid" }, existing);
  if (/v\.douyin\.com\/\w+/.test(s)) return { raw: s, liveId: null, kind: "invalid", note: "请打开分享链接后复制 live.douyin.com 的完整直播链接" };
  m = s.match(/^\d{6,20}$/);
  if (m) return mark({ raw: s, liveId: s, kind: "web_rid", note: "直播间号" }, existing);
  if (/^[A-Za-z0-9_-]{1,128}$/.test(s)) return { raw: s, liveId: s, kind: "douyin_id", note: "抖音短号，提交后由后端解析" };
  return { raw: s, liveId: null, kind: "invalid", note: "无法识别，请填直播间号或链接" };
}

function mark(p: Parsed, existing: Set<string>): Parsed {
  if (p.liveId && existing.has(p.liveId)) return { ...p, kind: "dup", note: "已在采集列表中，将复用现有任务" };
  return p;
}

export function AddRoomButton({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  return (
    <>
      <AnimatePresence initial={false}>
        {!open && (
          <motion.button
            layoutId="add-room"
            type="button"
            onClick={() => onOpenChange(true)}
            transition={SPRING}
            style={{ borderRadius: 999 }}
            className="inline-flex h-9 cursor-pointer items-center gap-1.5 bg-primary px-4 text-sm font-medium text-primary-foreground active:scale-[0.97]"
          >
            <motion.span layout="position" className="inline-flex items-center gap-1.5">
              <Plus className="size-4" strokeWidth={2.4} />
              添加直播间
            </motion.span>
          </motion.button>
        )}
      </AnimatePresence>
      {open && <span className="inline-flex h-9 w-[118px]" aria-hidden />}
      <Portal>
        <AnimatePresence>{open && <AddRoomDialog onClose={() => onOpenChange(false)} />}</AnimatePresence>
      </Portal>
    </>
  );
}

function AddRoomDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const rooms = useSim((s) => s.rooms);
  const existing = useMemo(() => new Set(rooms.map((r) => r.liveId)), [rooms]);
  const [mode, setMode] = useState<Mode>("id");
  const [text, setText] = useState("");
  const [tags, setTags] = useState("");
  const [pending, setPending] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const parsed = useMemo(() => {
    const parts = (mode === "batch" ? text.split(/[\s,，;；]+/) : [text]).map((x) => x.trim()).filter(Boolean);
    return parts.map((p) => parseOne(p, existing));
  }, [text, mode, existing]);
  const ok = parsed.filter((p) => p.kind !== "invalid" && p.kind !== "dup");

  const samples: Record<Mode, string> = {
    id: "403593241280",
    link: "https://live.douyin.com/740012345678",
    batch: "740012345678\nhttps://live.douyin.com/740098765432\nv.douyin.com/iRNBho6u\nxiaochi_666\n403593241280\nabc!!",
  };

  return (
    <>
      <motion.button
        type="button"
        aria-label="关闭"
        className="fixed inset-0 z-[70] cursor-default bg-black/30 dark:bg-black/50"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        onClick={onClose}
      />
      <div className="pointer-events-none fixed inset-0 z-[71] flex items-start justify-center overflow-y-auto p-3 pt-[10vh] sm:p-6 sm:pt-[12vh]">
        <motion.div
          layoutId="add-room"
          transition={SPRING}
          style={{ borderRadius: 28 }}
          role="dialog"
          aria-modal="true"
          aria-label="添加直播间"
          className="pointer-events-auto w-full max-w-[560px] overflow-hidden border border-border-strong bg-popover shadow-2xl"
        >
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1, transition: { delay: 0.1 } }}
            exit={{ opacity: 0, transition: { duration: 0.06 } }}
            className="p-5 sm:p-6"
          >
            <div className="flex items-start gap-3">
              <span className="inline-flex size-11 items-center justify-center rounded-2xl bg-primary text-primary-foreground">
                <Sparkles className="size-5" />
              </span>
              <div className="min-w-0 flex-1">
                <h2 className="text-lg font-semibold">添加直播间</h2>
                <p className="text-sm text-muted-foreground">支持直播间号、链接、抖音号；统一解析为 web_rid，重复的会自动复用</p>
              </div>
              <IconButton icon={X} label="关闭" onClick={onClose} className="-mt-1 -mr-2" />
            </div>

            <div className="mt-5">
              <Segmented<Mode>
                value={mode}
                onChange={(m) => {
                  setMode(m);
                  setText("");
                }}
                options={[
                  { value: "id", label: "填号", icon: Hash },
                  { value: "link", label: "粘贴链接", icon: Link2 },
                  { value: "batch", label: "批量粘贴", icon: ListPlus },
                ]}
              />
            </div>

            <div className="mt-4">
              {mode === "batch" ? (
                <textarea
                  autoFocus
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  placeholder={"每行一个，也可以用逗号或空格分隔\n例如：\n740012345678\nhttps://live.douyin.com/740098765432"}
                  className="h-36 w-full resize-none rounded-2xl border bg-background/60 p-3 font-mono text-sm leading-relaxed outline-none placeholder:font-sans placeholder:text-faint focus:border-ring/60"
                />
              ) : (
                <TextInput
                  autoFocus
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  placeholder={mode === "id" ? "直播间号（live.douyin.com/ 后面的数字）或抖音号" : "https://live.douyin.com/… 或 v.douyin.com/… 分享链接"}
                  className="h-11 rounded-2xl font-mono"
                  onKeyDown={(e) => e.key === "Enter" && ok.length && document.getElementById("add-room-submit")?.click()}
                />
              )}
              <button
                type="button"
                onClick={() => setText(samples[mode])}
                className="mt-2 inline-flex cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                <ClipboardPaste className="size-3.5" /> 填入示例
              </button>
            </div>

            <AnimatePresence initial={false}>
              {parsed.length > 0 && (
                <motion.div
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: "auto", opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                  transition={{ type: "tween" as const, duration: 0.18 }}
                  className="overflow-hidden"
                >
                  <div className="mt-3 rounded-2xl border bg-background/50 p-2">
                    <div className="flex items-center justify-between px-2 py-1 text-xs text-muted-foreground">
                      <span>解析预览</span>
                      <span>
                        可添加 <span className="num font-medium text-ink-ok">{ok.length}</span> · 共 {parsed.length}
                      </span>
                    </div>
                    <ul className="scroll-area max-h-48 overscroll-contain pb-1">
                      <AnimatePresence initial={false}>
                        {parsed.map((p, i) => {
                          const good = p.kind !== "invalid" && p.kind !== "dup";
                          return (
                            <motion.li
                              key={`${p.raw}-${i}`}
                              layout
                              initial={{ opacity: 0, x: -8 }}
                              animate={{ opacity: 1, x: 0 }}
                              exit={{ opacity: 0 }}
                              className="flex items-center gap-2.5 rounded-xl px-2 py-1.5"
                            >
                              {good ? (
                                <CheckCircle2 className="size-4 shrink-0 text-ink-ok" />
                              ) : (
                                <AlertCircle className={cn("size-4 shrink-0", p.kind === "dup" ? "text-ink-warn" : "text-ink-err")} />
                              )}
                              <span className="num min-w-0 flex-1 truncate text-sm">{p.raw}</span>
                              <span
                                className={cn(
                                  "shrink-0 rounded-md px-1.5 py-0.5 text-[11px]",
                                  good ? (p.kind === "web_rid" || p.kind === "link" ? "tint-ok" : "tint-info") : p.kind === "dup" ? "tint-warn" : "tint-err",
                                )}
                              >
                                {p.note}
                              </span>
                            </motion.li>
                          );
                        })}
                      </AnimatePresence>
                    </ul>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>

            <Field label={<span className="inline-flex items-center gap-1.5"><TagIcon className="size-3.5" />标签（可选）</span>} className="mt-4">
              <TextInput value={tags} onChange={(e) => setTags(e.target.value)} placeholder="用空格分隔，如：火锅 重点客户" />
            </Field>

            <div className="mt-5 flex items-center gap-2">
              <span className="text-xs text-muted-foreground">按全局启动限速（每秒 2 个）排队探测</span>
              <PillButton tone="ghost" className="ml-auto" onClick={onClose}>
                取消
              </PillButton>
              <PillButton
                id="add-room-submit"
                tone="primary"
                icon={Plus}
                disabled={ok.length === 0 || pending}
                onClick={async () => {
                  setPending(true);
                  const n = await sim.addRooms(
                    ok.map((p) => p.liveId!),
                    tags.split(/\s+/).filter(Boolean),
                  );
                  setPending(false);
                  if (!n) return;
                  onClose();
                  toast.show({
                    title: `已添加 ${n} 个直播间`,
                    description: "标记为“手动”，不会被自动释放；正在探测开播状态",
                    status: "success",
                  });
                }}
              >
                {pending ? "正在添加…" : `添加 ${ok.length || ""} 个`}
              </PillButton>
            </div>
          </motion.div>
        </motion.div>
      </div>
    </>
  );
}
