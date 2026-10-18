import {
  LedgerWalletKey,
  LedgerWalletManualOverrideKey,
} from "@paxeer-network/types";
import { useMemoizedFn } from "../shared/useMemoizedFn";
import { useLocalStorage } from "../useLocalStorage";

type LedgerWalletManualOverride = {
  address: string;
  adapterName: string;
};

const normalizeAdapterName = (adapterName: string) =>
  adapterName.trim().toLowerCase();

const isLedgerAdapter = (adapterName: string) =>
  normalizeAdapterName(adapterName) === "ledger";

const isValidManualOverride = (
  value: unknown,
): value is LedgerWalletManualOverride => {
  if (!value || typeof value !== "object") {
    return false;
  }

  const override = value as LedgerWalletManualOverride;
  return (
    typeof override.address === "string" &&
    typeof override.adapterName === "string" &&
    override.address.length > 0 &&
    override.adapterName.length > 0
  );
};

const isSameIdentity = (
  override: LedgerWalletManualOverride,
  address: string,
  adapterName: string,
) =>
  override.address === address &&
  override.adapterName === normalizeAdapterName(adapterName);

export const useStorageLedgerAddress = () => {
  const [storedLedgerWallet, setLedgerWallet] = useLocalStorage<string[]>(
    LedgerWalletKey,
    [],
  );
  const ledgerWallet = storedLedgerWallet as string[];
  const [storedManualOverride, setManualOverride] =
    useLocalStorage<LedgerWalletManualOverride | null>(
      LedgerWalletManualOverrideKey,
      null,
    );
  const manualOverride =
    storedManualOverride as LedgerWalletManualOverride | null;

  const updateLedgerWallet = useMemoizedFn((nextLedgerWallet: string[]) => {
    if (
      nextLedgerWallet.length !== ledgerWallet.length ||
      nextLedgerWallet.some((address, index) => address !== ledgerWallet[index])
    ) {
      setLedgerWallet(nextLedgerWallet);
    }
  });

  const setLedgerAddress = useMemoizedFn((address: string) => {
    if (!ledgerWallet.includes(address)) {
      updateLedgerWallet([...ledgerWallet, address]);
    }
  });

  const setManualLedgerAddress = useMemoizedFn(
    (address: string, adapterName: string) => {
      const normalizedAdapterName = normalizeAdapterName(adapterName);
      if (!normalizedAdapterName) {
        setLedgerAddress(address);
        return;
      }

      let nextLedgerWallet = ledgerWallet;
      if (
        isValidManualOverride(manualOverride) &&
        !isSameIdentity(manualOverride, address, normalizedAdapterName)
      ) {
        nextLedgerWallet = nextLedgerWallet.filter(
          (ledgerAddress) => ledgerAddress !== manualOverride.address,
        );
      }
      if (!nextLedgerWallet.includes(address)) {
        nextLedgerWallet = [...nextLedgerWallet, address];
      }

      updateLedgerWallet(nextLedgerWallet);
      setManualOverride({
        address,
        adapterName: normalizedAdapterName,
      });
    },
  );

  const clearManualLedgerAddress = useMemoizedFn(
    (address: string, adapterName: string) => {
      let nextLedgerWallet = ledgerWallet;
      if (isValidManualOverride(manualOverride)) {
        nextLedgerWallet = nextLedgerWallet.filter(
          (ledgerAddress) => ledgerAddress !== manualOverride.address,
        );
      }

      if (isLedgerAdapter(adapterName)) {
        if (!nextLedgerWallet.includes(address)) {
          nextLedgerWallet = [...nextLedgerWallet, address];
        }
      } else {
        nextLedgerWallet = nextLedgerWallet.filter(
          (ledgerAddress) => ledgerAddress !== address,
        );
      }

      updateLedgerWallet(nextLedgerWallet);
      if (manualOverride !== null) {
        setManualOverride(null);
      }
    },
  );

  const syncLedgerAddress = useMemoizedFn(
    (address: string, adapterName: string) => {
      if (
        isValidManualOverride(manualOverride) &&
        isSameIdentity(manualOverride, address, adapterName)
      ) {
        setLedgerAddress(address);
        return;
      }

      let nextLedgerWallet = ledgerWallet;
      if (isValidManualOverride(manualOverride)) {
        nextLedgerWallet = nextLedgerWallet.filter(
          (ledgerAddress) => ledgerAddress !== manualOverride.address,
        );
      }

      if (isLedgerAdapter(adapterName)) {
        if (!nextLedgerWallet.includes(address)) {
          nextLedgerWallet = [...nextLedgerWallet, address];
        }
      } else {
        nextLedgerWallet = nextLedgerWallet.filter(
          (ledgerAddress) => ledgerAddress !== address,
        );
      }

      updateLedgerWallet(nextLedgerWallet);
      if (manualOverride !== null) {
        setManualOverride(null);
      }
    },
  );

  return {
    setLedgerAddress,
    setManualLedgerAddress,
    clearManualLedgerAddress,
    syncLedgerAddress,
    ledgerWallet,
  };
};
