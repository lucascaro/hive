// The real command bus. key-scopes.test.ts mocks runCommand to pin
// routing, so the "declined key is left unconsumed" contract rests here.
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  findCommand,
  listCommands,
  registerCommandSource,
  runCommand,
  type Command,
} from '../../src/app/command-registry.js';

const offs: Array<() => void> = [];
function source(order: 'core' | 'plugins', ...cmds: Command[]) {
  offs.push(registerCommandSource(() => cmds, order));
}
afterEach(() => {
  for (const off of offs.splice(0)) off();
  vi.restoreAllMocks();
});

describe('runCommand', () => {
  it('an unknown id is not handled, and warns', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    expect(runCommand('nope')).toBe(false);
    expect(warn).toHaveBeenCalledOnce();
  });

  it('run() returning false is a decline', () => {
    source('core', { id: 'a', run: () => false });
    expect(runCommand('a')).toBe(false);
  });

  it('run() returning void or true is handled', () => {
    const run = vi.fn();
    source('core', { id: 'v', run }, { id: 't', run: () => true });
    expect(runCommand('v')).toBe(true);
    expect(run).toHaveBeenCalledOnce();
    expect(runCommand('t')).toBe(true);
  });
});

describe('sources', () => {
  it('core commands precede plugin ones whatever registered first', () => {
    source('plugins', { id: 'p', run: () => {} });
    source('core', { id: 'c', run: () => {} });
    expect(listCommands().map((c) => c.id)).toEqual(['c', 'p']);
  });

  it('a core id wins over a plugin one with the same id', () => {
    source('plugins', { id: 'x', title: 'plugin', run: () => {} });
    source('core', { id: 'x', title: 'core', run: () => {} });
    expect(findCommand('x')?.title).toBe('core');
  });

  it('unregistering twice removes only that source', () => {
    const off = registerCommandSource(
      () => [{ id: 'gone', run: () => {} }],
      'core',
    );
    source('core', { id: 'kept', run: () => {} });
    off();
    off();
    expect(findCommand('gone')).toBeUndefined();
    expect(findCommand('kept')).toBeDefined();
  });
});
