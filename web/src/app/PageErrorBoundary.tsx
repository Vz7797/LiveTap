import { AlertOctagon, RotateCw } from "lucide-react";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { Card, PillButton } from "@/components/kit";

/**
 * 页面级出错兜底：某个页面渲染出错时，只在内容区显示提示，侧栏和顶栏照常可用，
 * 不再整站白屏（2026-10-16 真实数据下直播间页白屏的教训）。切换页面会自动复位。
 */
export class PageErrorBoundary extends Component<{ resetKey: string; children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("页面渲染出错", error, info.componentStack);
  }

  componentDidUpdate(prev: { resetKey: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null });
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <Card className="mx-auto mt-10 max-w-lg p-6 text-center">
        <span className="mx-auto inline-flex size-11 items-center justify-center rounded-2xl tint-err">
          <AlertOctagon className="size-5" />
        </span>
        <h2 className="mt-4 text-base font-semibold">这个页面出了点问题</h2>
        <p className="mt-2 text-sm text-muted-foreground">其他页面不受影响。可以先重新加载；如果反复出现，请把下面的错误信息发给维护人员。</p>
        <p className="num mt-3 rounded-xl bg-muted px-3 py-2 text-left text-xs break-all text-muted-foreground">{this.state.error.message}</p>
        <PillButton tone="primary" icon={RotateCw} className="mt-5" onClick={() => window.location.reload()}>
          重新加载
        </PillButton>
      </Card>
    );
  }
}
