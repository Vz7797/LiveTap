// 同一 Chrome、视口、主题和构建模式下对比：静置 10 秒，然后滚动 5 秒。
// 用法：node tests/ui/performance.mjs before|after
// BASE_URL 可指向 Vite dev 或生产 preview；前后必须使用相同模式。
import { createRequire } from "node:module";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const require = createRequire(path.join(root, "web/package.json"));
const { chromium } = require("playwright-core");
const phase = process.argv[2] ?? "before";
if (!["before", "after", "production"].includes(phase)) throw new Error("阶段只能是 before、after 或 production");
const recordTrace = process.env.RECORD_TRACE !== "0";
const base = process.env.BASE_URL ?? "http://127.0.0.1:5310";
const out = path.join(root, "test-results/performance", phase);
mkdirSync(out, { recursive: true });
const assetDir = path.join(root, "web/dist/assets");
const buildAssets = existsSync(assetDir) ? readdirSync(assetDir).filter((name) => name.endsWith(".js")).map((name) => {
  const data = readFileSync(path.join(assetDir, name));
  return { name, bytes: data.length, gzipBytes: gzipSync(data).length };
}) : [];

const browser = await chromium.launch({ channel: "chrome", headless: true });
const context = await browser.newContext({
  viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1,
  colorScheme: "dark", reducedMotion: "no-preference",
});
await context.addInitScript(() => {
  localStorage.setItem("livetap-theme", "dark");
  window.__dcLongTasks = [];
  window.__dcDataTransitions = [];
  document.addEventListener("transitionrun", (event) => {
    if (/^(d|width|height|cx|cy|r|stroke-dasharray|stroke-dashoffset)$/.test(event.propertyName)) {
      window.__dcDataTransitions.push({ at: performance.now(), property: event.propertyName });
    }
  });
  new PerformanceObserver((list) => {
    for (const item of list.getEntries()) window.__dcLongTasks.push({ startTime: item.startTime, duration: item.duration });
  }).observe({ type: "longtask", buffered: true });
});
const page = await context.newPage();
const errors = [];
page.on("pageerror", (error) => errors.push(String(error)));
page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
const cdp = await context.newCDPSession(page);
await cdp.send("Performance.enable", { timeDomain: "timeTicks" });

const metrics = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map(({ name, value }) => [name, value]));
const snapshot = async () => page.evaluate(() => {
  const animations = document.getAnimations();
  return {
    domElements: document.querySelectorAll("*").length,
    animationsRunning: animations.filter((animation) => animation.playState === "running").length,
    infiniteAnimations: animations.filter((animation) => animation.effect?.getTiming().iterations === Infinity).length,
    scrollY: window.scrollY,
    scrollHeight: document.documentElement.scrollHeight,
    horizontalOverflowPx: document.documentElement.scrollWidth - window.innerWidth,
    documentVisible: document.visibilityState,
  };
});
const trace = async (name, action) => {
  if (recordTrace) await cdp.send("Tracing.start", {
    categories: "devtools.timeline,disabled-by-default-devtools.timeline,blink.user_timing,v8.execute,toplevel,cc,disabled-by-default-devtools.screenshot",
    options: "sampling-frequency=10000", transferMode: "ReturnAsStream",
  });
  const start = await metrics();
  const startAt = await page.evaluate(() => performance.now());
  const startSnapshot = await snapshot();
  await page.evaluate((label) => performance.mark(label), `${name}-start`);
  await action();
  await page.evaluate((label) => performance.mark(label), `${name}-end`);
  const endAt = await page.evaluate(() => performance.now());
  const end = await metrics();
  const endSnapshot = await snapshot();
  if (recordTrace) {
    const tracingComplete = new Promise((resolve) => cdp.once("Tracing.tracingComplete", resolve));
    await cdp.send("Tracing.end");
    const { stream } = await tracingComplete;
    const chunks = [];
    while (true) {
      const chunk = await cdp.send("IO.read", { handle: stream });
      chunks.push(chunk.base64Encoded ? Buffer.from(chunk.data, "base64") : Buffer.from(chunk.data));
      if (chunk.eof) break;
    }
    await cdp.send("IO.close", { handle: stream });
    writeFileSync(path.join(out, `${name}.trace.json`), Buffer.concat(chunks));
  }
  await page.screenshot({ path: path.join(out, `${name}.png`) });
  const longTasks = await page.evaluate(({ startAt, endAt }) => window.__dcLongTasks.filter((task) => task.startTime >= startAt && task.startTime < endAt), { startAt, endAt });
  const geometryTransitionCount = await page.evaluate(({ startAt, endAt }) => window.__dcDataTransitions.filter((item) => item.at >= startAt && item.at < endAt).length, { startAt, endAt });
  const elapsedSeconds = end.Timestamp - start.Timestamp;
  const taskSeconds = end.TaskDuration - start.TaskDuration;
  const result = {
    elapsedSeconds, taskSeconds,
    rendererMainThreadBusyPercent: (taskSeconds / elapsedSeconds) * 100,
    scriptSeconds: end.ScriptDuration - start.ScriptDuration,
    layoutSeconds: end.LayoutDuration - start.LayoutDuration,
    styleSeconds: end.RecalcStyleDuration - start.RecalcStyleDuration,
    longTaskCount: longTasks.length,
    longTaskTotalMs: longTasks.reduce((sum, task) => sum + task.duration, 0),
    longTaskMaxMs: Math.max(0, ...longTasks.map((task) => task.duration)),
    longTasks, geometryTransitionCount, startSnapshot, endSnapshot,
    trace: recordTrace ? `${name}.trace.json` : null, screenshot: `${name}.png`,
  };
  console.log(`${name}: main-thread=${result.rendererMainThreadBusyPercent.toFixed(2)}% long-tasks=${result.longTaskCount} DOM=${endSnapshot.domElements} infinite-animations=${endSnapshot.infiniteAnimations}`);
  return result;
};

try {
  await page.goto(`${base}/#/overview`, { waitUntil: "networkidle" });
  // 等首屏和模拟演示的前几次状态切换结束；前后均等待同样时间。
  await page.waitForTimeout(25_000);
  await page.mouse.move(1420, 450);
  const viteDev = await page.locator('script[src*="/@vite/client"]').count() > 0;
  const loadedJs = await page.evaluate(() => performance.getEntriesByType("resource").filter((entry) => /\.js$/.test(new URL(entry.name).pathname)).map((entry) => new URL(entry.name).pathname.split("/").pop()));
  const initialBuildJs = buildAssets.filter((asset) => loadedJs.includes(asset.name));
  const report = {
    phase, recordedAt: new Date().toISOString(), url: page.url(),
    browser: await browser.version(), headless: true,
    viewport: { width: 1440, height: 900 }, theme: "dark", reducedMotion: "no-preference",
    mode: viteDev ? "vite-dev" : "production-or-other", warmupSeconds: 25,
    buildAssets, totalBuildJsBytes: buildAssets.reduce((sum, file) => sum + file.bytes, 0),
    traceRecorded: recordTrace, initialBuildJs, initialBuildJsBytes: initialBuildJs.reduce((sum, file) => sum + file.bytes, 0),
    measurementNote: "CDP TaskDuration 差分 / Timestamp 差分是单个渲染主线程忙碌比例，不是整机 CPU，也不包含独立 GPU 进程负载。Trace 和截图记录真实页面；Trace 可导入 Chrome DevTools Performance。",
    idle: await trace("idle-10s", () => page.waitForTimeout(10_000)),
    scroll: await trace("scroll-5s", async () => {
      const started = Date.now();
      let direction = 1;
      while (Date.now() - started < 5000) {
        const { y, max } = await page.evaluate(() => ({ y: window.scrollY, max: document.documentElement.scrollHeight - window.innerHeight }));
        if (y >= max - 5) direction = -1;
        if (y <= 0) direction = 1;
        await page.mouse.wheel(0, direction * 160);
        await page.waitForTimeout(80);
      }
    }),
    errors,
  };
  writeFileSync(path.join(out, "report.json"), `${JSON.stringify(report, null, 2)}\n`);
  console.log(`报告：${path.join(out, "report.json")}`);
  if (errors.length) process.exitCode = 1;
} finally {
  await context.close();
  await browser.close();
}
