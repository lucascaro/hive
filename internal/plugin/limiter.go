package plugin

import (
	"context"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Limiter budget: a plugin may spend RatePerSec tokens a second, with a
// burst of Burst. Cheap reads cost 1, fan-out mutations 10, and work
// that spawns processes or destroys state 100 — so a runaway plugin is
// held to roughly 10 session spawns or 100 broadcasts a second while a
// well-behaved one never notices. Vars so tests can shrink them.
var (
	RatePerSec = 1000.0
	Burst      = 2000.0
)

const (
	costCheap   = 1
	costFanout  = 10
	costExpense = 100
)

// CostCreateMode is what a HELLO in create mode costs: it spawns a
// session exactly like CREATE_SESSION.
const CostCreateMode = costExpense

// FrameCost is the budget a plugin spends to send one frame of type ft.
func FrameCost(ft wire.FrameType) float64 {
	switch ft {
	case wire.FrameCreateSession, wire.FrameRestartSession, wire.FrameRestoreSession,
		wire.FrameKillSession, wire.FrameKillProject,
		wire.FrameCreateWorktree, wire.FrameRemoveWorktree, wire.FrameDeleteBranch,
		wire.FrameInstallPlugin, wire.FrameShutdown:
		return costExpense
	case wire.FrameUpdateSession, wire.FrameCreateProject, wire.FrameUpdateProject,
		wire.FrameRenameWorktree, wire.FrameSetWorktreeLabel,
		wire.FrameAddIdea, wire.FrameUpdateIdea, wire.FrameRemoveIdea,
		wire.FrameResolvePrompt, wire.FrameResolveWorktreeChoice, wire.FrameResolvePlanReview,
		wire.FrameClientCommand, wire.FrameSetPluginEnabled, wire.FrameRemovePlugin:
		return costFanout
	default:
		return costCheap
	}
}

// Limiter is a token bucket shared by every connection of one plugin.
// Wait sleeps rather than refusing: throttling a plugin's reads applies
// back-pressure on its own socket and never drops a frame.
type Limiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

// NewLimiter returns a full bucket at the package budget.
func NewLimiter() *Limiter {
	return &Limiter{tokens: Burst, last: time.Now(), rate: RatePerSec, burst: Burst}
}

// Wait blocks until cost tokens are available or ctx is done. It
// reserves the tokens up front (the balance may go negative) so
// concurrent waiters queue fairly instead of all waking at once.
func (l *Limiter) Wait(ctx context.Context, cost float64) error {
	l.mu.Lock()
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
	l.tokens -= cost
	deficit := -l.tokens
	l.mu.Unlock()
	if deficit <= 0 {
		return nil
	}
	t := time.NewTimer(time.Duration(deficit / l.rate * float64(time.Second)))
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
