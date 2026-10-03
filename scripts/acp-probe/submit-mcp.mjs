#!/usr/bin/env node
// Minimal stdio MCP server exposing one tool, `submit_result` (spec 492).
//
//   node submit-mcp.mjs <outFile>
//
// Every tools/call appends its arguments as one JSON line to <outFile>. The
// probe reads that file out-of-band to decide whether MCP injection worked,
// so the verdict never rests on what the agent *says* it did.
import { appendFileSync } from 'node:fs';
import { createInterface } from 'node:readline';

const outFile = process.argv[2];
if (!outFile) {
  process.stderr.write('usage: submit-mcp.mjs <outFile>\n');
  process.exit(2);
}

const SUBMIT_TOOL = {
  name: 'submit_result',
  description: 'Submit the typed result of this task. Call exactly once when done.',
  inputSchema: {
    type: 'object',
    properties: {
      status: { type: 'string', enum: ['ok', 'error'] },
      nonce: { type: 'string' },
    },
    required: ['status', 'nonce'],
    additionalProperties: false,
  },
};

const send = (msg) => process.stdout.write(`${JSON.stringify({ jsonrpc: '2.0', ...msg })}\n`);

createInterface({ input: process.stdin }).on('line', (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    return;
  }
  if (msg.id === undefined) return; // notifications (initialized, cancelled)
  switch (msg.method) {
    case 'initialize':
      return send({
        id: msg.id,
        result: {
          protocolVersion: msg.params?.protocolVersion ?? '2025-06-18',
          capabilities: { tools: {} },
          serverInfo: { name: 'hive', version: '0.0.0' },
        },
      });
    case 'ping':
      return send({ id: msg.id, result: {} });
    case 'tools/list':
      return send({ id: msg.id, result: { tools: [SUBMIT_TOOL] } });
    case 'tools/call': {
      if (msg.params?.name !== SUBMIT_TOOL.name) {
        return send({ id: msg.id, error: { code: -32602, message: `unknown tool ${msg.params?.name}` } });
      }
      appendFileSync(outFile, `${JSON.stringify(msg.params.arguments ?? {})}\n`);
      return send({ id: msg.id, result: { content: [{ type: 'text', text: 'recorded' }] } });
    }
    default:
      return send({ id: msg.id, error: { code: -32601, message: `method not found: ${msg.method}` } });
  }
});
