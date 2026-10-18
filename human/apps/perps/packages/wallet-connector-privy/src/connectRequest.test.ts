import { describe, expect, it, vi } from "vitest";
import { ChainNamespace } from "@paxeer-network/types";
import { ConnectRequestController } from "./connectRequest";
import { WalletConnectType } from "./types";

const createWallet = (address: string) => ({
  label: "test",
  icon: "",
  provider: {} as any,
  accounts: [{ address }],
  chains: [{ id: 1, namespace: ChainNamespace.evm }],
});

describe("ConnectRequestController", () => {
  it("keeps the request pending until the matching wallet is ready", async () => {
    const baseline = createWallet("0x1");
    const nextWallet = createWallet("0x2");
    const controller = new ConnectRequestController();
    const promise = controller.begin({ baselineWallet: baseline });

    controller.startProvider(WalletConnectType.EVM);
    controller.completeAggregatedWallet(nextWallet, WalletConnectType.SOL);
    expect(controller.hasPendingRequest).toBe(true);

    controller.completeAggregatedWallet(nextWallet, WalletConnectType.EVM);
    await expect(promise).resolves.toEqual([nextWallet]);
  });

  it("reuses a pending request", () => {
    const controller = new ConnectRequestController();
    const first = controller.begin({ baselineWallet: null });
    const second = controller.begin({ baselineWallet: null });

    expect(second).toBe(first);
    controller.dispose();
  });

  it("resolves an auto-select request immediately", async () => {
    const controller = new ConnectRequestController();
    await expect(
      controller.begin({ baselineWallet: null, autoSelect: true }),
    ).resolves.toEqual([]);
    expect(controller.hasPendingRequest).toBe(false);
  });

  it("resolves selection and provider cancellation with an empty list", async () => {
    const selectionController = new ConnectRequestController();
    const selection = selectionController.begin({ baselineWallet: null });
    selectionController.cancelFromDrawerClose();
    await expect(selection).resolves.toEqual([]);

    const providerController = new ConnectRequestController();
    const provider = providerController.begin({ baselineWallet: null });
    providerController.startProvider(WalletConnectType.PRIVY);
    providerController.cancelProvider(WalletConnectType.PRIVY);
    await expect(provider).resolves.toEqual([]);
  });

  it("cancels a connecting wallet-prompt provider when the drawer closes", async () => {
    const controller = new ConnectRequestController();
    const promise = controller.begin({ baselineWallet: null });

    controller.startProvider(WalletConnectType.EVM);
    expect(controller.cancelFromDrawerClose()).toBe(WalletConnectType.EVM);
    expect(controller.hasPendingRequest).toBe(false);
    await expect(promise).resolves.toEqual([]);
  });

  it("does not cancel a connecting privy login when the drawer closes", async () => {
    const controller = new ConnectRequestController();
    const wallet = createWallet("0x2");
    const promise = controller.begin({ baselineWallet: null });

    controller.startProvider(WalletConnectType.PRIVY);
    controller.cancelFromDrawerClose();
    expect(controller.hasPendingRequest).toBe(true);

    controller.completeAggregatedWallet(wallet, WalletConnectType.PRIVY);
    await expect(promise).resolves.toEqual([wallet]);
  });

  it("returns an explicitly selected existing wallet", async () => {
    const controller = new ConnectRequestController();
    const wallet = createWallet("0x2");
    const promise = controller.begin({ baselineWallet: wallet });

    controller.selectWallet(wallet);
    await expect(promise).resolves.toEqual([wallet]);
  });

  it("rejects real errors and clears connecting state", async () => {
    const onConnectingChange = vi.fn();
    const controller = new ConnectRequestController(onConnectingChange);
    const promise = controller.begin({ baselineWallet: null });

    controller.startProvider(WalletConnectType.ABSTRACT);
    controller.fail(new Error("connect failed"), WalletConnectType.ABSTRACT);

    await expect(promise).rejects.toThrow("connect failed");
    expect(onConnectingChange).toHaveBeenNthCalledWith(1, true);
    expect(onConnectingChange).toHaveBeenLastCalledWith(false);
  });

  it("resolves a pending request on dispose", async () => {
    const controller = new ConnectRequestController();
    const promise = controller.begin({ baselineWallet: null });
    controller.dispose();
    await expect(promise).resolves.toEqual([]);
  });

  it("ignores cancellation and errors from a different provider", async () => {
    const controller = new ConnectRequestController();
    const wallet = createWallet("0x2");
    const promise = controller.begin({ baselineWallet: null });

    controller.startProvider(WalletConnectType.EVM);
    expect(controller.cancelProvider(WalletConnectType.SOL)).toBe(false);
    expect(
      controller.fail(new Error("unrelated"), WalletConnectType.ABSTRACT),
    ).toBe(false);
    expect(controller.hasPendingRequest).toBe(true);

    controller.completeAggregatedWallet(wallet, WalletConnectType.EVM);
    await expect(promise).resolves.toEqual([wallet]);
  });
});
