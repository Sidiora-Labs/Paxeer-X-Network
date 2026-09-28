import {
  issue,
  parseBoundedString,
  parseHttpsUrl,
  parseRecord,
  rejectUnknownFields,
  type AccountRef,
  type BoundaryParser,
  type ChainId,
  type UnixMilliseconds,
} from '../shared';

export type DappMethod =
  | 'eth_requestAccounts'
  | 'eth_sendTransaction'
  | 'personal_sign'
  | 'wallet_addEthereumChain'
  | 'wallet_switchEthereumChain'
  | 'wallet_watchAsset'
  | 'eth_signTypedData_v4';

export interface DappPermission {
  readonly origin: string;
  readonly custody: 'self-custody' | 'managed' | 'funded';
  readonly accounts: readonly AccountRef[];
  readonly chains: readonly ChainId[];
  readonly methods: readonly DappMethod[];
  readonly expiresAt: UnixMilliseconds;
  readonly sessionOnly: boolean;
}

export interface DappRequest {
  readonly id: string;
  readonly origin: string;
  readonly tabId: string;
  readonly navigationGeneration: number;
  readonly method: DappMethod;
  readonly params: unknown;
}

const METHODS = new Set<DappMethod>([
  'eth_requestAccounts',
  'eth_sendTransaction',
  'personal_sign',
  'wallet_addEthereumChain',
  'wallet_switchEthereumChain',
  'wallet_watchAsset',
  'eth_signTypedData_v4',
]);

export const parseDappRequest: BoundaryParser<DappRequest> = (input) => {
  const record = parseRecord(input);
  if (!record.ok) return record;
  const fields = rejectUnknownFields(
    record.value,
    new Set([
      'id',
      'origin',
      'tabId',
      'navigationGeneration',
      'method',
      'params',
    ]),
  );
  if (!fields.ok) return fields;
  const id = parseBoundedString(record.value.id, {
    path: '$.id',
    minLength: 1,
    maxLength: 128,
  });
  if (!id.ok) return id;
  const originUrl = parseHttpsUrl(record.value.origin, { maxLength: 512 });
  if (!originUrl.ok) return originUrl;
  if (originUrl.value.pathname !== '/' || originUrl.value.search || originUrl.value.hash) {
    return issue('$.origin', 'invalid_format', 'Origin must not contain a path');
  }
  const tabId = parseBoundedString(record.value.tabId, {
    path: '$.tabId',
    minLength: 1,
    maxLength: 128,
  });
  if (!tabId.ok) return tabId;
  if (
    typeof record.value.navigationGeneration !== 'number' ||
    !Number.isSafeInteger(record.value.navigationGeneration) ||
    record.value.navigationGeneration < 0
  ) {
    return issue(
      '$.navigationGeneration',
      'invalid_format',
      'Navigation generation must be a non-negative integer',
    );
  }
  if (
    typeof record.value.method !== 'string' ||
    !METHODS.has(record.value.method as DappMethod)
  ) {
    return issue('$.method', 'unsupported_value', 'DApp method is not allowed');
  }
  const paramsSize = JSON.stringify(record.value.params ?? null).length;
  if (paramsSize > 131_072) {
    return issue('$.params', 'out_of_bounds', 'DApp params exceed the size limit');
  }
  return {
    ok: true,
    value: {
      id: id.value,
      origin: originUrl.value.origin,
      tabId: tabId.value,
      navigationGeneration: record.value.navigationGeneration,
      method: record.value.method as DappMethod,
      params: record.value.params,
    },
  };
};
