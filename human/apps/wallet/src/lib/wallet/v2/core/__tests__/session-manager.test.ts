import { beforeAll, describe, expect, it } from 'vitest';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { WebCryptoAdapter } from '../../adapters/web-crypto-adapter';
import { BrowserTimerAdapter } from '../../adapters/browser-timer-adapter';
import { SecurityEventBus } from '../../adapters/security-event-bus';
import type { SecurityEvent } from '../../types/events';
import { VaultManager } from '../vault-manager';
import { SessionManager } from '../session-manager';

const PASSWORD = '482915';
const MNEMONIC = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);

const wait = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

describe('SessionManager production lifecycle', () => {
  const crypto = new WebCryptoAdapter();
  const timer = new BrowserTimerAdapter();
  let manifest: Awaited<ReturnType<VaultManager['createVault']>>['manifest'];
  let vaultKey: CryptoKey;

  beforeAll(async () => {
    const created = await new VaultManager(crypto, null).createVault({
      password: PASSWORD,
      mnemonic: MNEMONIC,
    });
    manifest = created.manifest;
    vaultKey = created.vaultKey;
  });

  it('starts locked and a new instance cannot restore another session', async () => {
    const first = new SessionManager(null, null, timer);
    await first.unlock(vaultKey, manifest, 5_000);
    const reloaded = new SessionManager(null, null, timer);

    expect(first.isUnlocked()).toBe(true);
    expect(reloaded.isUnlocked()).toBe(false);
    expect(reloaded.getManifest()).toBeNull();
    first.destroy();
  });

  it('holds only a non-extractable key and emits typed lifecycle events', async () => {
    const bus = new SecurityEventBus();
    const events: SecurityEvent[] = [];
    const offUnlocked = bus.on('wallet:unlocked', event => events.push(event));
    const offLocked = bus.on('wallet:locked', event => events.push(event));
    const session = new SessionManager(null, bus, timer);

    await session.unlock(vaultKey, manifest, 5_000);
    expect(session.requireVaultKey().extractable).toBe(false);
    session.lock('manual');

    expect(events.map(event => event.kind)).toEqual([
      'wallet:unlocked',
      'wallet:locked',
    ]);
    expect(events[1].metadata).toEqual({ reason: 'manual' });
    offUnlocked();
    offLocked();
  });

  it('expires on the authoritative inactivity deadline', async () => {
    const session = new SessionManager(null, null, timer);
    await session.unlock(vaultKey, manifest, 1_000);
    await wait(1_100);
    expect(session.isUnlocked()).toBe(false);
  });

  it('read-only polling does not extend the inactivity deadline', async () => {
    const session = new SessionManager(null, null, timer);
    await session.unlock(vaultKey, manifest, 1_000);
    for (let i = 0; i < 4; i++) {
      await wait(225);
      session.getStatus();
      session.getManifest();
      session.isUnlocked();
    }
    await wait(200);
    expect(session.isUnlocked()).toBe(false);
  });

  it('step-up freshness does not extend the ordinary session', async () => {
    const session = new SessionManager(null, null, timer);
    await session.unlock(vaultKey, manifest, 1_000);
    await wait(700);
    session.recordStepUp();
    expect(session.isStepUpValid()).toBe(true);
    await wait(400);
    expect(session.isUnlocked()).toBe(false);
    expect(session.isStepUpValid()).toBe(false);
  });

  it('sensitive activity explicitly extends the session', async () => {
    const session = new SessionManager(null, null, timer);
    await session.unlock(vaultKey, manifest, 1_000);
    await wait(700);
    session.touchSensitiveActivity();
    await wait(500);
    expect(session.isUnlocked()).toBe(true);
    session.destroy();
  });

  it('cross-context revocation clears the complete session', async () => {
    const session = new SessionManager(null, null, timer);
    await session.unlock(vaultKey, manifest, 5_000, manifest.keySlots[0].id);
    session.onCrossContextLock();

    expect(session.isUnlocked()).toBe(false);
    expect(session.getAuthenticatedSlotId()).toBeNull();
    expect(() => session.requireVaultKey()).toThrow(/locked/i);
  });
});
