// Minimal typing for the `ws` package, used by scroll-codex.spec's Node-side
// JSON-RPC client on Node < 22 (no global WebSocket).
//
// Deliberately NOT `@types/ws`: that package pulls in @types/node, whose
// globals are program-wide — setTimeout starts returning NodeJS.Timeout and
// src/app/{session-term,view}.ts stop compiling. Same reason
// test/e2e/hive-global.d.ts hand-writes `process` instead.
//
// The spec only ever calls the constructor, and only through the DOM
// WebSocket surface, so that is all this declares.
declare module 'ws' {
  export const WebSocket: typeof globalThis.WebSocket;
}
