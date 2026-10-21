import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { PWA_NETWORK_ENV, PwaConfigError, networkOnlyBases, type PwaNetworkEnv } from '@/pwa/config';
import {
  assertConnectSrcCoversConfigured,
  buildContentSecurityPolicy,
  connectOrigins,
  createCspNonce,
} from './csp';

const FULL: PwaNetworkEnv = {
  NEXT_PUBLIC_PAXEER_WALLET_API: 'https://gateway.example.com/v1/',
  NEXT_PUBLIC_PAXEER_RPC_URL: 'https://rpc.example.com/evm',
  NEXT_PUBLIC_SUPABASE_URL: 'https://identity.example.com',
  NEXT_PUBLIC_PAXEER_ATTESTOR_URL: 'https://attestor.example.com',
  NEXT_PUBLIC_PAXEER_HUMAN_API: 'https://human.example.com',
  NEXT_PUBLIC_PAXEER_EXPLORER_URL: 'https://explorer.example.com',
  NEXT_PUBLIC_PNS_API_BASE: 'https://names.example.com',
  NEXT_PUBLIC_POINTS_API_BASE: 'https://points.example.com',
  NEXT_PUBLIC_MARKET_DATA_API: 'https://market.example.com/api',
  NEXT_PUBLIC_FX_RATES_API: 'https://rates.example.com/v6',
};

function connectSrc(policy: string): string[] {
  const part = policy.split(';').map((entry) => entry.trim()).find((entry) => entry.startsWith('connect-src '));
  return part ? part.split(/\s+/).slice(1) : [];
}

describe('connect-src from configuration', () => {
  it('lists exactly self and the configured origins', () => {
    const policy = buildContentSecurityPolicy(createCspNonce(), FULL);
    expect(connectSrc(policy)).toEqual([
      "'self'",
      'data:',
      'blob:',
      'https://gateway.example.com',
      'https://rpc.example.com',
      'https://identity.example.com',
      'https://attestor.example.com',
      'https://human.example.com',
      'https://explorer.example.com',
      'https://names.example.com',
      'https://points.example.com',
      'https://market.example.com',
      'https://rates.example.com',
      'wss://identity.example.com',
    ]);
    expect(() => assertConnectSrcCoversConfigured(policy, FULL)).not.toThrow();
  });

  it('covers every configuration name', () => {
    const variables = new Set(connectOrigins(FULL).map((entry) => entry.variable));
    expect([...variables].sort()).toEqual(Object.values(PWA_NETWORK_ENV).sort());
  });

  it('skips the attestor when unset', () => {
    const env = { ...FULL, NEXT_PUBLIC_PAXEER_ATTESTOR_URL: undefined };
    const policy = buildContentSecurityPolicy(createCspNonce(), env);
    expect(connectSrc(policy)).not.toContain('https://attestor.example.com');
    expect(() => assertConnectSrcCoversConfigured(policy, env)).not.toThrow();
  });

  it('uses ws for a plain http identity provider', () => {
    const env = { NEXT_PUBLIC_SUPABASE_URL: 'http://127.0.0.1:3096' };
    expect(connectSrc(buildContentSecurityPolicy(createCspNonce(), env))).toEqual([
      "'self'",
      'data:',
      'blob:',
      'http://127.0.0.1:3096',
      'ws://127.0.0.1:3096',
    ]);
  });

  it('names the variable of a missing origin', () => {
    for (const variable of Object.values(PWA_NETWORK_ENV)) {
      const policy = buildContentSecurityPolicy(createCspNonce(), { ...FULL, [variable]: undefined });
      expect(() => assertConnectSrcCoversConfigured(policy, FULL)).toThrow(
        `connect-src lacks the origin of ${variable}`,
      );
    }
  });

  it('names the origin of an extra source', () => {
    const policy = buildContentSecurityPolicy(createCspNonce(), FULL).replace(
      "connect-src 'self'",
      "connect-src 'self' https://extra.example.org",
    );
    expect(() => assertConnectSrcCoversConfigured(policy, FULL)).toThrow(
      'connect-src carries https://extra.example.org, which no configuration name yields',
    );
    const wider = buildContentSecurityPolicy(createCspNonce(), FULL);
    const narrower = { ...FULL, NEXT_PUBLIC_FX_RATES_API: undefined };
    expect(() => assertConnectSrcCoversConfigured(wider, narrower)).toThrow(
      'connect-src carries https://rates.example.com, which no configuration name yields',
    );
  });

  it('refuses a URL the service worker refuses', () => {
    const cases: PwaNetworkEnv[] = [
      { NEXT_PUBLIC_PAXEER_WALLET_API: 'not a url' },
      { NEXT_PUBLIC_PAXEER_RPC_URL: 'ftp://rpc.example.com/evm' },
      { NEXT_PUBLIC_SUPABASE_URL: 'https://user:pass@identity.example.com' },
      { NEXT_PUBLIC_MARKET_DATA_API: 'https://user@market.example.com/api' },
    ];
    for (const env of cases) {
      let expected: unknown;
      try {
        networkOnlyBases(env);
      } catch (error) {
        expected = error;
      }
      expect(expected).toBeInstanceOf(PwaConfigError);
      expect(() => buildContentSecurityPolicy(createCspNonce(), env)).toThrow(PwaConfigError);
      expect(() => buildContentSecurityPolicy(createCspNonce(), env)).toThrow((expected as Error).message);
    }
  });

  it('keeps no literal origin in the policy source', () => {
    const source = readFileSync(path.join(__dirname, 'csp.ts'), 'utf8');
    expect(source).not.toMatch(/\b(?:https?|wss?):\/\//);
  });

  it('covers the release environment', () => {
    expect(() => assertConnectSrcCoversConfigured(buildContentSecurityPolicy(createCspNonce()))).not.toThrow();
  });
});
