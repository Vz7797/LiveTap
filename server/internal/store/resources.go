package store

import (
	"encoding/json"
	"errors"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (s *Store) InitResources() error {
	for _, table := range []string{"proxies", "accounts"} {
		if _, e := s.DB.Exec("CREATE TABLE IF NOT EXISTS " + table + "(id TEXT PRIMARY KEY,data TEXT NOT NULL,secret TEXT NOT NULL)"); e != nil {
			return e
		}
	}
	return nil
}
func resourceTable(kind string) (string, error) {
	switch kind {
	case "proxy":
		return "proxies", nil
	case "account":
		return "accounts", nil
	}
	return "", errors.New("资源类型无效")
}
func (s *Store) Resources(kind string) ([]model.Resource, error) {
	table, e := resourceTable(kind)
	if e != nil {
		return nil, e
	}
	rows, e := s.DB.Query("SELECT id,data,secret FROM " + table + " ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Resource{}
	for rows.Next() {
		var id, data, encrypted string
		if e = rows.Scan(&id, &data, &encrypted); e != nil {
			return nil, e
		}
		var r model.Resource
		if e = json.Unmarshal([]byte(data), &r); e != nil {
			return nil, e
		}
		raw, e := s.vault.Open(encrypted, kind+":"+id)
		if e != nil {
			return nil, e
		}
		r.Secret = string(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SaveResource(r model.Resource) error {
	table, e := resourceTable(r.Kind)
	if e != nil {
		return e
	}
	r.Rooms = nil
	data, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO "+table+" VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data,secret=excluded.secret", r.ID, string(data), s.vault.Seal([]byte(r.Secret), r.Kind+":"+r.ID))
	return e
}
func (s *Store) DeleteResource(kind, id string) error {
	table, e := resourceTable(kind)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("DELETE FROM "+table+" WHERE id=?", id)
	return e
}
