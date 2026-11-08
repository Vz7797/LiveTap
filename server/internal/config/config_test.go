package config

import (
	"encoding/base64"
	"reflect"
	"testing"
	"time"
)

func TestWSPingConfiguration(t *testing.T) {
	t.Setenv("LIVETAP_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, tc := range []struct {
		raw     string
		seconds int
		bad     bool
	}{
		{"", 15, false}, {"20", 20, false}, {"1", 1, false}, {"0", 0, true}, {"31", 0, true}, {"abc", 0, true},
	} {
		t.Setenv("LIVETAP_WS_PING_SECONDS", tc.raw)
		cfg, err := Load()
		if (err != nil) != tc.bad || (!tc.bad && cfg.WSPingInterval != time.Duration(tc.seconds)*time.Second) {
			t.Fatal("ping configuration mismatch")
		}
	}
}

func TestStatusHeartbeatConfiguration(t *testing.T) {
	t.Setenv("LIVETAP_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, tc := range []struct {
		raw     string
		seconds int
		bad     bool
	}{
		{"", 60, false}, {"1", 1, false}, {"120", 120, false}, {"3600", 3600, false},
		{"0", 0, true}, {"-1", 0, true}, {"3601", 0, true}, {"1.5", 0, true}, {"abc", 0, true},
	} {
		t.Setenv("LIVETAP_STATUS_HEARTBEAT_SECONDS", tc.raw)
		cfg, err := Load()
		if (err != nil) != tc.bad || (!tc.bad && cfg.StatusHeartbeatInterval != time.Duration(tc.seconds)*time.Second) {
			t.Fatal("status heartbeat configuration mismatch")
		}
	}
}

func TestCookieSecureConfiguration(t *testing.T) {
	t.Setenv("LIVETAP_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, test := range []struct {
		value string
		want  bool
		bad   bool
	}{{"", false, false}, {"true", true, false}, {"false", false, false}, {"invalid", false, true}} {
		t.Setenv("LIVETAP_COOKIE_SECURE", test.value)
		cfg, err := Load()
		if (err != nil) != test.bad || (!test.bad && cfg.CookieSecure != test.want) {
			t.Fatal("secure configuration mismatch")
		}
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("LIVETAP_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("LIVETAP_COOKIE_SECURE", "false")
	for _, tc := range []struct {
		input   string
		want    []string
		invalid bool
	}{
		{"", nil, false},
		{" 127.0.0.1/32, 10.0.1.7/24, 2001:db8::7/64 ", []string{"127.0.0.1/32", "10.0.1.0/24", "2001:db8::/64"}, false},
		{"::ffff:192.0.2.7/120", []string{"192.0.2.0/24"}, false},
		{"localhost/32", nil, true}, {"127.0.0.1", nil, true},
		{"10.0.0.0/8,", nil, true}, {"10.0.0.0/33", nil, true}, {"::ffff:192.0.2.7/80", nil, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Setenv("LIVETAP_TRUSTED_PROXIES", tc.input)
			cfg, err := Load()
			if (err != nil) != tc.invalid {
				t.Fatal("unexpected configuration validation result")
			}
			if tc.invalid {
				return
			}
			var got []string
			for _, prefix := range cfg.TrustedProxies {
				got = append(got, prefix.String())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
