import { MotionConfig } from "motion/react";
import { createContext, useContext, useLayoutEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";

type MotionMode = "minimal" | "standard";
const KEY = "livetap-motion";
const reducedMotionQuery = "(prefers-reduced-motion: reduce)";
function subscribeReducedMotion(onChange: () => void) {
  const query = window.matchMedia(reducedMotionQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}
const readReducedMotion = () => window.matchMedia(reducedMotionQuery).matches;
const Context = createContext<{
  mode: MotionMode;
  minimal: boolean;
  setMode: (mode: MotionMode) => void;
} | null>(null);

export function MotionSettingsProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<MotionMode>(() => {
    try {
      return localStorage.getItem(KEY) === "standard" ? "standard" : "minimal";
    } catch {
      return "minimal";
    }
  });
  const reduce = useSyncExternalStore(subscribeReducedMotion, readReducedMotion);
  const minimal = mode === "minimal" || !!reduce;
  useLayoutEffect(() => {
    document.documentElement.dataset.motion = minimal ? "minimal" : "standard";
    try {
      localStorage.setItem(KEY, mode);
    } catch {
      // 隐私模式下只保留本次选择。
    }
  }, [mode, minimal]);
  const value = useMemo(() => ({ mode, minimal, setMode }), [mode, minimal]);
  return (
    <Context.Provider value={value}>
      <MotionConfig reducedMotion={minimal ? "always" : "user"} transition={{ type: "tween", duration: 0.18, ease: "easeOut" }}>
        {children}
      </MotionConfig>
    </Context.Provider>
  );
}

export function useMotionSettings() {
  const settings = useContext(Context);
  if (!settings) throw new Error("useMotionSettings 需要在 MotionSettingsProvider 内使用");
  return settings;
}
