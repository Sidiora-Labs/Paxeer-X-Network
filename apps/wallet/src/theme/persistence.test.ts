// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { DEFAULT_SELECTION } from './catalogue';
import {
    THEME_ACCOUNT_POINTER_KEY,
    THEME_STORAGE_KEY,
    accountStorageKey,
    readAccountSelection,
    readSelection,
    readStoredAccount,
    themeStorage,
    writeSelection,
    writeStoredAccount,
} from './persistence';
import type { ThemeSelection } from './schema';

const ACCOUNT = '0xAbCdEf0000000000000000000000000000001234';
const LIGHT: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'light', accent: 'green' };
const LARGE: ThemeSelection = { ...DEFAULT_SELECTION, size: 'large', font: 'system' };

beforeEach(() => {
    window.localStorage.clear();
});

describe('theme persistence', () => {
    it('uses the device storage of the browser', () => {
        expect(themeStorage()).toBe(window.localStorage);
    });

    it('stores the device selection under one versioned key', () => {
        const storage = themeStorage();
        expect(readSelection(storage, null)).toEqual(DEFAULT_SELECTION);
        expect(writeSelection(storage, null, LIGHT)).toBe(true);
        expect(JSON.parse(window.localStorage.getItem(THEME_STORAGE_KEY)!)).toEqual(LIGHT);
        expect(readSelection(storage, null)).toEqual(LIGHT);
    });

    it('keys the selection by the signed-in account and falls back to the device selection', () => {
        const storage = themeStorage();
        writeSelection(storage, null, LIGHT);
        expect(accountStorageKey(ACCOUNT)).toBe(`${THEME_STORAGE_KEY}:${ACCOUNT.toLowerCase()}`);
        expect(readAccountSelection(storage, ACCOUNT)).toBeNull();
        expect(readSelection(storage, ACCOUNT)).toEqual(LIGHT);
        writeSelection(storage, ACCOUNT, LARGE);
        expect(JSON.parse(window.localStorage.getItem(accountStorageKey(ACCOUNT))!)).toEqual(LARGE);
        expect(readSelection(storage, ACCOUNT.toUpperCase().replace('0X', '0x'))).toEqual(LARGE);
        expect(readSelection(storage, null)).toEqual(LIGHT);
    });

    it('records and clears the signed-in account pointer', () => {
        const storage = themeStorage();
        expect(readStoredAccount(storage)).toBeNull();
        writeStoredAccount(storage, ACCOUNT);
        expect(window.localStorage.getItem(THEME_ACCOUNT_POINTER_KEY)).toBe(ACCOUNT.toLowerCase());
        expect(readStoredAccount(storage)).toBe(ACCOUNT.toLowerCase());
        writeStoredAccount(storage, null);
        expect(window.localStorage.getItem(THEME_ACCOUNT_POINTER_KEY)).toBeNull();
    });

    it('is inert without storage', () => {
        expect(readSelection(null, ACCOUNT)).toEqual(DEFAULT_SELECTION);
        expect(writeSelection(null, ACCOUNT, LIGHT)).toBe(false);
        expect(writeStoredAccount(null, ACCOUNT)).toBe(false);
        expect(readStoredAccount(null)).toBeNull();
    });
});
