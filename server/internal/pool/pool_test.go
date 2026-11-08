package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	"github.com/Vz7797/LiveTap/server/internal/model"
	"github.com/Vz7797/LiveTap/server/internal/store"
)

func TestResourcesEncryptedAndSecretRetainedOnEdit(t *testing.T) {
	s, e := store.Open(t.TempDir(), bytes.Repeat([]byte{4}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := New(s, engine.NewBudget(100))
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range []model.Resource{{Kind: "account", Name: "test", Enabled: true, Limit: 5, Secret: "sessionid=synthetic-secret"}, {Kind: "proxy", Name: "test", Enabled: true, Limit: 5, Secret: "http://test:synthetic-secret@127.0.0.1:9000"}} {
		saved, err := p.Save(r)
		if err != nil {
			t.Fatal(err)
		}
		table, _ := resourceTableForTest(r.Kind)
		var data, secret string
		_ = s.DB.QueryRow("SELECT data,secret FROM "+table+" WHERE id=?", saved.ID).Scan(&data, &secret)
		if strings.Contains(data+secret, "synthetic-secret") {
			t.Fatal("plaintext persisted")
		}
		encoded, _ := json.Marshal(saved)
		if strings.Contains(string(encoded), "synthetic-secret") {
			t.Fatal("credential serialized")
		}
		saved.Secret = ""
		saved.Name = "edited"
		saved, err = p.Save(saved)
		if err != nil || saved.Secret != r.Secret {
			t.Fatal("blank edit erased credential")
		}
		p.CheckFunc = func(context.Context, model.Resource, string) Result {
			return Result{State: "invalid", Detail: "synthetic rejection"}
		}
		checked, err := p.Check(context.Background(), r.Kind, saved.ID, "")
		if err != nil || checked.State != "invalid" || Usable(checked) {
			t.Fatal("invalid resource still eligible")
		}
	}
}
func resourceTableForTest(kind string) (string, error) {
	if kind == "proxy" {
		return "proxies", nil
	}
	return "accounts", nil
}
func TestAccountCheckDoesNotInventValidity(t *testing.T) {
	for _, c := range []struct {
		status      int
		body, state string
	}{{200, `{"status_code":0,"user":{"uid":"123"}}`, "healthy"}, {200, `{"status_code":0}`, "unknown"}, {200, `{"status_code":2483}`, "invalid"}, {401, ``, "invalid"}, {200, `<html>changed page</html>`, "unknown"}} {
		if got := Classify("account", c.status, []byte(c.body)); got.State != c.state {
			t.Fatal(got)
		}
	}
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestResourceChecksShareBudgetAndCooldown(t *testing.T) {
	b := engine.NewBudget(100)
	calls := 0
	transport := &checkedTransport{budget: b, base: responseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("restricted")), Header: http.Header{}, Request: r}, nil
	})}
	request, _ := http.NewRequest("GET", "https://live.douyin.com/", nil)
	if _, e := transport.RoundTrip(request); e == nil || !b.Cooldown().Active {
		t.Fatal("restriction not applied to node")
	}
	if b.HomepageCount() != 1 {
		t.Fatal("request metric missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := transport.RoundTrip(request.WithContext(ctx)); e == nil || calls != 1 {
		t.Fatal("cooldown allowed another request")
	}
}
