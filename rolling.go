package glogs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// rollingWriter is a size- and date-based rolling file writer with retention.
// Current file:   <dir>/<name>.log
// Rotated files:  <dir>/<name>.YYYY-MM-DD.<index>.log
type rollingWriter struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxAgeDays int
	totalCap   int64
	now        func() time.Time

	file  *os.File
	size  int64
	day   string
	index int
}

func newRollingWriter(path string, maxSize int64, maxAgeDays int, totalCap int64) (*rollingWriter, error) {
	w := &rollingWriter{
		path:       path,
		maxSize:    maxSize,
		maxAgeDays: maxAgeDays,
		totalCap:   totalCap,
		now:        time.Now,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	w.cleanup()
	return w, nil
}

func (w *rollingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = fi.Size()
	w.day = w.now().Format("2006-01-02")
	return nil
}

func (w *rollingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	today := w.now().Format("2006-01-02")
	if today != w.day || (w.maxSize > 0 && w.size+int64(len(p)) > w.maxSize) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rollingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	base := strings.TrimSuffix(w.path, filepath.Ext(w.path))
	ext := filepath.Ext(w.path)
	for i := 0; ; i++ {
		rotated := fmt.Sprintf("%s.%s.%d%s", base, w.day, i, ext)
		if _, err := os.Stat(rotated); os.IsNotExist(err) {
			if err := os.Rename(w.path, rotated); err != nil && !os.IsNotExist(err) {
				return err
			}
			break
		}
	}
	w.size = 0
	w.day = w.now().Format("2006-01-02")
	return w.open()
}

func (w *rollingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rollingWriter) cleanup() {
	dir := filepath.Dir(w.path)
	base := strings.TrimSuffix(filepath.Base(w.path), filepath.Ext(w.path))
	ext := filepath.Ext(w.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type rotatedFile struct {
		path string
		date time.Time
		size int64
	}
	var files []rotatedFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base+".") || !strings.HasSuffix(name, ext) {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, base+"."), ext), ".")
		if len(parts) < 2 {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", parts[0], time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, rotatedFile{path: filepath.Join(dir, name), date: date, size: info.Size()})
	}
	cutoff := w.now().AddDate(0, 0, -w.maxAgeDays)
	var kept []rotatedFile
	var total int64
	for _, f := range files {
		if f.date.Before(cutoff) {
			_ = os.Remove(f.path)
			continue
		}
		kept = append(kept, f)
		total += f.size
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].date.Before(kept[j].date) })
	for _, f := range kept {
		if w.totalCap <= 0 || total <= w.totalCap {
			break
		}
		_ = os.Remove(f.path)
		total -= f.size
	}
}
