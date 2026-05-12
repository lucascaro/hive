import { describe, it, expect } from 'vitest';
import { orderSessions, sessionsForProject } from '../../src/lib/order.js';

const projects = [
  { id: 'p1', order: 0 },
  { id: 'p2', order: 1 },
];
const sessions = [
  { id: 's3', projectId: 'p2', order: 0 },
  { id: 's2', project_id: 'p1', order: 1 },
  { id: 's1', projectId: 'p1', order: 0 },
];

describe('orderSessions', () => {
  it('sorts by project order, then session order', () => {
    const out = orderSessions(sessions, projects).map((s) => s.id);
    expect(out).toEqual(['s1', 's2', 's3']);
  });
});

describe('sessionsForProject', () => {
  it('returns only sessions in the given project, sorted', () => {
    expect(sessionsForProject(sessions, 'p1').map((s) => s.id)).toEqual(['s1', 's2']);
    expect(sessionsForProject(sessions, 'p2').map((s) => s.id)).toEqual(['s3']);
  });
});
