package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLayaConnectionOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
		case "/v1/systemone":
			var body struct {
				State string `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.State != "$" {
				t.Errorf("probe sent state %q, want the placeholder", body.State)
			}
			fmt.Fprint(w, `{"answers":{"session_state":{"choice":"s2"}}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	if got := (&App{}).TestLayaConnection(srv.URL+"/", ""); got != "" {
		t.Errorf("TestLayaConnection = %q, want empty for a working Laya server", got)
	}
}

// Review finding (PR #464): any 200 on /health read as connected, even
// from a server that cannot classify.
func TestLayaConnectionHealthyButNotLaya(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	got := (&App{}).TestLayaConnection(srv.URL, "")
	if !strings.Contains(got, "could not classify") || !strings.Contains(got, "404") {
		t.Errorf("TestLayaConnection = %q, want it to say the server cannot classify (404)", got)
	}
}

func TestLayaConnectionReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if got := (&App{}).TestLayaConnection(srv.URL, ""); !strings.Contains(got, "503") {
		t.Errorf("TestLayaConnection = %q, want the 503 named", got)
	}
}

func TestLayaConnectionUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if got := (&App{}).TestLayaConnection(url, ""); got == "" {
		t.Error("TestLayaConnection reported a closed port as healthy")
	}
}

// Review finding (PR #464): the probe asked the server's default model,
// so the test could pass while every real call, which names the model
// set in Settings, failed.
func TestLayaConnectionProbesSelectedModel(t *testing.T) {
	var model string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/systemone" {
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			model = body.Model
			fmt.Fprint(w, `{"answers":{"session_state":{"choice":"s2"}}}`)
		}
	}))
	defer srv.Close()
	if got := (&App{}).TestLayaConnection(srv.URL, " laya-terminal-ft "); got != "" {
		t.Fatalf("TestLayaConnection = %q", got)
	}
	if model != "laya-terminal-ft" {
		t.Errorf("probe asked model %q, want the selected one", model)
	}
}
