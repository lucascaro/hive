//go:build e2e

// Plugin end-to-end tests: the real hived binary supervising real
// plugin processes (Node, via the SDK in plugins/sdk), isolated like
// every other test in this package.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/wire/testclient"
)

// requireNode skips when node is missing locally, but fails in CI —
// where a silent skip would leave criterion 1 unverified.
func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required for the plugin e2e tests")
		}
		t.Skip("node not installed")
	}
}

// hookSink is an HTTP server that records every POST body.
type hookSink struct {
	srv  *httptest.Server
	hits chan map[string]any
}

func newHookSink(t *testing.T) *hookSink {
	t.Helper()
	s := &hookSink{hits: make(chan map[string]any, 64)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		s.hits <- m
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *hookSink) await(t *testing.T, sessionID string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m := <-s.hits:
			if m["session_id"] == sessionID {
				return m
			}
		case <-deadline:
			t.Fatalf("no webhook POST for session %s within %s", sessionID, timeout)
			return nil
		}
	}
}

// writeWebhookConfig writes the reference plugin's config where
// docs/plugins.md says it lives: the plugin's data dir.
func writeWebhookConfig(t *testing.T, d *spawnedDaemon, url string) {
	t.Helper()
	dir := filepath.Join(d.stateDir, "plugin-data", "webhook")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"url": url})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func awaitPlugin(t *testing.T, c *testclient.Client, pred func(wire.PluginEvent) bool) wire.PluginEvent {
	t.Helper()
	ev, err := c.AwaitPluginEvent(pred, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// waitPluginLog waits for needle in the plugin's log — how the tests
// know a Node plugin has finished connecting.
func waitPluginLog(t *testing.T, d *spawnedDaemon, id, needle string) {
	t.Helper()
	path := filepath.Join(d.stateDir, "plugin-data", id, "plugin.log")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("plugin %s never logged %q; log:\n%s", id, needle, b)
}

func ringBell(t *testing.T, d *spawnedDaemon, sid string) {
	t.Helper()
	a := dialAttach(t, d, sid)
	if err := a.WriteStdin([]byte("printf '\\a'\n")); err != nil {
		t.Fatal(err)
	}
}

// installEnableWebhook installs source, checks the install ran nothing,
// enables it, and waits for it to connect.
func installEnableWebhook(t *testing.T, d *spawnedDaemon, c *testclient.Client, source string) wire.PluginInfo {
	t.Helper()
	if err := c.InstallPlugin(source, "n-install"); err != nil {
		t.Fatal(err)
	}
	added := awaitPlugin(t, c, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventAdded })
	if added.Nonce != "n-install" || added.Plugin.Enabled || added.Plugin.Status != wire.PluginStopped {
		t.Fatalf("install = %+v; want disabled, stopped, nonce echoed", added)
	}
	// Criterion 3, daemon side: nothing runs until enabled.
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(d.stateDir, "plugin-data", "webhook", "plugin.log")); err == nil {
		t.Fatal("the plugin ran before it was enabled")
	}
	if err := c.SetPluginEnabled("webhook", true); err != nil {
		t.Fatal(err)
	}
	awaitPlugin(t, c, func(ev wire.PluginEvent) bool { return ev.Plugin.Status == wire.PluginRunning })
	waitPluginLog(t, d, "webhook", "webhook: connected")
	return added.Plugin
}

// Criterion 1: the reference plugin, installed from a local folder,
// fires end to end.
func TestE2E_WebhookPlugin_FiresOnAttention(t *testing.T) {
	requireNode(t)
	d := spawnDaemon(t)
	sink := newHookSink(t)
	writeWebhookConfig(t, d, sink.srv.URL)
	c := dialControl(t, d)
	installEnableWebhook(t, d, c, filepath.Join(repoRoot(t), "plugins", "webhook"))

	sid := firstSession(t, d)
	ringBell(t, d, sid)
	hit := sink.await(t, sid, 10*time.Second)
	// A bell marks the session as needing attention and, for a
	// heuristic-tier session like this shell, also moves its state to
	// waiting_input; the webhook reports that transition.
	if ev := hit["event"]; ev != "attention" && ev != "waiting_input" {
		t.Fatalf("webhook body = %v", hit)
	}
}

// Criterion 2: the same plugin installed from a git URL.
func TestE2E_WebhookPlugin_InstallFromGitURL(t *testing.T) {
	requireNode(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	src := filepath.Join(repoRoot(t), "plugins", "webhook")
	for _, f := range []string{"hive-plugin.json", "main.mjs", "hive-plugin.mjs", "README.md"} {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(repo, f), b, 0o600)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "webhook"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	d := spawnDaemon(t)
	sink := newHookSink(t)
	writeWebhookConfig(t, d, sink.srv.URL)
	c := dialControl(t, d)
	info := installEnableWebhook(t, d, c, "file://"+repo)
	if len(info.Commit) != 40 {
		t.Fatalf("git install commit = %q, want a pinned sha", info.Commit)
	}
	sid := firstSession(t, d)
	ringBell(t, d, sid)
	sink.await(t, sid, 10*time.Second)
}

// writeFixturePlugin writes a Node plugin whose main.mjs is script, with
// the SDK vendored beside it.
func writeFixturePlugin(t *testing.T, id, script string) string {
	t.Helper()
	dir := t.TempDir()
	man := map[string]any{"id": id, "name": id, "version": "0.0.1", "api_version": "0.1",
		"main": map[string]any{"command": []string{"node", "main.mjs"}}}
	b, _ := json.Marshal(man)
	_ = os.WriteFile(filepath.Join(dir, "hive-plugin.json"), b, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "main.mjs"), []byte(script), 0o600)
	sdk, err := os.ReadFile(filepath.Join(repoRoot(t), "plugins", "sdk", "hive-plugin.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "hive-plugin.mjs"), sdk, 0o600)
	return dir
}

func installEnable(t *testing.T, c *testclient.Client, dir, id string) {
	t.Helper()
	if err := c.InstallPlugin(dir, ""); err != nil {
		t.Fatal(err)
	}
	awaitPlugin(t, c, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventAdded && ev.Plugin.ID == id })
	if err := c.SetPluginEnabled(id, true); err != nil {
		t.Fatal(err)
	}
}

// assertHealthy is criterion 5's bar: a live session still echoes, a
// second control client still gets events, and the daemon still stops
// promptly on SIGTERM.
func assertHealthy(t *testing.T, d *spawnedDaemon) {
	t.Helper()
	sid := firstSession(t, d)
	a := dialAttach(t, d, sid)
	if err := a.WriteStdin([]byte("echo healthy-$((20+22))\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WaitForData([]byte("healthy-42"), 5*time.Second); err != nil {
		t.Fatalf("session stopped responding: %v", err)
	}
	gui := dialControl(t, d)
	name := "still-alive"
	if err := gui.UpdateSession(wire.UpdateSessionReq{SessionID: sid, Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := gui.AwaitSessionEventFunc(func(ev wire.SessionEvent) bool { return ev.Session.Name == name },
		5*time.Second, "renamed session"); err != nil {
		t.Fatalf("control client stopped getting events: %v", err)
	}
	start := time.Now()
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = d.cmd.Wait(); close(done) }()
	select {
	case <-done:
		d.cmd = nil
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon did not stop within 5s of SIGTERM (%s)", time.Since(start))
	}
}

func TestE2E_PluginCrash_SessionsSurvive(t *testing.T) {
	requireNode(t)
	d := spawnDaemon(t)
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "crasher", "process.exit(3);\n"), "crasher")
	awaitPlugin(t, c, func(ev wire.PluginEvent) bool { return ev.Plugin.Restarts >= 2 })
	assertHealthy(t, d)
}

func TestE2E_PluginHang_SessionsAndShutdownSurvive(t *testing.T) {
	requireNode(t)
	// Reads the SESSIONS snapshot to learn a session id, then stops
	// reading its control connection and opens an attach it never reads
	// either — the client shape that used to stall a session's PTY.
	script := `import net from 'node:net';
const frame = (type, obj) => {
  const body = Buffer.from(JSON.stringify(obj));
  const head = Buffer.alloc(5); head[0] = type; head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
};
const ctl = net.createConnection(process.env.HIVE_SOCKET);
ctl.write(frame(1, { version: 1, client: 'hang', mode: 'control' }));
let buf = Buffer.alloc(0);
ctl.on('data', (d) => {
  buf = Buffer.concat([buf, d]);
  while (buf.length >= 5 && buf.length >= 5 + buf.readUInt32BE(1)) {
    const t = buf[0], len = buf.readUInt32BE(1);
    const body = buf.subarray(5, 5 + len); buf = buf.subarray(5 + len);
    if (t !== 0x08) continue;
    const sid = JSON.parse(body).sessions[0].id;
    ctl.pause(); ctl.removeAllListeners('data');
    const att = net.createConnection(process.env.HIVE_SOCKET);
    att.write(frame(1, { version: 1, client: 'hang', mode: 'attach', session_id: sid }));
    att.pause();
    console.log('hang: connected');
    return;
  }
});
setInterval(() => {}, 1 << 30);
`
	d := spawnDaemon(t)
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "hanger", script), "hanger")
	waitPluginLog(t, d, "hanger", "hang: connected")
	// Generate enough output and events to fill a non-reading client.
	sid := firstSession(t, d)
	a := dialAttach(t, d, sid)
	_ = a.WriteStdin([]byte("for i in $(seq 1 20000); do echo flood-line-$i; done\n"))
	for i := 0; i < 200; i++ {
		n := "churn-" + strconv.Itoa(i)
		_ = c.UpdateSession(wire.UpdateSessionReq{SessionID: sid, Name: &n})
	}
	assertHealthy(t, d)
}

func TestE2E_PluginFlood_OtherClientsServed(t *testing.T) {
	requireNode(t)
	script := `import { connect } from './hive-plugin.mjs';
const hive = await connect();
hive.on('PROJECTS', (p) => {
  const project_id = p.projects[0].id;
  console.log('flood: go');
  const blast = () => { for (let i = 0; i < 500; i++) hive.send('ADD_IDEA', { project_id, text: 'flood ' + i }); setImmediate(blast); };
  blast();
});
`
	d := spawnDaemon(t)
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "flooder", script), "flooder")
	waitPluginLog(t, d, "flooder", "flood: go")
	time.Sleep(time.Second)
	assertHealthy(t, d)
}

// A plugin that skips the SDK and claims to be a GUI is contained all
// the same: it is throttled because of the socket it dialed.
func TestE2E_NonSDKPlugin_Contained(t *testing.T) {
	requireNode(t)
	script := `import net from 'node:net';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
const frame = (type, obj) => {
  const body = Buffer.from(JSON.stringify(obj));
  const head = Buffer.alloc(5); head[0] = type; head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
};
const s = net.createConnection(process.env.HIVE_SOCKET);
s.write(frame(1, { version: 1, client: 'hivegui/impostor', mode: 'control' }));
let buf = Buffer.alloc(0), replies = 0, started = 0;
s.on('data', (d) => {
  buf = Buffer.concat([buf, d]);
  while (buf.length >= 5 && buf.length >= 5 + buf.readUInt32BE(1)) {
    const t = buf[0]; buf = buf.subarray(5 + buf.readUInt32BE(1));
    if (t === 0x08) replies++;
    if (t === 0x02) { started = Date.now(); pump(); }
  }
});
// One pre-built batch per tick, and never more than 1 MiB queued: many
// tiny queued writes become one writev that macOS rejects (EINVAL).
const batch = Buffer.concat(Array.from({ length: 100 }, () => frame(0x07, {})));
const pump = () => {
  if (Date.now() - started >= 3000) { setTimeout(report, 3000); return; }
  if (s.writableLength < (1 << 20)) s.write(batch);
  setImmediate(pump);
};
const report = () => writeFileSync(join(process.env.HIVE_PLUGIN_DATA_DIR, 'count'), String(replies));
setInterval(() => {}, 1 << 30);
`
	d := spawnDaemon(t)
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "impostor", script), "impostor")
	countFile := filepath.Join(d.stateDir, "plugin-data", "impostor", "count")
	deadline := time.Now().Add(20 * time.Second)
	var replies int
	for {
		if b, err := os.ReadFile(countFile); err == nil {
			replies, _ = strconv.Atoi(string(b))
			break
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(filepath.Join(d.stateDir, "plugin-data", "impostor", "plugin.log"))
			t.Fatalf("impostor never reported; log:\n%s", b)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Budget: 2000 burst + 1000/s. Over ~6s (3s of sending plus 3s of
	// draining the backlog) that is at most ~8000 LIST_SESSIONS answered.
	if replies > 8800 {
		t.Fatalf("impostor got %d replies; an untagged, unthrottled client", replies)
	}
	if replies == 0 {
		t.Fatal("impostor got no replies at all")
	}
	assertHealthy(t, d)
}

// A SIGKILLed daemon leaves its plugin behind; the SDK exits on EOF, and
// the stale per-run socket means nothing could reconnect anyway. The
// restarted daemon runs exactly one copy.
func TestE2E_DaemonKill_NoDuplicatePluginAfterRestart(t *testing.T) {
	requireNode(t)
	d := spawnDaemon(t)
	sink := newHookSink(t)
	writeWebhookConfig(t, d, sink.srv.URL)
	c := dialControl(t, d)
	installEnableWebhook(t, d, c, filepath.Join(repoRoot(t), "plugins", "webhook"))
	_ = c.Close()

	_ = d.cmd.Process.Kill()
	_, _ = d.cmd.Process.Wait()
	d.cmd = nil
	logPath := filepath.Join(d.stateDir, "plugin-data", "webhook", "plugin.log")
	before, _ := os.ReadFile(logPath)

	home := filepath.Dir(d.stateDir)
	d2 := startBinary(t, d.sockPath, d.stateDir, home)
	waitForSocket(t, d.sockPath, 5*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _ := os.ReadFile(logPath)
		if strings.Count(string(b), "webhook: connected") > strings.Count(string(before), "webhook: connected") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restarted daemon did not start the plugin")
		}
		time.Sleep(50 * time.Millisecond)
	}

	sid := firstSession(t, d2)
	ringBell(t, d2, sid)
	sink.await(t, sid, 10*time.Second)
	select {
	case m := <-sink.hits:
		t.Fatalf("a second webhook POST arrived — two plugin copies are running: %v", m)
	case <-time.After(2 * time.Second):
	}
	if _, err := os.Stat(d.sockPath + ".pid"); err != nil {
		t.Fatalf("the daemon's pidfile is gone after restart: %v", err)
	}
	if stale, _ := filepath.Glob(d.sockPath + ".plugin-*"); len(stale) != 1 {
		t.Fatalf("plugin sockets after restart = %v, want exactly the live one", stale)
	}
}

// The SDK's attach() must deliver the scrollback replay that can arrive
// in the same read as WELCOME — the caller adds its 'data' listener
// only after attach() returns.
func TestE2E_SDKAttach_ReceivesReplay(t *testing.T) {
	requireNode(t)
	d := spawnDaemon(t)
	sid := firstSession(t, d)
	a := dialAttach(t, d, sid)
	if err := a.WriteStdin([]byte("echo replay-$((40+2))-marker\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WaitForData([]byte("replay-42-marker"), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	_ = a.Close()

	script := `import { connect } from './hive-plugin.mjs';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
const hive = await connect();
hive.on('SESSIONS', async (resp) => {
  const term = await hive.attach(resp.sessions[0].id);
  let seen = '';
  term.on('data', (b) => {
    seen += b.toString();
    if (seen.includes('replay-42-marker')) writeFileSync(join(process.env.HIVE_PLUGIN_DATA_DIR, 'got'), 'yes');
  });
});
`
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "replayer", script), "replayer")
	got := filepath.Join(d.stateDir, "plugin-data", "replayer", "got")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(got); err == nil {
			return
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(filepath.Join(d.stateDir, "plugin-data", "replayer", "plugin.log"))
			t.Fatalf("plugin never saw the replayed marker; log:\n%s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
