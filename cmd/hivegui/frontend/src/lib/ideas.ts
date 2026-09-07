// Pure helpers for the idea inbox. No DOM, unit-testable — the same
// split lib/worktrees.ts makes for the worktree browser.

// relativeAge renders how long ago an idea was captured, in the one
// coarse unit that fits. Coarse on purpose: the inbox is a list of
// notes to triage, where "3d" is the whole signal and a live-updating
// "2 minutes ago" is churn.
//
// `iso` is the daemon's RFC 3339 timestamp. An unparseable one renders
// as an empty string rather than "NaNs": a bad clock on one row must
// not be louder than the note itself.
export function relativeAge(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  // Clamped at zero: a daemon whose clock is a second ahead of the
  // webview's would otherwise render a negative age.
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return 'just now';
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

// MAX_IDEA_TEXT mirrors wire.MaxIdeaText (internal/wire/control.go).
// The daemon REJECTS an oversize note rather than truncating it, and
// the capture sheet does not wait for the answer before closing — so
// without a check here the text is simply gone. Keep the two in sync.
export const MAX_IDEA_TEXT = 4 << 10; // 4 KiB

// ideaTextBytes measures what the daemon measures: UTF-8 bytes, not
// characters. A `maxLength` on the textarea would count UTF-16 code
// units instead, which lets a note of non-ASCII text sail past a
// 4096-unit limit and get rejected anyway.
export function ideaTextBytes(text: string): number {
  return new TextEncoder().encode(text).length;
}

// ideaTextTooLong is the submit guard, applied to the same trimmed
// string that goes on the wire.
export function ideaTextTooLong(text: string): boolean {
  return ideaTextBytes(text.trim()) > MAX_IDEA_TEXT;
}

// ideaPrompt is the agent's opening turn when a session is started
// from an idea.
//
// An instruction, not a label. A bare "Idea: the grid loses focus"
// tells the agent what was noticed and nothing about what to do with
// it, so it guesses — usually by starting to edit code off a one-line
// note that was never a specification. The kind is the only thing that
// distinguishes a bug report from a half-formed thought, so it picks
// the verb; the note itself is passed through verbatim at the end,
// where the agent reads it as the subject rather than as orders.
//
// One line, deliberately: the typed-delivery path (agents without
// Def.PositionalPrompt) sends this followed by a carriage return, and
// TUIs disagree about whether an embedded newline submits early.
//
// The idea verb for anything unrecognised: kind is a closed set the
// daemon validates, and a note that reaches here with a strange one is
// still a note worth starting.
export function ideaPrompt(idea: { kind: string; text: string }): string {
  switch (idea.kind) {
    case 'bug':
      return `A bug was reported in this project. Reproduce it first, find the root cause, and tell me what you found before changing any code. The report: ${idea.text}`;
    case 'feedback':
      return `Feedback was captured about this project. Work out what it would take to address, whether it is worth doing, and tell me what you recommend before changing any code. The feedback: ${idea.text}`;
    default:
      return `An idea was captured for this project. Explore what it would involve, ask me about anything ambiguous, and propose a plan before changing any code. The idea: ${idea.text}`;
  }
}
