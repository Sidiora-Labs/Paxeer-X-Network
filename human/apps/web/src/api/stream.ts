import {
  HumanApiDecodeError,
  HumanApiError,
  type HumanApiClient,
  type StreamEvent,
} from "./index";
import { singleCurrentSessionId } from "../auth/session";

export interface HumanStreamObserver {
  readonly signal: AbortSignal;
  readonly reset: () => void;
  readonly reconcile: () => Promise<void>;
  readonly process: (event: StreamEvent) => Promise<void>;
  readonly failed: (error: unknown) => void;
}

async function delay(milliseconds: number, signal: AbortSignal): Promise<void> {
  const deadline = Date.now() + milliseconds;
  let remaining = milliseconds;
  while (!signal.aborted && remaining > 0) {
    await new Promise<void>((resolve) => {
      const complete = () => {
        clearTimeout(timer);
        signal.removeEventListener("abort", complete);
        resolve();
      };
      const timer = setTimeout(complete, Math.min(remaining, 2_147_483_647));
      signal.addEventListener("abort", complete, { once: true });
    });
    remaining = deadline - Date.now();
  }
}

export async function observeHumanStream(
  client: HumanApiClient,
  observer: HumanStreamObserver,
): Promise<void> {
  let scope: string | undefined;
  let cursor: string | undefined;
  let reconcile = true;
  let retryDelay = 1_000;
  while (!observer.signal.aborted) {
    try {
      const [balance, sessions] = await Promise.all([
        client.accountBalance(),
        client.sessionList(),
      ]);
      if (observer.signal.aborted) return;
      const sessionId = singleCurrentSessionId(sessions.sessions);
      if (balance.account_id.length === 0 || sessionId === undefined) {
        throw new HumanApiDecodeError("stream requires one current authenticated account session");
      }
      const nextScope = JSON.stringify([balance.account_id, sessionId]);
      if (nextScope !== scope) {
        scope = nextScope;
        cursor = undefined;
        reconcile = true;
        observer.reset();
      }
      if (cursor === undefined) {
        cursor = (await client.streamOpen()).cursor;
      }
      if (observer.signal.aborted) return;
      if (reconcile) {
        await observer.reconcile();
        if (observer.signal.aborted) return;
        reconcile = false;
      }
      for await (const event of client.streamSubscribe(cursor, { signal: observer.signal })) {
        if (observer.signal.aborted) return;
        if (event.cursor === cursor) continue;
        await observer.process(event);
        if (observer.signal.aborted) return;
        cursor = event.cursor;
        retryDelay = 1_000;
      }
    } catch (error) {
      if (observer.signal.aborted) return;
      observer.failed(error);
      if (error instanceof HumanApiError) {
        if (error.detail.code === "unauthenticated" || error.detail.code === "session-expired" || error.detail.code === "forbidden") {
          throw error;
        }
        if (error.detail.code === "cursor-expired") {
          cursor = undefined;
          reconcile = true;
          continue;
        }
        if (error.detail.retry !== "retriable" && error.detail.retry !== "retriable-after") {
          throw error;
        }
        const requestedDelay = error.detail.retry_after_ms;
        if (requestedDelay !== undefined && (!Number.isSafeInteger(requestedDelay) || requestedDelay < 0)) {
          throw new HumanApiDecodeError("stream refusal retry_after_ms must be a nonnegative safe integer");
        }
        await delay(requestedDelay ?? retryDelay, observer.signal);
      } else if (error instanceof TypeError) {
        await delay(retryDelay, observer.signal);
      } else {
        throw error;
      }
      retryDelay = Math.min(retryDelay * 2, 30_000);
    }
  }
}
