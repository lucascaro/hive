//go:build !windows

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestSendToPiReachesTheRealExtension is the cross-language contract
// check for the inbox: the REAL embedded extension (under node, with a
// fake pi) and the REAL SendToPi. A fake sendUserMessage never resolves,
// like an idle Pi that has just started a turn, so this also proves the
// ack does not wait for the agent. Skipped without a node that can load
// .ts, like the other extension tests.
// The extension must drop a late note before Hive gives up on the ack
// and types it, or the note shows up twice.
func TestPiExtensionBudgetBelowAckWait(t *testing.T) {
	node := nodeForTS(t)
	if node == "" {
		t.Skip("no node that can load .ts (needs node >= 23.6, or 22.6 with --experimental-strip-types)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e",
		`const m = await import("./pi/hive.ts"); process.stdout.write(String(m.inboxTimeout.ms));`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	ms, err := strconv.Atoi(string(out))
	if err != nil {
		t.Fatalf("inboxTimeout.ms = %q: %v", out, err)
	}
	if budget := time.Duration(ms) * time.Millisecond; budget <= 0 || budget >= piAckTimeout {
		t.Fatalf("extension budget %v must be positive and below piAckTimeout %v", budget, piAckTimeout)
	}
}

func TestSendToPiReachesTheRealExtension(t *testing.T) {
	node := nodeForTS(t)
	if node == "" {
		t.Skip("no node that can load .ts (needs node >= 23.6, or 22.6 with --experimental-strip-types)")
	}
	dir := shortDir(t)
	events := filepath.Join(dir, "hived.sock")
	sock := PiInboxPath(events, "0f8fad5b-d9cb-469f-a165-70867728950e")
	if sock == "" {
		t.Fatal("no inbox path")
	}
	script := `
const m = await import("./pi/hive.ts");
const handlers = new Map();
const pi = {
  on: (n, f) => handlers.set(n, f),
  registerTool() {},
  sendUserMessage(text, opts) {
    process.stdout.write(JSON.stringify({ text, opts }) + "\n");
    return new Promise(() => {});
  },
};
m.default(pi);
handlers.get("session_start")({}, { model: { id: "m" }, isIdle: () => true, sessionManager: { getBranch: () => [] } });
process.stdout.write("ready\n");
`
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e", script)
	cmd.Dir = "." // internal/agent
	cmd.Env = append(os.Environ(),
		"HIVE_SESSION_ID=s1", "HIVE_SOCKET="+events, PiInboxEnv+"="+sock)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	rd := bufio.NewReader(out)
	if line, _ := rd.ReadString('\n'); line != "ready\n" {
		t.Fatalf("extension did not start: %q", line)
	}
	if err := SendToPi(sock, "rebase onto main\nplease"); err != nil {
		t.Fatalf("SendToPi: %v", err)
	}
	line, _ := rd.ReadString('\n')
	var got struct {
		Text string `json:"text"`
		Opts struct {
			DeliverAs string `json:"deliverAs"`
		} `json:"opts"`
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("sendUserMessage output %q: %v", line, err)
	}
	if got.Text != "rebase onto main\nplease" || got.Opts.DeliverAs != "followUp" {
		t.Fatalf("pi got %+v", got)
	}
	if fi, err := os.Stat(sock); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v; want 0600", fi, err)
	}
}
