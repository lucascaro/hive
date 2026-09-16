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
  const tools: any[] = [];
  return { events, tools, on: (name: string) => events.push(name), registerTool: (t: any) => tools.push(t) };
}

// handlerPi records the handlers instead of just their names, so a test
// can fire one and watch what reaches the socket.
function handlerPi() {
  const handlers = new Map<string, (event: unknown, ctx: unknown) => void>();
  const tools: any[] = [];
  return { handlers, tools, on: (name: string, fn: never) => handlers.set(name, fn), registerTool: (t: any) => tools.push(t) };
}

// collectConnections runs body against a throwaway unix socket server
// and resolves with the AGENT_EVENT payloads the extension posted,
// grouped by the connection they arrived on. This is the real socket
// path — the extension's own encoder, over a real connection — not a
// stub of it.
async function collectConnections(
  body: (sock: string) => void | Promise<void>,
  expected: number,
): Promise<Array<Array<Record<string, any>>>> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hive-pi-"));
  const sock = path.join(dir, "h.sock");
  const conns: Array<Array<Record<string, any>>> = [];
  let resolveDone: () => void;
  const done = new Promise<void>((r) => {
    resolveDone = r;
  });

  const server = net.createServer((conn) => {
    const chunks: Buffer[] = [];
    conn.on("data", (c) => chunks.push(c));
    conn.on("end", () => {
      let buf = Buffer.concat(chunks);
      const events: Array<Record<string, any>> = [];
      while (buf.length >= 5) {
        const type = buf.readUInt8(0);
        const len = buf.readUInt32BE(1);
        if (buf.length < 5 + len) break;
        if (type === 0x22) events.push(JSON.parse(buf.subarray(5, 5 + len).toString("utf8")));
        buf = buf.subarray(5 + len);
      }
      conns.push(events);
      if (conns.flat().length >= expected) resolveDone();
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
  return conns;
}

// collectFrames is collectConnections, flattened.
async function collectFrames(
  body: (sock: string) => void | Promise<void>,
  expected: number,
): Promise<Array<Record<string, any>>> {
  return (await collectConnections(body, expected)).flat();
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
      "agent_end",
      "agent_settled",
      "agent_start",
      "input",
      "session_shutdown",
      "session_start",
      "session_tree",
      "tool_execution_end",
      "tool_execution_start",
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

test("runEnd classifies a run by its final assistant message", () => {
  const ok = { role: "assistant", stopReason: "stop" };
  const failed = { role: "assistant", stopReason: "error", errorMessage: "429 rate limited" };
  const aborted = { role: "assistant", stopReason: "aborted" };
  const done = { kind: "done", text: "" };
  assert.deepEqual(mod.runEnd([ok, failed]), { kind: "error", text: "429 rate limited" });
  assert.deepEqual(mod.runEnd([failed, { role: "user" }]), { kind: "error", text: "429 rate limited" });
  // A retry that succeeded supersedes the earlier failure.
  assert.deepEqual(mod.runEnd([failed, ok]), done);
  assert.deepEqual(mod.runEnd([ok, aborted]), { kind: "aborted", text: "" });
  assert.deepEqual(mod.runEnd([{ role: "assistant", stopReason: "error" }]), { kind: "error", text: "error" });
  assert.deepEqual(mod.runEnd(undefined), done);
});

test("agent_settled after an Esc abort reports idle, not a finished turn", unixOnly, async () => {
  const events = await collectFrames(async (sock) => {
    await new Promise<void>((resolve) => {
      withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
        const pi = handlerPi();
        mod.default(pi as never);
        const ctx = { sessionManager: { getBranch: () => [] } };
        pi.handlers.get("agent_start")!({}, ctx);
        pi.handlers.get("agent_end")!({ messages: [{ role: "assistant", stopReason: "aborted" }] }, ctx);
        pi.handlers.get("agent_settled")!({}, ctx);
        setTimeout(resolve, 300);
      });
    });
  }, 2);

  assert.deepEqual(events.map((e) => e.kind), ["permission_resolved", "idle"]);
});

test("agent_settled reports a failed run as error, and a retried one as turn_end", unixOnly, async () => {
  const failed = { messages: [{ role: "assistant", stopReason: "error", errorMessage: "overloaded" }] };
  const ok = { messages: [{ role: "assistant", stopReason: "stop" }] };
  const events = await collectFrames(async (sock) => {
    await new Promise<void>((resolve) => {
      withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock }, () => {
        const pi = handlerPi();
        mod.default(pi as never);
        const ctx = { sessionManager: { getBranch: () => [] } };
        // A run that fails and settles.
        pi.handlers.get("agent_start")!({}, ctx);
        pi.handlers.get("agent_end")!(failed, ctx);
        pi.handlers.get("agent_settled")!({}, ctx);
        // A run that fails, is retried, and succeeds.
        setTimeout(() => {
          pi.handlers.get("agent_start")!({}, ctx);
          pi.handlers.get("agent_end")!(failed, ctx);
          pi.handlers.get("agent_start")!({}, ctx);
          pi.handlers.get("agent_end")!(ok, ctx);
          pi.handlers.get("agent_settled")!({}, ctx);
          setTimeout(resolve, 300);
        }, 100);
      });
    });
  }, 5);

  const kinds = events.map((e) => e.kind).filter((k) => k !== "permission_resolved");
  assert.deepEqual(kinds, ["error", "turn_end"]);
  assert.equal(events.find((e) => e.kind === "error")?.text, "overloaded");
});

test("ui_prompt_end reports idle outside a turn, not permission_resolved", unixOnly, async () => {
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
  assert.equal(events[0].kind, "idle");
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
    ["new", "idle"],
    ["resume", "idle"],
    ["fork", "idle"],
    ["reload", "idle"],
    [undefined, "idle"],
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
    if (want === "idle") {
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

// --- Phase 3: tool events, the hive_todo plan, the send queue ---

// underHive runs the extension with a real socket, fires events at it,
// and returns what arrived, grouped per connection. `expected` is the
// exact count: collection ends once it arrives, but fire's settle window
// always elapses first, so an unexpected extra event is still caught. HIVE_PI_TODO_TOOL is
// cleared unless the test sets it, so a developer's own environment
// cannot flip the default under test.
async function underHive(
  fire: (pi: ReturnType<typeof handlerPi>) => void | Promise<void>,
  expected: number,
  env: Record<string, string | undefined> = {},
  settleMs = 300,
) {
  return collectConnections(async (sock) => {
    let pi!: ReturnType<typeof handlerPi>;
    withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: sock, HIVE_PI_TODO_TOOL: undefined, ...env }, () => {
      pi = handlerPi();
      mod.default(pi as never);
    });
    await fire(pi);
    await new Promise((r) => setTimeout(r, settleMs));
  }, expected);
}

const todoBranch = (todos: unknown, extra: Record<string, unknown> = {}) => [
  { type: "message", message: { role: "toolResult", toolName: "hive_todo", details: { todos }, ...extra } },
];

test("tool_execution_start posts tool_start with the tool, a derived label and the call id", unixOnly, async () => {
  const conns = await underHive((pi) => {
    pi.handlers.get("tool_execution_start")!(
      { toolCallId: "call-1", toolName: "bash", args: { command: "FOO=1 npm test --secret=hunter2", extra: "SECRETVALUE" } },
      {},
    );
  }, 1);
  const events = conns.flat();
  assert.equal(events.length, 1);
  const { at, ...rest } = events[0];
  assert.ok(at);
  assert.deepEqual(rest, { session_id: "s1", kind: "tool_start", source: "extension", tool: "bash", target: "npm test", call_id: "call-1" });
  // The privacy rule, asserted on the bytes that crossed the socket.
  const raw = JSON.stringify(conns);
  assert.ok(!raw.includes("hunter2") && !raw.includes("SECRETVALUE") && !raw.includes("args"), raw);
});

test("tool_execution_end posts tool_end with ok always explicit and no result", unixOnly, async () => {
  const conns = await underHive((pi) => {
    const end = pi.handlers.get("tool_execution_end")!;
    end({ toolCallId: "c1", toolName: "read", result: { content: [{ type: "text", text: "FILEBODY" }] }, isError: false }, {});
    end({ toolCallId: "c2", toolName: "bash", result: { content: [{ type: "text", text: "boom" }] }, isError: true }, {});
  }, 2);
  const events = conns.flat();
  assert.deepEqual(
    events.map((e) => [e.kind, e.tool, e.call_id, e.ok]),
    [
      ["tool_end", "read", "c1", true],
      ["tool_end", "bash", "c2", false],
    ],
  );
  assert.ok(!JSON.stringify(conns).includes("FILEBODY"));
  assert.ok(events.every((e) => !("target" in e) && !("items" in e)));
});

test("todo tool is registered by default as hive_todo with a plain JSON-Schema", () => {
  for (const value of [undefined, "1", ""]) {
    withEnv({ HIVE_SESSION_ID: "s1", HIVE_SOCKET: "/tmp/x.sock", HIVE_PI_TODO_TOOL: value }, () => {
      const pi = fakePi();
      mod.default(pi as never);
      assert.equal(pi.tools.length, 1, `HIVE_PI_TODO_TOOL=${value}`);
      assert.equal(pi.tools[0].name, "hive_todo");
      assert.equal(pi.tools[0].parameters.type, "object");
      assert.deepEqual(pi.tools[0].parameters.required, ["todos"]);
    });
  }
});

test("a successful hive_todo call posts tool_end and the plan on one connection", unixOnly, async () => {
  const conns = await underHive(async (pi) => {
    const result = await pi.tools[0].execute("t1", {
      todos: [
        { text: "read the code", status: "done" },
        { text: "write the test", status: "active" },
        { text: "odd status", status: "blocked" },
      ],
    });
    assert.match(result.content[0].text, /1\/3 done/);
    pi.handlers.get("tool_execution_end")!({ toolCallId: "t1", toolName: "hive_todo", result, isError: false }, {});
  }, 2);
  assert.equal(conns.length, 1, "tool_end and plan must share a connection");
  const [end, plan] = conns[0];
  assert.equal(end.kind, "tool_end");
  assert.equal(plan.kind, "plan");
  assert.equal(end.at, plan.at);
  assert.deepEqual(plan.items, [
    { text: "read the code", status: "done" },
    { text: "write the test", status: "active" },
    { text: "odd status", status: "pending" },
  ]);
});

test("a failed hive_todo call changes no plan", unixOnly, async () => {
  const conns = await underHive(async (pi) => {
    const result = await pi.tools[0].execute("t1", { todos: [{ text: "x", status: "done" }] });
    pi.handlers.get("tool_execution_end")!({ toolCallId: "t1", toolName: "hive_todo", result, isError: true }, {});
  }, 1);
  assert.deepEqual(conns.flat().map((e) => e.kind), ["tool_end"]);
});

test("a hive_todo end without todo details posts only tool_end", unixOnly, async () => {
  const conns = await underHive((pi) => {
    const end = pi.handlers.get("tool_execution_end")!;
    for (const result of [undefined, {}, { details: {} }, { details: { todos: "nope" } }]) {
      end({ toolCallId: "t", toolName: "hive_todo", result, isError: false }, {});
    }
  }, 4);
  assert.deepEqual(conns.flat().map((e) => e.kind), ["tool_end", "tool_end", "tool_end", "tool_end"]);
});

test("an empty hive_todo list still posts a plan, which clears it", unixOnly, async () => {
  const conns = await underHive(async (pi) => {
    const result = await pi.tools[0].execute("t1", { todos: [] });
    pi.handlers.get("tool_execution_end")!({ toolCallId: "t1", toolName: "hive_todo", result, isError: false }, {});
  }, 2);
  assert.deepEqual(conns.flat().map((e) => e.kind), ["tool_end", "plan"]);
});

test("disabled todo tool registers nothing and posts no plan", unixOnly, async () => {
  let tools: unknown[] = [];
  const conns = await underHive(
    (pi) => {
      tools = pi.tools;
      const ctx = { sessionManager: { getBranch: () => todoBranch([{ text: "a", status: "done" }]) } };
      pi.handlers.get("session_start")!({ reason: "resume" }, ctx);
      pi.handlers.get("session_tree")!({}, ctx);
      pi.handlers.get("tool_execution_end")!(
        { toolCallId: "t", toolName: "hive_todo", result: { details: { todos: [{ text: "a", status: "done" }] } }, isError: false },
        {},
      );
    },
    2,
    { HIVE_PI_TODO_TOOL: "0" },
  );
  assert.deepEqual(tools, []);
  assert.deepEqual(conns.flat().map((e) => e.kind), ["ping", "tool_end"]);
});

test("session_start reports the plan of the branch it lands on", unixOnly, async () => {
  const conns = await underHive((pi) => {
    const start = pi.handlers.get("session_start")!;
    start({ reason: "resume" }, { sessionManager: { getBranch: () => todoBranch([{ text: "a", status: "active" }]) } });
    // /new: a fresh branch with no hive_todo result clears the old plan.
    start({ reason: "new" }, { sessionManager: { getBranch: () => [] } });
    // A branch Pi cannot read degrades to an empty plan, not a lost ping.
    start({ reason: "fork" }, { sessionManager: { getBranch: () => [null, { message: { role: "toolResult", toolName: "hive_todo", details: 7 } }] } });
    start({ reason: "startup" }, {});
  }, 8);
  assert.deepEqual(
    conns.map((c) => c.map((e) => [e.kind, e.items])),
    [
      [["ping", undefined], ["plan", [{ text: "a", status: "active" }]]],
      [["ping", undefined], ["plan", []]],
      [["ping", undefined], ["plan", []]],
      [["ping", undefined], ["plan", []]],
    ],
  );
});

test("session_tree rebuilds the plan for the branch it jumps to", unixOnly, async () => {
  let branch: unknown[] = [];
  const ctx = { sessionManager: { getBranch: () => branch } };
  const conns = await underHive((pi) => {
    const tree = pi.handlers.get("session_tree")!;
    branch = [];
    tree({}, ctx);
    branch = [...todoBranch([{ text: "old", status: "done" }]), ...todoBranch([{ text: "a", status: "done" }])];
    tree({}, ctx);
  }, 2);
  assert.deepEqual(conns.flat().map((e) => e.items), [[], [{ text: "a", status: "done" }]]);
});

test("planFromBranch skips an errored hive_todo result", () => {
  const good = todoBranch([{ text: "good", status: "done" }]);
  const bad = todoBranch([{ text: "bad", status: "done" }], { isError: true });
  assert.deepEqual(mod.planFromBranch({ sessionManager: { getBranch: () => [...good, ...bad] } }), [{ text: "good", status: "done" }]);
  assert.deepEqual(mod.planFromBranch({ sessionManager: { getBranch: () => bad } }), []);
  assert.deepEqual(mod.planFromBranch({ sessionManager: { getBranch: () => { throw new Error("x"); } } }), []);
});

test("deriveTarget matches the shared label vectors", () => {
  const url = new URL("./testdata/toollabel_vectors.json", import.meta.url);
  const doc = JSON.parse(fs.readFileSync(url, "utf8"));
  assert.ok(doc.cases.length > 0, "vectors.json has no cases");
  for (const tc of doc.cases) {
    const want = tc.want_ts ?? tc.want;
    assert.equal(typeof want, "string", `vector ${tc.name} has neither want nor want_ts`);
    assert.equal(mod.deriveTarget(tc.input), want, tc.name);
  }
});

test("deriveTarget caps the label at 120 bytes on a rune boundary", () => {
  assert.equal(Buffer.byteLength(mod.deriveTarget({ path: "/tmp/" + "n".repeat(500) }), "utf8"), 120);
  const out = mod.deriveTarget({ path: "/x/" + "世".repeat(100) });
  assert.ok(Buffer.byteLength(out, "utf8") <= 120);
  assert.ok(!out.includes("�"));
});

test("encodeFrames caps every string field and the plan", () => {
  const at = "2026-09-16T00:00:00.000Z";
  const buf = mod.encodeFrames(
    "s1",
    [
      {
        kind: "plan",
        text: "t".repeat(600),
        tool: "x".repeat(300),
        target: "y".repeat(300),
        call_id: "z".repeat(300),
        items: Array.from({ length: 150 }, () => ({ text: "p".repeat(300), status: "pending" })),
      },
    ],
    at,
  );
  const helloLen = buf.readUInt32BE(1);
  const ev = JSON.parse(buf.subarray(10 + helloLen).toString("utf8"));
  assert.equal(ev.text.length, 512);
  assert.equal(ev.tool.length, 128);
  assert.equal(ev.target.length, 120);
  assert.equal(ev.call_id.length, 128);
  assert.equal(ev.items.length, 100);
  assert.equal(ev.items[0].text.length, 200);
});

// A server whose connections close only when the test says so.
async function heldServer() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "hive-pi-"));
  const sock = path.join(dir, "h.sock");
  const log: string[] = [];
  const payloads: Array<Record<string, any>> = [];
  const held: net.Socket[] = [];
  let holding = true;
  const server = net.createServer({ allowHalfOpen: true }, (conn) => {
    const n = log.filter((l) => l.startsWith("open")).length;
    log.push(`open${n}`);
    const chunks: Buffer[] = [];
    conn.on("data", (c) => chunks.push(c));
    conn.on("end", () => {
      let buf = Buffer.concat(chunks);
      while (buf.length >= 5) {
        const len = buf.readUInt32BE(1);
        if (buf.readUInt8(0) === 0x22) payloads.push(JSON.parse(buf.subarray(5, 5 + len).toString("utf8")));
        buf = buf.subarray(5 + len);
      }
      const close = () => {
        log.push(`close${n}`);
        conn.end();
      };
      if (holding) held.push(Object.assign(conn, { close }) as never);
      else close();
    });
  });
  await new Promise<void>((r) => server.listen(sock, r));
  return {
    sock,
    log,
    payloads,
    release() {
      holding = false;
      for (const c of held.splice(0)) (c as any).close();
    },
    releaseOne() {
      (held.shift() as any)?.close();
    },
    done() {
      server.close();
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

test("sends are serialized: the next report dials only after the previous one closed", unixOnly, async () => {
  const srv = await heldServer();
  try {
    const s = mod.createSender(srv.sock, "s1");
    s.send([{ kind: "ping" }]);
    s.send([{ kind: "idle" }]);
    await sleep(200);
    assert.deepEqual(srv.log, ["open0"], "the second report dialed while the first was still open");
    assert.equal(s.pending(), 1);
    srv.release();
    await sleep(300);
    assert.deepEqual(srv.log, ["open0", "close0", "open1", "close1"]);
    assert.deepEqual(srv.payloads.map((p) => p.kind), ["ping", "idle"]);
  } finally {
    srv.done();
  }
});

test("a refused connection does not stall the queue", unixOnly, async () => {
  const srv = await heldServer();
  srv.release();
  const missing = srv.sock + ".missing";
  try {
    const dead = mod.createSender(missing, "s1");
    dead.send([{ kind: "ping" }]);
    dead.send([{ kind: "idle" }]);
    await sleep(200);
    assert.equal(dead.pending(), 0);
    // Same sender, socket now present: the queue must still be moving.
    fs.renameSync(srv.sock, missing);
    dead.send([{ kind: "turn_end" }]);
    await sleep(300);
    assert.deepEqual(srv.payloads.map((p) => p.kind), ["turn_end"]);
  } finally {
    srv.done();
  }
});

test("a synchronous connect throw advances the queue", () => {
  const s = mod.createSender({} as never, "s1");
  s.send([{ kind: "ping" }]);
  s.send([{ kind: "idle" }]);
  assert.equal(s.pending(), 0);
});

test("the queue is bounded and drops its oldest reports", unixOnly, async () => {
  const srv = await heldServer();
  try {
    const s = mod.createSender(srv.sock, "s1", 10_000);
    s.send([{ kind: "ping" }]); // in flight, held open
    await sleep(100);
    for (let i = 0; i < 70; i++) s.send([{ kind: "idle", text: `n${i}` }]);
    assert.equal(s.pending(), mod.MAX_PENDING_REPORTS);
    srv.release();
    await sleep(1500);
    const texts = srv.payloads.filter((p) => p.kind === "idle").map((p) => p.text);
    assert.deepEqual(texts, Array.from({ length: 64 }, (_, i) => `n${i + 6}`));
  } finally {
    srv.done();
  }
});

test("send refuses a batch the daemon would cut off", () => {
  const s = mod.createSender("/nonexistent/h.sock", "s1");
  assert.equal(s.send(Array.from({ length: 9 }, () => ({ kind: "ping" }))), false);
  assert.equal(s.send([]), false);
  assert.equal(s.pending(), 0);
});
