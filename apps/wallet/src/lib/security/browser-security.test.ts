import { describe, expect, it } from 'vitest';
import { buildContentSecurityPolicy, createCspNonce } from './csp';
import { isAllowedMediaContentType, safeMediaPath } from './media-policy';
import { validatedExternalUrl } from './navigation';

describe('browser security policy', () => {
  it('builds a nonce-bound CSP with embedded browser runtime execution', () => {
    const nonce = createCspNonce();
    const policy = buildContentSecurityPolicy(nonce);
    expect(policy).toContain(`script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`);
    expect(policy).toContain("object-src 'none'");
    expect(policy).toContain("frame-ancestors 'none'");
    expect(policy).toContain("manifest-src 'self';");
    expect(policy).toContain("'unsafe-eval'");
    expect(policy).not.toContain('img-src https:');
  });

  it('routes reviewed media through the same-origin proxy', () => {
    expect(
      safeMediaPath(
        'https://raw.githubusercontent.com/Paxeer-Network/brand/main/icon.png',
      ),
    ).toBe(
      '/wallet/api/media?url=https%3A%2F%2Fraw.githubusercontent.com%2FPaxeer-Network%2Fbrand%2Fmain%2Ficon.png',
    );
    expect(safeMediaPath('https://evil.example/payload.svg')).toBe(
      '/wallet/default_icon.webp',
    );
    expect(safeMediaPath('javascript:alert(1)')).toBe('/wallet/default_icon.webp');
    expect(safeMediaPath(null)).toBe('/wallet/default_icon.webp');
    expect(safeMediaPath('/pns.svg')).toBe('/wallet/pns.svg');
    expect(safeMediaPath('/wallet/pns.svg')).toBe('/wallet/pns.svg');
    expect(isAllowedMediaContentType('image/png; charset=binary')).toBe(true);
    expect(isAllowedMediaContentType('image/svg+xml')).toBe(false);
    expect(isAllowedMediaContentType('text/html')).toBe(false);
  });

  it('allows only reviewed credential-free external destinations', () => {
    expect(validatedExternalUrl('https://paxscan.io/tx/0x01')?.origin).toBe(
      'https://paxscan.io',
    );
    expect(validatedExternalUrl('https://evil.example')).toBeNull();
    expect(validatedExternalUrl('javascript:alert(1)')).toBeNull();
    expect(
      validatedExternalUrl('https://user:secret@paxscan.io/tx/0x01'),
    ).toBeNull();
  });
});
