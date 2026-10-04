export type Verification = "unverified" | "receipt-verified" | "checkpoint-finalised" | "paxeer-finalised";
export type KeyView = { key_id: string; disabled: boolean; requests_per_window: number; window_seconds: number; used_in_window: number; remaining_in_window: number };
export type RequestRecord = { at: number; operation_digest: string; outcome: string; verification: Verification };
export type Endpoint = { endpoint: string; url: string; suspended: boolean; pending: number; in_flight: number; retrying: number; delivered_total: number; dead_lettered_total: number; last_failure?: string };
export type Delivery = { delivery: string; endpoint: string; event: string; state: { state: string }; verification: Verification; receipt_digest?: string };
export type Fact = { name: string; value: string; verification: Verification; receipt_digest?: string };
export type Payment = { event: string; subject: string; amount?: string; asset?: string; verification: Verification; settlement_verification: Verification; receipt_digest?: string; settled: boolean; facts: Fact[] };
export type Overview = {
  principal: string;
  usage: { keys: number; live_keys: number; requests_allowed: number; requests_used: number; requests_remaining: number; utilisation_per_mille: number };
  keys: KeyView[];
  recent_requests: RequestRecord[];
  endpoints: Endpoint[];
  dead_letters: Delivery[];
  payments: Payment[];
};


const levels: Verification[] = ["unverified", "receipt-verified", "checkpoint-finalised", "paxeer-finalised"];
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid dashboard record");
  return value as Record<string, unknown>;
}
function text(value: unknown): string {
  if (typeof value !== "string" || value.length > 4096 || /[\r\n\0]/.test(value)) throw new Error("Invalid dashboard text");
  return value;
}
function number(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error("Invalid dashboard counter");
  return value;
}
function boolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw new Error("Invalid dashboard flag");
  return value;
}
function optional(value: unknown): string | undefined { return value == null ? undefined : text(value); }
function array<T>(value: unknown, decode: (value: unknown) => T): T[] {
  if (!Array.isArray(value) || value.length > 200) throw new Error("Invalid dashboard page");
  return value.map(decode);
}
function verification(value: unknown): Verification {
  if (!levels.includes(value as Verification)) throw new Error("Unknown protocol verification");
  return value as Verification;
}
function digest(value: unknown): string | undefined {
  const result = optional(value);
  if (result !== undefined && !/^[0-9a-f]{64}$/.test(result)) throw new Error("Invalid receipt digest");
  return result;
}
function evidence(level: Verification, receipt: string | undefined): void {
  if ((level === "unverified") !== (receipt === undefined)) throw new Error("Protocol evidence does not match verification");
}
function fact(value: unknown): Fact {
  const row = object(value);
  const result = { name: text(row.name), value: text(row.value), verification: verification(row.verification), receipt_digest: digest(row.receipt_digest) };
  evidence(result.verification, result.receipt_digest);
  return result;
}
function key(value: unknown): KeyView {
  const row = object(value);
  return { key_id: text(row.key_id), disabled: boolean(row.disabled), requests_per_window: number(row.requests_per_window), window_seconds: number(row.window_seconds), used_in_window: number(row.used_in_window), remaining_in_window: number(row.remaining_in_window) };
}
function request(value: unknown): RequestRecord {
  const row = object(value);
  const level = verification(row.verification);
  if (level !== "unverified" || !["pending", "completed", "rate-limited", "refused"].includes(text(row.outcome))) throw new Error("Invalid request evidence");
  return { at: number(row.at), operation_digest: text(row.operation_digest), outcome: text(row.outcome), verification: level };
}
function endpoint(value: unknown): Endpoint {
  const row = object(value);
  return { endpoint: text(row.endpoint), url: text(row.url), suspended: boolean(row.suspended), pending: number(row.pending), in_flight: number(row.in_flight), retrying: number(row.retrying), delivered_total: number(row.delivered_total), dead_lettered_total: number(row.dead_lettered_total), last_failure: optional(row.last_failure) };
}
function delivery(value: unknown): Delivery {
  const row = object(value), state = object(row.state);
  const kind = text(state.state);
  if (!["pending", "in-flight", "retrying", "delivered", "dead-lettered"].includes(kind)) throw new Error("Unknown delivery state");
  if (kind === "delivered" && (number(state.status) < 200 || number(state.status) >= 300)) throw new Error("Delivery has no accepting status");
  const result = { delivery: text(row.delivery), endpoint: text(row.endpoint), event: text(row.event), state: { state: kind }, verification: verification(row.verification), receipt_digest: digest(row.receipt_digest) };
  evidence(result.verification, result.receipt_digest);
  return result;
}
function payment(value: unknown): Payment {
  const row = object(value);
  const facts = array(row.facts, fact), level = verification(row.verification), settlement = verification(row.settlement_verification), receipt = digest(row.receipt_digest), settled = boolean(row.settled);
  if (facts.length === 0 || facts.length > 32 || levels.indexOf(level) !== Math.min(...facts.map(item => levels.indexOf(item.verification)))) throw new Error("Payment exceeds its evidence");
  const state = facts.find(item => item.name === "state");
  if (settlement !== (state?.verification ?? "unverified") || receipt !== state?.receipt_digest) throw new Error("Settlement evidence mismatch");
  if (settled !== (state?.value === "settled" && settlement !== "unverified" && receipt !== undefined)) throw new Error("Unsupported settlement");
  return { event: text(row.event), subject: text(row.subject), amount: optional(row.amount), asset: optional(row.asset), verification: level, settlement_verification: settlement, receipt_digest: receipt, settled, facts };
}
export function decodeRequests(value: unknown): RequestRecord[] { return array(value, request); }
export function decodeDeliveries(value: unknown): Delivery[] { return array(value, delivery); }
export function decodeOverview(value: unknown): Overview {
  const row = object(value), usage = object(row.usage);
  return { principal: text(row.principal), usage: { keys: number(usage.keys), live_keys: number(usage.live_keys), requests_allowed: number(usage.requests_allowed), requests_used: number(usage.requests_used), requests_remaining: number(usage.requests_remaining), utilisation_per_mille: number(usage.utilisation_per_mille) }, keys: array(row.keys, key), recent_requests: decodeRequests(row.recent_requests), endpoints: array(row.endpoints, endpoint), dead_letters: decodeDeliveries(row.dead_letters), payments: array(row.payments, payment) };
}
export type Receipt = { activity_id: string; event: string; receipt_digest: string; verification: Verification; settled: boolean };
export function decodeReceipt(value: unknown): Receipt {
  const row = object(value), receipt = digest(row.receipt_digest), level = verification(row.verification);
  if (!/^[0-9a-f]{64}$/.test(text(row.activity_id)) || !receipt || level === "unverified" || row.settled !== true) throw new Error("Receipt is not verified settlement evidence");
  return { activity_id: text(row.activity_id), event: text(row.event), receipt_digest: receipt, verification: level, settled: true };
}
