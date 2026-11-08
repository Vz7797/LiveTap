package pool

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
)

func TestCheckClientUsesBrowserTLSAndHeaders(t *testing.T) {
	hello := make(chan *tls.ClientHelloInfo, 1)
	headers := make(chan http.Header, 1)
	protocol := make(chan int, 1)
	fixture := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		protocol <- r.ProtoMajor
		_, _ = io.WriteString(w, `{"status_code":0,"user":{"uid":"123"}}`)
	}))
	fixture.EnableHTTP2 = true
	fixture.TLS = &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
		hello <- info
		return nil, nil
	}}
	fixture.StartTLS()
	defer fixture.Close()
	budget := engine.NewBudget(100)
	client, err := newCheckClient("", budget)
	if err != nil {
		t.Fatal(err)
	}
	defer client.GetTransport().CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	client.GetTLSClientConfig().RootCAs = roots
	response, err := client.R().SetContext(context.Background()).SetHeader("Cookie", "sessionid=synthetic-local-fixture").Get(fixture.URL)
	if err != nil {
		t.Fatal("local browser-profile request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || budget.Count() != 1 {
		t.Fatal("probe not counted or unsuccessful")
	}
	h := <-headers
	if !strings.Contains(h.Get("User-Agent"), "Windows NT 10.0") || !strings.Contains(h.Get("User-Agent"), "Chrome/133.0.0.0") || h.Get("Sec-Ch-Ua-Platform") != `"Windows"` || h.Get("Cookie") != "sessionid=synthetic-local-fixture" {
		t.Fatal("request did not use engine browser headers")
	}
	if <-protocol != 2 {
		t.Fatal("browser HTTP/2 negotiation missing")
	}
	info := <-hello
	grease := false
	for _, suite := range info.CipherSuites {
		if suite&0x0f0f == 0x0a0a && suite>>8 == suite&0xff {
			grease = true
		}
	}
	if !grease {
		t.Fatal("TLS ClientHello lacks Chrome GREASE fingerprint")
	}
}

func TestCheckClientRejectsRedirectsAndUsesExplicitProxy(t *testing.T) {
	var redirected atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			redirected.Add(1)
			return
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	defer fixture.Close()
	b := engine.NewBudget(100)
	client, err := newCheckClient("", b)
	if err != nil {
		t.Fatal(err)
	}
	defer client.GetTransport().CloseIdleConnections()
	if client.GetTransport().Proxy != nil {
		t.Fatal("direct checks inherit system proxy")
	}
	response, err := client.R().Get(fixture.URL)
	if err != nil {
		t.Fatal("local redirect probe failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 302 || redirected.Load() != 0 || b.Count() != 1 {
		t.Fatal("redirect followed or request budget incorrect")
	}
	for _, proxy := range []string{"http://127.0.0.1:19090", "socks5://127.0.0.1:19090"} {
		client, err := newCheckClient(proxy, b)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := http.NewRequest("GET", fixture.URL, nil)
		got, err := client.GetTransport().Proxy(r)
		client.GetTransport().CloseIdleConnections()
		if err != nil || got.String() != proxy {
			t.Fatal("explicit proxy not selected")
		}
	}
	if _, err := newCheckClient("invalid-proxy", b); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}

func TestBrowserChecksTriggerAndRespectNodeCooldown(t *testing.T) {
	var calls atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fixture.Close()
	b := engine.NewBudget(100)
	client, err := newCheckClient("", b)
	if err != nil {
		t.Fatal(err)
	}
	defer client.GetTransport().CloseIdleConnections()
	if _, err := client.R().Get(fixture.URL); err == nil || !b.Cooldown().Active {
		t.Fatal("browser request bypassed restriction hook")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.R().SetContext(ctx).Get(fixture.URL); err == nil || calls.Load() != 1 {
		t.Fatal("cooldown allowed another browser request")
	}
}
