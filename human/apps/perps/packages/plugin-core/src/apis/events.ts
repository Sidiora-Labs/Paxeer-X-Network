/**
 * Plugin events API - subscribable events from global EventEmitter (SimpleDI "EE").
 * Only supports TrackerEventName (track events) from @paxeer-network/types.
 */
import { EventEmitter, SimpleDI } from "@paxeer-network/core";
import type { TrackerEventName } from "@paxeer-network/types";

/**
 * Plugin events API interface.
 * Forwards to global EventEmitter (SimpleDI "EE") used by useTrack and useEventEmitter.
 */
export interface PluginEventsAPI {
  on(event: TrackerEventName, handler: (data: unknown) => void): void;
  off(event: TrackerEventName, handler: (data: unknown) => void): void;
}

/** Noop stub when EE is not yet available (e.g. before app bootstrap) */
export const NOOP_EVENTS: PluginEventsAPI = {
  on: () => {},
  off: () => {},
};

/**
 * Creates the events facade wired to the global EventEmitter (SimpleDI "EE").
 * Returns a noop stub if EE has not been initialized yet.
 */
export function createEventsFacade(): PluginEventsAPI {
  const ee = SimpleDI.get<EventEmitter>("EE");
  if (!ee) return NOOP_EVENTS;
  return {
    on: ee.on.bind(ee),
    off: ee.off.bind(ee),
  };
}
