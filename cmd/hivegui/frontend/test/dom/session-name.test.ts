import { describe, it, expect } from 'vitest';
import { displayName } from '../../src/lib/session-name.js';

describe('displayName', () => {
  it('strips the trailing agent id the row also shows', () => {
    expect(displayName({ name: 'rising-shore claude', agent: 'claude' })).toBe(
      'rising-shore',
    );
    expect(displayName({ name: 'feat-sidebar codex', agent: 'codex' })).toBe(
      'feat-sidebar',
    );
  });
  it('leaves a name whose tail is a different agent', () => {
    expect(displayName({ name: 'rising-shore claude', agent: 'codex' })).toBe(
      'rising-shore claude',
    );
  });
  it('leaves a name that is only the agent id', () => {
    expect(displayName({ name: 'claude', agent: 'claude' })).toBe('claude');
  });
  // A shell session stores Agent: "" but both name builders append the
  // literal "shell" — so the empty id still has a tail. Without this, two
  // shell sessions on one worktree stay byte-identical, which is exactly
  // what the worktree group panel exists to fix.
  it('drops the shell tail a session with an empty agent id carries', () => {
    expect(displayName({ name: 'feat-sidebar shell', agent: '' })).toBe(
      'feat-sidebar',
    );
    expect(displayName({ name: 'shell', agent: '' })).toBe('shell');
  });

  it('leaves names with no agent, no tail, or trailing space noise', () => {
    expect(displayName({ name: 'my session', agent: '' })).toBe('my session');
    expect(displayName({ name: 'shipit', agent: 'claude' })).toBe('shipit');
    expect(displayName({ name: '', agent: 'claude' })).toBe('');
  });
});
