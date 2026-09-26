package laya

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// server answers every call with choice and hands the decoded request
// to inspect.
func server(t *testing.T, choice string, inspect func(*http.Request, request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if inspect != nil {
			inspect(r, req)
		}
		fmt.Fprintf(w, `{"model":"laya","answers":{"session_state":{"choice":%q,"probabilities":{},"confidence":0.9}}}`, choice)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClassifyRequestShape(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("log line %03d", i))
	}
	lines = append(lines, "", "Allow edit to main.go? (y/n)")
	srv := server(t, "s4", func(r *http.Request, req request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Errorf("%s %s, want POST /v1/systemone", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q, want the Bearer key", got)
		}
		if req.Model != "ft" {
			t.Errorf("model = %q, want ft", req.Model)
		}
		if len(req.State) > MaxStateChars {
			t.Errorf("state is %d bytes, over MaxStateChars", len(req.State))
		}
		if !strings.HasSuffix(req.State, "Allow edit to main.go? (y/n)") {
			t.Errorf("state does not end with the screen's last line: %q", req.State[len(req.State)-40:])
		}
		if strings.Contains(req.State, "log line 000") {
			t.Error("state kept the top of the screen, not the tail")
		}
		q, ok := req.Questions["session_state"]
		if !ok || len(req.Questions) != 1 || q.Type != "choice" {
			t.Fatalf("questions = %+v, want one choice question", req.Questions)
		}
		for label := range q.Criteria {
			for _, s := range []string{"working", "idle", "waiting", "permission", "error", "yes", "no"} {
				if strings.Contains(label, s) {
					t.Errorf("label %q is not opaque", label)
				}
			}
		}
		if len(q.Criteria) != 5 {
			t.Errorf("%d options, want 5", len(q.Criteria))
		}
	})
	got, err := Classify(context.Background(), srv.Client(),
		Request{BaseURL: srv.URL + "/", Model: "ft", APIKey: "k"}, strings.Join(lines, "\n"))
	if err != nil || got != wire.StateWaitingPermission {
		t.Errorf("Classify = %q, %v; want waiting_permission", got, err)
	}
}

func TestClassifyNoKeyNoAuthHeader(t *testing.T) {
	srv := server(t, "s2", func(r *http.Request, req request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization sent with no key configured")
		}
		if req.Model != "" {
			t.Errorf("model = %q, want omitted", req.Model)
		}
	})
	if _, err := Classify(context.Background(), srv.Client(), Request{BaseURL: srv.URL}, "$ "); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyMapsChoiceToState(t *testing.T) {
	for label, want := range map[string]string{
		"s1": wire.StateWorking, "s2": wire.StateIdle, "s3": wire.StateWaitingInput,
		"s4": wire.StateWaitingPermission, "s5": wire.StateError,
	} {
		srv := server(t, label, nil)
		got, err := Classify(context.Background(), srv.Client(), Request{BaseURL: srv.URL}, "x")
		if err != nil || got != want {
			t.Errorf("%s: Classify = %q, %v; want %q", label, got, err, want)
		}
	}
}

func TestClassifyUnknownLabelIsError(t *testing.T) {
	srv := server(t, "s9", nil)
	if _, err := Classify(context.Background(), srv.Client(), Request{BaseURL: srv.URL}, "x"); err == nil {
		t.Error("an unknown label was accepted")
	}
}

func TestClassifyNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"too many options"}`, http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	_, err := Classify(context.Background(), srv.Client(), Request{BaseURL: srv.URL}, "x")
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Errorf("err = %v, want the 422 named", err)
	}
}

func TestClassifyTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Classify(ctx, srv.Client(), Request{BaseURL: srv.URL}, "x"); err == nil {
		t.Fatal("a hung server returned no error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Classify took %s past a 100ms deadline", d)
	}
}

func TestClassifyResponseCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"answers":{"session_state":{"choice":"s1","pad":"`))
		w.Write([]byte(strings.Repeat("a", 2*maxResponseBytes)))
		w.Write([]byte(`"}}}`))
	}))
	defer srv.Close()
	if _, err := Classify(context.Background(), srv.Client(), Request{BaseURL: srv.URL}, "x"); err == nil {
		t.Error("an oversized response was accepted")
	}
}

func TestTailKeepsBottom(t *testing.T) {
	if got := Tail("a\n\n b  \n\nc\n\n", 100); got != "a\n b\nc" {
		t.Errorf("Tail dropped the wrong lines: %q", got)
	}
	if got := Tail("first\nsecond\nthird", 12); got != "third" && got != "second\nthird" {
		t.Errorf("Tail = %q, want the last lines that fit", got)
	}
	if got := Tail("x\n"+strings.Repeat("y", 50)+"END", 10); got != "yyyyyyyEND" {
		t.Errorf("Tail of an over-long line = %q, want its end", got)
	}
}

// Review finding (PR #464): Go's default client re-sends a POST body on
// a 307/308, to any host. Screen text must reach only the configured URL.
func TestClassifyDoesNotFollowRedirects(t *testing.T) {
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = true
		fmt.Fprint(w, `{"answers":{"session_state":{"choice":"s2"}}}`)
	}))
	defer elsewhere.Close()
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer configured.Close()
	_, err := Classify(context.Background(), NewClient(), Request{BaseURL: configured.URL}, "secret screen")
	if leaked {
		t.Fatal("the screen was re-sent to the redirect target")
	}
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Errorf("err = %v, want the redirect reported as a 307 failure", err)
	}
}
