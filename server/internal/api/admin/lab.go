package admin

import (
	"net/http"

	"github.com/Vz7797/LiveTap/server/internal/lab"
)

func (s *Server) labRoutes(mux *http.ServeMux) {
	if !s.LabEnabled {
		mux.HandleFunc("/admin/api/lab/", func(w http.ResponseWriter, r *http.Request) {
			apiError(w, 404, "not_found", "测试实验室未开启；需要压测时设置 LIVETAP_LAB_ENABLED=true 后重启")
		})
		return
	}
	mux.HandleFunc("GET /admin/api/lab/runs", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			respond(w, 200, []any{})
			return
		}
		list, e := s.Lab.Listing()
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, list)
	})
	mux.HandleFunc("POST /admin/api/lab/runs", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			apiError(w, 503, "unavailable", "实验室尚未就绪")
			return
		}
		var p lab.Plan
		if !decode(w, r, &p) {
			return
		}
		out, e := s.Lab.Start(p)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 202, out)
	})
	mux.HandleFunc("POST /admin/api/lab/runs/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			apiError(w, 503, "unavailable", "实验室尚未就绪")
			return
		}
		if e := s.Lab.Stop(r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /admin/api/lab/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			apiError(w, 503, "unavailable", "实验室尚未就绪")
			return
		}
		if e := s.Lab.Delete(r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /admin/api/lab/runs/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			apiError(w, 503, "unavailable", "实验室尚未就绪")
			return
		}
		report, e := s.Lab.Get(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="lab-`+report.ID+`.json"`)
		_ = s.Lab.Export(report.ID, w)
	})
	mux.HandleFunc("POST /admin/api/lab/discover", func(w http.ResponseWriter, r *http.Request) {
		if s.Lab == nil {
			apiError(w, 503, "unavailable", "实验室尚未就绪")
			return
		}
		var v struct {
			Categories []string `json:"categories"`
		}
		if !decode(w, r, &v) {
			return
		}
		ids, e := s.Lab.Discover(r.Context(), v.Categories)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]any{"rooms": ids})
	})
}
