import type { Alert, AccountJob } from '../../../packages/production-contracts/src/types.gen.ts';
export type SharedModels = { alert: Alert; accountJob: AccountJob };
export type NativeRoute = { name: 'discovery' | 'portfolio' | 'watchlists' | 'notifications' | 'chat' | 'live' } | { name: 'token'; address: string } | { name: 'profile'; address: string };
export type PendingOperation = Readonly<{ requestId: string; chainId: 125; startedAt: number; returnRoute: '/portfolio'; transactionHash?: string; state: 'awaiting_wallet' | 'submitted' | 'unknown' }>;
export interface ProtectedStorage { readonly provider: 'ios-keychain' | 'android-keystore'; get(key: 'session' | 'pushToken'): Promise<string | null>; set(key: 'session' | 'pushToken', value: string): Promise<void>; clear(): Promise<void> }
export interface NativeWalletAdapter { readonly custody: 'external-wallet'; readonly supportedChain: 125; connect(signal: AbortSignal): Promise<{ address: string }>; handoff(operation: PendingOperation, signal: AbortSignal): Promise<void>; }
export interface PushRegistration { installationId: string; platform: 'ios' | 'android'; token: string; accountId: string }
export interface NotificationRegistrationAPI { register(input: PushRegistration, signal: AbortSignal): Promise<void>; revoke(installationId: string, accountId: string, signal: AbortSignal): Promise<void> }
export const storageInventory = Object.freeze({ session: { provider: 'protected', expiry: 'server-expiry', deleteOn: 'logout' }, pushToken: { provider: 'protected', expiry: 'rotation', deleteOn: 'logout' }, pendingOperation: { provider: 'nonsecret-local', expiry: 'reconciled', deleteOn: 'account-switch' }, publicSnapshot: { provider: 'local-lru', capBytes: 100 * 1024 * 1024, deleteOn: 'purge' }, privateSnapshot: { provider: 'protected-cache', capBytes: 20 * 1024 * 1024, deleteOn: 'logout' } });
const id = /^[a-zA-Z0-9_-]{16,128}$/;
const hash = /^0x[0-9a-fA-F]{64}$/;
export function validatePending(value: unknown): PendingOperation {
 if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('INVALID_PENDING');
 const p = value as Record<string, unknown>;
 const keys = ['requestId','chainId','startedAt','returnRoute','transactionHash','state'];
 if (!['requestId','chainId','startedAt','returnRoute','state'].every(key=>Object.hasOwn(p,key)) || Object.keys(p).some(k => !keys.includes(k)) || typeof p.requestId !== 'string' || !id.test(p.requestId) || p.chainId !== 125 || !Number.isSafeInteger(p.startedAt) || (p.startedAt as number) < 0 || p.returnRoute !== '/portfolio' || !['awaiting_wallet','submitted','unknown'].includes(p.state as string) || (p.transactionHash !== undefined && (typeof p.transactionHash !== 'string' || !hash.test(p.transactionHash))) || (p.state === 'submitted' && !p.transactionHash)) throw new Error('INVALID_PENDING');
 return Object.freeze({ ...p }) as PendingOperation;
}
// Callback is a wake-up hint only: it never supplies authoritative financial outcome.
export function consumeWalletReturn(raw: string, canonicalOrigin: string, operation: PendingOperation, consumed: ReadonlySet<string>): { operation: PendingOperation; consumed: ReadonlySet<string> } {
 validatePending(operation);
 if (raw.length > 2048) throw new Error('INVALID_LINK');
 const url = new URL(raw); const origin = new URL(canonicalOrigin);
 if (origin.protocol !== 'https:' || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash) throw new Error('INVALID_ORIGIN');
 if (url.origin !== origin.origin || url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/wallet/return' || url.hash || [...url.searchParams.keys()].some(k => k !== 'requestId') || url.searchParams.getAll('requestId').length !== 1) throw new Error('INVALID_LINK');
 if(raw!==`${origin.origin}/wallet/return?requestId=${encodeURIComponent(operation.requestId)}`)throw new Error('INVALID_LINK');
 const requestId = url.searchParams.get('requestId');
 if (requestId !== operation.requestId || consumed.has(requestId)) throw new Error('UNRELATED_OR_DUPLICATE_LINK');
 return { operation: Object.freeze({ ...operation, state: operation.transactionHash ? 'submitted' : 'unknown' }), consumed: new Set([...consumed, requestId]) };
}
export type PendingJournal = Readonly<{version:2;operation:PendingOperation;callbackConsumed:boolean}>;
export function decodePendingJournal(value:unknown):PendingJournal {
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('INVALID_PENDING_JOURNAL');
 if(!Object.hasOwn(value,'version')){const operation=validatePending(value);return Object.freeze({version:2,operation,callbackConsumed:operation.state!=='awaiting_wallet'});}
 const record=value as Record<string,unknown>;
 if(Object.keys(record).length!==3||Object.keys(record).some(key=>!['version','operation','callbackConsumed'].includes(key))||record.version!==2||typeof record.callbackConsumed!=='boolean')throw new Error('INVALID_PENDING_JOURNAL');
 return Object.freeze({version:2,operation:validatePending(record.operation),callbackConsumed:record.callbackConsumed});
}
export function receivePendingJournal(raw:string,origin:string,journal:PendingJournal):PendingJournal {
 const valid=decodePendingJournal(journal),result=consumeWalletReturn(raw,origin,valid.operation,new Set(valid.callbackConsumed?[valid.operation.requestId]:[]));
 return Object.freeze({version:2,operation:result.operation,callbackConsumed:true});
}
export function executionAllowed(state: { online: boolean; sessionFresh: boolean; manifestFresh: boolean; quoteFresh: boolean; receiptReconciled: boolean }): boolean { const keys = ['online', 'sessionFresh', 'manifestFresh', 'quoteFresh', 'receiptReconciled'] as const; return !!state && typeof state === 'object' && Object.keys(state).length === keys.length && keys.every(key => Object.hasOwn(state, key) && state[key] === true); }
export function retryDelay(attempt: number, random: number): number { if (!Number.isInteger(attempt) || attempt < 0 || !Number.isFinite(random) || random < 0 || random >= 1) throw new Error('INVALID_BACKOFF'); return Math.floor(random * Math.min(30000, 500 * 2 ** Math.min(attempt, 16))); }
export function safePushRoute(input: unknown, now: number): NativeRoute {
 if (!Number.isSafeInteger(now) || now < 0 || !input || typeof input !== 'object') throw new Error('INVALID_PUSH');
 const p = input as Record<string, unknown>;
 if (Object.keys(p).some(k => !['notificationId','category','expiresAt','route'].includes(k)) || typeof p.notificationId !== 'string' || !id.test(p.notificationId) || !['price','watchlist','fill','reward','social'].includes(p.category as string) || typeof p.expiresAt !== 'number' || !Number.isSafeInteger(p.expiresAt) || p.expiresAt <= now || p.route !== '/notifications') throw new Error('INVALID_PUSH');
 return { name: 'notifications' }; // Resolve authorized current entity after login; never trust payload content.
}
export function parseNativeRoute(path: string): NativeRoute {
 if (typeof path !== 'string' || path.length > 256 || path.includes('?') || path.includes('#') || path.includes('%')) throw new Error('INVALID_ROUTE');
 const simple: Record<string, NativeRoute> = {'/':{name:'discovery'},'/portfolio':{name:'portfolio'},'/watchlists':{name:'watchlists'},'/notifications':{name:'notifications'},'/chat':{name:'chat'},'/live':{name:'live'}};
 if (Object.hasOwn(simple, path)) return simple[path];
 const match = /^\/(token|profile)\/(0x[0-9a-fA-F]{40})$/.exec(path);
 if (!match) throw new Error('INVALID_ROUTE');
 return {name:match[1] as 'token' | 'profile',address:match[2]};
}
