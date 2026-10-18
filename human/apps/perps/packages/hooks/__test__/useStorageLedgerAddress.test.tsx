import { act, renderHook } from "@testing-library/react-hooks";
import {
  LedgerWalletKey,
  LedgerWalletManualOverrideKey,
} from "@paxeer-network/types";
import { useStorageLedgerAddress } from "../src/paxeer-network/useStorageLedgerAddress";

const readLedgerWallet = () =>
  JSON.parse(window.localStorage.getItem(LedgerWalletKey) ?? "[]");

const readManualOverride = () =>
  JSON.parse(
    window.localStorage.getItem(LedgerWalletManualOverrideKey) ?? "null",
  );

describe("useStorageLedgerAddress", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("removes a stale Ledger marker when the same address uses Phantom", () => {
    window.localStorage.setItem(
      LedgerWalletKey,
      JSON.stringify(["address-a", "address-b"]),
    );

    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.syncLedgerAddress("address-a", "Phantom");
    });

    expect(result.current.ledgerWallet).toEqual(["address-b"]);
    expect(readLedgerWallet()).toEqual(["address-b"]);
  });

  it("marks the address when the current adapter is Ledger", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.syncLedgerAddress("address-a", "Ledger");
    });

    expect(result.current.ledgerWallet).toEqual(["address-a"]);
    expect(readLedgerWallet()).toEqual(["address-a"]);
  });

  it("keeps a manual Ledger override for the same address and adapter", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.setManualLedgerAddress("address-a", "Phantom");
    });
    act(() => {
      result.current.syncLedgerAddress("address-a", "Phantom");
    });

    expect(result.current.ledgerWallet).toEqual(["address-a"]);
    expect(readManualOverride()).toEqual({
      address: "address-a",
      adapterName: "phantom",
    });
  });

  it("restores a persisted manual override for the same wallet identity", () => {
    window.localStorage.setItem(LedgerWalletKey, JSON.stringify(["address-a"]));
    window.localStorage.setItem(
      LedgerWalletManualOverrideKey,
      JSON.stringify({ address: "address-a", adapterName: "phantom" }),
    );

    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.syncLedgerAddress("address-a", "Phantom");
    });

    expect(result.current.ledgerWallet).toEqual(["address-a"]);
    expect(readManualOverride()).toEqual({
      address: "address-a",
      adapterName: "phantom",
    });
  });

  it("clears the manual override when the address changes", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.setManualLedgerAddress("address-a", "Phantom");
    });
    act(() => {
      result.current.syncLedgerAddress("address-b", "Phantom");
    });

    expect(result.current.ledgerWallet).toEqual([]);
    expect(readManualOverride()).toBeNull();
  });

  it("clears the manual override when the adapter changes", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.setManualLedgerAddress("address-a", "Phantom");
    });
    act(() => {
      result.current.syncLedgerAddress("address-a", "Jupiter");
    });

    expect(result.current.ledgerWallet).toEqual([]);
    expect(readManualOverride()).toBeNull();
  });

  it("returns a Phantom wallet to standard signing when requested", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.setManualLedgerAddress("address-a", "Phantom");
    });
    act(() => {
      result.current.clearManualLedgerAddress("address-a", "Phantom");
    });

    expect(result.current.ledgerWallet).toEqual([]);
    expect(readManualOverride()).toBeNull();
  });

  it("keeps native Ledger detection when clearing a manual override", () => {
    const { result } = renderHook(() => useStorageLedgerAddress());

    act(() => {
      result.current.setManualLedgerAddress("address-a", "Ledger");
    });
    act(() => {
      result.current.clearManualLedgerAddress("address-a", "Ledger");
    });

    expect(result.current.ledgerWallet).toEqual(["address-a"]);
    expect(readManualOverride()).toBeNull();
  });
});
