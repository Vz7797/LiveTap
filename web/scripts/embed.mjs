import { cpSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
const source = fileURLToPath(new URL("../dist/", import.meta.url));
const target = fileURLToPath(new URL("../../server/internal/webui/dist/", import.meta.url));
const root = fileURLToPath(new URL("../../", import.meta.url));
if (resolve(target) !== resolve(root, "server/internal/webui/dist")) throw new Error("Unexpected embed target");
rmSync(target, { recursive: true, force: true });
mkdirSync(target, { recursive: true });
cpSync(source, target, { recursive: true });
// 占位文件随仓库提交：未构建前端时 Go 的 embed 仍可编译。
writeFileSync(resolve(target, ".gitkeep"), "");
