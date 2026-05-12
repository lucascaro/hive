// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { buildSidebarModel, renderSidebar } from '../../src/lib/sidebar.js';

const projects = [
  { id: 'p1', name: 'alpha', order: 0 },
  { id: 'p2', name: 'beta', order: 1 },
];
const sessions = [
  { id: 's3', projectId: 'p2', order: 0, name: 'three', alive: true },
  { id: 's2', project_id: 'p1', order: 1, name: 'two', alive: false },
  { id: 's1', projectId: 'p1', order: 0, name: 'one', alive: true },
];

describe('buildSidebarModel', () => {
  it('groups sessions under their project and sorts them', () => {
    const model = buildSidebarModel({
      projects, sessions, activeProjectId: 'p1', collapsed: new Set(['p2']),
    });
    expect(model).toHaveLength(2);
    expect(model[0].project.id).toBe('p1');
    expect(model[0].active).toBe(true);
    expect(model[0].sessions.map((s) => s.id)).toEqual(['s1', 's2']);
    expect(model[1].collapsed).toBe(true);
    expect(model[1].active).toBe(false);
    expect(model[1].sessions.map((s) => s.id)).toEqual(['s3']);
  });
});

describe('renderSidebar (jsdom)', () => {
  it('produces <li.project> per project and <li.session> per session', () => {
    const model = buildSidebarModel({
      projects, sessions, activeProjectId: 'p1', collapsed: new Set(),
    });
    const root = renderSidebar(document, model, { attention: new Set(['s2']) });
    document.body.appendChild(root);

    const projLIs = root.querySelectorAll('li.project');
    expect(projLIs).toHaveLength(2);
    expect(projLIs[0].dataset.pid).toBe('p1');
    expect(projLIs[0].classList.contains('active')).toBe(true);
    expect(projLIs[1].classList.contains('active')).toBe(false);

    const allSessions = root.querySelectorAll('li.session');
    expect(allSessions).toHaveLength(3);
    expect(allSessions[0].dataset.sid).toBe('s1');
    expect(allSessions[0].classList.contains('alive')).toBe(true);
    expect(allSessions[1].classList.contains('dead')).toBe(true);
    expect(allSessions[1].classList.contains('attention')).toBe(true);
    expect(allSessions[2].dataset.pid).toBe('p2');
  });

  it('renders a collapsed project with the .collapsed class', () => {
    const model = buildSidebarModel({
      projects, sessions, activeProjectId: 'p1', collapsed: new Set(['p2']),
    });
    const root = renderSidebar(document, model);
    const beta = root.querySelector('li.project[data-pid="p2"]');
    expect(beta.classList.contains('collapsed')).toBe(true);
  });
});
