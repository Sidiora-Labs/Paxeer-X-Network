import type { FeedMarketsResponse, PriceUpdateItem } from 'types/api/paxeerXFeed';

// A failed submission moves no price; a market takes an observation only when it is newer than the one
// it shows, so a late or repeated message never rolls a row back.
export default function applyPriceUpdates(prevData: FeedMarketsResponse, updates: Array<PriceUpdateItem>): FeedMarketsResponse {
  const accepted = updates.filter((update) => update.status === 'ok');

  if (accepted.length === 0) {
    return prevData;
  }

  let isChanged = false;

  const items = prevData.items.map((market) => {
    let latest = market.latest;

    accepted.forEach((update) => {
      const observation = update.price_update.observations.find((item) => item.market_id === market.market_id);

      if (!observation) {
        return;
      }

      const isNewer = !latest ||
        update.block_number > latest.block_number ||
        (update.block_number === latest.block_number && observation.sequence > latest.sequence);

      if (isNewer) {
        latest = {
          index_price: observation.index_price,
          mark_price: observation.mark_price,
          source_timestamp_ms: observation.source_timestamp_ms,
          sequence: observation.sequence,
          update_hash: update.hash,
          block_number: update.block_number,
          timestamp: update.timestamp,
        };
      }
    });

    if (latest === market.latest) {
      return market;
    }

    isChanged = true;
    return { ...market, latest };
  });

  const chainHead = Math.max(prevData.chain_head, ...accepted.map((update) => update.block_number));

  if (!isChanged && chainHead === prevData.chain_head) {
    return prevData;
  }

  return { ...prevData, items, chain_head: chainHead };
}
