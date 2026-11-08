package admin

import (
	"maps"
	"net/http"

	"github.com/Vz7797/LiveTap/server/internal/api/northbound"
	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (s *Server) clientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/api/clients", func(w http.ResponseWriter, r *http.Request) {
		clients, _, _ := s.Northbound.Metrics()
		respond(w, 200, clients)
	})
	mux.HandleFunc("GET /admin/api/clients/{id}/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		subs, err := s.Northbound.ClientSubscriptions(r.PathValue("id"))
		if err != nil {
			northbound.WriteError(w, err)
			return
		}
		respond(w, 200, subs)
	})
	mux.HandleFunc("POST /admin/api/clients", func(w http.ResponseWriter, r *http.Request) {
		var v model.Client
		if !decode(w, r, &v) {
			return
		}
		c, key, err := s.Northbound.CreateClient(v)
		if err != nil {
			northbound.WriteError(w, err)
			return
		}
		respond(w, 201, map[string]any{"client": c, "api_key": key})
	})
	mux.HandleFunc("PUT /admin/api/clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v model.Client
		if !decode(w, r, &v) {
			return
		}
		if err := s.Northbound.UpdateClient(r.PathValue("id"), v); err != nil {
			northbound.WriteError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /admin/api/clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Northbound.DeleteClient(r.PathValue("id")); err != nil {
			northbound.WriteError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
}
func (s *Server) snapshot() map[string]any {
	original := s.Snapshots.Get()
	if s.Northbound == nil {
		return original
	}
	v := maps.Clone(original)
	clients, traffic, refs := s.Northbound.Metrics()
	v["clients"] = clients
	node := maps.Clone(v["node"].(map[string]any))
	v["node"] = node
	node["northbound"] = traffic
	node["version"] = northbound.Version
	rooms := []map[string]any{}
	for _, r := range original["rooms"].([]map[string]any) {
		copy := maps.Clone(r)
		copy["subscribers"] = refs[r["liveId"].(string)]
		rooms = append(rooms, copy)
	}
	v["rooms"] = rooms
	return v
}
