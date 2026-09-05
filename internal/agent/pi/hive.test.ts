// Behavioural checks for the Hive Pi extension that only make sense on
// the TS side. The cross-language frame contract is checked from Go
// (internal/agent/pi_test.go), which decodes what encodeFrames here
// actually produces with the real wire reader.
//
// Run: node --test internal/agent/pi/
import assert from "node:assert/strict";
import net from "node:net";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const mod = await import("./hive.ts");

function fakePi() {
  const events: string[] = [];
  return { events, on: (name: string) => events.push(name) };
}

// handlerPi records the handlers instead of just their names, so a test
// can fire one and watch what reaches the socket.
function handlerPi() {
  const handlers = new Map<string, (event: unknown, ctx: unknown) => void>();
  return { handlers, on: (name: string, fn: never) => handlers.set(name, fn) };
}

// collectFrames runs body against a throwaway unix socket server and
// resolves with every AGENT_EVENT payload the extension posted. This is
// the real socket path — the extension's own encoder, over a real
// connection — not a stub of it.
async function collectFrames(
  body: (sock: string) => void | Promise<void>,
  expected: number,
): Promise<Array<Record<string, string>>> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hive-pi-"));
  const sock = path.join(dir, "h.sock");
  const events: Array<Record<string, string>> = [];
  let resolveDone: () => void;
  const done = new Promise<void>((r) => {
    resolveDone = r;
  });

  const server = net.createServer((conn) => {
    const chunks: Buffer[] = [];
    conn.on("data", (c) => chunks.push(c));
    conn.on("end", () => {
      let buf = Buffer.concat(chunks);
      while (buf.length >= 5) {
        const type = buf.readUInt8(0);
        const len = buf.readUInt32BE(1);
        if (buf.length < 5 + len) break;
        if (type === 0x22) events.push(JSON.parse(buf.subarray(5, 5 + len).toString("utf8")));
        buf = buf.subarray(5 + len);
      }
      if (events.length >= expected) resolveDone();
    });
  });

  await new Promise<void>((r) => server.listen(sock, r));
  try {
    await body(sock);
    await Promise.race([done, new Promise((r) => setTimeout(r, 5000))]);
  } finally {
    server.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  return events;
}

function withEnv(env: Record<string, string | undefined>, fn: () => void) {
  const saved = { ...process.env };
  // Assigning undefined to process.env stores the string "undefined",
  // which would defeat the very guard this file is checking.
  for (const [k, v] of Object.entries(env)) {
    if (v === undefined) delete process.env[k];
    else process.env[k] = v;
  }
  try {
    fn();
  } finally {
    process.env = saved;
  }
}

test("inert outside Hive: no subscriptions without the env", () => {
  for (const env of [
    { HIVE_SESSION_ID: undefined, HIVE_SOCKET: undefined },
    { HIVE_SESSION_ID: "s1", HIVE_SOCKET: undefined },
    { HIVE_SESSION_ID: undefined, HIVE_SOCKET: "/tmp/x.sock" },
  ]) {
    withEnv(env, () => {
      const pi = fakePi();
      mod.default(pi as never);
      assert.deepEqual(pi.events, [], `subscribed with env ${JSON.stringify(env)}`);
    });
  }
});

test("under Hive: subscribes to exactly the reported events", () => {
  withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: "/tmp/x.sock" }, () => {
    const pi = fakePi();
    mod.default(pi as never);
    assert.deepEqual(pi.events.sort(), [
      "agent_settled",
      "agent_start",
      "input",
      "session_shutdown",
      "session_start",
      "ui_prompt_end",
      "ui_prompt_start",
    ]);
  });
});

test("truncate cuts at 512 bytes, not characters, without splitting UTF-8", () => {
  assert.equal(mod.truncate("a".repeat(100)), "a".repeat(100));
  assert.equal(Buffer.from(mod.truncate("a".repeat(600)), "utf8").length, 512);
  // 3-byte chars: 512 is not a multiple of 3, so the naive cut lands
  // mid-sequence and the replacement char must not survive.
  const out = mod.truncate("€".repeat(400));
  assert.ok(Buffer.from(out, "utf8").length <= 512);
  assert.ok(!out.includes("�"));
});

test("lastAssistantText takes the newest assistant text, tolerating junk", () => {
  const branch = [
    { message: { role: "assistant", content: [{ type: "text", text: "old" }] } },
    { message: { role: "user", content: "hi" } },
    {
      message: {
        role: "assistant",
        content: [
          { type: "thinking", text: "hmm" },
          { type: "text", text: "  new " },
          { type: "toolCall", name: "bash" },
        ],
      },
    },
    { type: "compaction" },
  ];
  assert.equal(mod.lastAssistantText({ sessionManager: { getBranch: () => branch } }), "new");
  assert.equal(mod.lastAssistantText({}), "");
  assert.equal(mod.lastAssistantText({ sessionManager: { getBranch: () => null } }), "");
});

// The whole event tier is unix-socket only — `hived hook` dials
// net.Dial("unix", ...) too — so the socket-backed cases below cannot
// run on Windows, the same way the daemon's own event-mode tests skip
// there. Everything else in this file is platform-neutral.
const unixOnly = process.platform === "win32" ? { skip: "unix sockets only" } : {};

test("ui_prompt_end reports turn_end outside a turn, not permission_resolved", unixOnly, async () => {
  // Reporting permission_resolved here would leave the session showing
  // "working" with no agent_settled coming to clear it — only the 30 s
  // staleness timer, and only while PTY bytes keep arriving.
  const events = await collectFrames(async (sock) => {
    await new Promise<void>((resolve) => {
      withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
        const pi = handlerPi();
        mod.default(pi as never);
        pi.handlers.get("ui_prompt_end")!({}, { sessionManager: { getBranch: () => [] } });
        setTimeout(resolve, 200);
      });
    });
  }, 1);

  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "turn_end");
  assert.equal(events[0].source, "extension");
});

test("ui_prompt_end reports permission_resolved inside a turn", unixOnly, async () => {
  const events = await collectFrames(async (sock) => {
    await new Promise<void>((resolve) => {
      withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
        const pi = handlerPi();
        mod.default(pi as never);
        pi.handlers.get("agent_start")!({}, {});
        pi.handlers.get("ui_prompt_end")!({}, {});
        setTimeout(resolve, 200);
      });
    });
  }, 2);

  assert.deepEqual(events.map((e) => e.kind), ["permission_resolved", "permission_resolved"]);
});

test("truncate keeps a U+FFFD the user actually typed at the cut", () => {
  // 509 bytes + a 3-byte U+FFFD lands the cut exactly after it, so a
  // trailing-replacement-char strip would eat text the user really
  // typed. The cut must back off continuation bytes instead.
  const s = "a".repeat(509) + "\uFFFD" + "b".repeat(20);
  const out = mod.truncate(s);
  assert.equal(Buffer.from(out, "utf8").length, 512);
  assert.ok(out.endsWith("\uFFFD"), "the cut stripped a genuine U+FFFD");
});

test("session_shutdown reports session_end only for quit", unixOnly, async () => {
  // StateExited is terminal in agentstate.Machine.Apply, so reporting
  // session_end for /new, /resume, /fork or a reload would pin a live
  // session at "exited" with no recovery path.
  for (const [reason, want] of [
    ["quit", "session_end"],
    ["new", "turn_end"],
    ["resume", "turn_end"],
    ["fork", "turn_end"],
    ["reload", "turn_end"],
    [undefined, "turn_end"],
  ] as Array<[string | undefined, string]>) {
    const events = await collectFrames(async (sock) => {
      await new Promise<void>((resolve) => {
        withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
          const pi = handlerPi();
          mod.default(pi as never);
          pi.handlers.get("session_shutdown")!({ reason }, {});
          setTimeout(resolve, 200);
        });
      });
    }, 1);
    assert.equal(events.length, 1, `reason=${reason}`);
    assert.equal(events[0].kind, want, `reason=${reason}`);
    if (want === "turn_end") {
      assert.ok(!events[0].text, `reason=${reason}: stale summary carried over`);
    }
  }
});

test("ui_prompt_start maps confirm/select to waiting_permission, the rest to waiting_input", unixOnly, async () => {
  // One of this PR's two documented deviations from the plan, and the
  // reason the `?`-suffix heuristic could be deleted. Inverting these
  // two kinds is invisible without this test.
  for (const [kind, want] of [
    ["confirm", "waiting_permission"],
    ["select", "waiting_permission"],
    ["input", "waiting_input"],
    ["editor", "waiting_input"],
    ["custom", "waiting_input"],
    [undefined, "waiting_input"],
  ] as Array<[string | undefined, string]>) {
    const events = await collectFrames(async (sock) => {
      await new Promise<void>((resolve) => {
        withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
          const pi = handlerPi();
          mod.default(pi as never);
          pi.handlers.get("ui_prompt_start")!({ kind }, {});
          setTimeout(resolve, 200);
        });
      });
    }, 1);
    assert.equal(events.length, 1, `kind=${kind}`);
    assert.equal(events[0].kind, want, `kind=${kind}`);
  }
});

test("input reports the user's prompt but not an extension's injection", unixOnly, async () => {
  // An extension-injected message is not the user asking for something;
  // reporting it would show the session working on nobody's request.
  const injected = await collectFrames(async (sock) => {
    await new Promise<void>((resolve) => {
      withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
        const pi = handlerPi();
        mod.default(pi as never);
        pi.handlers.get("input")!({ source: "extension", text: "injected" }, {});
        setTimeout(resolve, 300);
      });
    });
  }, 1);
  assert.deepEqual(injected, [], "an extension-injected message was reported as a prompt");

  for (const source of ["interactive", "rpc"]) {
    const events = await collectFrames(async (sock) => {
      await new Promise<void>((resolve) => {
        withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
          const pi = handlerPi();
          mod.default(pi as never);
          pi.handlers.get("input")!({ source, text: "what does this do?" }, {});
          setTimeout(resolve, 200);
        });
      });
    }, 1);
    assert.equal(events.length, 1, `source=${source}`);
    assert.equal(events[0].kind, "prompt", `source=${source}`);
    assert.equal(events[0].text, "what does this do?", `source=${source}`);
  }
});
