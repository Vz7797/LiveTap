package admin

import (
	"net/http"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (s *Server) poolRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"proxy", "account"} {
		path := "/admin/api/proxies"
		if kind == "account" {
			path = "/admin/api/accounts"
		}
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			v, e := s.Manager.ResourceViews(kind)
			if e != nil {
				fail(w, e)
				return
			}
			respond(w, 200, v)
		})
		save := func(w http.ResponseWriter, r *http.Request) {
			var v struct {
				Name         string   `json:"name"`
				Tags         []string `json:"tags"`
				Enabled      bool     `json:"enabled"`
				Limit        int      `json:"limit"`
				Secret       string   `json:"secret"`
				SmallAccount bool     `json:"small_account"`
			}
			if !decode(w, r, &v) {
				return
			}
			if kind == "account" && r.PathValue("id") == "" && !v.SmallAccount {
				apiError(w, 400, "small_account_required", "请确认只使用专用小号")
				return
			}
			out, e := s.Manager.SaveResource(model.Resource{ID: r.PathValue("id"), Kind: kind, Name: v.Name, Tags: v.Tags, Enabled: v.Enabled, Limit: v.Limit, Secret: v.Secret})
			if e != nil {
				fail(w, e)
				return
			}
			respond(w, 200, out)
		}
		mux.HandleFunc("POST "+path, save)
		mux.HandleFunc("PUT "+path+"/{id}", save)
		mux.HandleFunc("DELETE "+path+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			if e := s.Manager.DeleteResource(kind, r.PathValue("id")); e != nil {
				fail(w, e)
				return
			}
			respond(w, 200, map[string]bool{"ok": true})
		})
		mux.HandleFunc("POST "+path+"/{id}/check", func(w http.ResponseWriter, r *http.Request) {
			var v struct {
				ProxyID string `json:"proxy_id"`
			}
			if !decode(w, r, &v) {
				return
			}
			out, e := s.Manager.CheckResource(r.Context(), kind, r.PathValue("id"), v.ProxyID)
			if e != nil {
				fail(w, e)
				return
			}
			respond(w, 200, out)
		})
	}
	mux.HandleFunc("POST /admin/api/rooms/{id}/assignment", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Kind       string `json:"kind"`
			ResourceID string `json:"resource_id"`
			Policy     string `json:"policy"`
		}
		if !decode(w, r, &v) {
			return
		}
		if e := s.Manager.Assign(r.PathValue("id"), v.Kind, v.ResourceID, v.Policy); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
}
