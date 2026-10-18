import { useContext } from "react";
import { PaxeerNetworkContext } from "../paxeerNetworkContext";

export enum ENVType {
  prod = "prod",
  staging = "staging",
  qa = "qa",
  dev = "dev",
}

/**
 * env is determined by networkId and env
 * | networkId | env     | retrurn  |
 * |-----------|---------|----------|
 * | mainnet   | noset   | prod     |
 * | mainnet   | prod    | prod     |
 * | mainnet   | staging | prod     |
 * | mainnet   | qa      | prod     |
 * | mainnet   | dev     | prod     |
 * | testnet   | noset   | staging  |
 * | testnet   | prod    | staging  |
 * | testnet   | staging | staging  |
 * | testnet   | qa      | qa       |
 * | testnet   | dev     | dev      |
 * if env is not set, return staging
 * 
 * @returns {ENVType} 
 */
export const useGetEnv = (): ENVType => {
  const { configStore } = useContext(PaxeerNetworkContext);
  const env = configStore.get("env") as ENVType;
  const networkId = configStore.get("networkId");
  if (networkId === "mainnet") {
    return ENVType.prod;
  }
  if (networkId === "testnet" && env === "prod") {
    return ENVType.staging;
  }
  return env ?? ENVType.staging;
};
