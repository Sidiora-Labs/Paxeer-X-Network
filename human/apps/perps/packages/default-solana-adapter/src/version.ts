declare global {
  interface Window {
    __PAXEER_NETWORK_VERSION__?: {
      [key: string]: string;
    };
  }
}
if (typeof window !== "undefined") {
  window.__PAXEER_NETWORK_VERSION__ = window.__PAXEER_NETWORK_VERSION__ || {};
  window.__PAXEER_NETWORK_VERSION__["@paxeer-network/default-solana-adapter"] =
    "3.2.2";
}

export default "3.2.2";
