/**
 * @deprecated (3.1) Use backend stats endpoints instead:
 *   - GET /stats/:poolAddress/holders  for holder data
 *   - GET /stats/:poolAddress          for pre-computed holder stats (top10Concentration, creatorHoldingsPct)
 *
 * This client fetches directly from the Paxscan block explorer, which is
 * redundant (the stats service already tracks holders) and has an incorrect
 * supply calculation (sums only the first page of holders).
 */
const PAXSCAN_API = 'https://api.paxscan.io/api/v2';
const TOKEN_DECIMALS = 6;

export interface PaxscanHolder {
  address: string;
  isContract: boolean;
  balance: string;       // raw (6 dec)
  balanceFormatted: number;
  pctOfSupply: number;   // 0–100
}

export interface PaxscanCounters {
  holderCount: number;
  transferCount: number;
}

export interface PaxscanHolderResponse {
  holders: PaxscanHolder[];
  totalHolders: number;
  nextPageParams: Record<string, string> | null;
}

export interface DerivedHolderStats {
  holderCount: number;
  top10Concentration: number;  // 0–100
  creatorHoldingsPct: number;  // 0–100
}

function rawToFloat(raw: string): number {
  if (!raw || raw === '0') return 0;
  if (raw.length <= TOKEN_DECIMALS) return Number('0.' + raw.padStart(TOKEN_DECIMALS, '0'));
  const intPart = raw.slice(0, raw.length - TOKEN_DECIMALS);
  const fracPart = raw.slice(raw.length - TOKEN_DECIMALS);
  return Number(intPart + '.' + fracPart);
}

function computeSupplyPercent(balance: number, totalSupply: number): number {
  if (totalSupply <= 0) return 0;
  return (balance / totalSupply) * 100;
}

export async function fetchTokenCounters(tokenAddress: string): Promise<PaxscanCounters> {
  const res = await fetch(`${PAXSCAN_API}/tokens/${tokenAddress}/counters`, {
    headers: { accept: 'application/json' },
  });
  if (!res.ok) return { holderCount: 0, transferCount: 0 };
  const data = await res.json();
  return {
    holderCount: parseInt(data.token_holders_count ?? '0', 10),
    transferCount: parseInt(data.transfers_count ?? '0', 10),
  };
}

export async function fetchTokenHolders(
  tokenAddress: string,
  itemsCount = 50,
  pageParams?: Record<string, string>,
): Promise<PaxscanHolderResponse> {
  let url = `${PAXSCAN_API}/tokens/${tokenAddress}/holders?items_count=${itemsCount}`;
  if (pageParams) {
    const qs = new URLSearchParams(pageParams).toString();
    if (qs) url += `&${qs}`;
  }
  const res = await fetch(url, {
    headers: { accept: 'application/json' },
  });
  if (!res.ok) return { holders: [], totalHolders: 0, nextPageParams: null };
  const data = await res.json();

  const items: Array<{ address: { hash: string; is_contract: boolean }; value: string }> =
    data.items ?? [];

  // Compute total supply from all returned holders (best-effort; full precision requires on-chain call)
  const balances = items.map((i) => rawToFloat(i.value));
  const totalSupply = balances.reduce((sum, b) => sum + b, 0);

  const holders: PaxscanHolder[] = items.map((item, idx) => {
    const bal = balances[idx];
    return {
      address: item.address.hash,
      isContract: item.address.is_contract ?? false,
      balance: item.value,
      balanceFormatted: bal,
      pctOfSupply: computeSupplyPercent(bal, totalSupply),
    };
  });

  return {
    holders,
    totalHolders: holders.length,
    nextPageParams: data.next_page_params ?? null,
  };
}

export async function fetchDerivedHolderStats(
  tokenAddress: string,
  creatorAddress?: string,
): Promise<DerivedHolderStats> {
  const [counters, holdersRes] = await Promise.all([
    fetchTokenCounters(tokenAddress),
    fetchTokenHolders(tokenAddress, 50),
  ]);

  const holders = holdersRes.holders;
  const totalSupply = holders.reduce((s, h) => s + h.balanceFormatted, 0);

  // Top 10 concentration
  const top10 = holders
    .slice(0, 10)
    .reduce((s, h) => s + h.balanceFormatted, 0);
  const top10Pct = totalSupply > 0 ? (top10 / totalSupply) * 100 : 0;

  // Creator holdings
  let creatorPct = 0;
  if (creatorAddress) {
    const creatorHolder = holders.find(
      (h) => h.address.toLowerCase() === creatorAddress.toLowerCase(),
    );
    if (creatorHolder && totalSupply > 0) {
      creatorPct = (creatorHolder.balanceFormatted / totalSupply) * 100;
    }
  }

  return {
    holderCount: counters.holderCount,
    top10Concentration: top10Pct,
    creatorHoldingsPct: creatorPct,
  };
}
