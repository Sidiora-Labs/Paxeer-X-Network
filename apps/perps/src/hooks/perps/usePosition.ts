import type { Address } from "viem";

import { makeError } from "@/core/errors/classify";
import { PerpsPrecompileABI } from "@/core/network/abi/PerpsPrecompile";
import { PRECOMPILES } from "@/core/network/precompiles";
import {
  buildGetPosition,
  buildGetPositionDetails,
  buildGetUserPositionIds,
  type OpenPositionInput,
} from "@/core/perps/calls";
import { COLLATERAL_TOKENS, erc20Abi } from "@/core/perps/config";
import {
  planAddSize,
  planClose,
  planCollateral,
  planOpen,
  planPartialClose,
  positionFromTuple,
  type AllowanceSource,
  type PositionRecord,
  type PositionTuple,
  type WritePlan,
} from "@/core/perps/positionWrites";
import { getConnectedAddress } from "@/core/viem/clients";
import { SPOT_NATIVE_TOKENS } from "@/hooks/spot/useSpotApprovals";
import { getAllowance } from "./useAccount";
import { readContract, writeContract, waitForTx, type WaitForTxResult } from "./useContract";
import { useQuoter } from "./useQuoter";

const USDL: Address = COLLATERAL_TOKENS.usdl.address;

function connectedAccount(): Address {
  const account = getConnectedAddress();
  if (!account) throw makeError("WALLET_NOT_CONNECTED", "perps.position");
  return account;
}

/** The USDL allowance of the connected account to 0x1015, the planners' approval step. */
export function perpsAllowanceSource(): AllowanceSource {
  const account = connectedAccount();
  return { allowance: () => getAllowance(account), native: SPOT_NATIVE_TOKENS };
}

/** Send each step of a plan in order and wait for its receipt; returns the receipt of the last call. */
export async function sendPlan(plan: WritePlan): Promise<WaitForTxResult> {
  let receipt: WaitForTxResult | undefined;
  for (const step of plan) {
    const hash =
      step.functionName === "approve" ? await writeContract({ ...step, abi: erc20Abi }) : await writeContract(step);
    receipt = await waitForTx(hash);
  }
  if (!receipt) throw new Error("perpetuals plan sent no transaction");
  return receipt;
}

export function usePosition() {
  const quoter = useQuoter();

  async function getPosition(positionId: bigint): Promise<PositionTuple> {
    return (await readContract(buildGetPosition(positionId))) as PositionTuple;
  }

  async function getPositionRecord(positionId: bigint): Promise<PositionRecord> {
    return positionFromTuple(await getPosition(positionId));
  }

  /** Take-profit, stop-loss and liquidation price of a position. */
  async function getPositionDetails(positionId: bigint) {
    return readContract(buildGetPositionDetails(positionId));
  }

  async function getUserPositionIds(account: Address): Promise<readonly bigint[]> {
    return (await readContract(buildGetUserPositionIds(account))) as readonly bigint[];
  }

  async function getOpenInterest(marketId: bigint) {
    return readContract({
      address: PRECOMPILES.perps,
      abi: PerpsPrecompileABI,
      functionName: "getOpenInterest",
      args: [marketId],
    });
  }

  async function getPositionCollateralToken(positionId: bigint): Promise<Address> {
    return (await getPositionRecord(positionId)).collateralToken;
  }

  async function openPosition(input: OpenPositionInput) {
    return sendPlan(await planOpen(input, perpsAllowanceSource()));
  }

  /** Close the whole position at the quoteClosePosition exit price bound. */
  async function closePosition(positionId: bigint) {
    const position = await getPositionRecord(positionId);
    const { exitPrice } = await quoter.quoteClosePosition(positionId);
    return sendPlan(planClose({ positionId, isLong: position.isLong, sizeUsd: position.sizeUsd, exitPrice }));
  }

  /** Close `closeSizeUsd` (18 decimals) at the quotePartialClose exit price bound. */
  async function partialClose(positionId: bigint, closeSizeUsd: bigint) {
    const position = await getPositionRecord(positionId);
    const { exitPrice } = await quoter.quotePartialClose(positionId, closeSizeUsd);
    return sendPlan(planPartialClose({ positionId, isLong: position.isLong, closeSizeUsd, exitPrice }));
  }

  /** `amount` in USDL base units. */
  async function addCollateral(positionId: bigint, amount: bigint) {
    return sendPlan(await planCollateral({ action: "add", positionId, amount }, perpsAllowanceSource()));
  }

  /** `amount` in USDL base units. */
  async function removeCollateral(positionId: bigint, amount: bigint) {
    return sendPlan(await planCollateral({ action: "remove", positionId, amount }, perpsAllowanceSource()));
  }

  /** Grow the position on its market and side; `additionalCollateral` in USDL base units, leverage at 18 decimals. */
  async function addSize(positionId: bigint, additionalCollateral: bigint, leverage: bigint) {
    const source = perpsAllowanceSource();
    const position = await getPositionRecord(positionId);
    const quote = await quoter.quoteOpenPosition({
      marketId: position.marketId,
      collateralToken: USDL,
      collateralAmount: additionalCollateral,
      leverage,
      isLong: position.isLong,
    });
    return sendPlan(
      await planAddSize(
        { position: { marketId: position.marketId, isLong: position.isLong }, additionalCollateral, leverage, quote },
        source,
      ),
    );
  }

  return {
    getPosition,
    getPositionRecord,
    getPositionDetails,
    getUserPositionIds,
    getOpenInterest,
    getPositionCollateralToken,
    openPosition,
    closePosition,
    partialClose,
    addCollateral,
    removeCollateral,
    addSize,
  };
}
