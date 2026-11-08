import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from "react";
import {
  AnimatedToastStack,
  useAnimatedToastStack,
  type ToastInput,
} from "@/components/motion/animated-toast-stack";
import { sim } from "@/data/runtime";

interface ToastApi {
  show: (t: ToastInput) => string;
  update: (id: string, t: Partial<ToastInput>) => void;
  /** 加载中 → 成功/失败 的形变过渡，模拟一次后台操作 */
  run: (opts: { loading: string; success: string; description?: string; ms?: number; fail?: string }) => Promise<boolean>;
}

const Ctx = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const { toasts, showToast, updateToast, dismissToast } = useAnimatedToastStack({ limit: 6, defaultDuration: 4200 });

  const run = useCallback<ToastApi["run"]>(
    ({ loading, success, description, ms = 1100, fail }) =>
      new Promise((resolve) => {
        const id = showToast({ title: loading, status: "loading", duration: 0 });
        window.setTimeout(() => {
          if (fail) {
            updateToast(id, { title: fail, description, status: "error", duration: 4200 });
            resolve(false);
          } else {
            updateToast(id, { title: success, description, status: "success", duration: 3200 });
            resolve(true);
          }
        }, ms);
      }),
    [showToast, updateToast],
  );

  useEffect(
    () =>
      sim.onNotice((n) =>
        showToast({ title: n.title, description: n.description, status: n.tone === "success" ? "success" : n.tone === "error" ? "error" : "info" }),
      ),
    [showToast],
  );

  const api = useMemo(() => ({ show: showToast, update: updateToast, run }), [showToast, updateToast, run]);

  return (
    <Ctx.Provider value={api}>
      {children}
      <AnimatedToastStack
        toasts={toasts}
        onDismiss={dismissToast}
        position="bottom-right"
        fixed
        maxVisible={4}
        classNames={{ surface: "bg-popover/95 border-border-strong" }}
      />
    </Ctx.Provider>
  );
}

export function useToast() {
  const v = useContext(Ctx);
  if (!v) throw new Error("useToast 需要在 ToastProvider 内使用");
  return v;
}
