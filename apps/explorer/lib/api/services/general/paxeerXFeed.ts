import type { ApiResource } from '../../types';
import type { FeedMarketsResponse, MarketUpdatesResponse, PriceUpdateItem } from 'types/api/paxeerXFeed';

export const GENERAL_API_PAXEER_X_FEED_RESOURCES = {
  paxeer_x_feed_markets: {
    path: '/api/v2/paxeer-x/feed/markets',
  },
  paxeer_x_feed_update: {
    path: '/api/v2/paxeer-x/feed/updates/:hash',
    pathParams: [ 'hash' as const ],
  },
  paxeer_x_feed_market_updates: {
    path: '/api/v2/paxeer-x/feed/markets/:id/updates',
    pathParams: [ 'id' as const ],
    filterFields: [],
    paginated: true,
  },
} satisfies Record<string, ApiResource>;

export type GeneralApiPaxeerXFeedResourceName = `general:${ keyof typeof GENERAL_API_PAXEER_X_FEED_RESOURCES }`;

export type GeneralApiPaxeerXFeedResourcePayload<R extends GeneralApiPaxeerXFeedResourceName> =
  R extends 'general:paxeer_x_feed_markets' ? FeedMarketsResponse :
    R extends 'general:paxeer_x_feed_update' ? PriceUpdateItem :
      R extends 'general:paxeer_x_feed_market_updates' ? MarketUpdatesResponse :
        never;
