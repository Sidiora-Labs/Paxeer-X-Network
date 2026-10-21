declare const brand: unique symbol;

export type Brand<T, Name extends string> = T & {
  readonly [brand]: Name;
};

export type Address = Brand<string, 'Address'>;
export type ChainId = Brand<number, 'ChainId'>;
export type BaseUnitAmount = Brand<bigint, 'BaseUnitAmount'>;
export type CorrelationId = Brand<string, 'CorrelationId'>;
export type HexData = Brand<string, 'HexData'>;
export type UnixMilliseconds = Brand<number, 'UnixMilliseconds'>;

export interface ChainRef {
  readonly id: ChainId;
  readonly name: string;
  readonly nativeSymbol: string;
}

export interface AccountRef {
  readonly address: Address;
  readonly chainId: ChainId;
}

export interface AssetRef {
  readonly chainId: ChainId;
  readonly address: Address | 'native';
  readonly symbol: string;
  readonly decimals: number;
}

export interface MoneyAmount {
  readonly asset: AssetRef;
  readonly baseUnits: BaseUnitAmount;
}

export type DataSourceKind =
  | 'chain-rpc'
  | 'indexer'
  | 'managed-wallet'
  | 'injected-wallet'
  | 'price-provider'
  | 'local-operation'
  | 'user';

export interface Observation {
  readonly source: DataSourceKind;
  readonly observedAt: UnixMilliseconds;
  readonly blockNumber?: bigint;
}

export type Freshness =
  | { readonly status: 'fresh'; readonly observation: Observation }
  | {
      readonly status: 'stale';
      readonly observation: Observation;
      readonly staleAfter: UnixMilliseconds;
    }
  | {
      readonly status: 'partial';
      readonly observation: Observation;
      readonly missingSources: readonly DataSourceKind[];
    }
  | { readonly status: 'unavailable'; readonly reason: AppFailure };

export type AsyncState<T> =
  | { readonly status: 'idle' }
  | { readonly status: 'loading'; readonly previous?: T }
  | { readonly status: 'ready'; readonly value: T; readonly freshness: Freshness }
  | {
      readonly status: 'empty';
      readonly freshness: Exclude<Freshness, { status: 'unavailable' }>;
    }
  | {
      readonly status: 'error';
      readonly failure: AppFailure;
      readonly previous?: T;
    };

export type FailureDomain =
  | 'approval'
  | 'custody'
  | 'dapp'
  | 'navigation'
  | 'platform'
  | 'portfolio'
  | 'server-edge'
  | 'storage'
  | 'swap'
  | 'transaction'
  | 'unknown';

export type FailureKind =
  | 'cancelled'
  | 'corrupt'
  | 'denied'
  | 'offline'
  | 'programmer'
  | 'stale'
  | 'timeout'
  | 'unavailable'
  | 'validation';

export type RetryPolicy =
  | { readonly kind: 'never' }
  | { readonly kind: 'manual' }
  | {
      readonly kind: 'bounded';
      readonly attempts: number;
      readonly backoffMs: number;
    };

export interface AppFailure {
  readonly domain: FailureDomain;
  readonly kind: FailureKind;
  readonly code: string;
  readonly severity: 'info' | 'warning' | 'error' | 'fatal';
  readonly retry: RetryPolicy;
  readonly publicMessageKey: string;
  readonly correlationId: CorrelationId;
  readonly userAction?: string;
  readonly cause?: unknown;
}

export type ParseResult<T> =
  | { readonly ok: true; readonly value: T }
  | { readonly ok: false; readonly issues: readonly ParseIssue[] };

export interface ParseIssue {
  readonly path: string;
  readonly code:
    | 'invalid_format'
    | 'invalid_type'
    | 'out_of_bounds'
    | 'unknown_field'
    | 'unsupported_value';
  readonly message: string;
}

export type BoundaryParser<T> = (input: unknown) => ParseResult<T>;
