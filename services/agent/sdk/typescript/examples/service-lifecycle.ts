import { Client, type IdempotentMutation, type SubmissionState } from "../src/index.js";

export interface ServiceActivity { readonly stage: "commit" | "deliver" | "accept"; readonly canonicalBytes: Uint8Array }

export async function serviceLifecycle(
  client: Client,
  activities: readonly IdempotentMutation<ServiceActivity>[],
): Promise<SubmissionState[]> {
  const observed: SubmissionState[] = [];
  for (const activity of activities) {
    observed.push(await client.call("submit", activity));
  }
  return observed;
}
