export const PAXEER_NETWORK_ICON =
  "/assets/static/symbol_logo/ORDER.png";

export const PAXEER_NETWORK_VAULT_TITLE = "paxeer-network OmniVault";

export const PAXEER_NETWORK_VAULT_DESCRIPTION =
  "Earn passive yields effortlessly, no trading expertise required. OmniVault deploys market-making strategies, taking on liquidations, and accrue platform fees.";

export const getBrokerIconUrl = (brokerId: string) => {
  switch (brokerId) {
    case "paxeer-network":
      return "/assets/static/symbol_logo/ORDER.png";
    case "woofi_pro":
      return "/assets/static/broker_logo/woofi_pro.png";
    case "aden":
      return "/vaults/broker/aden.png";
    case "vooi":
      return "/assets/static/broker_logo/vooi.png";
    default:
      return `/assets/static/broker_logo/${brokerId}.png`;
  }
};
