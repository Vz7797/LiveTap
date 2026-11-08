package douyinlive

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Vz7797/LiveTap/server/internal/engine"
	dl "github.com/jwwsjlm/douyinLive/v2"
)

func TestClassifyProbe(t *testing.T) {
	other := errors.New("直播间信息接口返回异常 status_code=4003034")
	for _, c := range []struct {
		name      string
		err, ctx  error
		want      error
		wantCause string
	}{
		{"ok", nil, nil, nil, ""},
		{"captcha", fmt.Errorf("wrapped: %w", engine.ErrAccessRestricted), nil, engine.ErrAccessRestricted, "anonymous_access_restricted"},
		{"ambiguous engine result", fmt.Errorf("%w: live page without state", dl.ErrUpstreamAccessRestricted), nil, engine.ErrAccessUncertain, "upstream_uncertain"},
		{"4003099 after a stream ended", errors.New("直播间信息接口返回异常 status_code=4003099"), nil, engine.ErrAccessUncertain, "upstream_uncertain"},
		{"probe deadline", errors.New("request failed"), context.DeadlineExceeded, engine.ErrProbeTimeout, "deadline"},
		{"http client timeout", errors.New("Get web/enter: context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), nil, engine.ErrProbeTimeout, "deadline"},
		{"other status code", other, nil, other, "status_code=4003034"},
	} {
		got, cause := classifyProbe(c.err, c.ctx)
		if !errors.Is(got, c.want) && got != c.want || cause != c.wantCause {
			t.Errorf("%s: got (%v, %q), want (%v, %q)", c.name, got, cause, c.want, c.wantCause)
		}
	}
}
