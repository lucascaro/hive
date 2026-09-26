package plugin

import (
	"os"
	"sync"
)

// logFile is the plugin's stdout+stderr, under its data dir.
const logFile = "plugin.log"

// logCap bounds plugin.log; past it the file rotates to plugin.log.1
// (replacing any earlier one), so a chatty plugin costs at most twice
// this on disk. Var so tests can shrink it.
var logCap int64 = 1 << 20

// cappedLog is an append-only log file that rotates at logCap.
type cappedLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	n    int64
}

func openCappedLog(path string) (*cappedLog, error) {
	l := &cappedLog{path: path}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *cappedLog) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.n = f, st.Size()
	return nil
}

func (l *cappedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil // closed: swallow late output from a dying run
	}
	if l.n+int64(len(p)) > logCap && l.n > 0 {
		l.f.Close()
		_ = os.Rename(l.path, l.path+".1")
		if err := l.open(); err != nil {
			l.f = nil
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.n += int64(n)
	return n, err
}

func (l *cappedLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
