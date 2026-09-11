// Sidebar density. The row is two lines at 40px by default; `tight` keeps
// both lines in ~34px, `compact` drops to one line at 28px for a long
// session list.
//
// Shaped like theme.ts (same key/read/apply trio, same try/catch around
// localStorage — a storage that throws must not take the sidebar down)
// with one deliberate difference: there is NO matching pre-paint <script>
// in index.html. That duplication is a standing sync hazard, and the
// worst case here is a single frame of normal-height rows, not a
// full-window flash of the wrong theme.
export type Density = 'normal' | 'tight' | 'compact';

export const DENSITY_KEY = 'hive.sidebarDensity';
export const DEFAULT_DENSITY: Density = 'normal';

export interface DensityOption {
  id: Density;
  label: string;
}

// Order is the order the picker shows them.
export const DENSITIES: readonly DensityOption[] = [
  { id: 'normal', label: 'Normal' },
  { id: 'tight', label: 'Tight' },
  { id: 'compact', label: 'Compact' },
];

const VALID = new Set<string>(DENSITIES.map((d) => d.id));

export function readDensity(storage?: Storage): Density {
  try {
    const v = (storage ?? localStorage).getItem(DENSITY_KEY);
    return VALID.has(v ?? '') ? (v as Density) : DEFAULT_DENSITY;
  } catch {
    return DEFAULT_DENSITY;
  }
}

export function applyDensity(d: Density, doc: Document = document): void {
  doc.documentElement.dataset.density = VALID.has(d) ? d : DEFAULT_DENSITY;
}

// Side effect on import, like theme.ts: stamp before anything renders.
// Unlike the theme there is no pre-paint script to agree with, so this is
// the only writer at boot.
if (typeof document !== 'undefined') {
  applyDensity(readDensity());
}
