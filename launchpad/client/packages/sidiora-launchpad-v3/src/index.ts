import { getContract, isAddress, zeroAddress, type Address, type GetContractReturnType, type PublicClient } from 'viem';
import { factoryAbi, routerAbi, quoterAbi, protocolConfigAbi } from './abis.js';
export * from './abis.js';
export interface LaunchpadDeployment {
  chainId: 125;
  factory: Address;
  router: Address;
  quoter: Address;
  protocolConfig: Address;
  usdl: Address;
}
export function validateDeployment(deployment: LaunchpadDeployment): Readonly<LaunchpadDeployment> {
  if (deployment.chainId !== 125) throw new Error('Launchpad requires chain 125');
  for (const key of ['factory', 'router', 'quoter', 'protocolConfig', 'usdl'] as const) {
    if (!isAddress(deployment[key], { strict: false }) || deployment[key].toLowerCase() === zeroAddress) {
      throw new Error(`Invalid deployment address: ${key}`);
    }
  }
  return Object.freeze({ ...deployment });
}
export interface LaunchpadClient {
  deployment: Readonly<LaunchpadDeployment>;
  factory: GetContractReturnType<typeof factoryAbi, PublicClient>;
  router: GetContractReturnType<typeof routerAbi, PublicClient>;
  quoter: GetContractReturnType<typeof quoterAbi, PublicClient>;
  protocolConfig: GetContractReturnType<typeof protocolConfigAbi, PublicClient>;
}
export function createLaunchpadClient(client: PublicClient, input: LaunchpadDeployment): LaunchpadClient {
  const deployment = validateDeployment(input);
  if (client.chain?.id !== deployment.chainId) throw new Error('Public client must specify chain 125');
  return {
    deployment,
    factory: getContract({ address: deployment.factory, abi: factoryAbi, client }),
    router: getContract({ address: deployment.router, abi: routerAbi, client }),
    quoter: getContract({ address: deployment.quoter, abi: quoterAbi, client }),
    protocolConfig: getContract({ address: deployment.protocolConfig, abi: protocolConfigAbi, client }),
  };
}

export function deploymentFromManifest(manifest: {
  _meta: { network: string; usdl: string };
  SidioraFactory_proxy: string;
  Router_proxy: string;
  Quoter_proxy: string;
  ProtocolConfig_proxy: string;
}): Readonly<LaunchpadDeployment> {
  if (manifest._meta.network !== '125') throw new Error('Manifest requires chain 125');
  return validateDeployment({
    chainId: 125,
    factory: manifest.SidioraFactory_proxy as Address,
    router: manifest.Router_proxy as Address,
    quoter: manifest.Quoter_proxy as Address,
    protocolConfig: manifest.ProtocolConfig_proxy as Address,
    usdl: manifest._meta.usdl as Address,
  });
}
