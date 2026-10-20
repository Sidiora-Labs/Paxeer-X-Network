import { getAbiItem, type Address, type ContractFunctionReturnType } from "viem";

import { PerpsPrecompileABI } from "@/core/network/abi/PerpsPrecompile";
import { PERPS_SPENDER, planApproval, type ApproveCall, type NativeTokens } from "@/core/network/approvals";
import {
  buildAddCollateral,
  buildAddSize,
  buildClosePosition,
  buildOpenPosition,
  buildPartialClose,
  buildRemoveCollateral,
  type AddSizeInput,
  type ClosePositionInput,
  type OpenPositionInput,
  type PartialCloseInput,
} from "./calls";
import { COLLATERAL_TOKENS } from "./config";

const USDL: Address = COLLATERAL_TOKENS.usdl.address;

/** Where a planner reads the USDL allowance to 0x1015 and which tokens move by bank transfer. */
export interface AllowanceSource {
  allowance: () => Promise<bigint>;
  native: NativeTokens;
}

export type PlaceOrderCall = ReturnType<typeof buildOpenPosition>;
export type CloseCall = ReturnType<typeof buildClosePosition>;
export type CollateralCall = ReturnType<typeof buildAddCollateral> | ReturnType<typeof buildRemoveCollateral>;
export type PositionWrite = PlaceOrderCall | CloseCall | CollateralCall;

/** The ordered steps of one write: the exact USDL approval when the allowance is short, then the calls. */
export type WritePlan<C extends PositionWrite = PositionWrite> = readonly (ApproveCall | C)[];

/** The calls are built, and so refused, before the allowance is read. */
async function withApproval<C extends PositionWrite>(
  amount: bigint,
  calls: readonly C[],
  source: AllowanceSource,
): Promise<WritePlan<C>> {
  const approvals = await planApproval(USDL, PERPS_SPENDER, source.allowance, amount, source.native);
  return [...approvals, ...calls];
}

/** A market open: placeOrder MARKET IOC (FOK when chosen), pulling collateralAmount from the wallet. */
export async function planOpen(input: OpenPositionInput, source: AllowanceSource): Promise<WritePlan<PlaceOrderCall>> {
  return withApproval(input.collateralAmount, [buildOpenPosition(input)], source);
}

/** A bounded addSize: placeOrder MARKET IOC on the position's market and side, pulling the added collateral. */
export async function planAddSize(input: AddSizeInput, source: AllowanceSource): Promise<WritePlan<PlaceOrderCall>> {
  return withApproval(input.additionalCollateral, [buildAddSize(input)], source);
}

/** A full close pulls nothing from the wallet, so it never approves. */
export function planClose(input: ClosePositionInput): WritePlan<CloseCall> {
  return [buildClosePosition(input)];
}

/** A partial close pulls nothing from the wallet, so it never approves. */
export function planPartialClose(input: PartialCloseInput): WritePlan<CloseCall> {
  return [buildPartialClose(input)];
}

export interface CollateralInput {
  action: "add" | "remove";
  positionId: bigint;
  /** USDL base units (6 decimals). */
  amount: bigint;
}

/** addCollateral pulls the amount by transferFrom; removeCollateral pays out and never approves. */
export async function planCollateral(input: CollateralInput, source: AllowanceSource): Promise<WritePlan<CollateralCall>> {
  if (input.action === "remove") {
    return [buildRemoveCollateral(input.positionId, input.amount)];
  }
  return withApproval(input.amount, [buildAddCollateral(input.positionId, input.amount)], source);
}

/** The placeOrder calls of an order plan behind one approval of their summed collateral. */
export async function planOrder(
  calls: readonly PlaceOrderCall[],
  source: AllowanceSource,
): Promise<WritePlan<PlaceOrderCall>> {
  const total = calls.reduce((sum, c) => sum + c.args[0].collateralAmount, 0n);
  return withApproval(total, calls, source);
}

/* ── getPosition decoded by output name ─────────────────────────── */

export type PositionTuple = ContractFunctionReturnType<typeof PerpsPrecompileABI, "view", "getPosition">;

export interface PositionRecord {
  user: Address;
  marketId: bigint;
  isLong: boolean;
  sizeUsd: bigint;
  collateralUsd: bigint;
  collateralToken: Address;
  collateralAmount: bigint;
  entryPrice: bigint;
  timestamp: bigint;
  active: boolean;
}

const POSITION_OUTPUTS = getAbiItem({ abi: PerpsPrecompileABI, name: "getPosition" }).outputs;

/** Name each getPosition output by the ABI, so no field is read by its index. */
export function positionFromTuple(tuple: PositionTuple): PositionRecord {
  if (tuple.length !== POSITION_OUTPUTS.length) {
    throw new RangeError(`getPosition returned ${tuple.length} values, the ABI declares ${POSITION_OUTPUTS.length}`);
  }
  return Object.fromEntries(POSITION_OUTPUTS.map((output, i) => [output.name, tuple[i]])) as unknown as PositionRecord;
}
