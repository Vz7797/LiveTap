# 内核来源

- 来源：https://github.com/jwwsjlm/douyinLive
- 版本：v2.3.2（2026-10-26 由 v2.3.1 升级；上游只改了 Go 版本与说明文档，内核代码未变）
- 基线提交：`3b0c44876c2a9221cb11d27041af83720b9210c5`
- 许可：MIT，原始 `LICENSE` 保留在本目录。
- 本项目补丁：`../../deploy/patches/douyinlive-v2.3.2-http-hook.patch`。
- 原始文件 SHA-256 清单：`../../deploy/patches/douyinlive-v2.3.2-baseline.json`。

副本取自该提交的 Git 跟踪文件，不包含原工作区的 Git 元数据、运行数据或凭据。修改限于请求/响应钩子、可选的游客身份提供接口、查询房间信息时优先使用未签名 web/enter 接口（`danmu_room_enter.go`，直播页改为兜底）及相应离线测试。运行 `deploy/prepare-engine.ps1` 可离线反向应用补丁并核对所有基线文件。
