import { describe, expect, it, vi } from 'vitest';
import {
  logEvent,
  safeException,
  sanitizeTelemetry,
  toAppFailure,
} from './observability';

describe('observability safety', () => {
  it('recursively redacts case-insensitive keys, credentials, addresses, and URLs', () => {
    const sanitized = sanitizeTelemetry({
      nested: {
        Authorization: 'Bearer abcdefghijklmnop',
        profile: {
          walletAddress: '0x1111111111111111111111111111111111111111',
          link: 'https://user:pass@example.com/path?token=secret',
        },
      },
    });
    expect(sanitized).toEqual({
      nested: {
        Authorization: '[redacted]',
        profile: {
          walletAddress: '[redacted]',
          link: 'https://example.com/path',
        },
      },
    });
  });

  it('sanitizes exception messages and paths before transport', () => {
    const error = new Error(
      'request failed password=hunter2 for 0x1111111111111111111111111111111111111111',
    );
    error.stack = `Error: ${error.message}\n    at run (/root/project/secret.ts:10:2)`;
    const safe = safeException(error);
    expect(safe.message).not.toContain('hunter2');
    expect(safe.message).not.toContain('1111111111111111111111111111111111111111');
    expect(safe.stack).not.toContain('/root/project');
  });

  it('classifies failures with a correlation identifier and sanitized cause', () => {
    const failure = toAppFailure(new Error('Bearer abcdefghijklmnop'), {
      domain: 'server-edge',
      kind: 'timeout',
      code: 'UPSTREAM_TIMEOUT',
    });
    expect(failure.domain).toBe('server-edge');
    expect(failure.retry.kind).toBe('manual');
    expect(failure.correlationId).toBeTruthy();
    expect(failure.cause).toMatchObject({ message: '[redacted]' });
  });

  it('emits redacted structured logs', () => {
    const spy = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    logEvent('info', 'test_event', {
      nested: { PiN: '123456' },
      route: '/api/test',
    });
    const payload = JSON.parse(spy.mock.calls[0][0] as string);
    expect(payload.nested.PiN).toBe('[redacted]');
    expect(payload.route).toBe('/api/test');
    spy.mockRestore();
  });
});
