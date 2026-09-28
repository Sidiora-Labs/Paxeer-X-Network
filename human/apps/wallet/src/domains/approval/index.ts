import type {
  AccountRef,
  ChainRef,
  CorrelationId,
  HexData,
  UnixMilliseconds,
} from '../shared';

export type ApprovalOrigin =
  | { readonly kind: 'user'; readonly label: string }
  | { readonly kind: 'dapp'; readonly origin: string; readonly tabId: string }
  | { readonly kind: 'system'; readonly reason: string };

export type ApprovalOperation =
  | 'chain-change'
  | 'message-sign'
  | 'permission-grant'
  | 'secret-export'
  | 'transaction'
  | 'typed-data-sign';

export interface ApprovalIntent<Payload = unknown> {
  readonly version: 1;
  readonly id: CorrelationId;
  readonly createdAt: UnixMilliseconds;
  readonly expiresAt: UnixMilliseconds;
  readonly identityGeneration: number;
  readonly origin: ApprovalOrigin;
  readonly custody: 'self-custody' | 'managed' | 'funded';
  readonly account: AccountRef;
  readonly chain: ChainRef;
  readonly operation: ApprovalOperation;
  readonly payload: Readonly<Payload>;
  readonly simulationDigest?: HexData;
  readonly warnings: readonly string[];
  readonly presentationRevision: string;
  readonly digest: HexData;
}

export interface ApprovalAuthorization {
  readonly intentId: CorrelationId;
  readonly digest: HexData;
  readonly token: string;
  readonly expiresAt: UnixMilliseconds;
}
