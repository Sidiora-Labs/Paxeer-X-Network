// @vitest-environment jsdom

import { beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import {
  formatUsdAsFiat,
  getRatesFreshness,
  initCurrencyService,
} from './currency';
import { estimateFeeWei, resolveFeeOverrides } from './fees';
import {
  currencyRatesRepository,
  preferencesRepository,
} from '@/platform/storage/repositories';

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
