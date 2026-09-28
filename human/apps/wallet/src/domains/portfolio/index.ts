import type {
  AccountRef,
  AppFailure,
  AssetRef,
  BaseUnitAmount,
  Freshness,
} from '../shared';

export interface PortfolioPosition {
  readonly account: AccountRef;
  readonly asset: AssetRef;
  readonly balance: BaseUnitAmount;
  readonly freshness: Freshness;
  readonly price?: {
    readonly currency: string;
    readonly decimal: string;
    readonly freshness: Freshness;
  };
}

export type PortfolioResult =
  | {
      readonly status: 'ready';
      readonly positions: readonly PortfolioPosition[];
      readonly freshness: Freshness;
    }
  | {
      readonly status: 'partial';
      readonly positions: readonly PortfolioPosition[];
      readonly failures: readonly AppFailure[];
      readonly freshness: Freshness;
    }
  | { readonly status: 'unavailable'; readonly failure: AppFailure };

export interface PortfolioQueryIdentity {
  readonly custody: 'managed' | 'injected';
  readonly identityGeneration: number;
  readonly account: AccountRef;
  readonly resource: 'activity' | 'balances' | 'chart' | 'positions' | 'prices';
  readonly schemaVersion: number;
}
