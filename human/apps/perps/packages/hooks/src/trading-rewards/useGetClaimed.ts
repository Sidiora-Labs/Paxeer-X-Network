// import { ethers } from "ethers";
import { useCallback, useEffect, useRef, useState } from "react";

import { getContractByEnv } from "./abis/contrast";
import { toOrder } from "./utils";
import { useGetEnv } from "./useGetEnv";
import { useAccount } from "../useAccount";

export enum DistributionId {
  order = 0,
  esORder = 1,
  mmOrder = 2,
  mmEsOrder = 3,
}

export const useGetClaimed = (
  id: DistributionId
): [
  number | undefined,
  {
    refresh: () => void;
  }
] => {
  const [data, setData] = useState<number | undefined>(0);
  const { account } = useAccount();
  // account.wallet?.callOnChain
  const address = account.address;
  // const rpc = useRef<string | undefined>(undefined);
  // const provider = useRef<ethers.AbstractProvider | undefined>(undefined);
  // const contract = useRef<ethers.Contract | undefined>(undefined);
  const env = useGetEnv();

  // useEffect(() => {
  //   const params = getContractByEnv(env);
  //   rpc.current = params.paxeerNetworkChainRpcUrl;
  //   provider.current = ethers.getDefaultProvider(rpc.current);
  //   contract.current = new ethers.Contract(
  //     params.paxeerNetworkContract,
  //     params.paxeerNetworkContractABI,
  //     provider.current
  //   );
  // }, []);

  const refresh = useCallback(() => {
    const params = getContractByEnv(env);
    if (
      typeof address === "undefined"
      // ||
      // typeof rpc.current === "undefined" ||
      // typeof provider.current === "undefined" ||
      // typeof contract.current === "undefined"
    )
      return;

    console.log(`get claimed(${id})`, [id, address]);
    account.walletAdapter
      ?.callOnChain(
        // @ts-ignore
        { public_rpc_url: params.paxeerNetworkChainRpcUrl },
        params.paxeerNetworkContract,
        "getClaimed",
        [id, address],
        {
          abi: params.paxeerNetworkContractABI,
        }
      )
      .catch((error: any) => {
        // const parsedEthersError = getParsedEthersError(error);
        // throw parsedEthersError;
        throw error;
      })
      .then((res: any) => {
        const resOrder = toOrder(res);
        // const resOrder = toOrder(BigInt(2921867952260000000000));
        console.log(`new get claimed(${id})`, resOrder);
        setData(resOrder);
      })
      .catch((error: any) => {});

    // contract.current["getClaimed"]
    //   .apply(null, [id, address])
    //   .catch((error: any) => {
    //     const parsedEthersError = getParsedEthersError(error);
    //     throw parsedEthersError;
    //   })
    //   .then((res: any) => {
    //     const resOrder = toOrder(res);
    //     // const resOrder = toOrder(BigInt(2921867952260000000000));
    //     console.log(`get claimed(${id})`, resOrder);
    //     setData(resOrder);
    //   })
    //   .catch((error: any) => {});
  }, [address, env]);

  useEffect(() => {
    refresh();
  }, [env, id, address]);

  return [data, { refresh }];
};
