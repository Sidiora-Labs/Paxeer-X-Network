import { API } from "@paxeer-network/types";
import { isSolana } from "@paxeer-network/utils";
import { getEvmBlockTime } from "./getEvmBlockTime";
import { getSolanaBlockTime } from "./getSolanaBlockTime";

// https://tokenterminal.com/explorer/metrics/block-time
export async function getBlockTime(inputs: {
  chainId: number | string;
  chain: API.Chain;
}) {
  const { chain } = inputs;
  // Normalize before routing: some data sources provide chain_id as a string,
  // which `isSolana` would fail to match.
  const chainId = Number(inputs.chainId);
  if (isSolana(chainId)) {
    return getSolanaBlockTime(chain);
  }
  return getEvmBlockTime(chain);
}
