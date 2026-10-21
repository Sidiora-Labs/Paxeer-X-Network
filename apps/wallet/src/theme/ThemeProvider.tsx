'use client';

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useLayoutEffect,
    useMemo,
    useRef,
    useState,
    useSyncExternalStore,
    type ReactNode,
} from 'react';
import { themeCatalogue } from './catalogue';
import { browserEnvironment, type ThemeEnvironmentSource } from './environment';
import {
    normalizeAccount,
    readAccountSelection,
    readSelection,
    readStoredAccount,
    themeStorage,
    writeSelection,
    writeStoredAccount,
} from './persistence';
import { applyResolvedTheme, resolveTheme } from './resolve';
import {
    DEFAULT_ENVIRONMENT,
    type ResolvedTheme,
    type ThemeData,
    type ThemeEnvironment,
    type ThemeSelection,
} from './schema';

export interface ThemeContextValue {
    selection: ThemeSelection;
    resolved: ResolvedTheme;
    environment: ThemeEnvironment;
    account: string | null;
    catalogue: ThemeData;
    setSelection: (patch: Partial<ThemeSelection>) => void;
    setAccount: (account: string | null) => void;
}

interface ThemeState {
    account: string | null;
    selection: ThemeSelection;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

const useIsomorphicLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect;

export interface ThemeProviderProps {
    children: ReactNode;
    environment?: ThemeEnvironmentSource;
    storage?: Storage | null;
}

export function ThemeProvider({ children, environment, storage }: ThemeProviderProps) {
    const [store] = useState<Storage | null>(() => (storage === undefined ? themeStorage() : storage));
    const [source] = useState<ThemeEnvironmentSource>(
        () => environment ?? browserEnvironment(typeof window === 'undefined' ? undefined : window),
    );
    const env = useSyncExternalStore(source.subscribe, source.read, () => DEFAULT_ENVIRONMENT);
    const [state, setState] = useState<ThemeState>(() => {
        const account = readStoredAccount(store);
        return { account, selection: readSelection(store, account) };
    });
    const stateRef = useRef(state);
    stateRef.current = state;

    const resolved = useMemo(() => resolveTheme(state.selection, env), [state.selection, env]);

    useIsomorphicLayoutEffect(() => {
        applyResolvedTheme(document, resolved);
    }, [resolved]);

    const setSelection = useCallback(
        (patch: Partial<ThemeSelection>) => {
            const current = stateRef.current;
            const selection: ThemeSelection = { ...current.selection, ...patch };
            writeSelection(store, current.account, selection);
            const next = { account: current.account, selection };
            stateRef.current = next;
            setState(next);
        },
        [store],
    );

    const setAccount = useCallback(
        (account: string | null) => {
            const normalized = account === null ? null : normalizeAccount(account);
            const current = stateRef.current;
            if (normalized === current.account) return;
            writeStoredAccount(store, normalized);
            const selection = normalized
                ? (readAccountSelection(store, normalized) ?? current.selection)
                : readSelection(store, null);
            const next = { account: normalized, selection };
            stateRef.current = next;
            setState(next);
        },
        [store],
    );

    const value = useMemo<ThemeContextValue>(
        () => ({
            selection: state.selection,
            resolved,
            environment: env,
            account: state.account,
            catalogue: themeCatalogue,
            setSelection,
            setAccount,
        }),
        [state, resolved, env, setSelection, setAccount],
    );

    return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
    const value = useContext(ThemeContext);
    if (!value) throw new Error('useTheme must be used inside ThemeProvider');
    return value;
}

export function useThemeAccount(account: string | null | undefined): void {
    const { setAccount } = useTheme();
    useEffect(() => {
        if (account === undefined) return;
        setAccount(account);
    }, [account, setAccount]);
}
