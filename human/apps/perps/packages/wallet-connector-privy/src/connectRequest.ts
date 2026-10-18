import type { WalletState } from "@paxeer-network/hooks";
import { WalletConnectType } from "./types";

export type ConnectRequestPhase = "selecting" | "connecting";

type PendingConnectRequest = {
  promise: Promise<WalletState[]>;
  resolve: (wallets: WalletState[]) => void;
  reject: (error: Error) => void;
  phase: ConnectRequestPhase;
  baselineWallet: WalletState | null;
  targetWalletType?: WalletConnectType;
};

export class ConnectRequestController {
  private request: PendingConnectRequest | null = null;

  constructor(
    private readonly onConnectingChange: (
      connecting: boolean,
    ) => void = () => {},
  ) {}

  get hasPendingRequest() {
    return this.request !== null;
  }

  begin(options: {
    baselineWallet: WalletState | null;
    autoSelect?: boolean;
  }): Promise<WalletState[]> {
    if (options.autoSelect) {
      return Promise.resolve([]);
    }

    if (this.request) {
      return this.request.promise;
    }

    let resolve!: (wallets: WalletState[]) => void;
    let reject!: (error: Error) => void;
    const promise = new Promise<WalletState[]>((res, rej) => {
      resolve = res;
      reject = rej;
    });

    this.request = {
      promise,
      resolve,
      reject,
      phase: "selecting",
      baselineWallet: options.baselineWallet,
    };

    return promise;
  }

  startProvider(walletType: WalletConnectType) {
    if (!this.request) {
      return;
    }

    this.request.phase = "connecting";
    this.request.targetWalletType = walletType;
    this.onConnectingChange(true);
  }

  completeAggregatedWallet(
    wallet: WalletState | null,
    walletType: WalletConnectType | null,
  ) {
    const request = this.request;
    if (
      !request ||
      request.phase !== "connecting" ||
      !wallet ||
      walletType !== request.targetWalletType ||
      wallet === request.baselineWallet
    ) {
      return;
    }

    this.resolve([wallet]);
  }

  selectWallet(wallet: WalletState) {
    if (!this.request) {
      return;
    }
    this.resolve([wallet]);
  }

  /**
   * Resolves the pending request when the user dismisses the connect drawer.
   * Wallet-prompt providers (EVM/SOL/Abstract) must cancel here: an ignored
   * wallet popup never emits an error, so the request would stay pending
   * forever and later connect() calls would reuse it without reopening the
   * drawer. Privy logins are exempt — they run in their own modal or a page
   * redirect, and the drawer closes programmatically for them.
   */
  cancelFromDrawerClose(): WalletConnectType | undefined {
    if (
      this.request?.phase === "connecting" &&
      this.request.targetWalletType === WalletConnectType.PRIVY
    ) {
      return;
    }

    const walletType =
      this.request?.phase === "connecting"
        ? this.request.targetWalletType
        : undefined;
    this.resolve([]);
    return walletType;
  }

  cancelProvider(walletType: WalletConnectType) {
    if (!this.matchesProvider(walletType)) {
      return false;
    }

    this.resolve([]);
    return true;
  }

  fail(error: Error, walletType: WalletConnectType) {
    const request = this.request;
    if (!request || !this.matchesProvider(walletType)) {
      return false;
    }

    this.request = null;
    this.onConnectingChange(false);
    request.reject(error);
    return true;
  }

  dispose() {
    if (this.request) {
      this.resolve([]);
    }
  }

  private resolve(wallets: WalletState[]) {
    const request = this.request;
    if (!request) {
      return;
    }

    this.request = null;
    this.onConnectingChange(false);
    request.resolve(wallets);
  }

  private matchesProvider(walletType: WalletConnectType) {
    return (
      this.request?.phase === "connecting" &&
      this.request.targetWalletType === walletType
    );
  }
}
