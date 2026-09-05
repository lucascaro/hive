// Behavioural checks for the Hive Pi extension that only make sense on
// the TS side. The cross-language frame contract is checked from Go
// (internal/agent/pi_test.go), which decodes what encodeFrames here
// actually produces with the real wire reader.
//
// Run: node --test internal/agent/pi/
import assert from "node:assert/strict";
import test from "node:test";

const mod = await import("./hive.ts");

function fakePi() {
  const events: string[] = [];
  return { events, on: (name: string) => events.push(name) };
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
