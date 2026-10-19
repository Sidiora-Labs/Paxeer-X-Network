import {
  verifyReceipt,
  type AuthorizedReceiptBatch,
  type ReceiptVerification,
} from "../src/index.js";

export function offlineReceiptVerification(
  canonicalReceipt: Uint8Array,
  authorizedBatch: AuthorizedReceiptBatch,
): Promise<ReceiptVerification> {
  return verifyReceipt(canonicalReceipt, authorizedBatch);
}
