package config

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Env struct {
	Listen, AdminListen, DataDir, NodeName, InitialPassword, LogLevel string
	Key                                                               []byte
	CookieSecure                                                      bool
	LabEnabled                                                        bool // 测试实验室会真实连接抖音直播间，正式环境默认关闭
	UpdateCheck                                                       bool // 定时向 GitHub 查询内核作者有没有新版本，只在后台提示
	TrustedProxies                                                    []netip.Prefix
	WSPingInterval                                                    time.Duration
	StatusHeartbeatInterval                                           time.Duration
}

func Load() (Env, error) {
	e := Env{Listen: value("LIVETAP_LISTEN", "0.0.0.0:8090"), AdminListen: os.Getenv("LIVETAP_ADMIN_LISTEN"), DataDir: value("LIVETAP_DATA_DIR", "/data"), NodeName: value("LIVETAP_NODE_NAME", "collector-01"), InitialPassword: os.Getenv("LIVETAP_ADMIN_INITIAL_PASSWORD"), LogLevel: value("LIVETAP_LOG_LEVEL", "INFO"), UpdateCheck: true}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("LIVETAP_SECRET_KEY"))
	if err != nil || len(key) != 32 {
		return e, errors.New("LIVETAP_SECRET_KEY 必须是 Base64 编码的 32 字节密钥")
	}
	e.Key = key
	pingSeconds, err := strconv.Atoi(value("LIVETAP_WS_PING_SECONDS", "15"))
	if err != nil || pingSeconds < 1 || pingSeconds > 30 {
		return e, errors.New("LIVETAP_WS_PING_SECONDS 须为 1～30 秒的整数")
	}
	e.WSPingInterval = time.Duration(pingSeconds) * time.Second
	heartbeatSeconds, err := strconv.Atoi(value("LIVETAP_STATUS_HEARTBEAT_SECONDS", "60"))
	if err != nil || heartbeatSeconds < 1 || heartbeatSeconds > 3600 {
		return e, errors.New("LIVETAP_STATUS_HEARTBEAT_SECONDS 须为 1～3600 秒的整数")
	}
	e.StatusHeartbeatInterval = time.Duration(heartbeatSeconds) * time.Second
	if raw := strings.TrimSpace(os.Getenv("LIVETAP_TRUSTED_PROXIES")); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
			if err != nil {
				return e, errors.New("LIVETAP_TRUSTED_PROXIES 须为逗号分隔的 IP/CIDR 网段")
			}
			if prefix.Addr().Is4In6() {
				if prefix.Bits() < 96 {
					return e, errors.New("LIVETAP_TRUSTED_PROXIES 中 IPv4 映射网段无效")
				}
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			e.TrustedProxies = append(e.TrustedProxies, prefix.Masked())
		}
	}
	if raw := os.Getenv("LIVETAP_LAB_ENABLED"); raw != "" {
		e.LabEnabled, err = strconv.ParseBool(raw)
		if err != nil {
			return e, errors.New("LIVETAP_LAB_ENABLED 须为 true 或 false")
		}
	}
	if raw := os.Getenv("LIVETAP_UPDATE_CHECK"); raw != "" {
		e.UpdateCheck, err = strconv.ParseBool(raw)
		if err != nil {
			return e, errors.New("LIVETAP_UPDATE_CHECK 须为 true 或 false")
		}
	}
	if raw := os.Getenv("LIVETAP_COOKIE_SECURE"); raw != "" {
		e.CookieSecure, err = strconv.ParseBool(raw)
		if err != nil {
			return e, errors.New("LIVETAP_COOKIE_SECURE 须为 true 或 false")
		}
	}
	return e, nil
}
func value(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
