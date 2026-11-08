# 参与开发

## 基本原则

- **文档与代码同步**：行为或接口变更时，同步更新 [docs/](docs/) 中对应的文档。
- **只读采集**：不发送弹幕、不点赞、不处理验证码；不使用商家主账号，只用专用小号。
- **凭据不进仓库**：密码、主密钥、API Key、Cookie、代理账号一律不写入代码、文档、日志、测试输出或提交记录。测试使用明显的假值。
- 目录结构、构建与测试命令见 [docs/development.md](docs/development.md)。

## 分支与提交

- 默认分支 `main`。功能或修复在独立分支上开发，例如 `feat/lab-cold-rooms`、`fix/rooms-blank-page`，通过 Pull Request 合并。
- 提交信息采用 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/v1.0.0/)：

  ```text
  <类型>(<范围>): <一句话说明>

  <改了什么功能、为什么改>
  ```

  常用类型：`feat` 新功能、`fix` 修复、`docs` 文档、`test` 测试、`refactor` 重构、`perf` 性能、`build` 构建与依赖、`ci` 持续集成、`chore` 杂项。范围示例：`engine`、`room`、`northbound`、`web`、`sdk`、`deploy`。

- 一个提交只做一件事，提交信息只描述功能上的变化；对使用者或接入方有影响的变更，在 [CHANGELOG.md](CHANGELOG.md) 的“未发布”中记录。
- Pull Request 的说明里写清楚做了哪些验证。

## 提交前检查

```powershell
go -C server vet ./...
go -C server test ./...
go -C sdk/go test ./...
node deploy/verify-engine.mjs
pnpm --dir web build
```

修改界面时，检查深浅主题与手机宽度（`tests/ui/` 下有离线检查脚本）；修改内核时，重新生成补丁并通过 `verify-engine.mjs`。CI 会在每次推送和 Pull Request 时自动运行上述检查。
