/** Tiny formatting helpers — pure, no deps. */

/** Truncate a 0x address: `0xAbCd...e2F1`. */
export function truncateAddress(addr?: string | null, head = 6, tail = 4): string {
  if (!addr) return '';
  if (addr.length <= head + tail + 2) return addr;
  return `${addr.slice(0, head)}…${addr.slice(-tail)}`;
}

/** Convert a wei string/bigint to a fixed-decimal native amount string. */
export function weiToNative(wei: string | bigint | undefined, decimals = 4): string {
  if (wei === undefined || wei === null) return '0';
  const n = typeof wei === 'bigint' ? wei : BigInt(wei);
  const whole = n / 10n ** 18n;
  const frac = n % 10n ** 18n;
  if (frac === 0n) return whole.toString();
  const padded = frac.toString().padStart(18, '0').slice(0, decimals).replace(/0+$/, '');
  return padded.length ? `${whole.toString()}.${padded}` : whole.toString();
}

/** Validate a 0x-prefixed EVM address (case-insensitive, 40 hex chars). */
export function isAddress(s: string): s is `0x${string}` {
  return /^0x[a-fA-F0-9]{40}$/.test(s);
}

/** Convert a decimal native amount (e.g. "0.5") into a wei string. */
export function nativeToWei(input: string): string {
  const trimmed = input.trim();
  if (!/^\d+(\.\d+)?$/.test(trimmed)) throw new Error('Amount must be a decimal number');
  const [whole = '0', frac = ''] = trimmed.split('.');
  const fracPadded = (frac + '0'.repeat(18)).slice(0, 18);
  const wei = BigInt(whole) * 10n ** 18n + BigInt(fracPadded || '0');
  return wei.toString();
}
