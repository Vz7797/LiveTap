import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const pinned = "3b0c44876c2a9221cb11d27041af83720b9210c5";
const root = fileURLToPath(new URL("../", import.meta.url));

function filesUnder(directory) {
  const files = [];
  function walk(relative) {
    for (const item of fs.readdirSync(path.join(directory, relative), { withFileTypes: true })) {
      if (item.name === ".git") continue;
      if (item.isSymbolicLink()) throw new Error("内核目录不允许符号链接");
      const name = path.posix.join(relative, item.name);
      if (item.isDirectory()) walk(name);
      else if (name !== "ORIGIN.md") files.push(name);
    }
  }
  walk("");
  return files.sort();
}

export function verifyEngine({ enginePath = path.join(root, "third_party/douyinLive"), baselinePath = path.join(root, "deploy/patches/douyinlive-v2.3.2-baseline.json"), patchPath = path.join(root, "deploy/patches/douyinlive-v2.3.2-http-hook.patch"), temporaryRoot = path.join(root, "A-maintenance/tmp/engine-check") } = {}) {
  const baseline = JSON.parse(fs.readFileSync(baselinePath, "utf8"));
  if (baseline.commit !== pinned || baseline.version !== "v2.3.2") throw new Error("内核基线版本不符");
  const sourceFiles = filesUnder(enginePath);
  fs.mkdirSync(temporaryRoot, { recursive: true });
  const scratch = fs.mkdtempSync(path.join(temporaryRoot, "verify-"));
  try {
    for (const name of sourceFiles) {
      const destination = path.join(scratch, name);
      fs.mkdirSync(path.dirname(destination), { recursive: true });
      // 文本文件统一为 LF 再比对：上游仓库以 LF 存储，Windows 检出可能变成 CRLF。
      // 二进制文件（含 NUL 字节）原样复制。基线哈希按 LF 计算，任何系统检出都能校验。
      const raw = fs.readFileSync(path.join(enginePath, name));
      fs.writeFileSync(destination, raw.includes(0) ? raw : Buffer.from(raw.toString("latin1").replace(/\r\n/g, "\n"), "latin1"));
    }
    for (const args of [["init", "-q"], ["apply", "--reverse", "--whitespace=nowarn", path.resolve(patchPath)]]) {
      const result = spawnSync("git", ["-c", "core.autocrlf=false", "-c", "core.eol=lf", ...args], { cwd: scratch, encoding: "utf8", windowsHide: true });
      if (result.status !== 0) throw new Error("补丁不能还原当前内核；未修改原文件");
    }
    const names = filesUnder(scratch);
    if (JSON.stringify(names) !== JSON.stringify(Object.keys(baseline.files).sort())) throw new Error("还原后文件清单与基线不一致");
    for (const name of names) {
      const hash = createHash("sha256").update(fs.readFileSync(path.join(scratch, name))).digest("hex");
      if (hash !== baseline.files[name]) throw new Error(`基线校验失败：${name}`);
    }
    return { version: baseline.version, commit: baseline.commit, files: names.length };
  } finally {
    const absolute = path.resolve(scratch);
    if (!absolute.startsWith(path.resolve(temporaryRoot) + path.sep)) throw new Error("校验临时目录越界");
    fs.rmSync(absolute, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const result = verifyEngine();
  console.log(`内核补丁与 ${result.version} 基线一致，共 ${result.files} 个原始文件；未访问网络。`);
}
