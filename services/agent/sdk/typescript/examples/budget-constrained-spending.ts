import { Client, type IdempotentMutation } from "../src/index.js";

export interface BudgetRequest { readonly limit: bigint; readonly enforcement: "ProtocolBudget" | "DaemonLimit" }

export async function createBudget(
  client: Client,
  request: IdempotentMutation<BudgetRequest>,
): Promise<unknown> {
  if (request.operation.limit <= 0n) throw new RangeError("budget_limit");
  return client.call("budget.create", request);
}
