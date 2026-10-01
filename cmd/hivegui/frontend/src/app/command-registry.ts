// The command bus (spec 478): one registry of `id → run`, shared by
// keydown (app/key-scopes.ts), the native menu and the command palette
// (app/commands.ts), so the three paths run the same code by construction.
//
// A LEAF module on purpose — it imports nothing from app/. Everything
// that registers or runs commands depends on it, never the other way,
// which is what keeps the keyboard / palette / plugin-host graph acyclic.
//
// Features subscribe by registering a source: a function returning their
// current commands. A source is re-read on every lookup, so a plugin that
// activates or deactivates needs no re-registration.

export interface Command {
  id: string;
  /** Palette row name. Commands without one are reachable only from a
   * key or the menu (closing a modal, the dead-session overlay, …). */
  title?: string;
  /** Palette label override. Core commands take theirs from
   * lib/shortcuts.ts by id; plugins bring their own. */
  shortcut?: string;
  /** false = declined: the key that triggered it is left unconsumed
   * (⌘⏎ in single view, ⌘9 past the last session). */
  run: () => boolean | void;
}

export type SourceOrder = 'core' | 'plugins';

const sources: Record<SourceOrder, Array<() => readonly Command[]>> = {
  core: [],
  plugins: [],
};

// The order is explicit rather than registration order: module
// evaluation order decides which source registers first, and the
// palette must always list core commands before plugin ones.
export function registerCommandSource(
  source: () => readonly Command[],
  order: SourceOrder,
): () => void {
  sources[order].push(source);
  return () => {
    const list = sources[order];
    const i = list.indexOf(source);
    if (i >= 0) list.splice(i, 1);
  };
}

export function listCommands(): Command[] {
  return [...sources.core, ...sources.plugins].flatMap((s) => [...s()]);
}

export function findCommand(id: string): Command | undefined {
  return listCommands().find((c) => c.id === id);
}

/** Runs a command by id. Returns false when it declined or does not
 * exist, so a key bound to a missing command is left to the terminal. */
export function runCommand(id: string): boolean {
  const cmd = findCommand(id);
  if (!cmd) {
    console.warn(`command "${id}" is not registered`);
    return false;
  }
  return cmd.run() !== false;
}
