import fs from 'node:fs';

export default async function globalTeardown() {
  const pids = JSON.parse(process.env.HIVE_E2E_REAL_PIDS || '{}');
  for (const pid of Object.values(pids)) {
    if (!pid) continue;
    try { process.kill(pid, 'SIGTERM'); } catch { /* already gone */ }
  }
  // Best-effort cleanup; tmpfs reclaims this on reboot anyway.
  const tmp = process.env.HIVE_E2E_REAL_TMP;
  if (tmp && fs.existsSync(tmp)) {
    try { fs.rmSync(tmp, { recursive: true, force: true }); } catch { /* */ }
  }
}
