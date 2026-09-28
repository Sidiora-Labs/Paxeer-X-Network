import type { ethers } from 'ethers';

export interface SigningAuthorization {
  readonly approvalId: string;
  readonly accountAddress: string;
  readonly chainId: number;
}

export interface ApprovedMessageRequest extends SigningAuthorization {
  readonly message: string | Uint8Array;
}

export interface ApprovedTypedDataRequest extends SigningAuthorization {
  readonly domain: ethers.TypedDataDomain;
  readonly types: Record<string, ethers.TypedDataField[]>;
  readonly value: Record<string, unknown>;
}

export interface ApprovedTransactionRequest extends SigningAuthorization {
  readonly transaction: ethers.TransactionRequest;
}
