export interface QueueableOfflineAction {
  readonly kind: "read" | "report";
  readonly queueKey: string;
  readonly run: () => Promise<void>;
}

export interface MoneyOfflineAction {
  readonly kind: "money";
  readonly queueKey: string;
  readonly run: () => Promise<void>;
}

export type QueueDecision = "queued" | "duplicate" | "money-rejected";

export class OfflineActionQueue {
  readonly #queued = new Map<string, QueueableOfflineAction>();
  #activeFlush: Promise<void> | undefined;

  enqueue(action: QueueableOfflineAction | MoneyOfflineAction): QueueDecision {
    if (action.kind === "money") {
      return "money-rejected";
    }
    if (this.#queued.has(action.queueKey)) {
      return "duplicate";
    }
    this.#queued.set(action.queueKey, action);
    return "queued";
  }

  size(): number {
    return this.#queued.size;
  }

  flush(): Promise<void> {
    if (this.#activeFlush !== undefined) {
      return this.#activeFlush;
    }
    const flush: Promise<void> = this.#drain().finally(() => {
      if (this.#activeFlush === flush) {
        this.#activeFlush = undefined;
      }
    });
    this.#activeFlush = flush;
    return flush;
  }

  async #drain(): Promise<void> {
    for (const [queueKey, action] of this.#queued) {
      await action.run();
      this.#queued.delete(queueKey);
    }
  }
}
