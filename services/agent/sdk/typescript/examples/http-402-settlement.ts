import { Client, type IdempotentMutation, type SubmissionState } from "../src/index.js";

export interface SettlementRequest { readonly paymentRequirement: Uint8Array }

export async function settle402(
  client: Client,
  request: IdempotentMutation<SettlementRequest>,
): Promise<string> {
  const state = await client.call<typeof request, SubmissionState>("submit", request);
  if (state.kind === "Unknown") throw new Error("settlement_unknown");
  if (state.kind !== "Executed") throw new Error(`settlement_failed:${state.kind}`);
  return state.receiptRef;
}
