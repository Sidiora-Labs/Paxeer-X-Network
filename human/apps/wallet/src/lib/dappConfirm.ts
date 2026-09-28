import { ethers } from 'ethers';
import { getActiveRpcUrl, PAXEER_CONFIG } from '@/lib/constants';
import { estimateFeeWei, resolveFeeOverrides } from '@/lib/fees';

export type DAppConfirmationKind =
  | 'connect'
  | 'personal_sign'
  | 'eth_sign'
  | 'eth_signTypedData'
  | 'eth_signTypedData_v3'
  | 'eth_signTypedData_v4'
  | 'eth_sendTransaction';

export interface DAppConfirmationRequest {
  kind: DAppConfirmationKind;
  origin: string;
  title: string;
  description: string;
  details: Array<{ label: string; value: string }>;
  warnings?: string[];
}

export interface DAppConfirmationOptions {
  origin: string;
  activeAddress?: string;
  confirm: (request: DAppConfirmationRequest) => Promise<boolean>;
}

/** Message-signing methods (everything except `eth_sendTransaction`). */
export const SIGN_MESSAGE_METHODS = new Set([
  'personal_sign',
  'eth_sign',
  'eth_signTypedData',
  'eth_signTypedData_v3',
  'eth_signTypedData_v4',
]);

/** EIP-712 typed-data variants (the payload is a JSON string / object). */
export const TYPED_DATA_METHODS = new Set([
  'eth_signTypedData',
  'eth_signTypedData_v3',
  'eth_signTypedData_v4',
]);

const SIGNING_METHODS = SIGN_MESSAGE_METHODS;

export function needsDAppConfirmation(method: string): boolean {
  return SIGNING_METHODS.has(method) || method === 'eth_sendTransaction';
}

export function isTypedDataMethod(method: string): boolean {
  return TYPED_DATA_METHODS.has(method);
}

/** A 20-byte hex string (with `0x`), case-insensitive. */
export function isLikelyAddress(value: unknown): value is string {
  return typeof value === 'string' && /^0x[0-9a-fA-F]{40}$/.test(value);
}

export interface ParsedSignRequest {
  /** The signer address the dApp targeted, if it provided one. */
  address?: string;
  /** The raw message param as the dApp sent it (hex or UTF-8 for personal_sign/eth_sign; JSON for typed data). */
  message: string;
}

/**
 * Normalize the params of a message-signing request into `{ address, message }`.
 *
 * Wallets and libraries are inconsistent about argument order:
 *   - `personal_sign`            → `[message, address]` (MetaMask / viem / wagmi)
 *   - `eth_sign`                 → `[address, message]`
 *   - `eth_signTypedData*`       → `[address, typedData]`
 *
 * Some libraries swap the personal_sign order, so we detect the address by
 * shape rather than trusting the position. This is what lets metadata-upload
 * style `personal_sign` requests authenticate regardless of which app emits them.
 */
export function parseSignRequest(
  method: string,
  params: unknown[] | Record<string, unknown> | undefined,
): ParsedSignRequest {
  const list = Array.isArray(params) ? params : [];

  if (isTypedDataMethod(method)) {
    // Typed data: address is always first, payload second. Fall back to
    // scanning in case a caller flips them.
    const a = list[0];
    const b = list[1];
    if (isLikelyAddress(a)) {
      return { address: a, message: stringifyTypedData(b) };
    }
    if (isLikelyAddress(b)) {
      return { address: b, message: stringifyTypedData(a) };
    }
    return { message: stringifyTypedData(a ?? b) };
  }

  // personal_sign / eth_sign: exactly one of the two args is an address.
  const a = list[0];
  const b = list[1];
  if (isLikelyAddress(a) && typeof b === 'string') {
    return { address: a, message: b };
  }
  if (isLikelyAddress(b) && typeof a === 'string') {
    return { address: b, message: a };
  }
  // No detectable address — treat the first string-ish arg as the message.
  const message = typeof a === 'string' ? a : typeof b === 'string' ? b : '';
  return { message };
}

function stringifyTypedData(value: unknown): string {
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value ?? {});
  } catch {
    return '{}';
  }
}

/**
 * Convert a `personal_sign`/`eth_sign` message param into the bytes to sign.
 * Hex strings are decoded to their raw bytes; anything else is treated as
 * UTF-8 text. This never throws on a non-hex message (unlike `ethers.getBytes`).
 */
export function toSignableBytes(message: string): Uint8Array {
  if (typeof message === 'string' && ethers.isHexString(message)) {
    return ethers.getBytes(message);
  }
  return ethers.toUtf8Bytes(message ?? '');
}

export function decodePersonalSignMessage(message: string): string {
  if (!message) return '';
  if (!ethers.isHexString(message)) return message;
  try {
    return ethers.toUtf8String(message);
  } catch {
    return message;
  }
}

function shortAddress(address: string | undefined): string {
  if (!address) return 'Unknown';
  return `${address.slice(0, 6)}…${address.slice(-4)}`;
}

function transactionCall(data: string): string {
  if (!data || data === '0x') return 'Native transfer';
  const erc20 = new ethers.Interface([
    'function transfer(address to, uint256 amount)',
    'function approve(address spender, uint256 amount)',
    'function transferFrom(address from, address to, uint256 amount)',
  ]);
  try {
    const decoded = erc20.parseTransaction({ data });
    if (!decoded) return `Unknown contract call (${data.slice(0, 10)})`;
    return `${decoded.name}(${decoded.args.map((value) => String(value)).join(', ')})`;
  } catch {
    return `Unknown contract call (${data.slice(0, 10)})`;
  }
}

export async function confirmConnectRequest(
  origin: string,
  options: Pick<DAppConfirmationOptions, 'activeAddress' | 'confirm'>,
): Promise<boolean> {
  const request: DAppConfirmationRequest = {
    kind: 'connect',
    origin,
    title: 'Connect wallet',
    description: 'This site is requesting to view your wallet address and propose transactions.',
    details: [
      { label: 'Site', value: origin },
      { label: 'Wallet', value: shortAddress(options.activeAddress) },
    ],
  };
  return options.confirm(request);
}

export async function confirmDAppRequest(
  method: string,
  params: unknown[] | Record<string, unknown> | undefined,
  options: DAppConfirmationOptions,
): Promise<void> {
  if (!needsDAppConfirmation(method)) return;

  const list = Array.isArray(params) ? params : [];
  let request: DAppConfirmationRequest;

  if (method === 'personal_sign' || method === 'eth_sign') {
    const { address, message } = parseSignRequest(method, params);
    const decoded = decodePersonalSignMessage(message);
    request = {
      kind: method,
      origin: options.origin,
      title: 'Sign message',
      description:
        method === 'eth_sign'
          ? 'This site is requesting a raw signature. Only continue if you fully trust it.'
          : 'Only sign messages from sites you trust.',
      details: [
        { label: 'Site', value: options.origin },
        { label: 'Wallet', value: shortAddress(address || options.activeAddress) },
        { label: 'Message', value: decoded || message },
        ...(decoded !== message ? [{ label: 'Raw message', value: message }] : []),
      ],
    };
  } else if (method === 'eth_sendTransaction') {
    const tx = list[0] as ethers.TransactionRequest | undefined;
    if (!tx || typeof tx !== 'object') {
      throw { code: -32602, message: 'Transaction request is missing.' };
    }
    const provider = new ethers.JsonRpcProvider(getActiveRpcUrl());
    const from =
      typeof tx.from === 'string' ? tx.from : options.activeAddress;
    const to = typeof tx.to === 'string' ? tx.to : undefined;
    const value = tx.value == null ? 0n : BigInt(tx.value);
    const data = tx.data ? String(tx.data) : '0x';
    let gasLimit: bigint;
    let simulation = 'Succeeded';
    const warnings: string[] = [];
    try {
      gasLimit = tx.gasLimit != null
        ? BigInt(tx.gasLimit)
        : await provider.estimateGas({
            ...tx,
            ...(from ? { from } : {}),
          });
    } catch (caught) {
      gasLimit = data === '0x' ? 21_000n : 500_000n;
      warnings.push('Gas estimation failed; the displayed fee uses a conservative limit.');
      simulation =
        caught instanceof Error ? `Failed: ${caught.message}` : 'Failed';
    }
    if (simulation === 'Succeeded') {
      try {
        await provider.call({
          ...tx,
          ...(from ? { from } : {}),
        });
      } catch (caught) {
        simulation =
          caught instanceof Error ? `Failed: ${caught.message}` : 'Failed';
        warnings.push('Simulation failed. This transaction may revert.');
      }
    }
    const fees = await resolveFeeOverrides(provider);
    const nonce = tx.nonce !== undefined
      ? Number(tx.nonce)
      : from
        ? await provider.getTransactionCount(from, 'pending')
        : null;
    const maxNetworkFee = estimateFeeWei(gasLimit, fees);
    const call = transactionCall(data);
    if (/^approve\(.+115792089237316195423570985008687907853269984665640564039457584007913129639935/.test(call)) {
      warnings.push('This is an unlimited token approval.');
    }
    request = {
      kind: 'eth_sendTransaction',
      origin: options.origin,
      title: 'Approve transaction',
      description: 'Review the exact account, destination, amount, call, fee, nonce, and simulation before sending.',
      details: [
        { label: 'Site', value: options.origin },
        { label: 'From', value: from ?? 'Unknown' },
        { label: 'To', value: to ?? 'Contract creation' },
        { label: 'Network', value: `Paxeer Network (${PAXEER_CONFIG.chainId})` },
        { label: 'Value', value: `${ethers.formatEther(value)} PAX` },
        { label: 'Contract call', value: call },
        { label: 'Gas limit', value: gasLimit.toString() },
        { label: 'Maximum network fee', value: `${ethers.formatEther(maxNetworkFee)} PAX` },
        { label: 'Fee mode', value: `${fees.mode} (${fees.source})` },
        { label: 'Nonce', value: nonce === null ? 'Unavailable' : String(nonce) },
        { label: 'Simulation', value: simulation },
        { label: 'Raw data', value: data },
      ],
      warnings,
    };
  } else {
    // eth_signTypedData / _v3 / _v4
    const { address, message } = parseSignRequest(method, params);
    request = {
      kind: method as DAppConfirmationKind,
      origin: options.origin,
      title: 'Sign typed data',
      description: 'Typed-data signatures can authorize account actions.',
      details: [
        { label: 'Site', value: options.origin },
        { label: 'Wallet', value: shortAddress(address || options.activeAddress) },
        { label: 'Payload', value: message },
      ],
    };
  }

  const approved = await options.confirm(request);
  if (!approved) throw { code: 4001, message: 'User rejected the request.' };
}
