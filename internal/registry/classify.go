package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// The Laya classifier loop (spec 458; docs/design-docs/laya-state-classifier.md).
//
// A session no agent tier is speaking for — no hooks at all, or hooks
// that have sent nothing but heartbeats for HookStaleAfter — gets its
// visible screen classified by a user-run Laya model once the screen has
// changed and then held still. The call is made with r.mu RELEASED: it
// is a network round trip, and every registry operation, the state
// ticker included, blocks on that lock.

// Classifier labels a screen with one of the wire states. The daemon
// supplies it (settings, transport, API key); the registry only decides
// when to call it. An error of any kind leaves the session's state as
// it was.
type Classifier func(ctx context.Context, screen string) (agentstate.State, error)

// ErrClassifierOff is what a Classifier returns when the user has
// switched classification off. It is not a failure: nothing is recorded,
// captured or backed off, so switching it back on picks up where the
// screens are now.
var ErrClassifierOff = errors.New("registry: classifier off")

const (
	// classifyInterval is how often the loop looks for sessions to
	// classify. The same cadence as the state ticker, so a settled
	// screen is picked up within half a second of ClassifyQuietAfter.
	classifyInterval = 500 * time.Millisecond
	// classifyBackoffMin / Max bound the pause after a failed call. A
	// dead server costs one timed-out call per window, not one per
	// session per cycle.
	classifyBackoffMin = 2 * time.Second
	classifyBackoffMax = 60 * time.Second
)

// classifyCaptureDirEnv, when set, makes the daemon write every screen
// it asks Laya about to that directory, named with Laya's answer (or
// "unclassified" when the call failed): raw material for the labelled
// corpus in internal/laya/testdata/corpus.
const classifyCaptureDirEnv = "HIVE_LAYA_CAPTURE_DIR"

// layaEntry is one session's classifier bookkeeping. Guarded by r.mu.
type layaEntry struct {
	// tried is false until the first attempt; digest is the screen that
	// attempt saw, success or failure. A screen is asked about once —
	// a failed one waits for the screen to change rather than being
	// retried every cycle.
	tried  bool
	digest uint64
	// checkedAt is the last attempt, whatever it returned. It is what
	// LayaRecheckAfter is measured from, so a static screen Laya keeps
	// calling "working" costs one call per window, not one per cycle.
	checkedAt time.Time
}

// classifierState is the loop's registry-wide state.
type classifierState struct {
	// fn is nil until SetClassifier; a nil fn makes every cycle a no-op.
	// Guarded by r.mu.
	fn Classifier
	// backoff / backoffUntil pause the loop after a failed call.
	// Guarded by r.mu.
	backoff      time.Duration
	backoffUntil time.Time

	cancel context.CancelFunc
	done   chan struct{}
}

// SetClassifier installs the function the loop calls. nil disables it.
func (r *Registry) SetClassifier(fn Classifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifier.fn = fn
}

func (r *Registry) startClassifier() {
	ctx, cancel := context.WithCancel(context.Background())
	r.classifier.cancel = cancel
	r.classifier.done = make(chan struct{})
	go r.classifyStates(ctx)
}

// stopClassifier cancels the loop — aborting any call in flight — and
// waits for it to return. Safe on a Registry built without Open and
// safe to call twice.
func (r *Registry) stopClassifier() {
	if r.classifier.cancel == nil {
		return
	}
	r.classifier.cancel()
	<-r.classifier.done
}

func (r *Registry) classifyStates(ctx context.Context) {
	defer close(r.classifier.done)
	t := time.NewTicker(classifyInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			r.classifyCycle(ctx, now)
		}
	}
}

type classifyCandidate struct {
	e      *Entry
	agent  string
	digest uint64
	text   string
	quiet  time.Duration
}

// classifyCycle runs one pass: pick the sessions due for a
// classification under the lock, ask about them with it released, and
// apply each answer under the lock again. Tests drive it directly with
// a chosen clock.
func (r *Registry) classifyCycle(ctx context.Context, now time.Time) {
	r.mu.Lock()
	fn := r.classifier.fn
	if fn == nil || now.Before(r.classifier.backoffUntil) {
		r.mu.Unlock()
		return
	}
	var due []classifyCandidate
	for _, id := range r.order {
		e := r.entries[id]
		if e == nil || e.sess == nil || e.state == nil {
			continue
		}
		if c, ok := classifyDueLocked(e, now); ok {
			due = append(due, c)
		}
	}
	r.mu.Unlock()

	// The screen that settled most recently first: when several
	// sessions settle at once, the one the user most likely just
	// watched finish is the one not to keep waiting.
	sort.SliceStable(due, func(i, j int) bool { return due[i].quiet < due[j].quiet })

	captureDir := os.Getenv(classifyCaptureDirEnv)
	for _, c := range due {
		if ctx.Err() != nil {
			return
		}
		st, err := fn(ctx, c.text)
		if ctx.Err() != nil {
			// Close is waiting on us; do not re-take the lock to apply
			// an answer to a registry that is going away.
			return
		}
		if errors.Is(err, ErrClassifierOff) {
			return
		}

		if captureDir != "" {
			// Every attempt, failed ones included: the corpus is labelled
			// by hand, and the screens a server could not answer are
			// exactly the ones worth having.
			label := st
			if err != nil {
				label = "unclassified"
			}
			captureScreen(captureDir, c, label, now)
		}

		r.mu.Lock()
		c.e.laya = layaEntry{tried: true, digest: c.digest, checkedAt: now}
		if err != nil {
			r.classifier.backoff = min(max(2*r.classifier.backoff, classifyBackoffMin), classifyBackoffMax)
			r.classifier.backoffUntil = now.Add(r.classifier.backoff)
			r.mu.Unlock()
			if debugState {
				log.Printf("state: %s laya classify failed (backoff %s): %v", c.e.ID, r.classifier.backoff, err)
			}
			return
		}
		r.classifier.backoff = 0
		r.classifier.backoffUntil = time.Time{}
		r.applyClassificationLocked(c, st, now)
		r.mu.Unlock()
	}
}

// classifyDueLocked reports whether e should be classified now, and
// snapshots what the call needs.
func classifyDueLocked(e *Entry, now time.Time) (classifyCandidate, bool) {
	m := e.state
	if !m.Classifiable(now) {
		return classifyCandidate{}, false
	}
	// Only a settled screen: the sampler records every digest change,
	// so a digest the sampler has not seen yet is a screen still moving.
	d := e.sess.ScreenDigest()
	if d != e.screenDigest {
		return classifyCandidate{}, false
	}
	quiet := m.QuietFor(now)
	snap := m.Snapshot()
	var due bool
	switch {
	case !e.laya.tried || d != e.laya.digest:
		due = quiet >= agentstate.ClassifyQuietAfter
	case snap.Source == wire.StateSourceLaya && snap.State == wire.StateWorking:
		due = now.Sub(e.laya.checkedAt) >= agentstate.LayaRecheckAfter
	}
	if !due {
		return classifyCandidate{}, false
	}
	return classifyCandidate{e: e, agent: e.Agent, digest: d, text: e.sess.ScreenText(), quiet: quiet}, true
}

// applyClassificationLocked folds one answer in, provided the session is
// still the one that was asked about: same entry, still alive, screen
// unchanged. Anything else that happened meanwhile — an agent event, a
// bell, the user answering a wait — is caught by Classify's own
// Classifiable check.
func (r *Registry) applyClassificationLocked(c classifyCandidate, st agentstate.State, now time.Time) {
	e := c.e
	if r.entries[e.ID] != e || e.sess == nil || e.state == nil || e.screenDigest != c.digest {
		return
	}
	prev := e.state.Snapshot()
	if e.state.Classify(st, now) {
		r.announceStateLocked(e, prev, "laya")
	}
}

// captureScreen writes one screen for the corpus. The screen
// can hold anything the session printed, secrets included, so the
// directory and files are private. Best effort: a capture failure never
// affects state.
func captureScreen(dir string, c classifyCandidate, label string, now time.Time) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	agent := c.agent
	if agent == "" {
		agent = "shell"
	}
	if label == wire.StateIdle {
		label = "idle"
	}
	name := fmt.Sprintf("%s-%d-%s.txt", agent, now.UnixNano(), label)
	_ = os.WriteFile(filepath.Join(dir, name), []byte(c.text+"\n"), 0o600)
}

// classifierStopped reports whether the loop has returned. Tests only.
func (r *Registry) classifierStopped() bool {
	select {
	case <-r.classifier.done:
		return true
	default:
		return false
	}
}
