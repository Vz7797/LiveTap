package store

import (
	"database/sql"
	"encoding/json"
)

func (s *Store) LoadPrivate(key string, out any) error {
	if _, e := s.DB.Exec("CREATE TABLE IF NOT EXISTS private_settings(key TEXT PRIMARY KEY,data TEXT NOT NULL)"); e != nil {
		return e
	}
	var encrypted string
	e := s.DB.QueryRow("SELECT data FROM private_settings WHERE key=?", key).Scan(&encrypted)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	b, e := s.vault.Open(encrypted, "private:"+key)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (s *Store) SavePrivate(key string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO private_settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET data=excluded.data", key, s.vault.Seal(b, "private:"+key))
	return e
}
