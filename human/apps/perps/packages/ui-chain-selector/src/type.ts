export type TChainItem = {
  name: string;
  id: number;
  lowestFee?: boolean;
  isTestnet: boolean;
};

export enum ChainType {
  Mainnet = "Mainnet",
  Testnet = "Testnet",
}
