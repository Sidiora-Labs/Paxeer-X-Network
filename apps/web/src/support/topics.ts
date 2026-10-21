import type { SupportTopic as SupportTopicId } from "../api";

export interface SupportTopic {
  readonly id: SupportTopicId;
  readonly labelKey: string;
  readonly seedKey: string;
}

export const SUPPORT_TOPICS: readonly SupportTopic[] = Object.freeze([
  Object.freeze({ id: "deposit" as const, labelKey: "support.topic.deposit", seedKey: "support.topic.deposit.seed" }),
  Object.freeze({ id: "withdrawal" as const, labelKey: "support.topic.withdrawal", seedKey: "support.topic.withdrawal.seed" }),
  Object.freeze({ id: "agents" as const, labelKey: "support.topic.agents", seedKey: "support.topic.agents.seed" }),
  Object.freeze({ id: "account" as const, labelKey: "support.topic.account", seedKey: "support.topic.account.seed" }),
  Object.freeze({ id: "report" as const, labelKey: "support.topic.report", seedKey: "support.topic.report.seed" }),
]);

export function supportTopic(id: SupportTopicId): SupportTopic {
  const topic = SUPPORT_TOPICS.find((candidate) => candidate.id === id);
  if (topic === undefined) {
    throw new Error(`Unknown support topic: ${id}`);
  }
  return topic;
}
