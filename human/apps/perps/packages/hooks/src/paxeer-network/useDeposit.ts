import { useCallback, useEffect, useMemo, useState } from "react";
import useSWR from "swr";
import {
  API,
  ARBITRUM_MAINNET_CHAINID,
  ARBITRUM_TESTNET_CHAINID,
  AccountStatusEnum,
  ChainNamespace,
  DEPOSIT_FEE_RATE,
  ETHEREUM_MAINNET_CHAINID,
  MaxUint256,
  SDKError,
  TrackerEventName,
  isNativeTokenChecker,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { useAccount } from "../useAccount";
import { useConfig } from "../useConfig";
import { useTrack } from "../useTrack";
import {
  type AllowanceComparison,
  waitForAllowance as pollAllowanceUntilSatisfied,
} from "./allowance";
import { useChains } from "./useChains";

export type DepositOptions = {
  // from address
  address?: string;
  decimals?: number;

  srcChainId?: number;
  /** input token */
  srcToken?: string;
  /** output token */
  dstToken?: string;

  // swap deposit options
  /** cross chain route address */
  crossChainRouteAddress?: string;
  /** swap deposit vault address */
  depositorAddress?: string;
};

export type DST = {
  symbol: string;
  address: string;
  decimals: number;
  chainId: number;
  network: string;
};

export type UseDepositReturn = ReturnType<typeof useDeposit>;

export const useDeposit = (options: DepositOptions) => {
  const [quantity, setQuantity] = useState<string>("");
  const { account, state } = useAccount();
  const { track } = useTrack();

  const targetChain = useTargetChain(options.srcChainId);

  const dst = useMemo(() => {
    const symbol = options.dstToken || "USDC";

    const targetToken = targetChain?.token_infos.find(
      (token) => token.symbol === symbol,
    );

    return {
      symbol,
      address: targetToken?.address,
      decimals: targetToken?.decimals,
      chainId: targetChain?.network_infos.chain_id,
      network: targetChain?.network_infos.shortName,
    } as DST;
  }, [targetChain, options.dstToken]);

  const isNativeToken = useMemo(
    () => isNativeTokenChecker(options.address),
    [options.address],
  );

  const vaultAddress = useMemo(() => {
    // cross swap deposit vault address
    if (dst?.chainId !== options.srcChainId) {
      return options.crossChainRouteAddress;
    }

    // swap deposit vault address
    if (options.srcToken !== options.dstToken) {
      return options.depositorAddress;
    }

    // paxeer-network deposit vault address
    return targetChain?.network_infos.vault_address;
  }, [
    dst,
    options.srcChainId,
    options.srcToken,
    options.dstToken,
    options.crossChainRouteAddress,
    options.depositorAddress,
    targetChain,
  ]);

  const {
    balance,
    balanceRevalidating,
    fetchBalance,
    fetchBalances,
    setBalance,
  } = useBalance({
    address: options.address,
    decimals: options.decimals,
    srcToken: options.srcToken,
    account,
    status: state.status,
  });

  const {
    allowance,
    allowanceRevalidating,
    updateAllowanceWhenTxSuccess,
    enquireAllowance,
  } = useAllowance({
    address: options.address,
    decimals: options.decimals,
    vaultAddress,
    account,
    status: state.status,
    quantity,
    chainId: dst?.chainId,
  });

  const { depositFee, depositFeeRevalidating } = useDepositFee({
    quantity,
    account,
    targetChain,
    decimals: options.decimals,
    dstToken: options.dstToken,
    vaultAddress,
    isNativeToken,
    dst,
  });

  const resetApprove = useCallback(
    async (tokenAddress: string, decimal: number, vaultAddress: string) => {
      const result = await account.assetsManager.approve({
        address: tokenAddress,
        amount: "0",
        vaultAddress,
        decimals: decimal,
      });

      await updateAllowanceWhenTxSuccess(result.hash, "0", "eq");
    },
    [account, updateAllowanceWhenTxSuccess],
  );

  const approve = useCallback(
    async (amount?: string) => {
      if (!options.address) {
        throw new Error("address is required");
      }

      let isSetMaxValue = false;

      if (
        checkIfChainTokenNeedRestApprove(options.srcChainId!, options.srcToken!)
      ) {
        isSetMaxValue = true;
        if (allowance && new Decimal(allowance).gt(0)) {
          await resetApprove(
            options.address!,
            options.decimals!,
            vaultAddress!,
          );
        }
      }
      const result = await account.assetsManager.approve({
        address: options.address,
        amount,
        vaultAddress,
        isSetMaxValue,
        decimals: options.decimals!,
      });

      const expectedAllowance =
        amount ||
        account.walletAdapter?.formatUnits(MaxUint256, options.decimals!);

      return updateAllowanceWhenTxSuccess(result.hash, expectedAllowance);
    },
    [
      account,
      options.srcChainId,
      options.srcToken,
      allowance,
      options.address,
      options.decimals,
      vaultAddress,
      updateAllowanceWhenTxSuccess,
      resetApprove,
    ],
  );

  // only support paxeer-network deposit
  const deposit = useCallback(async () => {
    await enquireAllowance();

    const inputs: Parameters<typeof account.assetsManager.deposit>[0] = {
      amount: quantity,
      fee: depositFee,
      decimals: options.decimals!,
      token: options.srcToken,
      address: options.address,
      vaultAddress: vaultAddress!,
    };

    const depositPromise = isNativeToken
      ? account.assetsManager.depositNativeToken(inputs)
      : account.assetsManager.deposit(inputs);

    return depositPromise
      .then((result: any) => {
        void updateAllowanceWhenTxSuccess(result.hash).catch((error) => {
          console.error("deposit allowance refresh error", error);
        });
        // when deposit request success, update balance
        setBalance((value) =>
          value ? new Decimal(value).sub(quantity).toString() : "0",
        );

        track(TrackerEventName.depositSuccess, {
          wallet: state?.connectWallet?.name,
          network: targetChain?.network_infos.name,
          quantity,
          currency: options.srcToken,
        });
        return result;
      })
      .catch((e) => {
        track(TrackerEventName.depositFailed, {
          wallet: state?.connectWallet?.name,
          network: targetChain?.network_infos?.name,
          msg: JSON.stringify(e),
        });
        throw e;
      });
  }, [
    state,
    account,
    quantity,
    depositFee,
    targetChain,
    options.decimals,
    options.srcToken,
    enquireAllowance,
    updateAllowanceWhenTxSuccess,
    isNativeToken,
    vaultAddress,
  ]);

  return {
    balance,
    allowance,
    /** deposit fee, unit: wei */
    depositFee,
    balanceRevalidating,
    allowanceRevalidating,
    depositFeeRevalidating,
    isNativeToken,
    dst,
    targetChain,
    /** input quantiy */
    quantity,
    /** set input quantity */
    setQuantity,
    approve,
    deposit,
    fetchBalance,
    fetchBalances,
  };
};

function useBalance(options: {
  address?: string;
  decimals?: number;
  srcToken?: string;
  account: ReturnType<typeof useAccount>["account"];
  status: AccountStatusEnum;
}) {
  const { srcToken, address, decimals, account, status } = options;
  const [balance, setBalance] = useState<string | null>(null);

  const fetchBalance = useCallback(
    async (address: string, decimals?: number) => {
      try {
        if (isNativeTokenChecker(address)) {
          return await account.assetsManager.getNativeBalance({
            decimals,
          });
        }

        return await account.assetsManager.getBalance(address, {
          decimals,
        });
      } catch (err: any) {
        if (
          ignoreBalanceError({
            token: srcToken!,
            chainNamespace: account.walletAdapter?.chainNamespace!,
            err,
          })
        ) {
          console.log("ignore balance error: ", err);
          return "0";
        }
        throw err;
      }
    },
    [srcToken, account],
  );

  const fetchBalances = useCallback(
    async (tokens: API.TokenInfo[]) => {
      return account.assetsManager.getBalances(tokens);
    },
    [account],
  );

  // create key for useSWR, return null when conditions are not met
  const key = useMemo(() => {
    if (!address || status < AccountStatusEnum.Connected) {
      return null;
    }
    return ["balance", address, decimals];
  }, [status, address, decimals]);

  // create fetcher function
  const fetcher = useCallback(async () => {
    if (!address) {
      return "0";
    }
    const balance = await fetchBalance(address, decimals);
    // console.log("balance", balance);
    return balance;
  }, [address, decimals, fetchBalance]);

  // get refresh interval based on chain namespace, solana is 10s, other is 3s
  const refreshInterval = useMemo(() => {
    return account.walletAdapter?.chainNamespace === ChainNamespace.solana
      ? 10000
      : 3000;
  }, [account]);

  // use useSWR for polling balance
  const { data: swrBalance, isLoading: balanceRevalidating } = useSWR(
    key,
    fetcher,
    {
      revalidateOnFocus: true,
      refreshInterval: refreshInterval,
      errorRetryCount: 10,
    },
  );

  // sync balance from SWR data; reset to null (loading) when address changes
  useEffect(() => {
    if (swrBalance !== undefined) {
      setBalance(swrBalance || "0");
    } else {
      setBalance(null);
    }
  }, [swrBalance, address]);

  return {
    balance,
    balanceRevalidating: balanceRevalidating || balance === null,
    setBalance,
    fetchBalance,
    fetchBalances,
  };
}

function useAllowance(options: {
  address?: string;
  decimals?: number;
  vaultAddress?: string;
  account: ReturnType<typeof useAccount>["account"];
  status: AccountStatusEnum;
  quantity: string;
  chainId?: number;
}) {
  const {
    address,
    decimals,
    vaultAddress,
    account,
    status,
    quantity,
    chainId,
  } = options;
  const [allowance, setAllowance] = useState("0");

  const fetchAllowance = useCallback(
    async (options: {
      address?: string;
      decimals?: number;
      vaultAddress?: string;
    }) => {
      const { address, decimals, vaultAddress } = options;
      if (!address || !decimals || !vaultAddress) {
        return "0";
      }
      // native token and solana don't need to get allowance, so return max uint256
      if (
        isNativeTokenChecker(address) ||
        account.walletAdapter?.chainNamespace === ChainNamespace.solana
      ) {
        return account.walletAdapter?.formatUnits(MaxUint256, decimals!)!;
      }

      const allowance = await account.assetsManager.getAllowance({
        address,
        vaultAddress,
        decimals,
      });

      console.info("allowance", address, allowance);
      return allowance;
    },
    [account],
  );

  // create key for useSWR, return null when conditions are not met
  const key = useMemo(() => {
    if (
      !address ||
      !decimals ||
      !vaultAddress ||
      status < AccountStatusEnum.Connected
    ) {
      return null;
    }

    return ["allowance", address, vaultAddress, decimals];
  }, [status, address, vaultAddress, decimals, account]);

  // get refresh interval based on chain namespace, solana is 10s, other is 3s
  const refreshInterval = useMemo(() => {
    return account.walletAdapter?.chainNamespace === ChainNamespace.solana
      ? 10000
      : 3000;
  }, [account, chainId]);

  const fetcher = useCallback(async () => {
    return fetchAllowance({ address, decimals, vaultAddress });
  }, [address, decimals, vaultAddress, fetchAllowance]);

  // use useSWR for polling allowance
  const {
    data: swrAllowance,
    isLoading: allowanceRevalidating,
    mutate: mutateAllowance,
  } = useSWR(key, fetcher, {
    revalidateOnFocus: true,
    refreshInterval: refreshInterval,
    errorRetryCount: 10,
  });

  useEffect(() => {
    if (swrAllowance !== undefined) {
      setAllowance(swrAllowance || "0");
    }
  }, [swrAllowance]);

  const waitForAllowance = useCallback(
    async (expectedAmount: string, comparison: AllowanceComparison = "gte") => {
      return pollAllowanceUntilSatisfied({
        readAllowance: () =>
          fetchAllowance({
            address,
            decimals,
            vaultAddress,
          }),
        expectedAmount,
        comparison,
        onRead: setAllowance,
        onSatisfied: (currentAllowance) =>
          mutateAllowance(currentAllowance, { revalidate: false }).then(() => {
            return undefined;
          }),
      });
    },
    [address, decimals, fetchAllowance, mutateAllowance, vaultAddress],
  );

  const updateAllowanceWhenTxSuccess = useCallback(
    async (
      txHash: string,
      expectedAmount?: string,
      comparison: AllowanceComparison = "gte",
    ) => {
      const receipt =
        await account.walletAdapter?.pollTransactionReceiptWithBackoff(txHash);

      if (!receipt || Number(receipt.status) !== 1) {
        throw new SDKError("Transaction failed");
      }

      if (expectedAmount !== undefined) {
        return waitForAllowance(expectedAmount, comparison);
      }

      const currentAllowance = await fetchAllowance({
        address,
        decimals,
        vaultAddress,
      });
      setAllowance(currentAllowance);
      await mutateAllowance(currentAllowance, { revalidate: false });
      return currentAllowance;
    },
    [
      account,
      address,
      decimals,
      fetchAllowance,
      mutateAllowance,
      vaultAddress,
      waitForAllowance,
    ],
  );

  const enquireAllowance = useCallback(() => {
    return waitForAllowance(quantity);
  }, [quantity, waitForAllowance]);

  return {
    allowance,
    allowanceRevalidating,
    updateAllowanceWhenTxSuccess,
    enquireAllowance,
  };
}

function useDepositFee(options: {
  quantity: string;
  account: ReturnType<typeof useAccount>["account"];
  targetChain?: API.Chain;
  decimals?: number;
  dstToken?: string;
  vaultAddress?: string;
  isNativeToken?: boolean;
  dst?: DST;
}) {
  const {
    quantity,
    account,
    targetChain,
    decimals,
    dstToken,
    vaultAddress,
    isNativeToken,
    dst,
  } = options;

  const getDepositFee = useCallback(
    async (quantity: string) => {
      if (!quantity || Number(quantity) === 0 || isNaN(Number(quantity))) {
        return 0n;
      }

      const depositFee = await account.assetsManager.getDepositFee({
        amount: quantity,
        chain: targetChain?.network_infos!,
        decimals: decimals!,
        token: dstToken,
        // TODO: when swap deposit, dstToken address is not same as src token address
        address: dst?.address,
      });

      let estimatedGasFee = 0n;

      try {
        const inputs: Parameters<
          typeof account.assetsManager.estimateDepositGasFee
        >[0] = {
          amount: quantity,
          fee: depositFee,
          decimals: decimals!,
          token: dstToken,
          address: dst?.address,
          vaultAddress: vaultAddress!,
        };

        estimatedGasFee = isNativeToken
          ? await account.assetsManager.estimateNativeDepositGasFee(inputs)
          : await account.assetsManager.estimateDepositGasFee(inputs);
      } catch (error) {
        console.error("estimateDepositGas", error);
        estimatedGasFee = 0n;
      }

      console.log("depositFee", depositFee);
      console.log("estimatedGasFee", estimatedGasFee);

      return depositFee + estimatedGasFee;
    },
    [
      account,
      targetChain,
      decimals,
      dstToken,
      dst?.address,
      vaultAddress,
      isNativeToken,
      dst,
    ],
  );

  const key = useMemo(() => {
    if (
      !dst?.address ||
      !decimals ||
      !vaultAddress ||
      !dstToken ||
      !quantity ||
      !targetChain?.network_infos?.chain_id
    ) {
      return null;
    }

    return [
      "depositFee",
      dst?.address,
      vaultAddress,
      dstToken,
      decimals,
      targetChain?.network_infos?.chain_id,
      quantity,
    ];
  }, [quantity, targetChain, decimals, dstToken, dst, vaultAddress]);

  const fetcher = useCallback(async () => {
    const res = await getDepositFee(quantity);
    return res;
  }, [getDepositFee, quantity]);

  const {
    data,
    isValidating: depositFeeRevalidating,
    mutate: mutateDepositFee,
  } = useSWR(key, fetcher, {
    revalidateOnFocus: true,
  });

  const depositFee = useMemo(() => {
    const fee = data ?? 0n;
    return BigInt(
      new Decimal(fee.toString())
        .mul(DEPOSIT_FEE_RATE)
        .toFixed(0, Decimal.ROUND_UP)
        .toString(),
    );
  }, [data]);

  return { depositFee, depositFeeRevalidating, mutateDepositFee };
}

function useTargetChain(srcChainId?: number) {
  const networkId = useConfig("networkId");
  const [, { findByChainId, isTestnetChain }] = useChains(undefined);

  const targetChain = useMemo(() => {
    let chain: API.Chain | undefined;

    // paxeer-network testnet supported chain
    if (networkId === "testnet") {
      chain = findByChainId(
        isTestnetChain(srcChainId) ? srcChainId! : ARBITRUM_TESTNET_CHAINID,
      ) as API.Chain;
    } else {
      chain = findByChainId(srcChainId!) as API.Chain;
      // if is paxeer-network un-supported chain
      if (!chain?.network_infos?.bridgeless) {
        // paxeer-network mainnet supported chain
        chain = findByChainId(ARBITRUM_MAINNET_CHAINID) as API.Chain;
      }
    }
    return chain;
  }, [networkId, findByChainId, isTestnetChain, srcChainId]);

  return targetChain;
}

function checkIfChainTokenNeedRestApprove(chainId: number, token: string) {
  // check chain (except ethereum mainnet)
  return chainId === ETHEREUM_MAINNET_CHAINID && token === "USDT";
}

// When no SPL token found, get balance will throw TokenAccountNotFoundError
function ignoreBalanceError(options: {
  token: string;
  chainNamespace: string;
  err: any;
}) {
  const { token, chainNamespace, err } = options;
  return (
    chainNamespace === ChainNamespace.solana &&
    err?.name === "TokenAccountNotFoundError"
  );
}
