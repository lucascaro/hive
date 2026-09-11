// Display-time only. The stored name stays exactly as the daemon wrote it
// (internal/agent/names.go builds "adjective-noun <agentID>", and
// internal/registry/create.go:475 builds "<branch> <agentID>"), because
// rename, search and the `hive` CLI all key on it. The row already renders
// the agent as its own glyph, so the tail is the one thing we drop.
export function displayName(s: { name?: string; agent?: string }): string {
  const name = (s.name ?? '').trim();
  // A shell session stores Agent: "" (registry/create.go:495) while both
  // name builders append the literal "shell" (names.go:51, create.go:475),
  // so the empty id still has a tail to drop — and without this two shell
  // sessions on one worktree stay byte-identical, which is the whole
  // problem the group panel exists to solve.
  const agent = (s.agent ?? '').trim() || 'shell';
  if (!name) return name;
  const tail = ` ${agent}`;
  // `endsWith` alone would turn a session literally named "claude" into ''.
  if (!name.endsWith(tail) || name.length === tail.length) return name;
  return name.slice(0, -tail.length);
}
