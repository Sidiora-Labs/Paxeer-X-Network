// @vitest-environment jsdom

import { beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import {
  formatUsdAsFiat,
  getRatesFreshness,
  initCurrencyService,
} from './currency';
import {
  grantDappPermission,
  hasDappPermission,
  permissionForOrigin,
  recordDappMethod,
  revokeDappPermission,
} from './dapp-permissions';
import { estimateFeeWei, resolveFeeOverrides } from './fees';
import {
  currencyRatesRepository,
  preferencesRepository,
} from '@/platform/storage/repositories';

const ADDRESS = '0xf8850b62AE017c55be7f571BBad840b4f3DA7D49';

describe('priority wallet features', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('formats a real cached USD cross-rate with the selected locale and currency', () => {
    currencyRatesRepository.write({
      base: 'USD',
      rates: { USD: 1, EUR: 0.8 },
      fetchedAt: Date.now(),
    });
    initCurrencyService();
    expect(getRatesFreshness()).toBe('fresh');
    expect(formatUsdAsFiat(10, 'de-DE', 'EUR')).toBe('8,00 €');
  });

  it('binds dApp permissions to exact HTTPS origin, account, and chain', () => {
    const origin = 'https://dex.example';
    grantDappPermission(origin, ADDRESS, 125);
    expect(hasDappPermission(origin, ADDRESS, 125)).toBe(true);
    expect(hasDappPermission(origin, ADDRESS, 1)).toBe(false);
    expect(hasDappPermission('https://evil.example', ADDRESS, 125)).toBe(false);

    recordDappMethod(origin, 'personal_sign');
    expect(permissionForOrigin(origin)?.methods).toContain('personal_sign');

    revokeDappPermission(origin);
    expect(permissionForOrigin(origin)).toBeNull();
  });

  it('rejects non-HTTPS permission origins', () => {
    expect(() => grantDappPermission('http://dex.example', ADDRESS, 125))
      .toThrow(/origin is invalid/);
  });

  it('uses exact custom fee settings without making a network request', async () => {
    preferencesRepository.write({
      ...preferencesRepository.read(),
      feeMode: 'custom',
      customMaxFeeGwei: '125',
      customPriorityFeeGwei: '100',
    });
    const provider = new ethers.JsonRpcProvider('https://rpc.example');
    const fees = await resolveFeeOverrides(provider);
    expect(fees).toEqual({
      mode: 'custom',
      maxFeePerGas: ethers.parseUnits('125', 'gwei'),
      maxPriorityFeePerGas: ethers.parseUnits('100', 'gwei'),
      source: 'custom',
    });
    expect(estimateFeeWei(21_000n, fees)).toBe(
      21_000n * ethers.parseUnits('125', 'gwei'),
    );
  });
});
