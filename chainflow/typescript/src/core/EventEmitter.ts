// File generated from our OpenAPI spec by Scalar. See README.md for details.

type EventListener<Events, EventType extends keyof Events> = Events[EventType];

type EventListeners<Events, EventType extends keyof Events> = Array<{
  listener: EventListener<Events, EventType>;
  once?: boolean;
}>;

export type EventParameters<Events, EventType extends keyof Events> = {
  [Event in EventType]: EventListener<Events, EventType> extends (...args: infer P) => unknown ? P : never;
}[EventType];

export class EventEmitter<EventTypes extends Record<string, (...args: any[]) => unknown>> {
  #listeners: { [Event in keyof EventTypes]?: EventListeners<EventTypes, Event> } = {};

  on<Event extends keyof EventTypes>(event: Event, listener: EventListener<EventTypes, Event>): this {
    const listeners: EventListeners<EventTypes, Event> =
      this.#listeners[event] || (this.#listeners[event] = []);
    listeners.push({ listener });
    return this;
  }

  off<Event extends keyof EventTypes>(event: Event, listener: EventListener<EventTypes, Event>): this {
    const listeners = this.#listeners[event];
    if (!listeners) return this;
    const index = listeners.findIndex((item) => item.listener === listener);
    if (index >= 0) listeners.splice(index, 1);
    return this;
  }

  once<Event extends keyof EventTypes>(event: Event, listener: EventListener<EventTypes, Event>): this {
    const listeners: EventListeners<EventTypes, Event> =
      this.#listeners[event] || (this.#listeners[event] = []);
    listeners.push({ listener, once: true });
    return this;
  }

  /**
   * Resolves the next time `event` fires, as a promise-shaped alternative to {@link once}.
   *
   *   const message = await connection.emitted('message');
   *   const [code, reason, unsent] = await connection.emitted('close');
   *
   * Single-payload events resolve with the payload itself and parameterless events resolve with
   * `void`, so the common `await connection.emitted('message')` needs no destructuring. Events that
   * carry more than one argument — `close(code, reason, unsent)` — resolve with the whole argument
   * tuple, because there is no payload to single out and dropping the rest would silently lose data.
   */
  emitted<Event extends keyof EventTypes>(
    event: Event,
  ): Promise<
    // The three branches mirror the runtime rule below exactly: two-or-more required parameters keep
    // the tuple, no parameters resolve to `void`, and anything else (one required or one optional
    // parameter) unwraps to that parameter. They have to stay in step — a type that promises the
    // tuple while the body resolves with `args[0]` lets `const [code, reason] = await
    // emitted('close')` compile and then throw `TypeError: number is not iterable` at runtime.
    EventParameters<EventTypes, Event> extends [unknown, unknown, ...unknown[]]
      ? EventParameters<EventTypes, Event>
      : EventParameters<EventTypes, Event> extends []
        ? void
        : EventParameters<EventTypes, Event>[0]
  > {
    return new Promise((resolve) => {
      this.once(event, ((...args: EventParameters<EventTypes, Event>) =>
        resolve((args.length > 1 ? args : args[0]) as never)) as EventListener<EventTypes, Event>);
    });
  }

  protected _emit<Event extends keyof EventTypes>(
    event: Event,
    ...args: EventParameters<EventTypes, Event>
  ): void {
    const listeners = this.#listeners[event];
    if (!listeners) return;
    this.#listeners[event] = listeners.filter((listener) => !listener.once) as EventListeners<
      EventTypes,
      Event
    >;
    for (const { listener } of listeners)
      (listener as (...args: EventParameters<EventTypes, Event>) => unknown)(...args);
  }

  protected _hasListener(event: keyof EventTypes): boolean {
    return (this.#listeners[event]?.length ?? 0) > 0;
  }
}

export class InternalEventEmitter<
  EventTypes extends Record<string, (...args: any[]) => unknown>,
> extends EventEmitter<EventTypes> {
  override _emit<Event extends keyof EventTypes>(
    event: Event,
    ...args: EventParameters<EventTypes, Event>
  ): void {
    super._emit(event, ...args);
  }
}
