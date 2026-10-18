import { ethers } from "ethers";
import type { API } from "@paxeer-network/types";

// A wider span smooths out irregular block intervals (e.g. batch-y ZK chains
// where most blocks are seconds apart but occasional gaps exceed 150s).
const blockSpan = 25;
const maxAttempts = 2;

export async function getEvmBlockTime(chain: API.Chain) {
  const provider = new ethers.JsonRpcProvider(
    chain.network_infos.public_rpc_url,
  );

  let lastError: unknown;
  try {
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      try {
        // Two samples blockSpan apart give a stable average with only 2 RPC
        // calls, instead of one call per block.
        const head = await provider.getBlock("latest");
        if (head?.timestamp == null) {
          continue;
        }

        const tail = await provider.getBlock(head.number - blockSpan);
        if (tail?.timestamp == null) {
          continue;
        }

        const elapsed = Number(head.timestamp) - Number(tail.timestamp);
        if (elapsed > 0) {
          return elapsed / blockSpan;
        }
      } catch (error) {
        lastError = error;
      }
    }
    console.error(
      "getEvmBlockTime failed",
      chain.network_infos.chain_id,
      lastError ?? "no usable block timestamps",
    );
    return 0;
  } finally {
    provider.destroy();
  }
}
