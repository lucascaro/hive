package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

func layaSettings(t *testing.T, s agent.Settings) {
	t.Helper()
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	if err := agent.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
}

func TestLayaClassifierOffByDefault(t *testing.T) {
	layaSettings(t, agent.DefaultSettings())
	if _, err := layaClassifier()(context.Background(), "x"); !errors.Is(err, registry.ErrClassifierOff) {
		t.Errorf("err = %v, want ErrClassifierOff", err)
	}
}

func TestLayaClassifierUsesSettingsAndEnvKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"answers":{"session_state":{"choice":"s3"}}}`)
	}))
	defer srv.Close()
	s := agent.DefaultSettings()
	s.LayaEnabled, s.LayaURL = true, srv.URL
	layaSettings(t, s)
	t.Setenv(layaAPIKeyEnv, "k")
	got, err := layaClassifier()(context.Background(), "Proceed?")
	if err != nil || got != wire.StateWaitingInput {
		t.Fatalf("classify = %q, %v; want waiting_input", got, err)
	}
	if auth != "Bearer k" {
		t.Errorf("Authorization = %q, want the key from %s", auth, layaAPIKeyEnv)
	}
}

// A settings file that will not parse must not start sending screens.
func TestLayaClassifierBrokenSettingsIsOff(t *testing.T) {
	dir := t.TempDir()
	agent.SetCustomDir(dir)
	t.Cleanup(func() { agent.SetCustomDir("") })
	if err := os.WriteFile(filepath.Join(dir, agent.SettingsFileName), []byte(`{"laya_enabled": true,`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := layaClassifier()(context.Background(), "x"); !errors.Is(err, registry.ErrClassifierOff) {
		t.Errorf("err = %v, want ErrClassifierOff for an unparseable file", err)
	}
}

// Review finding (PR #464): live screens went to the server unscrubbed.
func TestLayaClassifierScrubsScreen(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = body.State
		fmt.Fprint(w, `{"answers":{"session_state":{"choice":"s2"}}}`)
	}))
	defer srv.Close()
	s := agent.DefaultSettings()
	s.LayaEnabled, s.LayaURL = true, srv.URL
	layaSettings(t, s)
	const secret = "sk-ant-api03-abcdefghijklmnopqrstuv"
	if _, err := layaClassifier()(context.Background(), "$ export KEY="+secret+"\n$ "); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent, secret) || !strings.Contains(sent, "<redacted") {
		t.Errorf("state sent = %q, want the key redacted", sent)
	}
}
