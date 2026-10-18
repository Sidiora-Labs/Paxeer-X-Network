import { injectable } from "@paxeer-network/ui";
import { OrderEntry } from "./orderEntry.ui";
import type { OrderEntryProps } from "./orderEntry.ui";

/**
 * OrderEntry component that can be intercepted by plugins.
 */
export const InjectableOrderEntry = injectable<OrderEntryProps>(
  OrderEntry,
  "OrderEntry",
);
