// What UI plugins contribute to the app (spec 471): the session view
// (modal and side panel), the session banner, sidebar badges and a
// settings section. The commands surface lives in the palette and the ⌘/
// overlay instead; app/plugin-host.ts resolves those.
//
// Every piece of plugin code rendered here sits inside a PluginBoundary,
// so a throw — or a surface that suspends and never comes back — fails
// that one plugin (failPlugin) instead of unmounting the app's single
// root. Descriptor callbacks (badge, banner) are called in try/catch for
// the same reason. Nothing here renders for a plugin that is not
// "active", so a failed plugin's surfaces are gone on the next commit.

import {
  Component,
  Suspense,
  useEffect,
  useLayoutEffect,
  type ComponentType,
  type ErrorInfo,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { mustEl } from '../app/el.js';
import {
  closeSessionView,
  failPlugin,
  RENDER_TIMEOUT_MS,
} from '../app/plugin-host.js';
import type { SessionInfo } from '../app/state.js';
import type {
  PluginBadge,
  PluginBanner as BannerSpec,
  PluginContributions,
} from '../lib/plugin-api.js';
import { setPluginPanel, useAppStore } from '../store/store.js';
import { Button } from './Button.js';
import { IconButton } from './IconButton.js';
import { ModalShell } from './modals/ModalShell.js';

type AnyComponent = ComponentType<Record<string, unknown>>;

// ---------- containment ----------

function SuspenseWatch({ id }: { id: string }): null {
  useEffect(() => {
    const t = setTimeout(
      () =>
        failPlugin(
          id,
          new Error(
            `a surface did not finish rendering within ${RENDER_TIMEOUT_MS / 1000}s`,
          ),
        ),
      RENDER_TIMEOUT_MS,
    );
    return () => clearTimeout(t);
  }, [id]);
  return null;
}

interface BoundaryProps {
  id: string;
  children: ReactNode;
}

/** Catches a plugin's render throws and endless suspends. */
export class PluginBoundary extends Component<
  BoundaryProps,
  { crashed: boolean }
> {
  state = { crashed: false };

  static getDerivedStateFromError(): { crashed: boolean } {
    return { crashed: true };
  }

  componentDidCatch(error: Error, _info: ErrorInfo): void {
    failPlugin(this.props.id, error);
  }

  render(): ReactNode {
    if (this.state.crashed) return null;
    return (
      <Suspense fallback={<SuspenseWatch id={this.props.id} />}>
        {this.props.children}
      </Suspense>
    );
  }
}

// ---------- selectors ----------

type UIMap = Readonly<
  Record<string, { status: string; contrib?: PluginContributions }>
>;

function activeContribs(ui: UIMap): [string, PluginContributions][] {
  return Object.keys(ui)
    .sort()
    .flatMap((id) => {
      const st = ui[id];
      return st.status === 'active' && st.contrib
        ? [[id, st.contrib] as [string, PluginContributions]]
        : [];
    });
}

function useContrib(id: string | null | undefined): PluginContributions | null {
  return useAppStore((s) => {
    const st = id ? s.pluginUI[id] : undefined;
    return st?.status === 'active' ? (st.contrib ?? null) : null;
  });
}

// Calls a plugin's descriptor function; a throw fails the plugin and
// renders nothing.
function describe<T>(
  id: string,
  fn: ((s: SessionInfo) => T | null) | undefined,
  s: SessionInfo,
): T | null {
  if (!fn) return null;
  try {
    return fn(s) ?? null;
  } catch (e) {
    // After render: failing mutates the store, which must not happen
    // while React is rendering this component.
    queueMicrotask(() => failPlugin(id, e));
    return null;
  }
}

// ---------- session view: modal ----------

function PluginViewModal({ root }: { root: HTMLElement }): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'plugin-view'),
  );
  const pluginId = entry?.id === 'plugin-view' ? entry.pluginId : null;
  const contrib = useContrib(pluginId);
  const session = useAppStore((s) =>
    entry?.id === 'plugin-view'
      ? s.sessions.find((x) => x.id === entry.sessionId)
      : undefined,
  );
  const modal = contrib?.sessionView?.modal;
  const open = !!(entry?.id === 'plugin-view' && modal && session);

  useLayoutEffect(() => {
    root.classList.toggle('hidden', !open);
  }, [root, open]);

  // The session went away, or the plugin failed or was disabled, under
  // an open view: close it rather than leave an empty dialog up.
  useEffect(() => {
    if (entry && !open) closeSessionView('dismissed');
  }, [entry, open]);

  if (!open || entry?.id !== 'plugin-view' || !modal || !session) return null;
  const Body = modal.component as AnyComponent;
  return createPortal(
    <ModalShell
      key={entry.seq}
      id="plugin-view"
      root={root}
      title={modal.title}
      size="lg"
      onClose={() => closeSessionView('dismissed')}
      hints={modal.hints ?? [{ keys: '[esc]', label: 'close' }]}
    >
      <div className="hv-plugin-view" data-plugin-id={entry.pluginId}>
        <PluginBoundary id={entry.pluginId}>
          <Body
            session={session}
            props={entry.props}
            close={() => closeSessionView('closed')}
          />
        </PluginBoundary>
      </div>
    </ModalShell>,
    root,
  );
}

// ---------- session view: side panel ----------

function PluginPanelHost({ aside }: { aside: HTMLElement }): ReactNode {
  const pluginId = useAppStore((s) => s.pluginPanel);
  const sessionId = useAppStore((s) =>
    s.view === 'single' ? s.activeId : null,
  );
  const session = useAppStore((s) =>
    s.sessions.find((x) => x.id === sessionId),
  );
  const contrib = useContrib(pluginId);
  const panel = contrib?.sessionView?.panel;
  const shown = !!(pluginId && panel && session);

  useLayoutEffect(() => {
    mustEl('app').classList.toggle('plugin-panel-open', shown);
    aside.hidden = !shown;
  }, [shown, aside]);

  // A failed or disabled plugin takes its panel with it.
  useEffect(() => {
    if (pluginId && !contrib) setPluginPanel(null);
  }, [pluginId, contrib]);

  if (!shown || !pluginId || !panel || !session) return null;
  const Body = panel.component as AnyComponent;
  return createPortal(
    <section
      className="hv-plugin-panel"
      data-plugin-id={pluginId}
      aria-label={panel.title}
    >
      <header className="hv-plugin-panel__header">
        <h3 className="hv-plugin-panel__title">{panel.title}</h3>
        <IconButton
          icon="x"
          label={`Close ${panel.title}`}
          className="hv-plugin-panel__close"
          onClick={() => setPluginPanel(null)}
        />
      </header>
      <div className="hv-plugin-panel__body">
        <PluginBoundary id={pluginId}>
          <Body key={session.id} session={session} />
        </PluginBoundary>
      </div>
    </section>,
    aside,
  );
}

// ---------- session banner ----------

/** The first plugin banner for the active session, above the status bar.
 * One at a time: the bar is a single 40px strip (plugin-surfaces.css). */
export function PluginBanner(): ReactNode {
  const ui = useAppStore((s) => s.pluginUI);
  // Descriptors are recomputed when sessions or a plugin's settings
  // change (docs/plugins.md); subscribing to the list is what the
  // second of those needs.
  useAppStore((s) => s.plugins);
  useAppStore((s) => s.pluginConfigOverlay);
  const session = useAppStore((s) =>
    s.sessions.find((x) => x.id === s.activeId),
  );
  const viewOpen = useAppStore((s) =>
    s.modals.some((m) => m.id === 'plugin-view'),
  );
  if (!session || viewOpen) return null;
  let hit: { id: string; banner: BannerSpec } | null = null;
  for (const [id, c] of activeContribs(ui)) {
    const b = describe(id, c.sessionView?.banner, session);
    if (b && typeof b.text === 'string') {
      hit = { id, banner: b };
      break;
    }
  }
  if (!hit) return null;
  const { id, banner } = hit;
  return (
    <div className="hv-plugin-banner" role="status" data-plugin-id={id}>
      <span className="hv-plugin-banner__label">{banner.text}</span>
      {banner.action ? (
        <Button
          label={banner.action.label}
          kind="primary"
          className="hv-plugin-banner__action"
          onClick={() => {
            try {
              banner.action?.run();
            } catch (e) {
              failPlugin(id, e);
            }
          }}
        />
      ) : null}
    </div>
  );
}

// ---------- sidebar badges ----------

/** Plugin badges on one session row. Renders nothing when no plugin is
 * active, which is every row's whole cost without UI plugins. */
export function PluginBadges({ session }: { session: SessionInfo }): ReactNode {
  const ui = useAppStore((s) => s.pluginUI);
  useAppStore((s) => s.plugins); // settings changes re-describe; see PluginBanner
  useAppStore((s) => s.pluginConfigOverlay);
  const items: { id: string; badge: PluginBadge }[] = [];
  for (const [id, c] of activeContribs(ui)) {
    const b = describe(id, c.badge, session);
    if (b && typeof b.text === 'string' && b.text) items.push({ id, badge: b });
  }
  if (items.length === 0) return null;
  return (
    <span className="hv-plugin-badges">
      {items.map(({ id, badge }) => (
        <span
          key={id}
          className="hv-plugin-badge"
          data-plugin-id={id}
          data-tone={badge.tone ?? 'neutral'}
          title={badge.title ?? badge.text}
        >
          {badge.text.slice(0, 12)}
        </span>
      ))}
    </span>
  );
}

// ---------- settings section ----------

/** A plugin's own section under its row in Settings → Plugins. */
export function PluginSettingsSection({ id }: { id: string }): ReactNode {
  const contrib = useContrib(id);
  const Section = contrib?.settings as AnyComponent | undefined;
  if (!Section) return null;
  return (
    <div className="settings-plugin-section" data-plugin-id={id}>
      <PluginBoundary id={id}>
        <Section />
      </PluginBoundary>
    </div>
  );
}

// ---------- mount ----------

export function PluginSurfaces(): ReactNode {
  return (
    <>
      <PluginViewModal root={mustEl('plugin-view')} />
      <PluginPanelHost aside={mustEl('plugin-panel')} />
    </>
  );
}
