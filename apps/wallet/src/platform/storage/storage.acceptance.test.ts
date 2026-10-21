// @vitest-environment jsdom

import { beforeEach, describe, expect, it } from 'vitest';
import { getBackgroundFailures } from '@/platform/status/background-failures';
import {
  resetStorageForLifecycle,
  storageCatalog,
  StorageCorruptionError,
} from './registry';
import {
  contactsRepository,
  custodyChoiceRepository,
  pendingSendRepository,
  preferencesRepository,
  transferObservationsRepository,
} from './repositories';

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});

describe('application storage registry', () => {
  it('catalogs every record with ownership, lifecycle, quota, and prohibited data', () => {
    const catalog = storageCatalog();
    expect(catalog.length).toBeGreaterThanOrEqual(11);
    expect(new Set(catalog.map((entry) => entry.id)).size).toBe(catalog.length);
    for (const entry of catalog) {
      expect(entry.owner).toBeTruthy();
      expect(entry.schema).toBeTruthy();
      expect(entry.retention).toBeTruthy();
      expect(entry.migration).toBeTruthy();
      expect(entry.quotaBytes).toBeGreaterThan(0);
      expect(entry.resetOn.length).toBeGreaterThan(0);
      expect(entry.prohibitedData).toContain('mnemonic');
      expect(entry.prohibitedData).toContain('private key');
    }
  });

  it('migrates and validates a legacy contact record into a versioned envelope', () => {
    localStorage.setItem(
      'paxeer_contacts',
      JSON.stringify([
        {
          id: 'contact-1',
          name: 'Treasury',
          address: '0x1111111111111111111111111111111111111111',
          createdAt: 1,
          updatedAt: 1,
        },
      ]),
    );
    expect(contactsRepository.read()).toHaveLength(1);
    expect(localStorage.getItem('paxeer_contacts')).toBeNull();
    expect(
      JSON.parse(localStorage.getItem('paxport:v1:contacts') ?? '{}'),
    ).toMatchObject({ version: 1 });
  });

  it('signals and resets optional corruption while security state fails closed', () => {
    localStorage.setItem('paxport:v1:contacts', '{"version":1,"value":');
    expect(contactsRepository.read()).toEqual([]);
    expect(getBackgroundFailures()[0]).toMatchObject({
      domain: 'storage',
      code: 'STORAGE_CORRUPT',
    });

    localStorage.setItem(
      'paxport:v1:custody-choice',
      JSON.stringify({ version: 1, writtenAt: 1, value: 'unknown' }),
    );
    expect(() => custodyChoiceRepository.read()).toThrow(StorageCorruptionError);
  });

  it('clears only lifecycle-owned records', () => {
    preferencesRepository.write({
      currency: 'EUR',
      language: 'de',
      customRpc: null,
      customNonce: null,
      feeMode: 'auto',
      customMaxFeeGwei: null,
      customPriorityFeeGwei: null,
      developerMode: false,
      showHexData: false,
      notifications: {},
    });
    custodyChoiceRepository.write('embedded');
    pendingSendRepository.write({
      symbol: 'PAX',
      amount: '1',
      decimals: 18,
      recipient: '0x1111111111111111111111111111111111111111',
      txHash:
        '0x1111111111111111111111111111111111111111111111111111111111111111',
      timestamp: 1,
    });

    resetStorageForLifecycle('custody-switch');

    expect(custodyChoiceRepository.read()).toBeNull();
    expect(pendingSendRepository.read()).toBeNull();
    expect(preferencesRepository.read().currency).toBe('EUR');
  });

  it('keeps the newest hundred legacy transfer observations by block, whatever the key order', () => {
    for (let block = 103; block >= 1; block -= 1) {
      localStorage.setItem(`paxeer.wallet.transferObservation:125:0x${block.toString(16)}`, JSON.stringify({ blockNumber: block }));
    }
    localStorage.setItem('paxeer.wallet.transferObservation:125:0xpending-a', JSON.stringify({ blockNumber: null }));
    localStorage.setItem('paxeer.wallet.transferObservation:125:0xpending-b', JSON.stringify({ truth: 'unknown' }));
    const kept = transferObservationsRepository.read();
    expect(Object.keys(kept)).toHaveLength(100);
    expect(kept['125:0xpending-a']).toEqual({ blockNumber: null });
    expect(kept['125:0xpending-b']).toEqual({ truth: 'unknown' });
    for (let block = 1; block <= 5; block += 1) expect(kept[`125:0x${block.toString(16)}`]).toBeUndefined();
    for (let block = 6; block <= 103; block += 1) expect(kept[`125:0x${block.toString(16)}`]).toEqual({ blockNumber: block });
  });

  it('skips a corrupt legacy transfer entry with a warning and migrates the rest', () => {
    const before = getBackgroundFailures().length;
    localStorage.setItem('paxeer.wallet.transferObservation:125:0xbad', '{not json');
    localStorage.setItem('paxeer.wallet.transferObservation:125:0xgood', JSON.stringify({ status: 'confirmed' }));
    expect(transferObservationsRepository.read()).toEqual({ '125:0xgood': { status: 'confirmed' } });
    const failures = getBackgroundFailures();
    expect(failures.length).toBe(before + 1);
    expect(failures[0]).toMatchObject({ domain: 'storage', code: 'STORAGE_LEGACY_ENTRY_SKIPPED' });
    expect(failures.some((failure) => failure.code === 'STORAGE_MIGRATION_FAILED')).toBe(false);
    expect(localStorage.getItem('paxeer.wallet.transferObservation:125:0xbad')).toBeNull();
    expect(localStorage.getItem('paxeer.wallet.transferObservation:125:0xgood')).toBeNull();
    expect(transferObservationsRepository.read()).toEqual({ '125:0xgood': { status: 'confirmed' } });
    expect(getBackgroundFailures().length).toBe(before + 1);
  });
});
