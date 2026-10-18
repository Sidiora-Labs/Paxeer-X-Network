import { Connection, type ConnectionConfig } from "@solana/web3.js";
import {
  type Account,
  SimpleDI,
  type MessageFactor,
} from "@paxeer-network/core";
import {
  SOLANA_MAINNET_CHAINID,
  SOLANA_TESTNET_CHAINID,
  SolanaDevnetChainInfo,
  type API,
} from "@paxeer-network/types";
import { getTimestamp } from "@paxeer-network/utils";

const SAMPLE_LIMIT = 60;
const SOLANA_RPC_PROXY_PATH = "/v1/solana-rpc-proxy";
/** RPC HTTP fetch timeout. */
const RPC_TIMEOUT_MS = 8_000;

function averageBlockTimeFromSamples(
  samples: Awaited<ReturnType<Connection["getRecentPerformanceSamples"]>>,
): number {
  let totalBlockTime = 0;
  let validSamples = 0;

  for (const sample of samples) {
    if (sample.numSlots > 0 && sample.samplePeriodSecs > 0) {
      totalBlockTime += sample.samplePeriodSecs / sample.numSlots;
      validSamples++;
    }
  }

  if (validSamples === 0) {
    return 0;
  }

  return totalBlockTime / validSamples;
}

async function fetchBlockTime(connection: Connection): Promise<number> {
  // The Solana RPC node keeps a history of performance samples, typically for
  // a few hours. A limit of 60 samples averages roughly the last hour.
  const samples = await connection.getRecentPerformanceSamples(SAMPLE_LIMIT);
  return averageBlockTimeFromSamples(samples);
}

function createConnection(
  url: string,
  options: ConnectionConfig = {},
): Connection {
  return new Connection(url, {
    commitment: "confirmed",
    // Avoid ~15s backoff on 429; fail fast and let the caller return 0.
    disableRetryOnRateLimit: true,
    fetch: (input, init) =>
      fetch(input, { ...init, signal: AbortSignal.timeout(RPC_TIMEOUT_MS) }),
    ...options,
  });
}

function createPaxeerNetworkProxyConnection(): Connection | null {
  const account = SimpleDI.get<Account>("account");
  if (!account?.apiBaseUrl || !account.accountId) {
    return null;
  }
  if (!account.keyStore?.getPaxeerNetworkKey()) {
    return null;
  }

  return createConnection(`${account.apiBaseUrl}${SOLANA_RPC_PROXY_PATH}`, {
    // web3.js only wraps fetchMiddleware in a sync try/catch and treats the
    // 3rd arg as a `next` callback (not fetch). An async throw would hang the
    // request forever, so always invoke `next` and never return a rejected promise.
    fetchMiddleware: (info, init, next) => {
      void (async () => {
        try {
          const payload: MessageFactor = {
            url: SOLANA_RPC_PROXY_PATH,
            method: (init?.method as MessageFactor["method"]) ?? "POST",
            data: JSON.parse((init?.body as string) || "{}"),
          };
          const signature = await account.signer.sign(payload, getTimestamp());
          next(info, {
            ...init,
            headers: {
              ...(init?.headers as Record<string, string>),
              ...signature,
              "paxeer-network-account-id": account.accountId!,
            },
          });
        } catch (error) {
          console.error("solana-rpc-proxy sign failed", error);
          // Proceed unsigned so the server can reject (e.g. 401) instead of hanging.
          next(info, init);
        }
      })();
    },
  });
}

/**
 * Average Solana slot time from recent performance samples. Mainnet routes
 * through paxeer-network `/v1/solana-rpc-proxy` (requires Account + paxeer-network key);
 * devnet connects directly to the chain public RPC.
 */
export async function getSolanaBlockTime(chain: API.Chain) {
  // Some data sources may provide chain_id as a string; normalize before compare.
  const chainId = Number(chain.network_infos.chain_id);

  try {
    if (chainId === SOLANA_MAINNET_CHAINID) {
      const connection = createPaxeerNetworkProxyConnection();
      if (!connection) {
        return 0;
      }
      return await fetchBlockTime(connection);
    }

    if (chainId === SOLANA_TESTNET_CHAINID) {
      // Devnet has no paxeer-network proxy; the public RPC needs no auth.
      const rpcUrl =
        chain.network_infos.public_rpc_url ||
        SolanaDevnetChainInfo.public_rpc_url;
      return await fetchBlockTime(createConnection(rpcUrl));
    }

    return 0;
  } catch (error) {
    console.error("getSolanaBlockTime failed", chainId, error);
    return 0;
  }
}
