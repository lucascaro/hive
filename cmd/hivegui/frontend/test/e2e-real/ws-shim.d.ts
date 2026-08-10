// Minimal typing for the `ws` package, used by scroll-codex.spec's Node-side
// JSON-RPC client on Node < 22 (no global WebSocket).
//
// Deliberately NOT `@types/ws`: that package pulls in @types/node, whose
// globals are program-wide — setTimeout starts returning NodeJS.Timeout and
// src/app/{session-term,view}.ts stop compiling. Same reason
// test/e2e/hive-global.d.ts hand-writes `process` instead.
//
// The spec only ever calls the constructor, and only through the DOM
// WebSocket surface, so that is all this declares. The lie runs one way:
// `ws`-only APIs (ws.on('message')) correctly fail to compile, while
// DOM-only ones `ws` never implemented (dispatchEvent, binaryType 'blob')
// would typecheck and throw on the Node < 22 path. Keep the call site to
// the members below and that stays theoretical.
declare module 'ws' {
  export const WebSocket: typeof globalThis.WebSocket;
}
