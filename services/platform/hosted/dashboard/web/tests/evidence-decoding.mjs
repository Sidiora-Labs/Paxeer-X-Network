import assert from "node:assert/strict";
import { decodeDeliveries, decodeReceipt, decodeRequests } from "../app/data.ts";
assert.throws(() => decodeRequests([{ at: 1, operation_digest: "operation", outcome: "completed", verification: "paxeer-finalised" }]), /request evidence/);
assert.throws(() => decodeDeliveries([{ delivery: "delivery", endpoint: "endpoint", event: "event", state: { state: "delivered", status: 500 }, verification: "unverified", receipt_digest: null }]), /accepting status/);
assert.throws(() => decodeReceipt({ activity_id: "a".repeat(64), event: "event", receipt_digest: "b".repeat(64), verification: "unverified", settled: true }), /not verified/);
assert.throws(() => decodeReceipt({ activity_id: "a".repeat(64), event: "event", receipt_digest: "b".repeat(64), verification: "invented", settled: true }), /Unknown protocol verification/);
console.log("dashboard evidence refusals: 4 passed");
