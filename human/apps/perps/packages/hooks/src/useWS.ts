import type { WS } from "@paxeer-network/net";
import { nativeWebSocket } from "./native/NativeWebSocket";
/** The native client publishes Paxeer subscriptions using the component hooks' event contracts. */
export const useWS = (): WS => nativeWebSocket as unknown as WS;
