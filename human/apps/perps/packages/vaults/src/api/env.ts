type Env = "prod" | "staging" | "qa" | "dev";

export const VAULTS_API_URLS: Record<Env, string> = {
  prod: "https://api.paxeer.network",
  staging: "https://testnet-api.paxeer.network",
  qa: "https://api-sv.qa.paxeer-network-i.network",
  dev: "https://api-sv.dev.paxeer-network-i.network",
};

export const VAULTS_WEBSITE_URLS: Record<Env, string> = {
  prod: "https://app.paxeer-network.network",
  staging: "https://staging-app.paxeer-network.network",
  qa: "https://qa-app.paxeer-network.network",
  dev: "https://dev-app.paxeer-network.network",
};
