import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, queryClient, setCSRF } from "@/data/api";
import { sim, type RuntimeState } from "@/data/runtime";
import { setFeatures } from "./router";
import { Logo } from "@/components/illustrations";
import { Card, PillButton } from "@/components/kit";
import { Field, TextInput } from "@/components/kit/form";

interface Session { authenticated: boolean; must_change_password?: boolean; csrf?: string; features?: { lab?: boolean } }
export function SessionGate({ children }: { children: ReactNode }) {
  const session = useQuery({ queryKey: ["session"], queryFn: () => api<Session>("/session"), retry: false, staleTime: 0 });
  if (session.isPending) return <div role="status" className="p-8 text-muted-foreground">正在连接采集节点…</div>;
  if (session.error) return <div className="p-8"><p>无法连接采集节点，请检查后端是否启动。</p><PillButton className="mt-4" onClick={() => void session.refetch()}>重试</PillButton></div>;
  setCSRF(session.data?.csrf ?? "");
  setFeatures(session.data?.features);
  if (!session.data?.authenticated || session.data.must_change_password) return <Login change={!!session.data?.must_change_password} />;
  return <LiveConnection>{children}</LiveConnection>;
}
function Login({ change }: { change: boolean }) {
  const [password, setPassword] = useState(""); const [next, setNext] = useState(""); const [confirm, setConfirm] = useState(""); const [error, setError] = useState(""); const [pending, setPending] = useState(false);
  return <main className="flex min-h-dvh items-center justify-center p-4"><Card className="w-full max-w-md p-6 sm:p-8"><Logo size={40} /><h1 className="mt-5 text-2xl font-semibold">{change ? "设置你的管理密码" : "登录 LiveTap"}</h1><p className="mt-2 text-sm text-muted-foreground">{change ? "首次登录需要更换初始密码，设置后进入管理后台。" : "连接当前节点，查看真实采集状态。"}</p><form className="mt-6 space-y-5" onSubmit={async (event) => { event.preventDefault(); setError(""); if (change && next !== confirm) { setError("两次新密码不一致"); return; } setPending(true); try { if (change) await api("/password", "POST", { current_password: password, new_password: next }); else { const session = await api<Session>("/login", "POST", { username: "admin", password }); setCSRF(session.csrf ?? ""); } setPassword(""); setNext(""); setConfirm(""); await queryClient.invalidateQueries({ queryKey: ["session"] }); } catch (e) { setError((e as Error).message); } finally { setPending(false); } }}>
    {!change && <Field label="账号"><TextInput value="admin" readOnly autoComplete="username" /></Field>}
    <Field label={change ? "当前密码" : "密码"}><TextInput required type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
    {change && <><Field label="新密码" hint="至少 12 位"><TextInput required minLength={12} type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} /></Field><Field label="再次输入新密码"><TextInput required type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} /></Field></>}
    {error && <p role="alert" className="text-sm text-ink-err">{error}</p>}<button type="submit" disabled={pending} className="h-10 w-full cursor-pointer rounded-full bg-primary font-medium text-primary-foreground disabled:opacity-50">{pending ? "正在处理…" : change ? "保存并进入后台" : "登录"}</button>
  </form></Card></main>;
}
/** 前台连续这么久连不上才提示断线：切回标签页、节点重启时的正常重连不打扰 */
const OFFLINE_GRACE_MS = 5000;

function LiveConnection({ children }: { children: ReactNode }) {
  const snapshot = useQuery({ queryKey: ["snapshot"], queryFn: () => api<RuntimeState>("/snapshot"), refetchInterval: 15000 });
  const [offline, setOffline] = useState(false);
  // 页面脚本是打开时从节点下载的；节点升级后旧页面会继续跑旧代码，看不到新功能，所以提示刷新
  const loadedBuild = useRef("");
  const [upgraded, setUpgraded] = useState("");
  useEffect(() => {
    if (!snapshot.data) return;
    sim.commit(snapshot.data);
    const node = snapshot.data.node;
    const build = `${node?.version ?? ""} ${node?.revision ?? ""}`.trim();
    if (!build) return;
    if (!loadedBuild.current) loadedBuild.current = build;
    else if (build !== loadedBuild.current) setUpgraded(node.version);
  }, [snapshot.data]);
  useEffect(() => {
    // 切到后台时主动断开实时连接（省流量），回到前台再连；主动断开不算断线
    let stopped = false; let retry: number | undefined; let grace: number | undefined; let socket: WebSocket | undefined;
    const armGrace = () => { if (grace === undefined) grace = window.setTimeout(() => { grace = undefined; setOffline(true); }, OFFLINE_GRACE_MS); };
    const clearGrace = () => { window.clearTimeout(grace); grace = undefined; };
    const connect = () => {
      if (stopped || document.hidden || (socket && socket.readyState <= WebSocket.OPEN)) return;
      armGrace();
      const s = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/admin/api/stream`);
      socket = s;
      s.onopen = () => { if (socket !== s) return; clearGrace(); setOffline(false); };
      s.onmessage = (event) => { try { queryClient.setQueryData(["snapshot"], JSON.parse(event.data)); } catch { s.close(); } };
      s.onclose = () => { if (socket !== s) return; socket = undefined; if (stopped || document.hidden) return; armGrace(); retry = window.setTimeout(connect, 3000); };
    };
    const visibility = () => {
      window.clearTimeout(retry);
      if (document.hidden) { clearGrace(); const s = socket; socket = undefined; s?.close(); }
      else { void snapshot.refetch(); connect(); }
    };
    connect(); document.addEventListener("visibilitychange", visibility);
    return () => { stopped = true; window.clearTimeout(retry); clearGrace(); socket?.close(); document.removeEventListener("visibilitychange", visibility); };
  }, []);
  if (!snapshot.data) return <div className="p-8" role="status">{snapshot.error ? "读取节点失败，正在重试…" : "正在读取采集状态…"}</div>;
  return <>
    {offline && <div role="status" className="sticky top-0 z-[80] bg-warn/15 px-4 py-2 text-center text-sm text-ink-warn">实时连接暂未连通，正在重试；当前显示最近一次数据。</div>}
    {!offline && upgraded && <div role="status" className="sticky top-0 z-[80] flex items-center justify-center gap-3 bg-info/12 px-4 py-2 text-sm text-ink-info">LiveTap 已更新到 {upgraded}，刷新页面即可使用新版。<button type="button" className="cursor-pointer rounded-full bg-card px-3 py-0.5 text-xs font-medium text-foreground shadow-[var(--shadow-card)]" onClick={() => window.location.reload()}>刷新</button></div>}
    {children}
  </>;
}
