import type {
  AccountRef,
  Address,
  AppFailure,
  ChainId,
  HexData,
  MoneyAmount,
} from '../shared';

export interface PublicWalletSnapshot {
  readonly accounts: readonly AccountRef[];
  readonly activeAccount: AccountRef;
  readonly locked: boolean;
}

export interface TransferRequest {
  readonly to: Address;
  readonly amount: MoneyAmount;
  readonly data?: HexData;
}

export interface SubmittedTransfer {
  readonly chainId: ChainId;
  readonly transactionHash: HexData;
}

export interface ReceiveCapability {
  readonly getReceiveAddress: () => Promise<Address>;
}

export interface TransferCapability {
  readonly submitTransfer: (request: TransferRequest) => Promise<SubmittedTransfer>;
}

export interface ManagedCapabilities
  extends ReceiveCapability,
    TransferCapability {
  readonly signOut: () => Promise<void>;
}

export interface FundedCapabilities {
  readonly submitPolicyCall: (
    request: TransferRequest,
  ) => Promise<SubmittedTransfer>;
  readonly signOut: () => Promise<void>;
}

export type CustodySession =
  | {
      readonly kind: 'managed';
      readonly identityId: string;
      readonly snapshot: PublicWalletSnapshot;
      readonly capabilities: ManagedCapabilities;
    }
  | {
      readonly kind: 'funded';
      readonly identityId: string;
      readonly snapshot: PublicWalletSnapshot;
      readonly policyRevision: string;
      readonly capabilities: FundedCapabilities;
    }
  | {
      readonly kind: 'unavailable';
      readonly requestedKind: 'managed' | 'funded';
      readonly failure: AppFailure;
    };

export function isActionableCustody(
  session: CustodySession,
): session is Exclude<CustodySession, { kind: 'unavailable' }> {
  return session.kind !== 'unavailable' && !session.snapshot.locked;
}
