/**
 * Session-scoped negative cache for wallet profiles. A wallet with no
 * media/user profile row is a normal outcome (most traders never publish one),
 * so once a lookup has said "no profile" the answer is kept for the browser
 * session (module memory plus sessionStorage, which survives full page loads)
 * and the wallet is never asked for again until the session ends or the
 * wallet saves a profile (forgetProfileAbsence).
 */
const storageKey = 'sidiora.profile-absent.v1';
const valid = /^0x[0-9a-f]{40}$/;
let absent: Set<string> | undefined;

function storage(): Storage | null {
  try { return typeof window === 'undefined' ? null : window.sessionStorage; } catch { return null; }
}

function known(): Set<string> {
  if (absent) return absent;
  absent = new Set();
  try {
    const parsed: unknown = JSON.parse(storage()?.getItem(storageKey) ?? '[]');
    if (Array.isArray(parsed)) for (const value of parsed) if (typeof value === 'string' && valid.test(value)) absent.add(value);
  } catch { /* unreadable storage: start empty */ }
  return absent;
}

function persist(set: Set<string>) {
  try { storage()?.setItem(storageKey, JSON.stringify([...set])); } catch { /* storage full or blocked: memory still holds it */ }
}

export function isProfileAbsent(wallet: string): boolean {
  return known().has(wallet.toLowerCase());
}

export function rememberProfileAbsent(wallet: string): void {
  const key = wallet.toLowerCase();
  if (!valid.test(key)) return;
  const set = known();
  if (set.has(key)) return;
  set.add(key); persist(set);
}

/** Called when a wallet publishes a profile, so the next lookup reads it. */
export function forgetProfileAbsence(wallet: string): void {
  const set = known();
  if (set.delete(wallet.toLowerCase())) persist(set);
}

/** Test seam for suites that need a fresh session; not used by the app. */
export function resetProfileAbsenceForSession(): void {
  absent = undefined;
  try { storage()?.removeItem(storageKey); } catch { /* ignore */ }
}
