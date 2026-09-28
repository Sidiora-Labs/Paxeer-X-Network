'use client';

import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { EndpointClient, HumanClient, KernelAvailability, KernelClient } from '@paxeer/wallet';
import type { AccountConfig } from './config';

export interface AccountClients {
    readonly config: AccountConfig;
    readonly endpoint: EndpointClient;
    readonly kernel: KernelAvailability;
    readonly kernelReads: KernelClient;
    readonly human: HumanClient;
    readonly fetch: typeof fetch;
}

const AccountContext = createContext<AccountClients | null>(null);

export interface AccountProviderProps {
    readonly config: AccountConfig;
    readonly children: ReactNode;
    readonly authorization?: () => Promise<string | null> | string | null;
    readonly fetch?: typeof fetch;
}

export function AccountProvider({ config, children, authorization, fetch: fetchImpl }: AccountProviderProps) {
    const clients = useMemo<AccountClients>(() => {
        const boundFetch = fetchImpl ?? globalThis.fetch.bind(globalThis);
        const endpoint = new EndpointClient({ url: config.endpointUrl, fetch: boundFetch });
        const kernel = new KernelAvailability(endpoint);
        return {
            config,
            endpoint,
            kernel,
            kernelReads: new KernelClient(endpoint, kernel),
            human: new HumanClient({ url: config.humanUrl, kernel, fetch: boundFetch, authorization }),
            fetch: boundFetch,
        };
    }, [config, authorization, fetchImpl]);
    return <AccountContext.Provider value={clients}>{children}</AccountContext.Provider>;
}

export function useAccountClients(): AccountClients {
    const context = useContext(AccountContext);
    if (!context) throw new Error('useAccountClients must be used inside AccountProvider');
    return context;
}
