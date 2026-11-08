package admin

import (
	"net/http"

	"github.com/Vz7797/LiveTap/server/internal/auth"
)

func (s *Server) sourceIP(r *http.Request) string { return auth.SourceIP(r, s.TrustedProxies) }
