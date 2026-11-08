// Package buildinfo 是服务版本号的唯一来源。发布新版本时只改同目录的 VERSION 文件，
// 后台、健康检查和北向接口都从这里读取。
package buildinfo

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var version string

// Version 是语义化版本号，例如 1.0.0。
var Version = strings.TrimSpace(version)

// Homepage 是 LiveTap 的项目主页。
const Homepage = "https://github.com/Vz7797/LiveTap"

// 采集内核：作者的 GitHub 仓库与 third_party/douyinLive 固定的版本。升级内核时与
// deploy/patches/douyinlive-<版本>-baseline.json 一起改，测试会核对两者一致。
const (
	EngineRepo    = "jwwsjlm/douyinLive"
	EngineVersion = "v2.3.2"
	// EngineLabel 是后台和北向接口显示的内核版本（含本项目的请求钩子补丁）。
	EngineLabel = EngineVersion + " + request-hook"
)

// Revision 是构建所用的提交（前 12 位）；工作区有未提交改动时带 -dirty。拿不到时为空。
func Revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev != "" && dirty {
		rev += "-dirty"
	}
	return rev
}
