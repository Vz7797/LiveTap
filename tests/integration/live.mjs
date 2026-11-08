// Explicit live test. Never import this file to run a test automatically.
// Credentials exist only in this process; reports contain diagnostics, not bodies.
import { spawn } from "node:child_process";
import { randomBytes, createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { observeFor, beginObservation, recordObservation, finishObservation } from "./observation.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const output = path.join(root, "test-results/live");
const temporaryRoot = path.join(root, "test-results/live-tmp");
const temporary = path.join(temporaryRoot, `run-${Date.now()}`);
const binary = process.env.LIVETAP_TEST_BINARY ?? path.join(root, "bin/collector.exe");
const base = "http://127.0.0.1:8090";
const functionalOnly = process.env.LIVETAP_FUNCTIONAL_ONLY === "1";
const startupSeconds = Number(process.env.LIVETAP_STARTUP_SECONDS ?? (functionalOnly ? 30 : 120));
const observationMinutes = Number(process.env.LIVETAP_LIVE_MINUTES ?? (functionalOnly ? 1 : 10));
const shareGuestIdentity = process.env.LIVETAP_SHARE_GUEST_IDENTITY === "1";
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const secret = randomBytes(32).toString("base64");
const initial = randomBytes(24).toString("base64url");
const password = randomBytes(24).toString("base64url");
const report = {
  started: new Date().toISOString(),
  mode: functionalOnly ? "short-observation" : "stability",
  startupSeconds,
  observationMinutes,
  shareGuestIdentity,
  completed: false,
  limitations: ["本机结果不代表服务器容量", "请求数为过去 60 秒滚动计数", "静默误判只统计旁证期间主连接自行恢复，不推断未知情况", "不保存消息正文或凭据"],
};
let child;
let sessionCookie = "";
let csrf = "";

// 每次实测单独成文件，便于对照“共享游客身份”开与关两轮结果，不互相覆盖。
const reportFile = `live-check-${report.mode}-guest-${shareGuestIdentity ? "shared" : "own"}-${report.started.replace(/[:.]/g, "-")}.json`;

function save() {
  fs.mkdirSync(output, { recursive: true });
  fs.writeFileSync(path.join(output, reportFile), JSON.stringify(report, null, 2) + "\n");
}

async function api(route, method = "GET", data) {
  const response = await fetch(`${base}/admin/api${route}`, {
    method,
    headers: { "Content-Type": "application/json", Cookie: sessionCookie, "X-CSRF-Token": csrf },
    body: data === undefined ? undefined : JSON.stringify(data),
    signal: AbortSignal.timeout(10000),
  });
  for (const cookie of response.headers.getSetCookie()) sessionCookie = cookie.split(";", 1)[0];
  if (!response.ok) throw new Error(`管理接口 ${route} 返回 HTTP ${response.status}`);
  return response.json();
}

async function discoverRooms() {
  const configured = process.env.LIVETAP_TEST_ROOMS?.split(/[\s,]+/).filter(Boolean);
  if (configured?.length) {
    if (configured.some(id => !/^\d{9,20}$/.test(id))) throw new Error("LIVETAP_TEST_ROOMS 只接受数字直播间号");
    return [...new Set(configured)];
  }
  const response = await fetch("https://live.douyin.com/category/1_1", {
    headers: { "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/133.0.0.0 Safari/537.36" },
    signal: AbortSignal.timeout(20000),
  });
  const html = (await response.text()).replaceAll('\\"', '"');
  const ids = [...new Set([...html.matchAll(/"web_rid"\s*:\s*"(\d+)"/g)].map(match => match[1]))];
  if (!ids.length) throw new Error("分类页未提供可用房间；停止发现，不反复请求。可指定 LIVETAP_TEST_ROOMS");
  return ids.slice(0, functionalOnly ? 3 : 14);
}

async function start() {
  try {
    await fetch(`${base}/healthz`, { signal: AbortSignal.timeout(1000) });
    throw new Error("8090 已有服务，测试不会停止其他进程");
  } catch (error) {
    if (error.message.includes("已有服务")) throw error;
  }
  report.binarySHA256 = createHash("sha256").update(fs.readFileSync(binary)).digest("hex");
  const env = {
    ...process.env,
    LIVETAP_LISTEN: "127.0.0.1:8090",
    LIVETAP_ADMIN_LISTEN: "",
    LIVETAP_DATA_DIR: temporary,
    LIVETAP_SECRET_KEY: secret,
    LIVETAP_ADMIN_INITIAL_PASSWORD: initial,
    LIVETAP_NODE_NAME: "本机稳定性观察",
  };
  for (const key of Object.keys(env)) if (/^(all_proxy|http_proxy|https_proxy)$/i.test(key)) delete env[key];
  child = spawn(binary, [], { cwd: root, env, windowsHide: true, stdio: "ignore" });
  let startFailed = false;
  child.on("error", () => { startFailed = true; });
  const end = Date.now() + 30000;
  while (Date.now() < end && !startFailed) {
    if (child.exitCode !== null) break;
    try {
      const response = await fetch(`${base}/healthz`, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return;
    } catch { /* Only this local readiness check is retried. */ }
    await sleep(250);
  }
  throw new Error("本地测试实例未能启动");
}

async function main() {
  if (!Number.isFinite(startupSeconds) || startupSeconds < 0 || !Number.isFinite(observationMinutes) || observationMinutes <= 0) {
    throw new Error("启动秒数和观察分钟数必须有效");
  }
  await start();
  const login = await api("/login", "POST", { username: "admin", password: initial });
  csrf = login.csrf;
  await api("/password", "POST", { current_password: initial, new_password: password });
  csrf = (await api("/session")).csrf;
  await api("/settings", "PUT", { settings: { share_guest_identity: shareGuestIdentity }, apply: "new" });
  const ids = await discoverRooms();
  const added = await api("/rooms", "POST", { live_ids: ids, tags: ["本机实测"] });
  report.addResults = added.results;
  report.startupSamples = [];
  console.log(`启动期 ${startupSeconds} 秒；随后固定观察 ${observationMinutes} 分钟，不因连通数量不足提前结束。`);
  await observeFor({
    durationMs: startupSeconds * 1000,
    read: () => api("/snapshot?view=full"),
    sample: sample => {
      report.startupSamples.push(sample.error ? sample : {
        at: sample.at,
        rooms: sample.snapshot.rooms.map(room => ({ id: room.liveId, state: room.status, reason: room.reason })),
        cooldown: sample.snapshot.node.cooldown,
      });
      save();
    },
  });
  const first = await api("/snapshot?view=full");
  report.observation = beginObservation(first, Date.now());
  let lastProgress = 0;
  await observeFor({
    durationMs: observationMinutes * 60000,
    read: () => api("/snapshot?view=full"),
    sample: sample => {
      recordObservation(report.observation, sample);
      save();
      if (sample.at - lastProgress >= 30000) {
        const connected = sample.snapshot?.rooms.filter(room => room.status === "collecting").length ?? "未知";
        console.log(`观察中：${connected}/${ids.length} 采集中；启动时未连通的原因已单独记录。`);
        lastProgress = sample.at;
      }
    },
  });
  finishObservation(report.observation, Date.now());
  report.completed = true;
  report.stabilityPassed = !functionalOnly && report.observation.stabilityPassed;
  report.acceptance = report.stabilityPassed ? "本次设定时长内满足连续观察条件；仍需人工复核指标" : "观察已完成，10 个以上房间持续稳定采集尚未通过";
  save();
  console.log(report.acceptance);
  if (!report.stabilityPassed && !functionalOnly) process.exitCode = 1;
}

try {
  await main();
} catch (error) {
  report.error = error.message;
  save();
  console.error("实测未完成：" + error.message);
  process.exitCode = 1;
} finally {
  if (child?.pid && child.exitCode === null) {
    const stopped = new Promise(resolve => child.once("exit", resolve));
    child.kill();
    await stopped;
  }
  const absolute = path.resolve(temporary);
  if (absolute.startsWith(path.resolve(temporaryRoot) + path.sep)) fs.rmSync(absolute, { recursive: true, force: true });
}
