import { AlertTriangle, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useState, type ReactNode } from "react";
import { IconTile, PillButton, Portal } from "@/components/kit";
import type { Tone } from "@/lib/meta";

/**
 * 危险操作确认弹窗（清理、批量删除等不可恢复的操作）。
 * onConfirm 抛错时弹窗保持打开并显示错误；成功后自动关闭。
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "确认",
  tone = "err",
  icon = AlertTriangle,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  tone?: Tone;
  icon?: typeof AlertTriangle;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const close = () => {
    if (pending) return;
    setError("");
    onClose();
  };
  return (
    <Portal>
      <AnimatePresence>
        {open && (
          <>
            <motion.div key="mask" className="fixed inset-0 z-[90] bg-black/30 dark:bg-black/50" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={close} />
            <div key="dialog" className="pointer-events-none fixed inset-0 z-[91] flex items-start justify-center p-3 pt-[16vh]">
              <motion.div
                role="alertdialog"
                aria-modal="true"
                aria-label={title}
                initial={{ opacity: 0, y: 10, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: 6, scale: 0.98 }}
                transition={{ type: "tween", duration: 0.18, ease: "easeOut" }}
                className="pointer-events-auto w-full max-w-md rounded-[28px] border border-border-strong bg-popover p-5 shadow-2xl sm:p-6"
                onKeyDown={(e) => e.key === "Escape" && close()}
              >
                <div className="flex items-start gap-3">
                  <IconTile icon={icon} tone={tone} size={44} />
                  <div className="min-w-0 flex-1">
                    <h2 className="text-lg font-semibold">{title}</h2>
                    <div className="mt-1 text-sm leading-relaxed text-muted-foreground">{description}</div>
                  </div>
                  <button type="button" aria-label="关闭" onClick={close} disabled={pending} className="cursor-pointer rounded-full p-2 text-muted-foreground hover:bg-accent">
                    <X className="size-4" />
                  </button>
                </div>
                {error && <p role="alert" className="mt-3 text-sm text-ink-err">{error}</p>}
                <div className="mt-6 flex justify-end gap-2">
                  <PillButton tone="ghost" onClick={close} disabled={pending}>
                    取消
                  </PillButton>
                  <PillButton
                    tone={tone === "err" ? "danger" : "primary"}
                    disabled={pending}
                    autoFocus
                    onClick={async () => {
                      setPending(true);
                      setError("");
                      try {
                        await onConfirm();
                        onClose();
                      } catch (e) {
                        setError(e instanceof Error ? e.message : "操作失败，请重试");
                      } finally {
                        setPending(false);
                      }
                    }}
                  >
                    {pending ? "处理中…" : confirmLabel}
                  </PillButton>
                </div>
              </motion.div>
            </div>
          </>
        )}
      </AnimatePresence>
    </Portal>
  );
}
