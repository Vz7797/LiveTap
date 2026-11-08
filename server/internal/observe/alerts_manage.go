package observe

import (
	"fmt"
	"strings"
	"time"
)

// AlertFilter selects alerts for listing, bulk acknowledgement and cleanup.
// Empty fields match everything; Acked is "", "0" (open) or "1" (acknowledged).
type AlertFilter struct {
	Level, Rule, LiveID, Acked string
	IDs                        []int64
}

func (f AlertFilter) where() (string, []any) {
	clauses, args := []string{"1=1"}, []any{}
	for _, c := range []struct{ column, value string }{{"level", f.Level}, {"rule", f.Rule}, {"live_id", f.LiveID}} {
		if c.value != "" {
			clauses = append(clauses, c.column+"=?")
			args = append(args, c.value)
		}
	}
	switch f.Acked {
	case "0":
		clauses = append(clauses, "acked=0")
	case "1":
		clauses = append(clauses, "acked=1")
	}
	if len(f.IDs) > 0 {
		clauses = append(clauses, "id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.IDs)), ",")+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	return strings.Join(clauses, " AND "), args
}

// Filtered lists the newest matching alerts (at most limit, capped at 500).
func (a *Alerts) Filtered(f AlertFilter, limit int) ([]Alert, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	where, args := f.where()
	rows, e := a.m.Store.DB.Query("SELECT id,at,rule,level,live_id,detail,acked FROM alerts WHERE "+where+" ORDER BY id DESC LIMIT ?", append(args, limit)...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var v Alert
		if e = rows.Scan(&v.ID, &v.At, &v.Rule, &v.Level, &v.LiveID, &v.Detail, &v.Acked); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AlertSummary feeds the header of the alerts page.
type AlertSummary struct {
	Open     map[string]int `json:"open"`      // unacknowledged by level
	Today    int            `json:"today"`     // raised since local midnight
	Total    int            `json:"total"`     // stored alerts
	TopRules []RuleCount    `json:"top_rules"` // most frequent rules in the last 24 h
	Oldest   int64          `json:"oldest"`    // Unix ms of the oldest stored alert, 0 when empty
}
type RuleCount struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

func (a *Alerts) Summary(now time.Time) (AlertSummary, error) {
	s := AlertSummary{Open: map[string]int{"critical": 0, "warning": 0, "info": 0}, TopRules: []RuleCount{}}
	db := a.m.Store.DB
	rows, e := db.Query("SELECT level,count(*) FROM alerts WHERE acked=0 GROUP BY level")
	if e != nil {
		return s, e
	}
	for rows.Next() {
		var level string
		var n int
		if e = rows.Scan(&level, &n); e != nil {
			rows.Close()
			return s, e
		}
		s.Open[level] = n
	}
	rows.Close()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	if e = db.QueryRow("SELECT count(*),COALESCE(MIN(at),0),COALESCE(SUM(at>=?),0) FROM alerts", midnight).Scan(&s.Total, &s.Oldest, &s.Today); e != nil {
		return s, e
	}
	rows, e = db.Query("SELECT rule,count(*) AS n FROM alerts WHERE at>=? GROUP BY rule ORDER BY n DESC LIMIT 3", now.Add(-24*time.Hour).UnixMilli())
	if e != nil {
		return s, e
	}
	defer rows.Close()
	for rows.Next() {
		var r RuleCount
		if e = rows.Scan(&r.Rule, &r.Count); e != nil {
			return s, e
		}
		s.TopRules = append(s.TopRules, r)
	}
	return s, rows.Err()
}

// AckMatching acknowledges every open alert matching the filter.
func (a *Alerts) AckMatching(f AlertFilter) (int64, error) {
	f.Acked = "0"
	where, args := f.where()
	res, e := a.m.Store.DB.Exec("UPDATE alerts SET acked=1 WHERE "+where, args...)
	if e != nil {
		return 0, e
	}
	n, _ := res.RowsAffected()
	return n, a.m.Store.Audit("alert_ack_bulk", "alerts", fmt.Sprintf("批量确认 %d 条告警", n))
}

// Purge deletes alerts: only acknowledged ones, or everything raised before
// the given time (Unix ms). Open alerts are kept unless they are older than before.
func (a *Alerts) Purge(ackedOnly bool, before int64) (int64, error) {
	query, args := "DELETE FROM alerts WHERE acked=1", []any{}
	if !ackedOnly {
		if before <= 0 {
			return 0, fmt.Errorf("需要指定清理时间点")
		}
		query, args = "DELETE FROM alerts WHERE at<?", []any{before}
	}
	res, e := a.m.Store.DB.Exec(query, args...)
	if e != nil {
		return 0, e
	}
	n, _ := res.RowsAffected()
	detail := fmt.Sprintf("清理已确认的告警 %d 条", n)
	if !ackedOnly {
		detail = fmt.Sprintf("清理 %s 之前的告警 %d 条", time.UnixMilli(before).Format("2006-01-02 15:04"), n)
	}
	return n, a.m.Store.Audit("alert_purge", "alerts", detail)
}
