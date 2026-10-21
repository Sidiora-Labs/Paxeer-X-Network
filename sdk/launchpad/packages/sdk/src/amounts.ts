import { decodeContract } from './validate.js';
declare const brand: unique symbol;
export type DecimalString = string & { readonly [brand]: 'decimal' };
export type Sequence = string & { readonly [brand]: 'sequence' };
export type CanonicalAddress = string & { readonly [brand]: 'address' };
export type Pax18 = DecimalString & { readonly asset: 'PAX18' };
export type Usdl6 = DecimalString & { readonly asset: 'USDL6' };
export function decimalString(value: unknown): DecimalString {
  return decodeContract('UInt256', value) as DecimalString;
}
export function sequence(value: unknown): Sequence {
  return decodeContract('UInt63', value) as Sequence;
}
export function canonicalAddress(value: unknown): CanonicalAddress {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(value)) throw new TypeError('Invalid chain125 address');
  return value.toLowerCase() as CanonicalAddress;
}
export const pax18 = (value: unknown): Pax18 => decimalString(value) as Pax18;
export const usdl6 = (value: unknown): Usdl6 => decimalString(value) as Usdl6;
export const POOL_CREATION_FEE = usdl6('1000000');
export type RoundingMode = 'down' | 'up' | 'half-up';
/** Display only. Never converts an authoritative value through Number. */
export function formatRaw(value: DecimalString, decimals: number, places: number, rounding: RoundingMode): string {
  decimalString(value);
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 255 || !Number.isInteger(places) || places < 0 || places > 255) throw new RangeError('Invalid display precision');
  if (!['down','up','half-up'].includes(rounding)) throw new TypeError('Explicit rounding mode required');
  let n = BigInt(value);
  if (decimals > places) {
    const divisor = 10n ** BigInt(decimals - places), remainder = n % divisor;
    n /= divisor;
    if ((rounding === 'up' && remainder > 0n) || (rounding === 'half-up' && remainder * 2n >= divisor)) n++;
  } else n *= 10n ** BigInt(places - decimals);
  const padded = n.toString().padStart(places + 1, '0');
  return places ? padded.slice(0,-places) + '.' + padded.slice(-places) : padded;
}

export type DisplayPriceProvenance = import('@kindle/sdk-v2/types').ContractTypes['PriceSource'];
export interface DisplayAmount { readonly formatted: string; readonly raw: DecimalString; readonly provenance?: DisplayPriceProvenance }
export function displayAmount(raw: DecimalString, decimals: number, places: number, rounding: RoundingMode, provenance?: DisplayPriceProvenance): DisplayAmount { return Object.freeze({ formatted: formatRaw(raw,decimals,places,rounding), raw, ...(provenance ? { provenance: Object.freeze({...provenance}) } : {}) }); }
