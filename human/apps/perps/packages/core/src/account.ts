import { PublicKey } from "@solana/web3.js";
import { AbiCoder, keccak256 } from "ethers";
import EventEmitter from "eventemitter3";
import {
  AccountStatusEnum,
  SDKError,
  ChainNamespace,
  API,
  DEFAUL_PAXEER_NETWORK_KEY_SCOPE,
  EMPTY_LIST,
} from "@paxeer-network/types";
import { AdditionalInfoRepository } from "./additionalInfoRepository";
import { Assets } from "./assets";
import { ConfigStore } from "./configStore/configStore";
import { EVENT_NAMES } from "./constants";
import { BaseContract, IContract } from "./contract";
import { BasePaxeerNetworkKeyPair, PaxeerNetworkKeyPair } from "./keyPair";
import { PaxeerNetworkKeyStore } from "./keyStore";
import { LocalStorageRepository } from "./repository";
import { BaseSigner, MessageFactor, Signer } from "./signer";
import { SubAccount } from "./subAccount";
import {
  getTimestamp,
  isHex,
  parseAccountId,
  parseBrokerHash,
  SignatureDomain,
} from "./utils";
import { WalletAdapter } from "./wallet/walletAdapter";
import { WalletAdapterManager } from "./walletAdapterManager";

export interface AccountState {
  status: AccountStatusEnum;

  // checking: boolean;

  /**
   * whether the account is validating
   */
  validating: boolean;
  /**
   * whether the account is revalidating
   */
  // revalidating?: boolean;

  /**
   * The current active account ID
   * Can be either a root account ID or sub-account ID
   * @since 2.3.0
   */
  accountId?: string;
  /**
   * root account id
   * @since 2.3.0
   */
  mainAccountId?: string;
  /**
   * sub account id
   * @since 2.3.0
   */
  subAccountId?: string | undefined;
  userId?: string;
  address?: string;
  chainNamespace?: ChainNamespace;
  /** new account */
  isNew?: boolean;

  connectWallet?: {
    name: string;
    chainId: number;
  };

  /**
   * sub accounts
   * @since 2.3.0
   */
  subAccounts?: SubAccount[];

  // balance: string;
  // leverage: number;
}

/**
 * Account
 * @example
 * ```ts
 * const account = new Account();
 * account.login("0x1234567890");
 * ```
 */
export class Account {
  static instanceName = "account";
  static additionalInfoRepositoryName = "paxeerNetwork_walletAdditionalInfo";
  static ACTIVE_SUB_ACCOUNT_ID_KEY = "ACTIVE_SUB_ACCOUNT_ID";
  // private walletClient?: WalletClient;
  private _singer?: Signer;

  private _ee = new EventEmitter();

  private walletAdapterManager: WalletAdapterManager;

  assetsManager: Assets;

  private additionalInfoRepository: AdditionalInfoRepository;

  private _state: AccountState = {
    status: AccountStatusEnum.NotConnected,
    // balance: "",
    // checking: false,
    validating: false, // if address exists, validating is available
    // leverage: Number.NaN,
    isNew: false,
  };

  private readonly contractManger;

  constructor(
    private readonly configStore: ConfigStore,
    readonly keyStore: PaxeerNetworkKeyStore,
    // private readonly getWalletAdapter: getWalletAdapterFunc, // private readonly walletAdapterClass: { new (options: any): WalletAdapter } // private walletClient?: WalletClient
    walletAdapters: WalletAdapter[],
    options?: Partial<{
      /**
       * smart contract configuration class
       * provide contract address and abi
       */
      contracts: IContract;
    }>,
  ) {
    // this._initState();

    if (options?.contracts) {
      this.contractManger = options.contracts;
    } else {
      this.contractManger = new BaseContract(configStore);
    }

    this.assetsManager = new Assets(configStore, this.contractManger, this);
    this.walletAdapterManager = new WalletAdapterManager(walletAdapters);

    this.additionalInfoRepository = new AdditionalInfoRepository(
      new LocalStorageRepository(Account.additionalInfoRepositoryName),
    );

    this._bindEvents();
  }

  logout() {
    // ...
  }

  async setAddress(
    address: string,
    wallet: {
      provider: any;
      chain: { id: string | number; namespace: ChainNamespace };
      wallet: {
        name: string;
      };
      [key: string]: any;
    },
    options?: {
      /**
       * @deprecated will be remove this in next minor version
       */
      validatePaxeerNetworkKeyScope?: (scope: string) => boolean;
    },
  ): Promise<AccountStatusEnum> { if (!address || !wallet?.provider) throw new SDKError("Connect a Paxeer wallet first.");
 const chainId=this.parseChainId(wallet.chain.id);this.keyStore.setAddress(address);
 this.walletAdapterManager.switchWallet(wallet.chain.namespace,address,chainId,{provider:wallet.provider,contractManager:this.contractManger});
 const nextState:AccountState={status:AccountStatusEnum.EnableTrading,validating:false,address,accountId:address,mainAccountId:address,isNew:false,chainNamespace:wallet.chain.namespace,connectWallet:{name:wallet.wallet.name,chainId},subAccounts:[]};
 this.updateState(nextState);this._ee.emit(EVENT_NAMES.statusChanged,nextState);return nextState.status; }
  private saveAdditionalInfo(
    address: string,
    wallet: {
      [key: string]: any;
      additionalInfo?: Record<string, any>;
    },
  ) {
    if (typeof wallet.additionalInfo !== "undefined") {
      if (typeof wallet.additionalInfo !== "object") {
        console.warn("`wallet.additionalInfo` is not an object");
      } else {
        this.additionalInfoRepository.save(address, wallet.additionalInfo);
      }
    }
  }

  get stateValue(): AccountState {
    // return this._state$.getValue();
    return this._state;
  }

  get accountId(): string | undefined {
    const state = this.stateValue;
    return state.subAccountId || state.mainAccountId;
  }

  get mainAccountId(): string | undefined {
    const state = this.stateValue;
    return state.mainAccountId;
  }

  get subAccountId(): string | undefined {
    const state = this.stateValue;
    return state.subAccountId;
  }

  get isSubAccount(): boolean {
    const state = this.stateValue;
    // Not connected / no active account → not a sub-account
    if (!state.accountId || !state.mainAccountId) {
      return false;
    }
    return state.mainAccountId !== state.accountId;
  }

  get accountIdHashStr(): string | undefined {
    if (!this.address) {
      throw new Error("address is error");
    }

    // if (!this.configStore.get("brokerId")) {
    //   throw new Error("brokerId is undefined");
    // }
    const brokerId = this.configStore.get<string>("brokerId");
    if (this.walletAdapter?.chainNamespace === ChainNamespace.solana) {
      const userAccount = new PublicKey(this.address);
      const decodedUserAccount = Buffer.from(userAccount.toBytes());
      const abicoder = AbiCoder.defaultAbiCoder();
      return keccak256(
        abicoder.encode(
          ["bytes32", "bytes32"],
          [decodedUserAccount, parseBrokerHash(brokerId)],
        ),
      );
    }
    return parseAccountId(this.address, brokerId);
  }

  get address(): string | undefined {
    return this.stateValue.address;
  }

  get chainId(): number | string | undefined {
    // return this.walletClient?.chainId;
    if (!this.walletAdapterManager.isAdapterExist) {
      return;
    }
    return this.walletAdapterManager.chainId;
  }
  get apiBaseUrl(): string | undefined {
    return this.configStore.get("apiBaseUrl");
  }

  private updateState(state: AccountState) {
    this._state = state;
  }

  private _bindEvents() {
    this._ee.addListener(EVENT_NAMES.statusChanged, (state: AccountState) => {
      this._state = state;
    });
  }

  // Check account status
  private async _checkAccount(
    address: string,
    options?: {
      /**
       * @deprecated will be remove this in next minor version
       */
      validatePaxeerNetworkKeyScope?: (scope: string) => boolean;
    },
  ): Promise<AccountStatusEnum> {
    // if (!this.walletClient) return;
    //
    let nextState: AccountState;
    try {
      // check account exists
      const accountInfo = await this._checkAccountExist(address);
      //

      if (accountInfo && accountInfo.account_id) {
        this.keyStore.setAccountId(address, accountInfo.account_id);
        // this.keyStore.setAddress(address);

        nextState = {
          ...this.stateValue,
          status: AccountStatusEnum.SignedIn,
          accountId: accountInfo.account_id,
          mainAccountId: accountInfo.account_id,
          userId: accountInfo.user_id,
        };
        this._ee.emit(EVENT_NAMES.statusChanged, nextState);
        //
      } else {
        nextState = {
          ...this.stateValue,
          validating: false,
          status: AccountStatusEnum.NotSignedIn,
        };

        this._ee.emit(EVENT_NAMES.statusChanged, nextState);

        return AccountStatusEnum.NotSignedIn;
      }
      // check paxeerNetworkKey state
      // get paxeerNetworkKey from keyStore
      // const paxeerNetworkKey = this.keyStore.getPaxeerNetworkKey(address);
      const paxeerNetworkKey = this.keyStore.getPaxeerNetworkKey();

      // nextState = {
      //   ...this.stateValue,
      //   isNew: false,
      //   validating: false,
      //   status: AccountStatusEnum.DisabledTrading,
      // };

      if (!paxeerNetworkKey) {
        nextState = {
          ...this.stateValue,
          isNew: false,
          validating: false,
          status: AccountStatusEnum.DisabledTrading,
        };
        this._ee.emit(EVENT_NAMES.statusChanged, nextState);

        return AccountStatusEnum.DisabledTrading;
      }

      const publicKey = await paxeerNetworkKey.getPublicKey();

      const paxeerNetworkKeyStatus = await this._checkPaxeerNetworkKeyState(
        accountInfo.account_id,
        publicKey,
      );

      if (
        paxeerNetworkKeyStatus &&
        paxeerNetworkKeyStatus.paxeerNetwork_key &&
        paxeerNetworkKeyStatus.key_status === "ACTIVE"
      ) {
        const now = getTimestamp();
        const expiration = paxeerNetworkKeyStatus.expiration;

        const validScope =
          typeof options?.validatePaxeerNetworkKeyScope === "function"
            ? options.validatePaxeerNetworkKeyScope(paxeerNetworkKeyStatus.scope)
            : true;

        if (now > expiration || !validScope) {
          // paxeerNetworkKey is expired, remove paxeerNetworkKey
          nextState = {
            ...this.stateValue,
            validating: false,
            status: AccountStatusEnum.DisabledTrading,
          };
          this._ee.emit(EVENT_NAMES.statusChanged, nextState);
          this.keyStore.cleanKey(address, "paxeerNetworkKey");
          return AccountStatusEnum.DisabledTrading;
        }

        /**
         * Fetch sub-accounts list when trading is enabled for this account
         */
        // const subAccounts = await this.getSubAccounts();

        // nextState = {
        //   ...this.stateValue,
        //   validating: false,
        //   status: AccountStatusEnum.EnableTrading,
        // };

        // if (subAccounts.length) {
        //   // load active sub-account from local storage
        //   const activeSubAccount = this.additionalInfoRepository.get(
        //     address,
        //     Account.ACTIVE_SUB_ACCOUNT_ID_KEY
        //   );

        //   nextState.subAccounts = subAccounts.map(
        //     (subAccount: {
        //       sub_account_id: string;
        //       description: string;
        //       holding: API.Holding[];
        //     }) => ({
        //       id: subAccount.sub_account_id,
        //       description: subAccount.description,
        //       holding: subAccount.holding,
        //     })
        //   );

        //   if (activeSubAccount) {
        //     // check if the activeSubAccount is included in the subAccounts
        //     if (
        //       nextState.subAccounts?.find(
        //         (subAccount) => subAccount.id === activeSubAccount
        //       )
        //     ) {
        //       nextState.subAccountId = activeSubAccount;
        //       nextState.accountId = activeSubAccount;
        //     }
        //   }
        // }

        nextState = await this._restoreSubAccount();

        this._ee.emit(EVENT_NAMES.statusChanged, nextState);

        return AccountStatusEnum.EnableTrading;
      }

      this._ee.emit(EVENT_NAMES.statusChanged, {
        ...this.stateValue,
        validating: false,
        // status: AccountStatusEnum.DisabledTrading,
      });

      this.keyStore.cleanKey(address, "paxeerNetworkKey");

      return AccountStatusEnum.NotConnected;
    } catch (err) {
      // return this.stateValue.status;
      this._ee.emit(EVENT_NAMES.statusChanged, {
        ...this.stateValue,
        validating: false,
        // status: AccountStatusEnum.DisabledTrading,
      });
    }

    return AccountStatusEnum.NotSignedIn;
  }

  private async _checkAccountExist(
    address: string,
    chainNamespace?: string,
  ): Promise<{ account_id: string; user_id: string } | null> {
    // const brokerId = this.configStore.get("brokerId");
    const res = await this._getAccountInfo(address, chainNamespace);

    if (res.success) {
      return res.data;
    } else {
      // throw new Error(res.message);
      return null;
    }
  }

  private async _restoreSubAccount(): Promise<AccountState> {
    const subAccounts = await this.getSubAccounts();

    const nextState = {
      ...this.stateValue,
      validating: false,
      status: AccountStatusEnum.EnableTrading,
      subAccounts: [] as SubAccount[],
      subAccountId: undefined,
    };

    if (subAccounts.length) {
      // load active sub-account from local storage
      const activeSubAccount = this.additionalInfoRepository.get(
        this.stateValue.address!,
        Account.ACTIVE_SUB_ACCOUNT_ID_KEY,
      );

      nextState.subAccounts = subAccounts.map((subAccount) => ({
        id: subAccount.sub_account_id,
        description: subAccount.description,
        holding: subAccount.holding,
      }));

      if (activeSubAccount) {
        // check if the activeSubAccount is included in the subAccounts
        if (
          nextState.subAccounts?.find(
            (subAccount) => subAccount.id === activeSubAccount,
          )
        ) {
          nextState.subAccountId = activeSubAccount;
          nextState.accountId = activeSubAccount;
        }
      }
    }

    return nextState;
  }

  async restoreSubAccount(): Promise<AccountState> { return this.stateValue; }

  async createAccount(): Promise<any> { if(!this.address) throw new SDKError("Connect your Paxeer wallet first.");return this.accountId; }

  async createSubAccount(description: string = ""): Promise<any> {
    if (!this.stateValue.mainAccountId) {
      throw new Error("mainAccountId is undefined");
    }

    const url = "/v1/client/add_sub_account";

    const data = {
      description,
    };

    const signature = await this.signData(url, data);

    const res = await this._simpleFetch(url, {
      method: "POST",
      body: JSON.stringify(data),
      headers: {
        // "X-Account-Id": this.stateValue.mainAccountId,
        "paxeer-network-account-id": this.stateValue.mainAccountId,
        "Content-Type": "application/json",
        ...signature,
      },
    });

    if (res.success) {
      const subAccount = {
        id: res.data.sub_account_id,
        description,
        holding: [],
      };

      const nextState = { ...this._state };

      // if (!Array.isArray(nextState.subAccounts)) {
      //   nextState.subAccounts = [];
      // }
      nextState.subAccounts = [...(nextState.subAccounts ?? []), subAccount];

      this._ee.emit(EVENT_NAMES.subAccountCreated, subAccount);
      this._ee.emit(EVENT_NAMES.statusChanged, nextState);

      return res.data;
    } else {
      throw new Error(res.message);
    }
  }

  async updateSubAccount({
    subAccountId,
    description = "",
  }: {
    subAccountId: string;
    description?: string;
  }) {
    if (!this.stateValue.mainAccountId) {
      throw new Error("mainAccountId is undefined");
    }

    if (description?.trim() !== "") {
      // Validate description format: up to 50 characters, only allows English characters,
      // numbers, @, comma, space, underscore and dash
      const descriptionRegex = /^[a-zA-Z0-9@,\s_\-]{0,50}$/;
      if (description && !descriptionRegex.test(description)) {
        throw new Error(
          "Description must be up to 50 characters and can only contain English characters, numbers, @, comma, space, underscore and dash.",
        );
      }
    }

    // check the subAccountId is included in the subAccounts
    const subAccount = this.stateValue.subAccounts?.find(
      (subAccount) => subAccount.id === subAccountId,
    );
    if (!subAccount) {
      throw new Error(
        `Sub-account with ID ${subAccountId} not found. Please verify the sub-account ID and try again.`,
      );
    }

    const url = "/v1/client/update_sub_account";

    const data = {
      sub_account_id: subAccountId,
      description,
    };

    const signature = await this.signData(url, data);

    const res = await this._simpleFetch(url, {
      method: "POST",
      body: JSON.stringify(data),
      headers: {
        "paxeer-network-account-id": this.stateValue.mainAccountId,
        "Content-Type": "application/json",
        ...signature,
      },
    });

    if (res.success) {
      const nextState = { ...this._state };
      nextState.subAccounts = nextState.subAccounts?.map((subAccount) => {
        if (subAccount.id === subAccountId) {
          return { ...subAccount, description: description ?? "" };
        }
        return subAccount;
      });

      this._ee.emit(EVENT_NAMES.subAccountUpdated, data);

      this._ee.emit(EVENT_NAMES.statusChanged, nextState);
      return res.data;
    } else {
      throw new Error(res.message);
    }
  }

  switchAccount(accountId: string) { if(accountId!==this.accountId) throw new SDKError("Subaccounts are not available on the configured Paxeer contracts.");return this.stateValue; }

  async getSubAccounts(): Promise<
    ({
      sub_account_id: string;
      description: string;
    } & { holding: API.Holding[] })[]
  > { return []; }

  async getSubAccountBalances(): Promise<Record<string, API.Holding[]>> { return {}; }

  /**
   * refresh sub account balances
   * @since 2.3.0
   */
  async refreshSubAccountBalances(): Promise<Record<string,API.Holding[]>> { return {}; }

  /**
   * @since 2.3.0
   * if you are using main account paxeer-network key, you can create api key for sub account
   */
  async createSubAccountApiKey(
    /**
     * expiration days
     */
    expiration: number,
    options: {
      tag?: string;
      scope?: string;
      subAccountId: string;
    },
  ) {
    try {
      const { res, keyPair } = await this.generateApiKey(expiration, options);
      if (res.success) {
        const key = await keyPair.getPublicKey();
        return {
          key: key.replace("ed25519:", ""),
          secretKey: keyPair.secretKey?.replace("ed25519:", ""),
        };
      }
    } catch (e) {
      console.log("createApiKey error", e);

      if (`${e}`.includes("user rejected action")) {
        throw new Error("User rejected the request.");
      }

      throw e;
    }
    throw new Error("create api key failed");
  }

  async createApiKey(
    /**
     * expiration days
     */
    expiration: number,
    options?: {
      tag?: string;
      scope?: string;
    },
  ) {
    try {
      const { res, keyPair } = await this.generateApiKey(expiration, options);
      if (res.success) {
        const key = await keyPair.getPublicKey();
        return {
          key: key.replace("ed25519:", ""),
          secretKey: keyPair.secretKey?.replace("ed25519:", ""),
        };
      }
    } catch (e) {
      console.log("createApiKey error", e);

      if (`${e}`.includes("user rejected action")) {
        throw new Error("User rejected the request.");
      }

      throw e;
    }
    throw new Error("create api key failed");
  }

  async createPaxeerNetworkKey(
    expiration?: number,
    options?: {
      tag?: string;
      scope?: string;
    },
  ): Promise<any> { throw new SDKError("paxeer-network signs contract transactions with the connected wallet; a separate trading key is not required."); }

  private async generateApiKey(
    expiration?: number,
    options?: {
      tag?: string;
      scope?: string;
      /**
       * @since 2.3.0
       * if you are main account, you can pass the subAccountId to create the api key for the sub account
       * */
      subAccountId?: string;
    },
  ) {
    if (this.stateValue.mainAccountId === undefined) {
      throw new Error("account id is undefined");
    }

    // if (!this.walletAdapterManager) {
    //   throw new Error("walletAdapterManager is undefined");
    // }

    if (!this.walletAdapter) {
      throw new Error("walletAdapter is undefined");
    }

    if (!this.walletAdapterManager.isAdapterExist) {
      throw new Error("wallet adapter is not exist");
    }

    if (typeof expiration !== "number") {
      throw new Error("the 'expiration' must be valid number");
    }

    // const primaryType = "AddPaxeerNetworkKey";
    // const keyPair = this.keyStore.generateKey();
    const secretKey = this.walletAdapter.generateSecretKey();

    const keyPair = new BasePaxeerNetworkKeyPair(secretKey);
    const publicKey = await keyPair.getPublicKey();

    const address = this.stateValue.address;

    if (!address) {
      throw new Error("address is undefined");
    }

    const { message, signatured } =
      await this.walletAdapter.generateAddPaxeerNetworkKeyMessage({
        // chainId: number;
        brokerId: this.configStore.get("brokerId"),
        publicKey,
        expiration,
        timestamp: getTimestamp(),
        // domain: any;
        scope: options?.scope || DEFAUL_PAXEER_NETWORK_KEY_SCOPE,
        tag: options?.tag,
        subAccountId: options?.subAccountId,
      });

    // console.log("generateAPiKey", publicKey, address);

    const res = await this._simpleFetch("/v1/paxeerNetwork_key", {
      method: "POST",
      body: JSON.stringify({
        signature: signatured,
        message,
        userAddress: address,
      }),
      headers: {
        // TODO: remove this header
        "X-Account-Id": this.stateValue.mainAccountId,
        "Content-Type": "application/json",
      },
    });

    return { res, address, keyPair };
  }

  async importPaxeerNetworkKey(options: {
    secretKey: string;
    address: string;
    chainNamespace: ChainNamespace;
  }) {
    const { address, secretKey, chainNamespace } = options;
    if (!address || !secretKey || !chainNamespace) return;

    const accountInfo = await this._checkAccountExist(address, chainNamespace);
    const accountId = accountInfo?.account_id;

    if (!accountId) return;

    const paxeerNetworkKey = new BasePaxeerNetworkKeyPair(secretKey);
    const res = await this.checkPaxeerNetworkKey(address, paxeerNetworkKey, accountId);
    if (res) {
      this.configStore.set("chainNamespace", chainNamespace);
    }

    return res;
  }

  async checkPaxeerNetworkKey(
    address: string,
    paxeerNetworkKey: PaxeerNetworkKeyPair,
    accountId: string,
  ) {
    if (!address || !paxeerNetworkKey || !accountId) return;
    const publicKey = await paxeerNetworkKey.getPublicKey();

    const paxeerNetworkKeyStatus = await this._checkPaxeerNetworkKeyState(
      accountId,
      publicKey,
    );

    const { paxeerNetwork_key, key_status, expiration } = paxeerNetworkKeyStatus || {};
    const now = getTimestamp();

    if (paxeerNetwork_key && key_status === "ACTIVE" && expiration > now) {
      this.keyStore.setAddress(address);

      this.keyStore.setKey(address, paxeerNetworkKey);
      this.keyStore.setAccountId(address, accountId);

      const nextState = {
        ...this.stateValue,
        address,
        accountId,
        mainAccountId: accountId,
        // userId: accountInfo.user_id,
        status: AccountStatusEnum.EnableTradingWithoutConnected,
      };
      // should update state first, because get sub accounts need to use the main account id
      this.updateState(nextState);
      const subAccountState = await this._restoreSubAccount();
      this._ee.emit(EVENT_NAMES.statusChanged, {
        ...nextState,
        subAccounts: subAccountState.subAccounts,
        subAccountId: undefined,
      });

      return true;
    }
  }

  /**
   * @since 2.3.0
   * settle sub account pnl
   */
  async settleSubAccount(options?: {
    /**
     * if you are main account and want to settle the sub account, you need to pass the subAccountId
     * if you are sub account, not need to pass the subAccountId
     * */
    subAccountId?: string;
  }): Promise<any> {
    const subAccountId = options?.subAccountId || this.stateValue.accountId;
    const nonce = await this._getSettleNonce(subAccountId);
    const url = "/v1/sub_account_settle_pnl";

    const data = {
      settle_nonce: nonce,
    };

    const signature = await this.signData(url, data);

    const res = await this._simpleFetch(url, {
      method: "POST",
      body: JSON.stringify(data),
      headers: {
        "Content-Type": "application/json",
        "paxeer-network-account-id": subAccountId!,
        ...signature,
      },
    });

    if (res.success) {
      return res;
    } else {
      throw res;
    }
  }

  /** set main account pnl */
  async settle(options?: {
    /**
     * if you are main account, you don't need to pass the accountId
     * if you are sub account and you want to settle the main account, you need to pass main account id
     * */
    accountId?: string;
  }): Promise<any> { throw new SDKError("Paxeer position PnL settles through the position contract when a position is closed."); }

  private async signData(url: string, data: any) {
    const payload: MessageFactor = {
      method: "POST",
      url,
      data,
    };

    const signature = await this.signer.sign(payload, getTimestamp());
    // const signature = await this.signMessageFactor(payload);
    return signature;
  }

  async destroyPaxeerNetworkKey(): Promise<void> {
    if (!!this.stateValue.address) {
      // const key = await this.keyStore.getPaxeerNetworkKey()?.getPublicKey();
      this.keyStore.cleanKey(this.stateValue.address, "paxeerNetworkKey");
      // TODO: remove sub-account
    }

    const nextState = {
      ...this.stateValue,
      status: AccountStatusEnum.DisabledTrading,
    };
    this._ee.emit(EVENT_NAMES.statusChanged, nextState);
  }

  async disconnect(): Promise<void> { this.updateState({status:AccountStatusEnum.NotConnected,validating:false,address:undefined,accountId:undefined,mainAccountId:undefined,subAccountId:undefined,subAccounts:[]});this._ee.emit(EVENT_NAMES.statusChanged,this.stateValue); }

  switchChainId(chainId: number | string) {
    chainId = this.parseChainId(chainId);

    const nextState: AccountState = {
      ...this.stateValue,
      connectWallet: {
        // ...wallet?.wallet,
        ...this.stateValue.connectWallet!,
        chainId: chainId,
      },
      // revalidating: this._state.validating,
    };

    if (this.walletAdapter) {
      this.walletAdapter.chainId = chainId as number;
    }

    this._ee.emit(EVENT_NAMES.statusChanged, nextState);
  }

  private parseChainId(chainId: string | number): number {
    if (
      typeof chainId === "string" &&
      (isHex(chainId) || (chainId.startsWith("0x") && isHex(chainId.slice(2))))
    ) {
      chainId = parseInt(chainId, 16);
    }
    return chainId as number;
  }

  private async _checkPaxeerNetworkKeyState(accountId: string, paxeerNetworkKey: string) {
    const res = await this._simpleFetch(
      `/v1/get_paxeerNetwork_key?account_id=${accountId}&paxeerNetwork_key=${paxeerNetworkKey}`,
    );

    if (res.success) {
      return res.data;
    } else {
      // throw new Error(res.message);
      return null;
    }
  }

  get signer(): Signer {
    if (!this._singer) {
      // this._singer = getMockSigner();
      this._singer = new BaseSigner(this.keyStore);
    }
    return this._singer;
  }

  get walletAdapter() {
    return this.walletAdapterManager.adapter;
  }

  private async _getRegisterationNonce(address: string) {
    const res = await this._simpleFetch(
      `/v1/registration_nonce?address=${encodeURIComponent(address)}`,
      {
        headers: {
          "paxeer-network-account-id": this.stateValue.accountId!,
        },
      },
    );

    if (res.success) {
      return {
        nonce: res.data?.registration_nonce,
        timestamp: res.timestamp,
      };
    } else {
      throw new Error(res.message);
    }
  }

  private async _getTimestampFromServer() {
    const res = await this._getAccountInfo();

    if (res.success) {
      return res.timestamp;
    } else {
      throw new SDKError("get timestamp error");
    }
  }

  private async _getAccountInfo(address?: string, chainNamespace?: string) {
    const brokerId = this.configStore.get("brokerId");

    const addr = address || this.address;
    const chainType = chainNamespace || this.stateValue.chainNamespace;

    const res = await this._simpleFetch(
      `/v1/get_account?address=${addr}&broker_id=${brokerId}&chain_type=${chainType}`,
    );
    return res;
  }

  private async _getSettleNonce(accountId?: string) {
    const timestamp = getTimestamp().toString();
    const url = "/v1/settle_nonce";
    const message = [timestamp, "GET", url].join("");

    const signer = this.signer;

    const { publicKey, signature } = await signer.signText(message);

    const res = await this._simpleFetch("/v1/settle_nonce", {
      headers: {
        "paxeer-network-account-id": accountId || this.stateValue.accountId!,
        "paxeer-network-key": publicKey,
        "paxeer-network-timestamp": timestamp,
        "paxeer-network-signature": signature,
      },
    });

    if (res.success) {
      return res.data?.settle_nonce;
    } else {
      throw new Error(res.message);
    }
  }

  private async _simpleFetch(url: string, init: RequestInit = {}) { const {nativeRequest}=await import("@paxeer-network/paxeer-backend/nativeClient");return nativeRequest(url,init); }

  private async _getVaultOperationNonce() {
    const res = await this._simpleFetch(`/v1/public/sv_nonce`);
    if (res.success) {
      return res.data?.nonce;
    } else {
      throw new Error(res.message);
    }
  }

  // getDomain(onChainDomain?: boolean): SignatureDomain {
  //   if (!this.walletClient) {
  //     throw new Error("walletClient is undefined");
  //   }
  //   const chainId = this.walletClient.chainId;
  //   // const {verifyContractAddress} = this.contract.getContractInfoByEnv();
  //   return {
  //     name: "paxeer-network",
  //     version: "1",
  //     chainId,
  //     verifyingContract: onChainDomain
  //       ? this.contractManger.getContractInfoByEnv().verifyContractAddress
  //       : "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
  //   };
  // }

  getAdditionalInfo(): Record<string, any> | null {
    if (!this.stateValue.address) {
      return null;
    }
    return this.additionalInfoRepository.getAll(this.stateValue.address);
  }

  get on() {
    return this._ee.on.bind(this._ee);
  }

  get once() {
    return this._ee.once.bind(this._ee);
  }

  get off() {
    return this._ee.off.bind(this._ee);
  }

  /**
   * Generate DexRequest message for DEX operations
   * @param inputs DexRequest parameters
   * @returns Signed message and domain
   */
  async generateDexRequest(inputs: {
    payloadType: number;
    // nonce: string;
    // receiver: string;
    amount: string;
    vaultId: string;
    token: string;
    domain: SignatureDomain;
    // dexBrokerId: string;
  }) {
    if (!this.walletAdapter || !this.stateValue.address) {
      throw new Error("walletAdapter is undefined");
    }
    const nonce = await this._getVaultOperationNonce();
    const receiver = this.stateValue.address;
    const dexBrokerId = this.configStore.get("brokerId");

    const { message, signatured, domain } =
      await this.walletAdapter.generateDexRequestMessage({
        ...inputs,
        nonce,
        receiver,
        dexBrokerId,
        timestamp: getTimestamp(),
        domain: inputs.domain,
      });

    return {
      message,
      signatured,
      domain,
    };
  }

  /**
   * Signs a GET MessageFactor payload with the current timestamp.
   * @param url The URL for the GET request
   * @returns The signature
   */
  private async signGetMessageFactor(url: string) {
    const payload: MessageFactor = {
      method: "GET",
      url,
    };
    return this.signer.sign(payload, getTimestamp());
  }
}
