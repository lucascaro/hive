#!/usr/bin/env python3
"""One-shot: replace colour/font-size literals in style.css with tokens.
Mapping is the classic preset (themes.css) inverted. Unmapped literals are
printed so they can be added to the map or left with a `/* ui-lint: allow */`."""
import re, sys, pathlib
p = pathlib.Path('cmd/hivegui/frontend/src/style.css')
s = p.read_text()
COLORS = {
  '#000': 'var(--bg)', '#000000': 'var(--bg)',
  '#0a0a0a': 'var(--surface)', '#111': 'var(--surface-raised)',
  '#1f1f1f': 'var(--border)', '#ddd': 'var(--fg)', '#888': 'var(--fg-muted)',
  '#666': 'var(--fg-subtle)', '#f59e0b': 'var(--accent)', '#1a1a1a': 'var(--sel)',
  '#2a2a2a': 'var(--hover)', '#22c55e': 'var(--state-running)',
  '#ff9a9a': 'var(--state-error)',
}
SIZES = {'11px': 'var(--text-xs)', '12px': 'var(--text-sm)', '13px': 'var(--text-md)',
         '14px': 'var(--text-lg)', '16px': 'var(--text-xl)'}
for k, v in COLORS.items():
    s = re.sub(rf'(?<![\w-]){re.escape(k)}(?![\w])', v, s, flags=re.I)
for k, v in SIZES.items():
    s = re.sub(rf'font-size:\s*{k}', f'font-size: {v}', s)
p.write_text(s)
left = sorted(set(re.findall(r'#[0-9a-fA-F]{3,8}\b', s)))
print('unmapped colours:', left)
print('unmapped font-sizes:', sorted(set(re.findall(r'font-size:\s*[\d.]+px', s))))
