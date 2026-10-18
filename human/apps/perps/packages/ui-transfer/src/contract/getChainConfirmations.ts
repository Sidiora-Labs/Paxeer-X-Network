import { ethers } from "ethers";
import { API } from "@paxeer-network/types";
import { isSolana } from "@paxeer-network/utils";
import endpointV2Abi from "./abi/endpointV2Abi.json";
import getAppUlnConfigAbi from "./abi/getAppUlnConfigAbi.json";
import { getEndpointId } from "./endpointId";
import {
  getPaxeerNetworkNetworkKey,
  PAXEER_NETWORK_ENDPOINT_V2,
  PAXEER_NETWORK_RELAY_OAPP,
  PAXEER_NETWORK_RPC_URL,
  PAXEER_NETWORK_SOL_PEER_OAPP,
  type PaxeerNetworkNetworkKey,
} from "./layerzeroConfig";

/**
 * Required confirmations for a deposit pathway (paxeer-network Receive ULN).
 * Reads the real RelayV2 / Sol peer OApp config. When the OApp config is
 * unset (`confirmations = 0`), falls back to the default ULN config
 * (`oapp = 0x0`) on the same receive library resolved from the endpoint
 * (see the fallback comment below for details).
 */
export async function getChainConfirmations(chain: API.Chain) {
  const chainId = chain.network_infos.chain_id;
  const isMainnet = chain.network_infos.mainnet;
  const networkKey = getPaxeerNetworkNetworkKey(isMainnet);
  const sourceEid = getEndpointId(chainId);

  if (!sourceEid) {
    return 0;
  }

  const oapp = resolveDepositOApp(chainId, networkKey);
  const provider = new ethers.JsonRpcProvider(PAXEER_NETWORK_RPC_URL[networkKey]);

  try {
    // Resolve the actually-effective receive library instead of hardcoding
    // the ULN address, so the fallback below always reads the live ULN.
    const endpoint = new ethers.Contract(
      PAXEER_NETWORK_ENDPOINT_V2[networkKey],
      endpointV2Abi,
      provider,
    );
    const [receiveLibAddress] = await endpoint.getReceiveLibrary(
      oapp,
      sourceEid,
    );
    if (!receiveLibAddress || receiveLibAddress === ethers.ZeroAddress) {
      return 0;
    }

    const receiveLib = new ethers.Contract(
      receiveLibAddress,
      getAppUlnConfigAbi,
      provider,
    );
    const config = await receiveLib.getAppUlnConfig(oapp, sourceEid);
    const confirmations = Number(config.confirmations || 0);
    if (confirmations > 0) {
      return confirmations;
    }

    // Fallback to the default ULN config (oapp = 0x0) when the OApp config is
    // unset. On paxeer-network testnet the RelayV2 OApp has no per-chain ULN config
    // on-chain (getAppUlnConfig returns 0 for every chain), and on mainnet
    // some chains are unset too (verified on-chain: Gnosis/Blast/Linea/
    // HyperEVM return 0 for the RelayV2 OApp while the default config holds
    // the real values). Without this fallback the deposit status UI hides
    // the estimated time entirely (a zero confirmations count yields no
    // estimate).
    const defaultConfig = await receiveLib.getAppUlnConfig(
      ethers.ZeroAddress,
      sourceEid,
    );
    return Number(defaultConfig.confirmations || 0);
  } catch (error) {
    console.error("getChainConfirmations error", error);
    return 0;
  }
}

function resolveDepositOApp(
  chainId: number,
  networkKey: PaxeerNetworkNetworkKey,
): string {
  if (isSolana(chainId)) {
    return PAXEER_NETWORK_SOL_PEER_OAPP[networkKey];
  }

  return PAXEER_NETWORK_RELAY_OAPP[networkKey];
}
