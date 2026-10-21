import type {
  AccountRef,
  AppFailure,
  AssetRef,
  BaseUnitAmount,
  ChainId,
  HexData,
  UnixMilliseconds,
} from '../shared';

export interface TransactionDraft {
  readonly chainId: ChainId;
  readonly account: AccountRef;
  readonly to: string;
  readonly asset: AssetRef;
  readonly amount: BaseUnitAmount;
  readonly data: HexData;
}

interface OperationBase {
  readonly id: string;
  readonly chainId: ChainId;
  readonly account: AccountRef;
  readonly createdAt: UnixMilliseconds;
  readonly updatedAt: UnixMilliseconds;
}

export type OperationRecord =
  | (OperationBase & { readonly status: 'draft' })
  | (OperationBase & { readonly status: 'submitting' })
  | (OperationBase & {
      readonly status: 'submitted';
      readonly transactionHash: HexData;
    })
  | (OperationBase & {
      readonly status: 'confirmed';
      readonly transactionHash: HexData;
      readonly blockNumber: bigint;
    })
  | (OperationBase & {
      readonly status: 'failed';
      readonly transactionHash?: HexData;
      readonly failure: AppFailure;
    })
  | (OperationBase & {
      readonly status: 'replaced' | 'cancelled';
      readonly transactionHash: HexData;
      readonly replacementHash: HexData;
    })
  | (OperationBase & {
      readonly status: 'dropped' | 'reorged';
      readonly transactionHash: HexData;
    });
