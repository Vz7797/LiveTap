package observe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// LogFile describes one log file on disk (the current file and rotated backups).
type LogFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

// LogStats summarises the log storage for the logs page.
type LogStats struct {
	Files      []LogFile      `json:"files"`
	TotalBytes int64          `json:"total_bytes"`
	MaxBytes   int64          `json:"max_bytes"`
	Backups    int            `json:"backups"`
	Buffered   int            `json:"buffered"`
	ByLevel    map[string]int `json:"by_level"`
}

// files lists the current log and its backups, oldest first.
func (l *Logs) files() []LogFile {
	out := []LogFile{}
	names := []string{"collector.log"}
	for i := 1; i <= l.backups; i++ {
		names = append(names, fmt.Sprintf("collector.log.%d", i))
	}
	for _, name := range names {
		if st, e := os.Stat(filepath.Join(l.dir, name)); e == nil {
			out = append(out, LogFile{Name: name, Size: st.Size(), Modified: st.ModTime().UnixMilli()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified < out[j].Modified })
	return out
}

func (l *Logs) Stats() LogStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := LogStats{Files: l.files(), MaxBytes: l.max, Backups: l.backups, Buffered: len(l.entries), ByLevel: map[string]int{}}
	for _, f := range s.Files {
		s.TotalBytes += f.Size
	}
	for _, e := range l.entries {
		s.ByLevel[e.Level]++
	}
	return s
}

// Export writes every log file, oldest first, as one redacted text stream (log
// lines are redacted when written).
func (l *Logs) Export(w io.Writer) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.files() {
		file, e := os.Open(filepath.Join(l.dir, f.Name))
		if e != nil {
			continue
		}
		_, e = io.Copy(w, file)
		file.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

// Purge deletes rotated backups, empties the current file and the in-memory
// buffer. It returns the number of bytes freed.
func (l *Logs) Purge() (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var freed int64
	for _, f := range l.files() {
		path := filepath.Join(l.dir, f.Name)
		if f.Name == "collector.log" {
			// Reopen instead of truncating the append-only handle (not allowed on Windows).
			if e := l.file.Close(); e != nil {
				return freed, e
			}
			if e := os.WriteFile(path, nil, 0600); e != nil {
				return freed, e
			}
			if e := l.open(); e != nil {
				return freed, e
			}
		} else if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
			return freed, e
		}
		freed += f.Size
	}
	l.entries = nil
	return freed, nil
}
