import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MotionSettingsProvider } from "@/app/motion";
import { ThemeProvider } from "@/app/theme";
import { ToastProvider } from "@/app/toast";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/data/api";
import { SessionGate } from "@/app/session";
import App from "./App";
import { PageErrorBoundary } from "@/app/PageErrorBoundary";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* 所有 Motion 动效遵守系统“减少动态效果”设置 */}
    <MotionSettingsProvider>
      <ThemeProvider>
        <TooltipProvider delayDuration={200}>
          <ToastProvider>
            <QueryClientProvider client={queryClient}>
              {/* 最外层兜底：外壳（侧栏、顶栏、搜索）出错时也不整站白屏 */}
              <PageErrorBoundary resetKey="app"><SessionGate><App /></SessionGate></PageErrorBoundary>
            </QueryClientProvider>
          </ToastProvider>
        </TooltipProvider>
      </ThemeProvider>
    </MotionSettingsProvider>
  </StrictMode>,
);
