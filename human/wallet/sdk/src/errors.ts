export const PROVIDER_ERROR_CODES = {
  userRejectedRequest: 4001,
  unauthorized: 4100,
  unsupportedMethod: 4200,
  disconnected: 4900,
  chainDisconnected: 4901,
  invalidParams: -32602,
  internal: -32603,
} as const;

export type ProviderErrorCode = (typeof PROVIDER_ERROR_CODES)[keyof typeof PROVIDER_ERROR_CODES];

export class ProviderRpcError extends Error {
  constructor(
    public readonly code: number,
    message: string,
    public readonly data?: unknown,
  ) {
    super(message);
    this.name = 'ProviderRpcError';
  }
}

export class UserRejectedRequestError extends ProviderRpcError {
  constructor(message = 'the user rejected the request', data?: unknown) {
    super(PROVIDER_ERROR_CODES.userRejectedRequest, message, data);
    this.name = 'UserRejectedRequestError';
  }
}

export type UnauthorizedReason =
  | 'not_connected'
  | 'no_token'
  | 'account_mismatch'
  | 'missing_construction'
  | 'unknown_construction'
  | 'digest_mismatch'
  | 'custody_not_supported';

export class UnauthorizedError extends ProviderRpcError {
  constructor(
    public readonly reason: UnauthorizedReason,
    message: string,
  ) {
    super(PROVIDER_ERROR_CODES.unauthorized, message, { reason });
    this.name = 'UnauthorizedError';
  }
}

export class UnsupportedMethodError extends ProviderRpcError {
  constructor(public readonly method: string) {
    super(PROVIDER_ERROR_CODES.unsupportedMethod, `method ${method} is not supported`, { method });
    this.name = 'UnsupportedMethodError';
  }
}

export class DisconnectedError extends ProviderRpcError {
  constructor(message = 'the provider is disconnected from all chains', data?: unknown) {
    super(PROVIDER_ERROR_CODES.disconnected, message, data);
    this.name = 'DisconnectedError';
  }
}

export class ChainDisconnectedError extends ProviderRpcError {
  constructor(
    public readonly requestedChainId: number,
    public readonly connectedChainId: number,
  ) {
    super(
      PROVIDER_ERROR_CODES.chainDisconnected,
      `the provider is connected to chain ${connectedChainId}, not ${requestedChainId}`,
      { requestedChainId, connectedChainId },
    );
    this.name = 'ChainDisconnectedError';
  }
}

export class InvalidParamsError extends ProviderRpcError {
  constructor(
    public readonly field: string,
    message: string,
  ) {
    super(PROVIDER_ERROR_CODES.invalidParams, message, { field });
    this.name = 'InvalidParamsError';
  }
}

export const GATEWAY_REFUSAL_REASONS = [
  'unauthorized',
  'invalid_body',
  'no_wallet',
  'wallet_provision_failed',
  'sign_failed',
  'send_failed',
  'RATE_LIMIT',
  'TX_VALUE_CAP',
  'DAILY_VALUE_CAP',
  'WALLET_DISABLED',
] as const;

export type GatewayRefusalReason = (typeof GATEWAY_REFUSAL_REASONS)[number] | 'gateway_error';

export class GatewayRefusalError extends ProviderRpcError {
  constructor(
    public readonly reason: GatewayRefusalReason,
    public readonly status: number,
    message: string,
    public readonly body: unknown,
  ) {
    super(gatewayCode(status), message, { reason, status, body });
    this.name = 'GatewayRefusalError';
  }
}

export class RpcResponseError extends ProviderRpcError {
  constructor(code: number, message: string, data?: unknown) {
    super(code, message, data);
    this.name = 'RpcResponseError';
  }
}

function gatewayCode(status: number): number {
  if (status === 401 || status === 403 || status === 404) return PROVIDER_ERROR_CODES.unauthorized;
  if (status === 400) return PROVIDER_ERROR_CODES.invalidParams;
  return PROVIDER_ERROR_CODES.internal;
}

export function gatewayRefusal(status: number, body: unknown): GatewayRefusalError {
  const record = typeof body === 'object' && body !== null ? (body as Record<string, unknown>) : {};
  const raw = typeof record.error === 'string' ? record.error : '';
  const known = (GATEWAY_REFUSAL_REASONS as readonly string[]).includes(raw);
  const reason: GatewayRefusalReason = known
    ? (raw as GatewayRefusalReason)
    : status === 401
      ? 'unauthorized'
      : 'gateway_error';
  const message =
    typeof record.message === 'string'
      ? record.message
      : typeof record.detail === 'string'
        ? record.detail
        : raw || `gateway request failed: ${status}`;
  return new GatewayRefusalError(reason, status, message, body);
}
