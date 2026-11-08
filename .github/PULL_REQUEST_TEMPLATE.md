## 改了什么

<!-- 一两句话说明这个 PR 做了什么、为什么要做。关联的 Issue 写在这里。 -->

## 怎么验证的

- [ ] `go -C server test ./...`、`go -C sdk/go test ./...` 通过
- [ ] `node deploy/verify-engine.mjs` 通过（改了内核时必须）
- [ ] `pnpm --dir web build` 通过（改了界面时，另检查深浅主题与手机宽度）
- [ ] 真实采集验证：<!-- 是否连接真实直播间测试过；没有就写“未实测” -->

## 影响范围

- [ ] 北向接口或事件格式有变化（已更新 `docs/api.md` 并通知接入方）
- [ ] 设计行为有变化（已更新 `docs/design.md`）
- [ ] 已在 `CHANGELOG.md` 的“未发布”中记录

## 安全自查

- [ ] 没有提交密码、主密钥、API Key、Cookie、代理账号或服务器地址
