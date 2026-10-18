import * as ed from "@noble/ed25519";
import { getAccount } from "@solana/spl-token";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";
import {
  clusterApiUrl,
  Connection,
  PublicKey,
  Transaction,
  TransactionInstruction,
} from "@solana/web3.js";
import { encode as bs58encode } from "bs58";
import { bytesToHex } from "ethereum-cryptography/utils";
import {
  AddPaxeerNetworkKeyInputs,
  BaseWalletAdapter,
  Message,
  RegisterAccountInputs,
  SettleInputs,
  SignatureDomain,
  SimpleDI,
  WithdrawInputs,
  Account,
  MessageFactor,
  DexRequestInputs,
  InternalTransferInputs,
} from "@paxeer-network/core";
import {
  API,
  MaxUint256,
  ChainNamespace,
  isNativeTokenChecker,
} from "@paxeer-network/types";
import {
  addPaxeerNetworkKeyMessage,
  checkIsLedgerWallet,
  deposit,
  getDepositQuoteFee,
  registerAccountMessage,
  settleMessage,
  internalTransferMessage,
  withdrawMessage,
  dexRequestMessage,
} from "./helper";
import { getTokenAccounts } from "./solana.util";
import { SolanaAdapterOption, SolanaWalletProvider } from "./types";

class DefaultSolanaWalletAdapter extends BaseWalletAdapter<SolanaAdapterOption> {
  chainNamespace: ChainNamespace = ChainNamespace.solana;

  private _address!: string;
  private _chainId!: number;
  private _provider!: SolanaWalletProvider;
  private _connection!: Connection;

  constructor() {
    super();
  }

  get address(): string {
    return this._address;
  }

  get chainId(): number {
    return this._chainId;
  }

  set chainId(chainId: number) {
    this._chainId = chainId;
  }

  get connection(): Connection {
    if (this._connection) {
      return this._connection;
    }
    if (this._provider.rpcUrl) {
      this._connection = new Connection(this._provider.rpcUrl, {
        commitment: "confirmed",
      });
      return this._connection;
    }
    if (this._provider.network === WalletAdapterNetwork.Devnet) {
      this._connection = new Connection(clusterApiUrl(this._provider.network), {
        commitment: "confirmed",
      });
      return this._connection;
    }

    const account = SimpleDI.get<Account>("account");
    const url = "/v1/solana-rpc-proxy";
    this._connection = new Connection(`${account.apiBaseUrl}${url}`, {
      commitment: "confirmed",
      fetchMiddleware: async (info, init, fetch) => {
        const payload: MessageFactor = {
          url,
          method: init?.method as "GET" | "POST" | "PUT" | "DELETE",
          data: JSON.parse(init?.body as string),
        };
        // console.log('payload', payload);
        const signature = await this.signMessageByPaxeerNetworkKey(payload);
        for (const key of Object.keys(signature)) {
          (init?.headers as any)[key] = signature[
            key as keyof typeof signature
          ] as string;
        }
        return fetch(info, init);
      },
    });
    return this._connection;
  }

  private setConfig(config: SolanaAdapterOption) {
    this._address = config.address;
    this._chainId = config.chain.id;
    if (config.provider) {
      this._provider = config.provider;
    }
  }

  private lifecycleName(name: string, data: any) {
    console.log("lifecycle", name, data);
  }

  active(config: SolanaAdapterOption): void {
    this.setConfig(config);
    this.lifecycleName("active", config);
  }

  deactivate(): void {
    this.lifecycleName("deactivate", {});
  }

  update(config: SolanaAdapterOption): void {
    this.lifecycleName("update", config);
    this.setConfig(config);
  }

  generateSecretKey(): string {
    let privKey, secretKey;
    do {
      privKey = ed.utils.randomPrivateKey();
      secretKey = bs58encode(privKey);
    } while (secretKey.length !== 44);

    return secretKey;
  }

  uint8ArrayToHexString(uint8Array: Uint8Array): string {
    return Array.from(uint8Array)
      .map((byte) => byte.toString(16).padStart(2, "0"))
      .join("");
  }

  async signMessage(message: Uint8Array): Promise<string> {
    const isLedger = checkIsLedgerWallet(this._address);
    // test ledger wallet
    // if (!isLedger) {
    //   console.log("-- test error");
    //   throw new Error(
    //     "xxx Signing off chain messages with Ledger is not yet supported"
    //   );
    // }

    if (isLedger) {
      const transaction = new Transaction();

      transaction.add(
        new TransactionInstruction({
          keys: [],
          programId: new PublicKey(
            "ComputeBudget111111111111111111111111111111",
          ),
          data: new Uint8Array([3, 0, 0, 0, 0, 0, 0, 0, 0]) as any,
        }),
      );

      transaction.add(
        new TransactionInstruction({
          keys: [],
          programId: new PublicKey(
            "ComputeBudget111111111111111111111111111111",
          ),
          data: new Uint8Array([2, 0, 0, 0, 0]) as any,
        }),
      );

      transaction.add(
        new TransactionInstruction({
          keys: [],
          programId: new PublicKey(
            "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr",
          ),
          data: message as any,
        }),
      );

      const userPublicKey = new PublicKey(this.address);

      transaction.feePayer = userPublicKey;

      const zeroHash = new Uint8Array(32).fill(0);
      transaction.recentBlockhash = new PublicKey(zeroHash).toString();

      const signedTransaction =
        await this._provider.signTransaction(transaction);

      const signature = signedTransaction.signatures[0].signature;
      if (signature) {
        return this.uint8ArrayToHexString(signature as any);
      } else {
        console.log("-- sign message error", signature);
        throw new Error("Unsupported signature");
      }
    }
    const signRes = await this._provider.signMessage(message);
    return "0x" + bytesToHex(signRes);
  }

  async generateRegisterAccountMessage(
    inputs: RegisterAccountInputs,
  ): Promise<Message> {
    const [message, toSignatureMessage] = registerAccountMessage({
      ...inputs,
      chainId: this.chainId,
    });

    const signature = await this.signMessage(toSignatureMessage as Uint8Array);

    return {
      message: {
        ...message,
        chainType: "SOL",
      },
      signatured: signature,
    };
  }

  async generateWithdrawMessage(
    inputs: WithdrawInputs,
  ): Promise<Message & { domain: SignatureDomain }> {
    const [message, toSignatureMessage] = withdrawMessage({
      ...inputs,
      chainId: this.chainId,
    });
    const signature = await this.signMessage(toSignatureMessage as Uint8Array);

    return {
      message: {
        ...message,
        chainType: "SOL",
      },
      domain: {
        name: "",
        version: "",
        chainId: this.chainId,
        verifyingContract: inputs.verifyContract!,
      },
      signatured: signature,
    };
  }

  async generateInternalTransferMessage(
    inputs: InternalTransferInputs,
  ): Promise<Message & { domain: SignatureDomain }> {
    const [message, toSignatureMessage] = internalTransferMessage({
      ...inputs,
      chainId: this.chainId,
    });
    const signature = await this.signMessage(toSignatureMessage as Uint8Array);

    return {
      message: {
        ...message,
        chainType: "SOL",
      },
      domain: {
        name: "",
        version: "",
        chainId: this.chainId,
        verifyingContract: inputs.verifyContract!,
      },
      signatured: signature,
    };
  }

  async generateAddPaxeerNetworkKeyMessage(
    inputs: AddPaxeerNetworkKeyInputs,
  ): Promise<Message> {
    const [message, toSignatureMessage] = addPaxeerNetworkKeyMessage({
      ...inputs,
      chainId: this.chainId,
    });
    const signature = await this.signMessage(toSignatureMessage as Uint8Array);

    return {
      message: {
        ...message,
        chainType: "SOL",
      },
      signatured: signature,
    };
  }

  async generateSettleMessage(
    inputs: SettleInputs,
  ): Promise<Message & { domain: SignatureDomain }> {
    const [message, toSignatureMessage] = settleMessage({
      ...inputs,
      chainId: this.chainId,
    });
    const signature = await this.signMessage(toSignatureMessage as Uint8Array);
    return {
      message: {
        ...message,
        chainType: "SOL",
      },
      domain: {
        name: "",
        version: "",
        chainId: this.chainId,
        verifyingContract: inputs.verifyContract!,
      },
      signatured: signature,
    };
  }

  async generateDexRequestMessage(inputs: DexRequestInputs): Promise<
    Message & {
      domain: SignatureDomain;
    }
  > {
    const [message, toSignatureMessage] = await dexRequestMessage({
      ...inputs,
      chainId: this.chainId,
    });

    // use signMessage method to sign, instead of signTypedData
    const signature = await this.signMessage(toSignatureMessage as Uint8Array);

    return {
      message: {
        ...inputs,
        chainType: "SOL",
      },
      signatured: signature,
      domain: inputs.domain,
    };
  }

  async getBalance(): Promise<bigint> {
    const publicKey = new PublicKey(this.address);
    const lamports = await this.connection.getBalance(publicKey);
    return BigInt(lamports);
  }

  async getBalances(addresses: string[]) {
    const userPublicKey = new PublicKey(this._address);
    const connection = this.connection;

    // Check which addresses are native SOL vs SPL tokens
    const isNativeToken = addresses.map((address) =>
      isNativeTokenChecker(address),
    );

    // Get native SOL balance once (if any address is SOL)
    const hasNativeSOL = addresses.some((address) =>
      isNativeTokenChecker(address),
    );
    let nativeSolBalance: bigint | null = null;
    if (hasNativeSOL) {
      nativeSolBalance = await this.getBalance();
    }

    // Get SPL token accounts for non-SOL addresses
    const splTokenAddresses = addresses.filter(
      (address) => !isNativeTokenChecker(address),
    );

    let splTokenBalances: bigint[] = [];

    if (splTokenAddresses.length > 0) {
      const tokenPublicKeys = splTokenAddresses.map(
        (address) => new PublicKey(address),
      );
      const userTokenAccounts = tokenPublicKeys.map((tokenPublicKey) =>
        getTokenAccounts(tokenPublicKey, userPublicKey),
      );

      // Fetch each token account individually to handle missing accounts gracefully
      splTokenBalances = await Promise.all(
        userTokenAccounts.map(async (account) => {
          try {
            const tokenAccount = await getAccount(
              connection,
              account,
              "confirmed",
            );
            return tokenAccount.amount;
          } catch (err: any) {
            if (err?.name === "TokenAccountNotFoundError") {
              return 0n;
            }
            throw err;
          }
        }),
      );
    }

    // Combine results in original order
    const results: bigint[] = [];
    let splIndex = 0;
    for (let i = 0; i < addresses.length; i++) {
      if (isNativeToken[i]) {
        results.push(nativeSolBalance!);
      } else {
        results.push(splTokenBalances[splIndex]);
        splIndex++;
      }
    }

    return results;
  }

  async getBalanceByAddress(address: string): Promise<bigint> {
    const tokenPublicKey = new PublicKey(address);
    const userPublicKey = new PublicKey(this._address);
    const userTokenAccount = getTokenAccounts(tokenPublicKey, userPublicKey);
    const connection = this.connection;

    const tokenAmount = await getAccount(
      connection,
      userTokenAccount,
      "confirmed",
    );
    return tokenAmount.amount;
  }

  async call(
    address: string,
    method: string,
    params: any[],
    options?: {
      abi: any;
    },
  ) {
    if (method === "balanceOf") {
      return this.getBalanceByAddress(address);
    }
    if (method === "allowance") {
      // sol does not require allowance
      return MaxUint256;
    }
    return BigInt(0);
  }

  async sendTransaction(
    contractAddress: string,
    method: string,
    payload: {
      from: string;
      to?: string;
      data: any[];
      value?: bigint;
    },
    options: {
      abi: any;
    },
  ) {
    console.log("-- solanan sendTransaction", {
      contractAddress,
      method,
      payload,
      options,
    });
    if (method === "deposit") {
      return deposit({
        vaultAddress: contractAddress,
        userAddress: this._address,
        connection: this.connection,
        depositData: payload.data[0],
        sendTransaction: this._provider.sendTransaction,
      });
    }
  }

  async callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    },
  ): Promise<any> {
    console.log("-- params ", {
      chain,
      address,
      method,
      params,
    });
    if (method === "getDepositFee") {
      return getDepositQuoteFee({
        vaultAddress: address,
        userAddress: this._address,
        connection: this.connection,
        depositData: params[1],
      });
    }
    return 0;
  }

  async estimateGasFee(
    contractAddress: string,
    method: string,
    payload: {
      from: string;
      to?: string;
      data: any[];
      value?: bigint;
    },
  ): Promise<bigint> {
    return BigInt(0);
  }

  async pollTransactionReceiptWithBackoff(
    txHash: string,
    baseInterval?: number,
    maxInterval?: number,
    maxRetries?: number,
  ): Promise<any> {
    return Promise.resolve({ status: 1 });
  }
}

export { DefaultSolanaWalletAdapter };
