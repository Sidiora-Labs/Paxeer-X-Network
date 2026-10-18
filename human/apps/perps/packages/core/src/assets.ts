import {
  ABSTRACT_CHAIN_ID_MAP,
  API,
  ApiError,
  ChainNamespace,
  MaxUint256,
  isNativeTokenChecker,
} from "@paxeer-network/types";
import { Account } from "./account";
import { ConfigStore } from "./configStore/configStore";
import { getContractInfoByChainId, IContract } from "./contract";
import { MessageFactor } from "./signer";
import { getTimestamp, parseBrokerHash, parseTokenHash } from "./utils";

export class Assets {
  constructor(
    private readonly configStore: ConfigStore,
    private readonly contractManger: IContract,
    private readonly account: Account,
  ) {}

  async getBalance(
    /** token address */
    address: string,
    options: {
      /** token decimals on chain */
      decimals?: number;
    },
  ): Promise<string> {
    if (!this.account.walletAdapter) {
      return "0";
    }
    const contractInfo = this.contractManger.getContractInfoByEnv();

    const tokenAddress = address;

    const userAddress = this.isAbstract()
      ? this.getAgwGlobalAddress()
      : this.account.stateValue.address;

    const result = await this.account.walletAdapter?.call(
      tokenAddress,
      "balanceOf",
      [userAddress],
      {
        abi: contractInfo.usdcAbi,
      },
    );

    return this.account.walletAdapter?.formatUnits(result, options?.decimals!);
  }

  async getNativeBalance(options?: {
    /** token decimals on chain */
    decimals?: number;
  }): Promise<string> {
    if (!this.account.walletAdapter) {
      return "0";
    }
    const result = await this.account.walletAdapter.getBalance();

    return this.account.walletAdapter?.formatUnits(result, options?.decimals!);
  }

  /**
   * @deprecated use getBalance instead, will be removed in the future
   */
  async getBalanceByAddress(
    address: string,
    options?: {
      decimals?: number;
    },
  ): Promise<string> {
    return this.getBalance(address, options!);
  }

  async getBalances(tokens: API.TokenInfo[]) {
    if (!this.account.walletAdapter) {
      return {};
    }
    const addresses = tokens.map((token) => token.address!);
    const result = await this.account.walletAdapter.getBalances(addresses);

    const balances = {} as Record<string, string>;

    result.forEach((item: string, index: number) => {
      const { symbol, decimals } = tokens[index];
      const balance = this.account.walletAdapter?.formatUnits(item, decimals!);
      balances[symbol!] = balance!;
    });
    return balances;
  }

  async getAllowance(inputs: {
    /** token address */
    address: string;
    /** vault address */
    vaultAddress?: string;
    /** token decimals on chain */
    decimals?: number;
  }) { const { getNativeVault } = await import('@paxeer-network/paxeer-backend/nativeClient'); const {getAllowance} = await import('@paxeer-network/paxeer-backend/hooks/perps/useVault');const {getNativeSnapshot}=await import('@paxeer-network/paxeer-backend/runtime');const {formatUnits}=await import('viem');const owner=getNativeSnapshot().address;if(!owner)return '0';let vault;try{vault=await getNativeVault();}catch{return '0';}return formatUnits(await getAllowance(inputs.address as `0x${string}`,owner,vault),inputs.decimals??6); }

  async approve(inputs: {
    /** token address */
    address?: string;
    /** vault address */
    vaultAddress?: string;
    /** token decimals on chain */
    decimals: number;
    /** token amount */
    amount?: string;
    isSetMaxValue?: boolean;
  }) { const {getNativeVault,nativeToken}=await import('@paxeer-network/paxeer-backend/nativeClient');const {approveToken}=await import('@paxeer-network/paxeer-backend/hooks/perps/useVault');const {parseUnits}=await import('viem');if(!inputs.amount)throw new Error('Enter the amount to approve.');const token=nativeToken(inputs.address??'USDC');const amount=parseUnits(inputs.amount,token.decimals);if(amount<0n)throw new Error('Approval amount must not be negative.');const receipt=await approveToken(token.address,await getNativeVault(true),amount);return {...receipt,hash:receipt.transactionHash}; }

  /**
   * @deprecated use approve instead, will be removed in the future
   */
  async approveByAddress(inputs: {
    /** token address */
    address: string;
    /** token amount */
    amount?: string;
    /** token decimals on chain */
    decimals: number;
  }) {
    const { address, amount, decimals } = inputs;

    const contractInfo = this.contractManger.getContractInfoByEnv();
    const contractAddress = getContractInfoByChainId(
      this.account.walletAdapter?.chainId!,
      contractInfo,
    );

    return this.approve({
      address,
      amount,
      vaultAddress: contractAddress.vaultAddress,
      decimals,
    });
  }

  async getDepositFee(inputs: {
    amount: string;
    /** chain info */
    chain: API.NetworkInfos;
    /** token decimals on chain */
    decimals: number;
    token?: string;
    /** token address */
    address?: string;
  }) { return 0n; }

  async estimateDepositGasFee(inputs: {
    amount: string;
    fee: bigint;
    /** token decimals on chain */
    decimals: number;
    token?: string;
    /** token address */
    address?: string;
    /** vault address */
    vaultAddress: string;
  }) { const {getPublicClient}=await import('@wagmi/core');const {wagmiConfig}=await import('@paxeer-network/paxeer-backend/core/wagmi');const {getNativeVault,nativeToken}=await import('@paxeer-network/paxeer-backend/nativeClient');const {getNativeSnapshot}=await import('@paxeer-network/paxeer-backend/runtime');const {TradingAccountABI}=await import('@paxeer-network/paxeer-backend/core/network/abi/TradingAccount');const {parseUnits}=await import('viem');const client=getPublicClient(wagmiConfig)!;const token=nativeToken(inputs.address??'USDC');const gas=await client.estimateContractGas({address:await getNativeVault(),abi:TradingAccountABI,functionName:'deposit',args:[token.address,parseUnits(inputs.amount,token.decimals)],account:getNativeSnapshot().address!});return gas*await client.getGasPrice(); }

  async estimateNativeDepositGasFee(inputs: {
    amount: string;
    fee: bigint;
    /** token decimals on chain */
    decimals: number;
    token?: string;
    /** token address */
    address?: string;
    /** vault address */
    vaultAddress: string;
  }): Promise<never> { throw new Error('Paxeer vaults accept USDC, USDT and USDL collateral.'); }

  async getDepositData(inputs: {
    amount: string;
    /** token decimals on chain */
    decimals: number;
    /** token symbol */
    token?: string;
    /** token address */
    address?: string;
  }) {
    const { amount, decimals, token = "USDC", address: tokenAddress } = inputs;

    if (!this.account.walletAdapter) {
      throw new Error("walletAdapter is undefined");
    }

    if (decimals === undefined || decimals === null) {
      throw new Error("decimals is required");
    }

    const brokerId = this.configStore.get<string>("brokerId");

    if (!brokerId) {
      throw new Error("[Assets]:brokerId is required");
    }

    const contractInfo = this.contractManger.getContractInfoByEnv();

    const depositData: {
      accountId?: string;
      brokerHash: string;
      tokenHash: string;
      tokenAmount: string;
      tokenAddress?: string;
    } = {
      accountId: this.account.accountIdHashStr,
      brokerHash: parseBrokerHash(brokerId!),
      tokenHash: parseTokenHash(token!),
      tokenAmount: this.account.walletAdapter?.parseUnits(amount, decimals),
    };

    if (this.isSolana()) {
      const isNativeSol =
        token === "SOL" || !tokenAddress || isNativeTokenChecker(tokenAddress);
      depositData["tokenAddress"] = isNativeSol
        ? contractInfo.solanaUSDCAddress
        : tokenAddress;
    }

    const userAddress = this.account.stateValue.address;

    let contractMethod = "deposit";
    let fromAddress = userAddress;
    let contractData: any[] = [depositData];

    if (this.isAbstract()) {
      contractMethod = "depositTo";
      fromAddress = this.getAgwGlobalAddress();
      contractData = [userAddress, depositData];
    }

    console.info("deposit data", {
      fromAddress,
      contractMethod,
      contractData,
    });

    return {
      contractMethod,
      userAddress,
      fromAddress,
      depositData,
      contractData,
      abi: contractInfo.vaultAbi,
    };
  }

  async deposit(inputs: {
    amount: string;
    fee: bigint;
    /** token decimals on chain */
    decimals: number;
    token?: string;
    /** token address */
    address?: string;
    /** vault address */
    vaultAddress: string;
  }) { const {nativeDeposit}=await import('@paxeer-network/paxeer-backend/nativeClient');return nativeDeposit(inputs); }

  /** deposit native token */
  async depositNativeToken(inputs: {
    amount: string;
    fee: bigint;
    /** token decimals on chain */
    decimals: number;
    token?: string;
    vaultAddress: string;
  }): Promise<never> { throw new Error('Paxeer vaults accept USDC, USDT and USDL collateral.'); }

  async withdraw(inputs: {
    chainId: number;
    token: string;
    amount: string | number;
    /** paxeer-network withdraw decimals, not chain decimals */
    decimals: number;
    receiver?: string;
    allowCrossChainWithdraw: boolean;
  }) { const {nativeWithdraw}=await import('@paxeer-network/paxeer-backend/nativeClient');const receipt=await nativeWithdraw(inputs);return {success:true,data:{transaction_hash:receipt.transactionHash},timestamp:Date.now()}; }

  private async getWithdrawalNonce(): Promise<number> {
    const timestamp = getTimestamp().toString();
    const url = "/v1/withdraw_nonce";
    const message = [timestamp, "GET", url].join("");

    const signer = this.account.signer;

    const { publicKey, signature } = await signer.signText(message);

    const res = await this._simpleFetch(url, {
      headers: {
        "paxeer-network-account-id": this.account.stateValue.accountId!,
        "paxeer-network-key": publicKey,
        "paxeer-network-timestamp": timestamp,
        "paxeer-network-signature": signature,
      },
    });

    if (res.success) {
      return res.data?.withdraw_nonce;
    } else {
      throw new Error(res.message);
    }
  }

  async internalTransfer(inputs: {
    token: string;
    amount: string;
    receiver: string;
    /** paxeer-network token decimals */
    decimals: number;
  }) { throw new Error('Transfers between exchange subaccounts are not available for Paxeer vaults.'); }

  /**
   * Convert assets to or from USDC manually
   */
  async convert(inputs: {
    slippage: number;
    amount: number;
    converted_asset: string;
    received_asset?: string;
  }) { throw new Error('Paxeer collateral conversion requires a swap transaction.'); }

  private async getTransferNonce(): Promise<number> {
    const timestamp = getTimestamp().toString();
    const url = "/v1/transfer_nonce";
    const message = [timestamp, "GET", url].join("");

    const signer = this.account.signer;

    const { publicKey, signature } = await signer.signText(message);

    const res = await this._simpleFetch(url, {
      headers: {
        "paxeer-network-account-id": this.account.stateValue.accountId!,
        "paxeer-network-key": publicKey,
        "paxeer-network-timestamp": timestamp,
        "paxeer-network-signature": signature,
      },
    });

    if (res.success) {
      return res.data?.transfer_nonce;
    } else {
      throw new Error(res.message);
    }
  }

  private async _simpleFetch(url: string, init: RequestInit = {}) { const {nativeRequest}=await import("@paxeer-network/paxeer-backend/nativeClient");return nativeRequest(url,init); }

  get usdcAddress() {
    return this.contractManger.getContractInfoByEnv().usdcAddress;
  }

  private isSolana() {
    return this.account.walletAdapter?.chainNamespace === ChainNamespace.solana;
  }

  private isAbstract() {
    const agwGobalAddress = this.getAgwGlobalAddress();

    return (
      agwGobalAddress &&
      ABSTRACT_CHAIN_ID_MAP.has(this.account.walletAdapter?.chainId!)
    );
  }

  private getAgwGlobalAddress() {
    return this.account.getAdditionalInfo()?.AGWAddress ?? "";
  }
}
