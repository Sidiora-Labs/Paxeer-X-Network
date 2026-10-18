import { API } from "@paxeer-network/types";

// Chain-info rows are loosely typed at the store boundary; validation works
// on the raw wire shape before the historical `API.Chain` cast.
type ChainInfoRow = Record<string, unknown>;

// Hard requirements: these fields are consumed by wallet switching, deposit
// (vault_address is the on-chain call target) and number formatting — a row
// missing any of them cannot be used safely. `explorer_base_url` is display
// only (ecosystem chains legitimately ship without an explorer) and is not
// required.
const requiredStringFields = [
  "name",
  "public_rpc_url",
  "currency_symbol",
  "vault_address",
] as const;

/**
 * `currency_decimal` may arrive as a number or a numeric string; other shapes
 * (empty string, null, boolean, NaN, negative) cannot be used downstream.
 */
const toCurrencyDecimal = (raw: unknown): number | null => {
  if (typeof raw === "number") {
    return Number.isFinite(raw) && raw >= 0 ? raw : null;
  }
  if (typeof raw === "string") {
    const trimmed = raw.trim();
    if (trimmed.length === 0) {
      return null;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
  }
  return null;
};

const sanitizeChain = (item: unknown): ChainInfoRow | null => {
  if (item === null || typeof item !== "object") {
    return null;
  }

  const chain = item as ChainInfoRow;
  const chainId = Number(chain.chain_id);
  if (!Number.isFinite(chainId) || chainId <= 0) {
    return null;
  }

  if (
    requiredStringFields.some(
      (field) =>
        typeof chain[field] !== "string" ||
        (chain[field] as string).trim().length === 0,
    )
  ) {
    return null;
  }

  const currencyDecimal = toCurrencyDecimal(chain.currency_decimal);
  if (currencyDecimal === null) {
    return null;
  }

  if (currencyDecimal === chain.currency_decimal) {
    return chain;
  }

  return { ...chain, currency_decimal: currencyDecimal };
};

/**
 * Drops chain rows that miss the runtime shape required by chain consumers
 * and wallet providers, keeping the valid ones. Returns null only when no row
 * survives. The chain mix is not constrained: a broker may legitimately expose
 * Solana-only (or EVM-only) chains, and wallet connectors derive their
 * supported wallet types from whatever survives.
 */
export const sanitizeChainInfoData = (data: unknown): API.Chain[] | null => {
  if (!Array.isArray(data) || data.length === 0) {
    return null;
  }

  const chains: ChainInfoRow[] = [];
  for (const item of data) {
    const chain = sanitizeChain(item);
    if (chain !== null) {
      chains.push(chain);
    }
  }

  return chains.length > 0 ? (chains as unknown as API.Chain[]) : null;
};
