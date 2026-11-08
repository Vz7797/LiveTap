package lab

import (
	"encoding/json"
	"io"
)

func (s *Service) Export(id string, w io.Writer) error {
	report, e := s.Get(id)
	if e != nil {
		return e
	}
	data, e := json.Marshal(report)
	if e != nil {
		return e
	}
	rows, e := s.m.Store.DB.Query("SELECT data FROM lab_samples WHERE run_id=? ORDER BY id", id)
	if e != nil {
		return e
	}
	defer rows.Close()
	if _, e = io.WriteString(w, `{"report":`+string(data)+`,"samples":[`); e != nil {
		return e
	}
	first := true
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return e
		}
		if !first {
			if _, e = io.WriteString(w, ","); e != nil {
				return e
			}
		}
		first = false
		data, e := unpack(raw)
		if e != nil {
			return e
		}
		if _, e = w.Write(data); e != nil {
			return e
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	_, e = io.WriteString(w, "]}\n")
	return e
}
