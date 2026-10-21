import { describe, expect, it } from 'vitest';
import { compileOrigin } from '../src/cors.js';

/** Tiny helper: does `pattern` match `origin`? Mirrors what Fastify CORS does. */
function matches(pattern: string | RegExp, origin: string): boolean {
  return typeof pattern === 'string' ? pattern === origin : pattern.test(origin);
}

describe('compileOrigin', () => {
  it('returns the input verbatim when there is no wildcard', () => {
    const p = compileOrigin('https://app.wallet.example');
    expect(p).toBe('https://app.wallet.example');
    expect(matches(p, 'https://app.wallet.example')).toBe(true);
    expect(matches(p, 'https://other.wallet.example')).toBe(false);
  });

  it('compiles a single-subdomain wildcard to a RegExp', () => {
    const p = compileOrigin('https://*.wallet.example');
    expect(p).toBeInstanceOf(RegExp);
    expect(matches(p, 'https://app.wallet.example')).toBe(true);
    expect(matches(p, 'https://connect.wallet.example')).toBe(true);
    expect(matches(p, 'https://anything.wallet.example')).toBe(true);
  });

  it('does not match the apex / bare domain', () => {
    const p = compileOrigin('https://*.wallet.example');
    expect(matches(p, 'https://wallet.example')).toBe(false);
  });

  it('does not match nested subdomains (single level only)', () => {
    const p = compileOrigin('https://*.wallet.example');
    expect(matches(p, 'https://evil.app.wallet.example')).toBe(false);
    expect(matches(p, 'https://x.y.wallet.example')).toBe(false);
  });

  it('does not allow path-style attacks via the wildcard', () => {
    const p = compileOrigin('https://*.wallet.example');
    expect(matches(p, 'https://attacker.example/x.wallet.example')).toBe(false);
    expect(matches(p, 'https://app.wallet.example.attacker.example')).toBe(false);
  });

  it('respects scheme', () => {
    const p = compileOrigin('https://*.wallet.example');
    expect(matches(p, 'http://app.wallet.example')).toBe(false);
    expect(matches(p, 'ftp://app.wallet.example')).toBe(false);
  });

  it('respects port if specified', () => {
    const p = compileOrigin('http://*.wallet.example:3000');
    expect(matches(p, 'http://app.wallet.example:3000')).toBe(true);
    expect(matches(p, 'http://app.wallet.example:4000')).toBe(false);
    expect(matches(p, 'http://app.wallet.example')).toBe(false);
  });

  it('handles multiple TLDs in the brand list', () => {
    const patterns = [
      compileOrigin('https://*.one.example'),
      compileOrigin('https://*.two.example'),
      compileOrigin('https://*.three.example'),
      compileOrigin('https://*.wallet.example'),
    ];
    const isAllowed = (o: string) => patterns.some((p) => matches(p, o));

    expect(isAllowed('https://app.one.example')).toBe(true);
    expect(isAllowed('https://swap.two.example')).toBe(true);
    expect(isAllowed('https://m.three.example')).toBe(true);
    expect(isAllowed('https://connect.wallet.example')).toBe(true);
    expect(isAllowed('https://attacker.example')).toBe(false);
    expect(isAllowed('https://one.example.attacker.example')).toBe(false);
  });

  it('escapes regex metacharacters in the literal portions', () => {
    // Defensive: a brand domain with a `.` in it must not act as `any char`.
    const p = compileOrigin('https://*.one.example');
    expect(matches(p, 'https://app.oneXexample')).toBe(false);
    expect(matches(p, 'https://app.one.example')).toBe(true);
  });
});
