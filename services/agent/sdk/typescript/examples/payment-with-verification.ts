import {
  Client,
  VerificationLevel,
  requireVerified,
  type IdempotentMutation,
  type SubmissionState,
  type VerifiedRead,
} from "../src/index.js";

export interface PaymentRequest { readonly canonicalBytes: Uint8Array }

export async function paymentWithVerification(
  client: Client,
  request: IdempotentMutation<PaymentRequest>,
): Promise<VerifiedRead<SubmissionState>> {
  const result = await client.call<typeof request, VerifiedRead<SubmissionState>>("submit", request);
  const verified = requireVerified(VerificationLevel.SequencerSigned, result);
  if (verified.value.kind !== "Executed") throw new Error(`payment_not_executed:${verified.value.kind}`);
  return verified;
}
