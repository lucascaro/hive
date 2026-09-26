package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lucascaro/hive/internal/wire"
)

// The classification question Hive asks. Laya's /v1/systemone protocol
// (github.com/NandhaKishorM/laya laya/serve.py) is what every Laya
// server speaks — upstream laya-serve, laya-server, Rapid-MLX's
// laya-mlx integration — so it is the one wire format here.
const (
	systemOnePath = "/v1/systemone"
	questionName  = "session_state"
	instructions  = "This is the bottom of a terminal running a program or AI coding agent. What is it doing right now?"

	// MaxStateChars bounds the screen text sent. The English checkpoint
	// reads 512 tokens in all, question and options included, and
	// silently truncates the state to its FIRST window — which on a
	// terminal is the part that no longer matters. Sending only the tail
	// keeps the prompt, where "waiting for you" is written.
	MaxStateChars = 1200

	// maxResponseBytes caps the reply read. The URL is user-supplied; a
	// misbehaving server must not be able to make the daemon buffer an
	// unbounded body.
	maxResponseBytes = 64 << 10
)

// options are the five states as opaque labels. Laya's docs note that
// labels shaped like the answer ("yes", "true", a state name) can pull
// the model toward the label rather than the state, so the meaning
// lives only in the description.
var options = []struct {
	label, desc string
	state       string
}{
	{"s1", "a program is busy: output streaming, a spinner or progress bar, thinking, or a tool or command running", wire.StateWorking},
	{"s2", "nothing is happening and nothing is being asked: an empty shell prompt, or a program idle at its input", wire.StateIdle},
	{"s3", "the agent finished its reply or asked a question and is waiting for the user to answer or give the next instruction", wire.StateWaitingInput},
	{"s4", "the program is asking for permission or approval before it runs a command, edits a file or uses a tool (allow / deny, yes / no)", wire.StateWaitingPermission},
	{"s5", "the last step failed: an error message, a stack trace, an API or network error, or a failure summary at the end", wire.StateError},
}

// Request is one classification call's settings.
type Request struct {
	// BaseURL is the server root; /v1/systemone is appended.
	BaseURL string
	// Model names a checkpoint; empty lets the server pick.
	Model string
	// APIKey, when non-empty, is sent as a Bearer token.
	APIKey string
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type request struct {
	State     string              `json:"state"`
	Questions map[string]question `json:"questions"`
	Model     string              `json:"model,omitempty"`
}

type response struct {
	Answers map[string]struct {
		Choice string `json:"choice"`
	} `json:"answers"`
}

// Classify asks the server what screen shows. Every failure — transport,
// status, body, an answer outside the five labels — is an error; the
// caller keeps the state it had.
func Classify(ctx context.Context, client *http.Client, req Request, screen string) (string, error) {
	criteria := make(map[string]string, len(options))
	for _, o := range options {
		criteria[o.label] = o.desc
	}
	body, err := json.Marshal(request{
		State: Tail(screen, MaxStateChars),
		Questions: map[string]question{questionName: {
			Type: "choice", Instructions: instructions, Criteria: criteria,
		}},
		Model: req.Model,
	})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(req.BaseURL, "/") + systemOnePath
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxResponseBytes {
		return "", fmt.Errorf("laya: response over %d bytes", maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("laya: %s: %s", resp.Status, snippet(raw))
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("laya: bad response: %w", err)
	}
	choice := out.Answers[questionName].Choice
	for _, o := range options {
		if o.label == choice {
			return o.state, nil
		}
	}
	return "", fmt.Errorf("laya: unknown answer %q", choice)
}

// Tail returns the last lines of screen that fit in max bytes, dropping
// blank lines (they spend context and say nothing). A single line longer
// than max keeps its end.
func Tail(screen string, max int) string {
	lines := strings.Split(screen, "\n")
	var kept []string
	size := 0
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimRight(lines[i], " ")
		if l == "" {
			continue
		}
		if size+len(l)+1 > max {
			if len(kept) == 0 {
				kept = append(kept, strings.ToValidUTF8(l[len(l)-max:], ""))
			}
			break
		}
		kept = append(kept, l)
		size += len(l) + 1
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "\n")
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = strings.ToValidUTF8(s[:200], "") + "…"
	}
	return s
}
