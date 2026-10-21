import { DEFAULT_SELECTION } from './catalogue';
import { parseSelection } from './resolve';
import type { ThemeSelection } from './schema';

export const THEME_STORAGE_KEY = 'paxeer.theme.v1';
export const THEME_ACCOUNT_POINTER_KEY = 'paxeer.theme.v1.account';

export function normalizeAccount(account: string): string {
    return account.trim().toLowerCase();
}

export function accountStorageKey(account: string): string {
    return `${THEME_STORAGE_KEY}:${normalizeAccount(account)}`;
}

export function themeStorage(): Storage | null {
    if (typeof window === 'undefined') return null;
    try {
        return window.localStorage;
    } catch {
        return null;
    }
}

function read(storage: Storage | null, key: string): string | null {
    if (!storage) return null;
    try {
        return storage.getItem(key);
    } catch {
        return null;
    }
}

function write(storage: Storage | null, key: string, value: string | null): boolean {
    if (!storage) return false;
    try {
        if (value === null) storage.removeItem(key);
        else storage.setItem(key, value);
        return true;
    } catch {
        return false;
    }
}

export function readStoredAccount(storage: Storage | null): string | null {
    const account = read(storage, THEME_ACCOUNT_POINTER_KEY);
    return account ? normalizeAccount(account) : null;
}

export function writeStoredAccount(storage: Storage | null, account: string | null): boolean {
    return write(storage, THEME_ACCOUNT_POINTER_KEY, account === null ? null : normalizeAccount(account));
}

export function readAccountSelection(storage: Storage | null, account: string): ThemeSelection | null {
    return parseSelection(read(storage, accountStorageKey(account)));
}

export function readDeviceSelection(storage: Storage | null): ThemeSelection | null {
    return parseSelection(read(storage, THEME_STORAGE_KEY));
}

export function readSelection(storage: Storage | null, account: string | null): ThemeSelection {
    return (account ? readAccountSelection(storage, account) : null) ?? readDeviceSelection(storage) ?? DEFAULT_SELECTION;
}

export function writeSelection(storage: Storage | null, account: string | null, selection: ThemeSelection): boolean {
    return write(storage, account ? accountStorageKey(account) : THEME_STORAGE_KEY, JSON.stringify(selection));
}
