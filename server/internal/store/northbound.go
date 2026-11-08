package store

import (
	"encoding/json"

	"github.com/Vz7797/LiveTap/server/internal/model"
)

func (s *Store) InitNorthbound() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS clients(id TEXT PRIMARY KEY, key_hash TEXT NOT NULL UNIQUE, data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,live_id TEXT NOT NULL,data TEXT NOT NULL,UNIQUE(client_id,live_id));`)
	return err
}
func (s *Store) Clients() ([]model.Client, error) {
	rows, err := s.DB.Query("SELECT id,key_hash,data FROM clients ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Client{}
	for rows.Next() {
		var c model.Client
		var data string
		if err = rows.Scan(&c.ID, &c.KeyHash, &data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) SaveClient(c model.Client) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO clients VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", c.ID, c.KeyHash, string(data))
	return err
}
func (s *Store) DeleteClient(id string) error {
	_, err := s.DB.Exec("DELETE FROM clients WHERE id=?", id)
	return err
}
func (s *Store) Subscriptions() ([]model.Subscription, error) {
	rows, err := s.DB.Query("SELECT client_id,data FROM subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Subscription{}
	for rows.Next() {
		var sub model.Subscription
		var data string
		if err = rows.Scan(&sub.ClientID, &data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
func (s *Store) SaveSubscription(sub model.Subscription) error {
	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO subscriptions VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", sub.ID, sub.ClientID, sub.LiveID, string(data))
	return err
}
func (s *Store) DeleteSubscription(id string) error {
	_, err := s.DB.Exec("DELETE FROM subscriptions WHERE id=?", id)
	return err
}
