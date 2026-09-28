import { QueryClient } from '@tanstack/react-query';
import React from 'react';

import getErrorObjPayload from 'lib/errors/getErrorObjPayload';
import getErrorObjStatusCode from 'lib/errors/getErrorObjStatusCode';

import type { ResourceName } from './resources';

const SECOND = 1_000;

// How long an answer stays fresh, by what its resource describes. A resource outside these classes
// keeps the library's own default of zero, so every component that mounts asks for it again.
export const STALE_TIME = {
  // What the chain has already written - a transaction, a block, a receipt, a trace - does not change
  // while a tab is open, so the second reader of it gets the answer the first one already has.
  record: 30 * SECOND,
  // What the backend rewrites when it is deployed.
  config: 5 * 60 * SECOND,
  // The network statistics and the counters beside them, which the backend recomputes on its own cadence.
  stats: 15 * SECOND,
  // What follows the chain head, where a page shows what the socket has not delivered yet.
  head: 5 * SECOND,
} as const;

const RESOURCE_STALE_TIME: Partial<Record<ResourceName, number>> = {
  'general:tx': STALE_TIME.record,
  'general:tx_raw_trace': STALE_TIME.record,
  'general:tx_state_changes': STALE_TIME.record,
  'general:tx_interpretation': STALE_TIME.record,
  'general:block': STALE_TIME.record,
  'general:token_instance': STALE_TIME.record,
  'general:paxeer_x_receipt': STALE_TIME.record,

  'general:config_backend': STALE_TIME.config,
  'general:config_backend_version': STALE_TIME.config,
  'general:config_csv_export': STALE_TIME.config,
  'general:config_contract_languages': STALE_TIME.config,

  'general:stats': STALE_TIME.stats,
  'general:txs_stats': STALE_TIME.stats,
  'general:stats_charts_txs': STALE_TIME.stats,
  'general:stats_charts_market': STALE_TIME.stats,
  'general:address_counters': STALE_TIME.stats,
  'general:address_tabs_counters': STALE_TIME.stats,
  'general:token_counters': STALE_TIME.stats,
  'general:verified_contracts_counters': STALE_TIME.stats,
  'stats:counters': STALE_TIME.stats,
  'stats:lines': STALE_TIME.stats,
  'stats:pages_main': STALE_TIME.stats,
  'stats:pages_transactions': STALE_TIME.stats,
  'stats:pages_contracts': STALE_TIME.stats,

  'general:address': STALE_TIME.head,
  'general:token': STALE_TIME.head,
  'general:homepage_blocks': STALE_TIME.head,
  'general:homepage_txs': STALE_TIME.head,
  'general:homepage_indexing_status': STALE_TIME.head,
  'general:paxeer_x_unified_account': STALE_TIME.head,
};

export function getResourceStaleTime(resource: ResourceName) {
  return RESOURCE_STALE_TIME[resource];
}

export const retry = (failureCount: number, error: unknown) => {
  const errorPayload = getErrorObjPayload<{ status: number }>(error);
  const status = errorPayload?.status || getErrorObjStatusCode(error);
  if (status && status >= 400 && status < 500) {
    // don't do retry for client error responses
    return false;
  }
  return failureCount < 2;
};

export default function useQueryClientConfig() {
  const [ queryClient ] = React.useState(() => new QueryClient({
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        retry,
        throwOnError: (error, query) => {
          const status = getErrorObjStatusCode(error);

          // we don't catch error only for "Too many requests" response
          if (status !== 429) {
            return false;
          }

          const EXTERNAL_API_RESOURCES: Array<ResourceName> = [
            'general:contract_solidity_scan_report',
            'general:address_xstar_score',
            'general:address_3rd_party_info',
            'general:noves_transaction',
            'general:noves_address_history',
            'general:noves_describe_txs',
            // these resources are not proxied by the backend
            'external:safe_transaction_api',
          ];
          const isExternalApiResource = EXTERNAL_API_RESOURCES.some((resource) => query.queryKey[0] === resource);

          return !isExternalApiResource;
        },
      },
    },
  }));

  return queryClient;
}
