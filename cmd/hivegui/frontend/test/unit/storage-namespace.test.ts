// Key namespacing for the persisted project-id sets (#340).
//
// Every hivegui process shares ONE webview localStorage — WKWebView keys
// its store on the bundle id, not on the daemon socket — while each
// daemon owns a registry with its own project UUIDs. Without a suffix,
// one instance's boot prune deletes another's ids as "projects that no
// longer exist", and the user's tray empties on every launch.
import { describe, it, expect } from 'vitest';
import {
  namespacedKey,
  COLLAPSED_STORAGE_KEY,
  MINIMIZED_PROJECTS_STORAGE_KEY,
} from '../../src/lib/collapsed.js';

describe('namespacedKey', () => {
  it('suffixes the base key with the state-dir id', () => {
    expect(namespacedKey(MINIMIZED_PROJECTS_STORAGE_KEY, 'a1b2c3d4')).toBe(
      'hive.minimizedProjects.a1b2c3d4',
    );
    expect(namespacedKey(COLLAPSED_STORAGE_KEY, 'a1b2c3d4')).toBe(
      'hive.collapsedProjects.a1b2c3d4',
    );
  });

  it('keeps two state dirs in separate buckets', () => {
    expect(namespacedKey(COLLAPSED_STORAGE_KEY, 'aaaa1111')).not.toBe(
      namespacedKey(COLLAPSED_STORAGE_KEY, 'bbbb2222'),
    );
  });

  // The empty case means "daemon unidentified". It returns the bare key
  // so the value stays readable for migration, but callers must treat it
  // as a signal to stop persisting — see store.ts, which never writes
  // under an empty namespace.
  it('returns the bare key for an empty namespace', () => {
    expect(namespacedKey(COLLAPSED_STORAGE_KEY, '')).toBe(
      COLLAPSED_STORAGE_KEY,
    );
  });
});
