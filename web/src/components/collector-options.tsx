import { Field, Segmented, TextInput, ToggleChip } from "@/components/kit/form";
import { MSG_TYPES, TYPE_META } from "@/lib/meta";
import { SelectBox } from "@/components/kit/select";

export interface PublicOptions {
  engine: string; proxy_url: string; cookie: string; sign_provider: string; tikhub_token: string; protocol_mode: string;
  poll_seconds: number; silence_seconds: number; event_types: string[];
  proxy_url_configured?: boolean; cookie_configured?: boolean; tikhub_token_configured?: boolean;
}
export function CollectorOptions({ value, onChange }: { value: PublicOptions; onChange: (patch: Partial<PublicOptions>) => void }) {
  return <div className="space-y-6"><div className="grid gap-5 md:grid-cols-2">
    <Field label="代理地址" hint={value.proxy_url_configured ? "已配置；留空保持不变，凭据不回显" : "不填写即直连；支持 HTTP / SOCKS5"}><TextInput type="password" autoComplete="off" value={value.proxy_url} onChange={(e) => onChange({ proxy_url: e.target.value })} placeholder="http://host:port 或 socks5://host:port" />{value.proxy_url_configured && <label className="mt-2 flex items-center gap-2 text-xs"><input type="checkbox" onChange={(e) => e.target.checked && onChange({ proxy_url: "", proxy_url_configured: false })} />清除已保存的代理</label>}</Field>
    <Field label="Cookie" hint={value.cookie_configured ? "已加密保存；留空保持不变" : "游客模式自动获取 ttwid，无需填写"}><TextInput type="password" autoComplete="off" value={value.cookie} onChange={(e) => onChange({ cookie: e.target.value })} placeholder="可选：游客或小号 Cookie" />{value.cookie_configured && <label className="mt-2 flex items-center gap-2 text-xs"><input type="checkbox" onChange={(e) => e.target.checked && onChange({ cookie: "", cookie_configured: false })} />清除并恢复游客身份</label>}</Field>
    <Field label="签名方式"><Segmented value={value.sign_provider} onChange={(v) => onChange({ sign_provider: v })} options={[{ value: "local", label: "本地" }, { value: "tikhub", label: "TikHub" }]} /></Field>
    <Field label="协议画像"><Segmented value={value.protocol_mode} onChange={(v) => onChange({ protocol_mode: v })} options={[{ value: "web", label: "web（稳定）" }, { value: "pc", label: "pc（测试中）" }]} /></Field>
    {value.sign_provider === "tikhub" && <Field label="TikHub 密钥" hint={value.tikhub_token_configured ? "已加密保存；留空保持不变" : "TikHub 请求不使用直播间代理"}><TextInput type="password" autoComplete="off" value={value.tikhub_token} onChange={(e) => onChange({ tikhub_token: e.target.value })} /></Field>}
    <Field label="引擎"><Segmented value="douyinlive" onChange={() => {}} options={[{ value: "douyinlive", label: "A · douyinLive" }, { value: "browser", label: "B · 浏览器（二期）", disabled: true }]} /></Field>
    <Field label="开播轮询" hint="等待时间变长后逐步放宽，最长 120 秒"><Segmented value={String(value.poll_seconds)} onChange={(v) => onChange({ poll_seconds: Number(v) })} options={[{ value: "15", label: "15 秒起" }, { value: "30", label: "30 秒起" }, { value: "60", label: "60 秒起" }]} /></Field>
    <Field label="静默阈值" hint="自适应基于所有消息间隔，范围 90～300 秒"><SelectBox ariaLabel="静默阈值" value={String(value.silence_seconds)} onChange={(v) => onChange({ silence_seconds: Number(v) })} options={[{ value: "0", label: "自适应", hint: "推荐" }, ...[90, 120, 180, 240, 300].map((v) => ({ value: String(v), label: `${v} 秒` }))]} /></Field>
  </div><Field label="显示的消息类型" hint="立即生效；在线人数和静默检测始终统计所有消息"><div className="flex flex-wrap gap-2">{MSG_TYPES.map((type) => <ToggleChip key={type} tone={TYPE_META[type].tone} icon={TYPE_META[type].icon} active={value.event_types.includes(type)} onClick={() => onChange({ event_types: value.event_types.includes(type) ? value.event_types.filter((t) => t !== type) : [...value.event_types, type] })}>{TYPE_META[type].label}</ToggleChip>)}</div></Field></div>;
}
// Untouched secrets are omitted. The server retains their encrypted values.
export function optionPatch(dirty: Partial<PublicOptions>) { return Object.fromEntries(Object.entries(dirty).filter(([key]) => !key.endsWith("_configured"))); }
