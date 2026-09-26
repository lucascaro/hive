package daemon

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/laya"
	"github.com/lucascaro/hive/internal/registry"
)

const (
	// layaAPIKeyEnv holds the Laya server's key, when it wants one. It
	// is read from the daemon's environment on every call and never
	// written anywhere by Hive.
	layaAPIKeyEnv = "HIVE_LAYA_API_KEY"
	// layaCallTimeout bounds one classification. A warm local server
	// answers in tens to hundreds of milliseconds; past this the answer
	// is late enough to be about a different screen anyway.
	layaCallTimeout = 1500 * time.Millisecond
	// layaSettingsTTL is how long a read of agent-settings.json is
	// reused. The classifier consults it every time a session is due,
	// which while switched off is every cycle; a second is still "the
	// toggle applies at once".
	layaSettingsTTL = time.Second
)

// layaClassifier is the registry's Classifier: settings read live, the
// Laya client, and the per-call deadline. The registry decides when to
// call it and never learns about HTTP or keys.
func layaClassifier() registry.Classifier {
	client := laya.NewClient()
	var (
		mu     sync.Mutex
		cached agent.Settings
		readAt time.Time
	)
	settings := func() agent.Settings {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(readAt) >= layaSettingsTTL {
			// A file that will not parse reads as off: the Settings
			// screen reports it, and a broken file must not start
			// sending screens anywhere.
			s, err := agent.LoadSettings()
			if err != nil {
				s = agent.Settings{}
			}
			cached, readAt = s, time.Now()
		}
		return cached
	}
	return func(ctx context.Context, screen string) (agentstate.State, error) {
		s := settings()
		if !s.LayaEnabled {
			return "", registry.ErrClassifierOff
		}
		ctx, cancel := context.WithTimeout(ctx, layaCallTimeout)
		defer cancel()
		return laya.Classify(ctx, client, laya.Request{
			BaseURL: s.LayaEndpoint(),
			Model:   s.LayaModel,
			APIKey:  os.Getenv(layaAPIKeyEnv),
			// Defence in depth: the server is one the user chose, but a
			// screen can show anything a program printed. Redacting the
			// shapes of secrets costs the classifier nothing it needs.
		}, laya.Scrub(screen))
	}
}
