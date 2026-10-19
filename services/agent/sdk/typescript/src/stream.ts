import { PlatformSdkError } from "./production.js";

declare const streamCursorBrand: unique symbol;
export type StreamCursor = string & { readonly [streamCursorBrand]: true };

export function streamCursor(value: string): StreamCursor {
  if (value.length === 0 || value.length > 512 || value.includes("\0")) {
    throw new PlatformSdkError({ code: "invalid-argument", retry: "never" });
  }
  return value as StreamCursor;
}

export interface StreamEvent<T> {
  readonly eventId: string;
  readonly previousCursor: StreamCursor;
  readonly cursor: StreamCursor;
  readonly value: T;
}

export interface StreamPage<T> {
  readonly requestedCursor: StreamCursor;
  readonly events: readonly StreamEvent<T>[];
  readonly nextCursor: StreamCursor;
}

export type StreamPageSource<T> = (
  cursor: StreamCursor,
  signal?: AbortSignal,
) => Promise<StreamPage<T>>;

export class ResumableStream<T> {
  #cursor: StreamCursor;
  readonly #seen = new Set<string>();

  public constructor(cursor: StreamCursor) {
    this.#cursor = cursor;
  }

  public get cursor(): StreamCursor {
    return this.#cursor;
  }

  public accept(page: StreamPage<T>): readonly StreamEvent<T>[] {
    if (page.requestedCursor !== this.#cursor) {
      throw new PlatformSdkError({ code: "decode-failure", retry: "never" });
    }
    let expected = this.#cursor;
    const accepted: StreamEvent<T>[] = [];
    const pageEventIds = new Set<string>();
    for (const event of page.events) {
      if (
        event.eventId.length === 0
        || event.previousCursor !== expected
        || this.#seen.has(event.eventId)
        || pageEventIds.has(event.eventId)
      ) {
        throw new PlatformSdkError({ code: "decode-failure", retry: "never" });
      }
      pageEventIds.add(event.eventId);
      accepted.push(event);
      expected = event.cursor;
    }
    if (page.nextCursor !== expected) {
      throw new PlatformSdkError({ code: "decode-failure", retry: "never" });
    }
    for (const eventId of pageEventIds) {
      this.#seen.add(eventId);
    }
    this.#cursor = page.nextCursor;
    return Object.freeze(accepted);
  }

  public async *events(source: StreamPageSource<T>, signal?: AbortSignal): AsyncGenerator<StreamEvent<T>> {
    while (signal?.aborted !== true) {
      const page = await source(this.#cursor, signal);
      const events = this.accept(page);
      for (const event of events) {
        yield event;
      }
    }
  }
}
