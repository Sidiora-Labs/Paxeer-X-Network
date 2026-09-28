/**
 * Strict EIP-681 payment-request parser.
 *
 * Native transfer:
 *   ethereum:<recipient>@<chainId>?value=<wei>
 *
 * ERC-20 transfer:
 *   ethereum:<tokenContract>@<chainId>/transfer?address=<recipient>&uint256=<atomicAmount>
 */

export interface Eip681Params {
  address: string;
  chainId?: number;
  tokenAddress?: string;
  functionName?: 'transfer';
  amount?: string;
  value?: string;
  uint256?: string;
  gas?: string;
  gasPrice?: string;
}

const ADDRESS_RE = /^0x[a-fA-F0-9]{40}$/;
const INTEGER_RE = /^(?:0|[1-9]\d*)$/;
const NUMBER_RE = /^(?:0|[1-9]\d*)(?:\.\d+)?(?:e\+?\d+)?$/i;

function address(value: string | null): string | null {
  if (!value || !ADDRESS_RE.test(value)) return null;
  return value.toLowerCase();
}

function boundedInteger(value: string | null): string | null {
  if (!value || value.length > 100 || !INTEGER_RE.test(value)) return null;
  return value;
}

function boundedNumber(value: string | null): string | null {
  if (!value || value.length > 100 || !NUMBER_RE.test(value)) return null;
  return value;
}

export function parseAtomicAmount(value: string): bigint | null {
  if (INTEGER_RE.test(value)) return BigInt(value);
  const match = value.match(/^(\d+)(?:\.(\d+))?e\+?(\d+)$/i);
  if (!match) return null;
  const whole = match[1];
  const fraction = match[2] ?? '';
  const exponent = Number.parseInt(match[3], 10);
  if (!Number.isSafeInteger(exponent) || exponent > 100) return null;
  if (fraction.length > exponent) return null;
  return BigInt(`${whole}${fraction}${'0'.repeat(exponent - fraction.length)}`);
}

export function parseEip681(raw: string): Eip681Params | null {
  if (typeof raw !== 'string') return null;
  const trimmed = raw.trim();
  const plainAddress = address(trimmed);
  if (plainAddress) return { address: plainAddress };
  if (!/^ethereum:/i.test(trimmed)) return null;

  let body = trimmed.slice(trimmed.indexOf(':') + 1);
  if (body.toLowerCase().startsWith('pay-')) body = body.slice(4);

  const queryIndex = body.indexOf('?');
  const beforeQuery = queryIndex >= 0 ? body.slice(0, queryIndex) : body;
  const query = queryIndex >= 0 ? body.slice(queryIndex + 1) : '';
  const slashIndex = beforeQuery.indexOf('/');
  const targetWithChain =
    slashIndex >= 0 ? beforeQuery.slice(0, slashIndex) : beforeQuery;
  const functionName =
    slashIndex >= 0 ? beforeQuery.slice(slashIndex + 1) : '';
  const atIndex = targetWithChain.indexOf('@');
  const targetText =
    atIndex >= 0 ? targetWithChain.slice(0, atIndex) : targetWithChain;
  const target = address(targetText);
  if (!target) return null;

  let chainId: number | undefined;
  if (atIndex >= 0) {
    const rawChain = targetWithChain.slice(atIndex + 1);
    if (!INTEGER_RE.test(rawChain)) return null;
    const parsed = Number(rawChain);
    if (!Number.isSafeInteger(parsed) || parsed <= 0) return null;
    chainId = parsed;
  }

  const params = new URLSearchParams(query);
  const gas = boundedInteger(params.get('gas'));
  const gasPrice = boundedInteger(params.get('gasPrice'));
  if ((params.has('gas') && !gas) || (params.has('gasPrice') && !gasPrice)) {
    return null;
  }

  if (functionName) {
    if (functionName !== 'transfer') return null;
    const recipient = address(params.get('address'));
    const uint256 = boundedNumber(params.get('uint256'));
    if (!recipient || !uint256) return null;
    return {
      address: recipient,
      tokenAddress: target,
      functionName: 'transfer',
      ...(chainId ? { chainId } : {}),
      uint256,
      ...(gas ? { gas } : {}),
      ...(gasPrice ? { gasPrice } : {}),
    };
  }

  const value = boundedNumber(params.get('value'));
  const amount = boundedNumber(params.get('amount'));
  if (params.has('value') && !value) return null;
  if (params.has('amount') && !amount) return null;
  return {
    address: target,
    ...(chainId ? { chainId } : {}),
    ...(value ? { value } : {}),
    ...(amount ? { amount } : {}),
    ...(gas ? { gas } : {}),
    ...(gasPrice ? { gasPrice } : {}),
  };
}

export function extractAddress(data: string): string | null {
  return parseEip681(data)?.address ?? null;
}
