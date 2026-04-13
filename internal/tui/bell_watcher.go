package tui

import (
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/audio"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
)

// attachBellWatcher polls for tmux bell flags while the TUI is suspended during
// session attachment. It plays the configured audio sound for each new bell
// edge and accumulates the sessionIDs that rang so the visual bell badge can be
// restored when the TUI resumes.
//
// Use newAttachBellWatcher to create, start to begin polling, stop to halt and
// retrieve results. Tests may replace getPaneTitlesFn to avoid requiring a live
// tmux server.
type attachBellWatcher struct {
	done             chan struct{}
	wg               sync.WaitGroup
	mu               sync.Mutex
	newBells         map[string]bool // sessionID → true (accumulated during watch)
	getPaneTitlesFn  func(session string) (map[string]string, map[string]bool, error)
}

func newAttachBellWatcher() *attachBellWatcher {
	return &attachBellWatcher{
		done:            make(chan struct{}),
		newBells:        make(map[string]bool),
		getPaneTitlesFn: mux.GetPaneTitles,
	}
}

// start begins polling for bell events. sessionTargets maps sessionID to tmux
// target ("session:windowIndex"). It must be called at most once per watcher.
// volume is the bell playback percentage (1–100; 0 is treated as 100).
func (w *attachBellWatcher) start(bellSound string, volume int, sessionTargets map[string]string) {
	// Build reverse map: target → sessionID.
	targetToSession := make(map[string]string, len(sessionTargets))
	for sid, target := range sessionTargets {
		targetToSession[target] = sid
	}

	// Snapshot the initial bell state so we only fire for new edges,
	// not bells that were already pending before the user attached.
	_, initialBells, _ := w.getPaneTitlesFn(mux.HiveSession)
	alreadyRinging := make(map[string]bool, len(initialBells))
	for t := range initialBells {
		alreadyRinging[t] = true
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		var lastBellTime time.Time

		for {
			select {
			case <-w.done:
				return
			case <-ticker.C:
				_, bells, err := w.getPaneTitlesFn(mux.HiveSession)
				if err != nil {
					continue
				}

				// Edge detection: find targets that are newly ringing.
				hasNew := false
				for target := range bells {
					if !alreadyRinging[target] {
						hasNew = true
						alreadyRinging[target] = true
						// Record by sessionID for the visual badge.
						if sid, ok := targetToSession[target]; ok {
							w.mu.Lock()
							w.newBells[sid] = true
							w.mu.Unlock()
						}
					}
				}
				// Clear entries for windows no longer ringing so they can re-fire.
				for target := range alreadyRinging {
					if !bells[target] {
						delete(alreadyRinging, target)
					}
				}

				if hasNew && time.Since(lastBellTime) > 500*time.Millisecond {
					audio.Play(bellSound, volume)
					lastBellTime = time.Now()
				}
			}
		}
	}()
}

// stop signals the goroutine to exit, waits for it to finish, and returns the
// set of sessionIDs that rang at least once during the watch period.
func (w *attachBellWatcher) stop() map[string]bool {
	close(w.done)
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make(map[string]bool, len(w.newBells))
	for k, v := range w.newBells {
		result[k] = v
	}
	return result
}

// buildSessionTargets constructs the sessionID → tmux target map from appState.
func buildSessionTargets(appState *state.AppState) map[string]string {
	sessions := state.AllSessions(appState)
	targets := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
		}
	}
	return targets
}
